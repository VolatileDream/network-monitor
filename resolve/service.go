package resolve

// Contains a small go routine that takes a config.Config and
// periodically resolves it, outputting the result to a channel.

import (
	"context"
	"log"
	"net/netip"
	"sync"
	"time"

	"github.com/VolatileDream/workbench/web/network-monitor/config"
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
}

type resolution struct {
	target config.LatencyTarget
	addrs  []netip.Addr
	err    error
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
	var cfg config.Config
	select {
	case <-ctx.Done():
		// If the ctx is canceled, exit.
		// It's possible, but unlikely that this happens while
		// we wait for the config.
		return
	case cfg = <-r.loader:
		// yay.
	}

	// Force a resolution immediately.
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()

	cache := make(map[config.LatencyTarget][]netip.Addr)

resolve_loop:
	for {
		select {
		case <-ctx.Done():
			break resolve_loop
		case cfg = <-r.loader:
			timer.Reset(cfg.ResolveInterval)
		case <-timer.C:
			timer.Reset(cfg.ResolveInterval)
		}

		// If we can't resolve everything quickly relative to the interval,
		// then what was the point in trying to resolve them all?
		rCtx, cancel := context.WithTimeout(ctx, cfg.ResolveInterval/2)
		result := r.resolve(rCtx, cfg.Targets)
		cancel()

		R := Result{
			Resolved: make([]Resolution, 0, len(result)),
		}
		newCache := make(map[config.LatencyTarget][]netip.Addr)
		for _, res := range result {
			if res.err == nil {
				newCache[res.target] = res.addrs
			} else {
				newCache[res.target] = cache[res.target]
				log.Printf("failed to resolve '%s': %v", res.target, res.err)
			}

			if addrs := newCache[res.target]; addrs != nil {
				R.Resolved = append(R.Resolved, Resolution{
					Target: res.target,
					Addrs:  addrs,
				})
			}
		}
		cache = newCache

		// A caller could forever avoid reading the result, so we have to
		// double up on exiting if the context gets cancelled. But also we
		// want to time out on attempting to write this out, and write a
		// message out. Not reading the results out in a timely manner is
		// not okay.
		//
		// Note: rCtx time + this time must be < ResolveInterval.
		expiry := time.NewTimer(cfg.ResolveInterval / 4)
		select {
		case <-expiry.C:
			log.Printf("timed out (%s) writing resolve result. reader hung?\n",
				cfg.ResolveInterval/4)

		case r.results <- R:
		case <-ctx.Done():
			// Do not return. Handled by the top of the loop.
		}
		expiry.Stop()
	}

	close(r.results)
}

func (r *ResolverService) resolve(ctx context.Context, targets []config.LatencyTarget) []resolution {
	// Resolve them all concurrently
	var wg sync.WaitGroup

	var rlock sync.Mutex
	results := make([]resolution, 0, len(targets))

	for _, target := range targets {
		wg.Add(1)
		go func(t config.LatencyTarget) {
			defer wg.Done()
			addrs, err := r.resolver.Resolve(ctx, t)
			log.Printf("resolved %s to %v\n", t.MetricName(), addrs)

			rlock.Lock()
			defer rlock.Unlock()
			results = append(results, resolution{
				target: t,
				addrs:  addrs,
				err:    err,
			})
		}(target)
	}

	wg.Wait()
	return results
}
