package resolve

// Contains a small go routine that takes a config.Config and
// periodically resolves it, outputting the result to a channel.

import (
	"context"
	"log"
	"net/netip"
	"sync"
	"time"

	"web/network-monitor/config"
)

type ConfigLoader <-chan config.Config
type ResolverService struct {
	// TODO

	// loader is a channel that propagates configurations from somewhere
	// into the service.
	loader ConfigLoader

	// The actual resolution mechanism.
	resolver Resolver

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

func NewServiceWithStaticConfig(resolver Resolver, conf config.Config) (*ResolverService, <-chan Result) {
	l := make(chan config.Config, 1)
	l <- conf

	c := make(chan Result, 100)
	r := &ResolverService{
		loader:   l,
		resolver: resolver,
		results:  c,
	}
	return r, c
}

func NewService(loader ConfigLoader, resolver Resolver) (*ResolverService, <-chan Result) {
	c := make(chan Result, 100)
	r := &ResolverService{
		loader:   loader,
		resolver: resolver,
		results:  c,
	}
	return r, c
}

func (r *ResolverService) Run(ctx context.Context) {
	var config config.Config
	select {
	case <-ctx.Done():
		// If the ctx is canceled, exit.
		// It's possible, but unlikely that this happens while
		// we wait for the config.
		return
	case config = <-r.loader:
		// yay.
	}

	// Force a resolution immediately.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case config = <-r.loader:
			ticker.Reset(config.ResolveInterval)
		case <-ticker.C:
			ticker.Reset(config.ResolveInterval)
		}

		// If we can't resolve everything quickly relative to the interval,
		// then what was the point in trying to resolve them all?
		rCtx, cancel := context.WithTimeout(ctx, config.ResolveInterval/2)
		result := r.resolve(rCtx, config.Targets)
		cancel()

		// A caller could forever avoid reading the result, so we have to
		// double up on exiting if the context gets cancelled. But also we
		// want to time out on attempting to write this out, and write a
		// message out. Not reading the results out in a timely manner is
		// not okay.
		//
		// Note: rCtx time + this time must be < ResolveInterval.
		expiry := time.NewTimer(config.ResolveInterval / 4)
		select {
		case <-expiry.C:
			log.Printf("timed out (%s) writing resolve result. reader hung?\n",
				config.ResolveInterval/4)

		case r.results <- result:
		case <-ctx.Done():
			// Do not return. Handled by the top of the loop.
		}
		expiry.Stop()
	}
}

func (r *ResolverService) resolve(ctx context.Context, targets []config.LatencyTarget) Result {
	// Resolve them all concurrently
	var wg sync.WaitGroup

	var rlock sync.Mutex
	results := Result{
		Resolved: make([]Resolution, 0, len(targets)),
	}

	for _, target := range targets {
		wg.Add(1)
		go func(t config.LatencyTarget) {
			defer wg.Done()
			addrs, err := r.resolver.Resolve(ctx, t)

			rlock.Lock()
			defer rlock.Unlock()
			results.Resolved = append(results.Resolved, Resolution{
				Target: t,
				Addrs:  addrs,
				Error:  err,
			})
		}(target)
	}

	wg.Wait()
	return results
}
