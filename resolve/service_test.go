package resolve

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"web/network-monitor/config"
)

type testResolver struct {
	t      *testing.T
	result map[config.LatencyTarget]resolverResult
}

var _ Resolver = &testResolver{}

type resolverResult struct {
	addrs []netip.Addr
	err   error
}

func (tr *testResolver) Resolve(ctx context.Context, target config.LatencyTarget) ([]netip.Addr, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if result, ok := tr.result[target]; ok {
		return result.addrs, result.err
	}
	tr.t.Fatalf("no resolver result for %v", target)
	panic("did no find resolver result")
}

func Test_ResolverService_ExitsBeforeGivenConfig(t *testing.T) {
	tCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan config.Config, 1)
	s, _ := NewService(c, &testResolver{})

	startCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		close(startCh)
		s.Run(tCtx)
		close(doneCh)
	}()

	select {
	case <-startCh:
		// go routine is now running.
	case <-doneCh:
		t.Fatal("did not expect doneCh to close until after cancel")
	}

	cancel()

	select {
	case <-doneCh:
		// go routine is dead.
	}
}

type waitResolver struct {
	callCh chan struct{}
	doneCh chan struct{}
}

var _ Resolver = &waitResolver{}

func (wr *waitResolver) Resolve(ctx context.Context, t config.LatencyTarget) ([]netip.Addr, error) {
	close(wr.callCh)
	select {
	case <-wr.doneCh:
	}
	return nil, nil
}

func Test_ResolverService_WaitsForAllTargetsBeforeResolving(t *testing.T) {
	tCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Provide config immediately to get started.
	c := make(chan config.Config, 1)
	c <- config.Config{
		Targets: []config.LatencyTarget{
			&config.HostnameTarget{
				Host: "test-host",
			},
		},
		ResolveInterval: time.Hour,
	}

	res := &waitResolver{
		callCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	s, _ := NewService(c, res)

	exitCh := make(chan struct{})
	go func() {
		s.Run(tCtx)
		close(exitCh)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timed out waiting for callCh")
	case <-res.callCh:
		// go routine is now running.
	}

	close(res.doneCh)

	select {
	case <-time.After(100 * time.Millisecond):
	case <-exitCh:
		t.Errorf("exitCh should not be closed")
	}

	cancel()

	select {
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timed out waiting for exitCh")
	case <-exitCh:

	}
}
