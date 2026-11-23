package actions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/rykhalskyi/killswitch/internal/config"
)

// ErrIrreversible indicates an action that has no restore path.
var ErrIrreversible = errors.New("action is irreversible")

const (
	TalosAPIPort = "50000"
	nodeUp       = "up"
	nodeDown     = "down"

	// DialTimeout bounds a single reachability probe.
	DialTimeout = 2 * time.Second
	// DefaultShutdownCommandTimeout bounds one talosctl invocation.
	DefaultShutdownCommandTimeout = 30 * time.Second
	// DefaultNodeDownTimeout bounds how long we wait for one node to disappear.
	DefaultNodeDownTimeout = 90 * time.Second
)

// TalosClient observes node reachability and shuts nodes down.
type TalosClient interface {
	NodeUp(ctx context.Context, node string) (bool, error)
	// Shutdown powers off a node and waits for it to become unreachable.
	// A zero timeout uses the client default.
	Shutdown(ctx context.Context, node string, timeout time.Duration) error
}

// TalosCLI shells out to talosctl and probes the Talos API port.
type TalosCLI struct {
	Command  string
	Port     string
	Timeout  time.Duration
	Interval time.Duration
	// DialContext is optional and overrides the TCP probe (used by tests).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewTalosCLI returns a Talos client with production defaults.
func NewTalosCLI() *TalosCLI {
	return &TalosCLI{
		Command:  "talosctl",
		Port:     TalosAPIPort,
		Timeout:  DefaultNodeDownTimeout,
		Interval: 2 * time.Second,
	}
}

// NodeUp reports whether the node's Talos API is reachable. A node that cannot
// be dialed is reported as down; a canceled or expired context is an error, so
// callers never mistake a lost probe for a powered-off node.
func (c *TalosCLI) NodeUp(ctx context.Context, node string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	port := c.Port
	if port == "" {
		port = TalosAPIPort
	}
	dial := c.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: DialTimeout}).DialContext
	}
	conn, err := dial(ctx, "tcp", net.JoinHostPort(node, port))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}

// Shutdown asks talosctl to power off the node and waits until it is gone.
// It is idempotent: an already-down node is treated as success. Probe errors
// (such as a canceled context) are returned so they cannot be mistaken for a
// successful shutdown. A zero timeout uses the client default.
func (c *TalosCLI) Shutdown(ctx context.Context, node string, timeout time.Duration) error {
	up, err := c.NodeUp(ctx, node)
	if err != nil {
		return fmt.Errorf("probing node %s before shutdown: %w", node, err)
	}
	if !up {
		return nil
	}

	command := c.Command
	if command == "" {
		command = "talosctl"
	}
	cmdCtx, cancel := context.WithTimeout(ctx, DefaultShutdownCommandTimeout)
	defer cancel()
	// Target the node directly on both endpoints and nodes so a talosconfig
	// endpoint that has already been powered off does not block later steps.
	out, cmdErr := exec.CommandContext(cmdCtx, command, "shutdown", "--nodes", node, "--endpoints", node).CombinedOutput()
	if cmdErr != nil {
		// The connection commonly drops while the node powers off, especially
		// when this node is also the talosctl endpoint (normally the last,
		// control-plane node). Treat a node that has gone away as success.
		up, probeErr := c.NodeUp(ctx, node)
		if probeErr != nil {
			return fmt.Errorf("talosctl shutdown --nodes %s: %w (probe: %v): %s", node, cmdErr, probeErr, strings.TrimSpace(string(out)))
		}
		if !up {
			return nil
		}
		// It is still reachable, which is expected while it is powering off.
		// Keep waiting for the node to disappear before failing the step.
		if waitErr := c.waitDown(ctx, node, timeout); waitErr != nil {
			return fmt.Errorf("talosctl shutdown --nodes %s: %w (wait: %v): %s", node, cmdErr, waitErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return c.waitDown(ctx, node, timeout)
}

func (c *TalosCLI) waitDown(ctx context.Context, node string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = c.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultNodeDownTimeout
	}
	interval := c.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		up, err := c.NodeUp(waitCtx, node)
		if err != nil {
			return fmt.Errorf("waiting for node %s to shut down: %w", node, err)
		}
		if !up {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("node %s did not shut down within %s: %w", node, timeout, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// TalosShutdownHandler implements talos.shutdown. It is irreversible: there is
// no power-on path, so ON transitions are refused by the engine.
type TalosShutdownHandler struct {
	Client TalosClient
}

func (h *TalosShutdownHandler) Validate(step config.Step) error {
	if step.Target.Kind != config.KindNode {
		return fmt.Errorf("talos.shutdown does not support kind %q", step.Target.Kind)
	}
	if step.Target.Name == "" {
		return errors.New("talos.shutdown requires a node address in target.name")
	}
	if h.Client == nil {
		return errors.New("talos.shutdown client is not configured")
	}
	return nil
}

func (h *TalosShutdownHandler) Observe(ctx context.Context, step config.Step) (any, bool, error) {
	up, err := h.Client.NodeUp(ctx, step.Target.Name)
	if err != nil {
		return nil, false, err
	}
	if up {
		return nodeUp, true, nil
	}
	return nodeDown, true, nil
}

func (h *TalosShutdownHandler) Capture(ctx context.Context, step config.Step) (any, error) {
	v, _, err := h.Observe(ctx, step)
	return v, err
}

func (h *TalosShutdownHandler) Desired(step config.Step, direction string, original any) (any, error) {
	if direction == config.DirectionOn {
		return nil, ErrIrreversible
	}
	return nodeDown, nil
}

func (h *TalosShutdownHandler) Apply(ctx context.Context, step config.Step, desired any) (any, error) {
	v, ok := desired.(string)
	if !ok || v != nodeDown {
		return nil, fmt.Errorf("talos.shutdown desired value is not %q", nodeDown)
	}
	if err := h.Client.Shutdown(ctx, step.Target.Name, step.Timeout.Duration()); err != nil {
		return nil, err
	}
	return nodeDown, nil
}

func (h *TalosShutdownHandler) Restore(ctx context.Context, step config.Step, original any) (any, error) {
	return nil, ErrIrreversible
}

func (h *TalosShutdownHandler) Equal(a, b any) bool {
	as, aok := a.(string)
	bs, bok := b.(string)
	return aok && bok && as == bs
}
