package config

import (
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"time"
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

	// Name returns the human readable name that describes this target.
	// Eg: "gateway", "isp", "router", "desktop", "wifi", etc.
	//
	// This is passed along and displayed in metrics as a more stable
	// identifier in addition to the ip addresses.
	MetricName() string
}

// TraceHops attempts to run a traceroute to Dest, and uses the IP address
// for the Hop-th hop in the route. Only usable if the process is sufficiently
// privileged to run traceroute (eg: root, etc.)
//
// Hop can take all values between (- total hops, + total hops), other values
// will fail to resolve. Negative values index from the last hop _before_ the
// Dest specified.
type TraceHops struct {
	Name string
	Dest netip.Addr
	// Hop specifies which of the trace route hops to resolve to.
	// Zero specifies the current host, one the first hop and so on.
	// Negative indicies are allowed, -1 specifies the hop before the Dest.
	Hop int
}

var _ LatencyTarget = &TraceHops{}

func (s *TraceHops) MetricName() string {
	return s.Name
}

func (s *TraceHops) String() string {
	return fmt.Sprintf("TraceHops{Name: %s, Dest:%s, Hop:%d}", s.Name, s.Dest, s.Hop)
}

type StaticIP struct {
	Name string
	IP   netip.Addr
}

var _ LatencyTarget = &StaticIP{}

func (s *StaticIP) MetricName() string {
	return s.Name
}
func (s *StaticIP) String() string {
	return fmt.Sprintf("StaticIps{Name:%s, IP:%+v}", s.Name, s.IP)
}

type HostnameTarget struct {
	Name string
	Host string
}

var _ LatencyTarget = &HostnameTarget{}

func (s *HostnameTarget) MetricName() string {
	return s.Name
}
func (s *HostnameTarget) String() string {
	return fmt.Sprintf("Hostname{Name:%s, Host:%s}", s.Name, s.Host)
}
