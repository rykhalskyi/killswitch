package actions

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func reachableDial(ctx context.Context, _, _ string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, b := net.Pipe()
	_ = b.Close()
	return a, nil
}

func TestNodeUpReportsCanceledContext(t *testing.T) {
	c := &TalosCLI{DialContext: reachableDial}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	up, err := c.NodeUp(ctx, "192.168.2.235")
	if up {
		t.Fatal("NodeUp reported up for a canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NodeUp error = %v, want context.Canceled", err)
	}
}

func TestNodeUpTreatsDialFailureAsDown(t *testing.T) {
	c := &TalosCLI{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}}

	up, err := c.NodeUp(context.Background(), "192.168.2.235")
	if up || err != nil {
		t.Fatalf("NodeUp = %v, %v; want false, nil", up, err)
	}
}

func TestWaitDownFailsWhenContextIsCanceled(t *testing.T) {
	c := &TalosCLI{
		DialContext: reachableDial,
		Timeout:     5 * time.Second,
		Interval:    5 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	defer cancel()

	err := c.waitDown(ctx, "192.168.2.235", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitDown error = %v, want context.Canceled", err)
	}
}

func TestWaitDownFailsOnTimeout(t *testing.T) {
	c := &TalosCLI{
		DialContext: reachableDial,
		Timeout:     30 * time.Millisecond,
		Interval:    5 * time.Millisecond,
	}

	if err := c.waitDown(context.Background(), "192.168.2.235", 0); err == nil {
		t.Fatal("waitDown reported success while the node stayed reachable")
	}
}

func TestShutdownFailsOnCanceledContext(t *testing.T) {
	c := NewTalosCLI()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.Shutdown(ctx, "192.168.2.235", 0); err == nil {
		t.Fatal("Shutdown reported success with a canceled context")
	}
}

func TestShutdownSkipsNodeThatIsAlreadyDown(t *testing.T) {
	c := &TalosCLI{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}}

	if err := c.Shutdown(context.Background(), "192.168.2.235", 0); err != nil {
		t.Fatalf("Shutdown = %v, want nil for an already-down node", err)
	}
}
