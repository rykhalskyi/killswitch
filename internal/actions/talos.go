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

	dialTimeout    = 2 * time.Second
	commandTimeout = 30 * time.Second
)

// TalosClient observes node reachability and shuts nodes down.
type TalosClient interface {
	NodeUp(ctx context.Context, node string) (bool, error)
	Shutdown(ctx context.Context, node string) error
}

// TalosCLI shells out to talosctl and probes the Talos API port.
type TalosCLI struct {
	Command  string
	Port     string
	Timeout  time.Duration
	Interval time.Duration
}

// NewTalosCLI returns a Talos client with production defaults.
func NewTalosCLI() *TalosCLI {
	return &TalosCLI{
		Command:  "talosctl",
		Port:     TalosAPIPort,
		Timeout:  90 * time.Second,
		Interval: 2 * time.Second,
	}
}

// NodeUp reports whether the node's Talos API is reachable. An unreachable
// node is reported as down, not as an error.
func (c *TalosCLI) NodeUp(ctx context.Context, node string) (bool, error) {
	port := c.Port
	if port == "" {
		port = TalosAPIPort
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(node, port))
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}

// Shutdown asks talosctl to power off the node and waits until it is gone.
// It is idempotent: an already-down node is treated as success.
func (c *TalosCLI) Shutdown(ctx context.Context, node string) error {
	if up, _ := c.NodeUp(ctx, node); !up {
		return nil
	}

	command := c.Command
	if command == "" {
		command = "talosctl"
	}
	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// Target the node directly on both endpoints and nodes so a talosconfig
	// endpoint that has already been powered off does not block later steps.
	out, err := exec.CommandContext(cmdCtx, command, "shutdown", "--nodes", node, "--endpoints", node).CombinedOutput()
	if err != nil {
		// The connection commonly drops while the node powers off.
		if up, _ := c.NodeUp(ctx, node); !up {
			return nil
		}
		return fmt.Errorf("talosctl shutdown --nodes %s: %w: %s", node, err, strings.TrimSpace(string(out)))
	}
	return c.waitDown(ctx, node)
}

func (c *TalosCLI) waitDown(ctx context.Context, node string) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
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
		if up, _ := c.NodeUp(waitCtx, node); !up {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("node %s did not shut down within %s", node, timeout)
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
	if err := h.Client.Shutdown(ctx, step.Target.Name); err != nil {
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
