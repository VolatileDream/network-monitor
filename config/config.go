package config

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"time"

	"web/network-monitor/trace"
)

const (
	SmallestResolveInterval = time.Minute
	SmallestPingInterval    = 10 * time.Millisecond
)

var (
	cfgFlag = flag.String("config",
		"config.json",
		"Json encoded configuration file to use.")
)

func LoadConfig() (*Config, error) {
	file, err := os.Open(*cfgFlag)
	defer file.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	c, err := ParseConfig(file)
	if err != nil {
		return nil, err
	}

	if c.ResolveInterval < SmallestResolveInterval {
		log.Printf("configured resolve interval is lower than the minimum allowed: %v < %v\n", c.ResolveInterval, SmallestResolveInterval)
		c.ResolveInterval = SmallestResolveInterval
	}

	if c.PingInterval < SmallestPingInterval {
		log.Printf("configured ping interval is lower than the minimum allowed: %v < %v\n", c.PingInterval, SmallestPingInterval)
		c.PingInterval = SmallestPingInterval
	}

	return c, nil
}

type Config struct {
	// Targets are the destinations to monitor for connection latency.
	Targets []LatencyTarget

	// ResolveInterval is how often targets should be reresolved.
	// Smaller durations result in more accurate measurements
	// in the face of network changes, but create more load.
	//
	// Lowest value accepted is 1min.
	ResolveInterval time.Duration

	// PingInterval sets the duration to wait between latency
	// measurements. Lower values create a more granular picture
	// of the network latency, but create more load on the network.
	//
	// The lowest value accepted is 10ms.
	PingInterval time.Duration
}

type LatencyTarget interface {
	fmt.Stringer

	// TODO: move this out to web/network-monitor/resolve
	Resolve(context.Context, *net.Resolver) ([]netip.Addr, error)
}

// TraceHops attempts to run a traceroute to Dest, and uses the IP address
// for the Hop-th hop in the route. Only usable if the process is sufficiently
// privileged to run traceroute (eg: root, etc.)
//
// Hop can take all values between (- total hops, + total hops), other values
// will fail to resolve. Negative values index from the last hop _before_ the
// Dest specified.
type TraceHops struct {
	Dest netip.Addr
	// Hop specifies which of the trace route hops to resolve to.
	// Zero specifies the current host, one the first hop and so on.
	// Negative indicies are allowed, -1 specifies the hop before the Dest.
	Hop int
}

var _ LatencyTarget = &TraceHops{}

func (s *TraceHops) Resolve(ctx context.Context, r *net.Resolver) ([]netip.Addr, error) {
	res, err := trace.TraceRoute(ctx, s.Dest, trace.TraceRouteOptions{
		MaxHops:    s.Hop + 1,
		Retries:    5,
		HopTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	index := s.Hop
	if index < 0 {
		index += len(res.Hops)
	}
	// If the index is outside the range of reasonable, then it's an exception.
	// Since it's not possible to know the number of hops without having run a
	// trace route out of band, this likely constrains passed indexes to the
	// range between -2 and 2.
	if index < 0 || len(res.Hops) <= index {
		return nil, fmt.Errorf("traceroute has less than %d hops", s.Hop)
	}

	return []netip.Addr{
		res.Hops[index],
	}, nil
}

func (s *TraceHops) String() string {
	return fmt.Sprintf("TraceHops{Dest:%s, Hop:%d}", s.Dest, s.Hop)
}

type StaticIP struct {
	IP netip.Addr
}

var _ LatencyTarget = &StaticIP{}

func (s *StaticIP) Resolve(_ context.Context, _ *net.Resolver) ([]netip.Addr, error) {
	return []netip.Addr{s.IP}, nil
}
func (s *StaticIP) String() string {
	return fmt.Sprintf("StaticIps{%+v}", s.IP)
}

type HostnameTarget struct {
	Name string
}

var _ LatencyTarget = &HostnameTarget{}

func (s *HostnameTarget) Resolve(ctx context.Context, r *net.Resolver) ([]netip.Addr, error) {
	return r.LookupNetIP(ctx, "ip", s.Name)
}

func (s *HostnameTarget) String() string {
	return fmt.Sprintf("Hostname{Name:%s}", s.Name)
}
