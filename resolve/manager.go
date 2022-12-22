package resolve

// Contains a small go routine that takes a config.Config and
// periodically resolves it, outputting the result to a channel.

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"web/network-monitor/config"
)

type ConfigLoader <-chan config.Config
type Resolver struct {
	// TODO

	loader ConfigLoader

	// Resolver to use, otherwise uses net.DefaultResolver
	resolver *net.Resolver

	results chan Result
}

type Result struct {
	Resolved []Resolution
}

type Resolution struct {
	Target config.LatencyTarget
	Addrs  []netip.Addr
	Error  error
}

func NewResolver(loader ConfigLoader) (*Resolver, <-chan Result) {
	c := make(chan Result, 100)
	r := &Resolver{
		loader:  loader,
		results: c,
	}
	return r, c
}

func (r *Resolver) Run(ctx context.Context) {
	config := <-r.loader

	// Force a resolution immediately.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case config = <-r.loader:
		case <-ticker.C:
			ticker.Reset(config.ResolveInterval)
		}

		// If we can't resolve everything in quickly relative to the interval,
		// then what was the point in trying to resolve them all?
		rCtx, cancel := context.WithTimeout(ctx, config.ResolveInterval/2)
		result := r.resolve(rCtx, config.Targets)
		cancel()

		r.results <- result
	}
}

func (r *Resolver) resolve(ctx context.Context, targets []config.LatencyTarget) Result {
	// Try to resolve them all concurrently...
	var wg sync.WaitGroup

	var rlock sync.Mutex
	results := Result{
		Resolved: make([]Resolution, 0, len(targets)),
	}

	for _, target := range targets {
		wg.Add(1)
		go func(t config.LatencyTarget) {
			addrs, err := t.Resolve(ctx, r.resolver)

			rlock.Lock()
			results.Resolved = append(results.Resolved, Resolution{
				Target: t,
				Addrs:  addrs,
				Error:  err,
			})
			rlock.Unlock()

			wg.Done()
		}(target)
	}

	wg.Wait()
	return results
}
