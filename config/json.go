package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"time"
)

const (
	defaultResolveInterval = 15 * time.Minute
	defaultPingInterval    = 1 * time.Second
)

// JsonConfig exists to serialize Configs to and from disk, because of the
// nature of the dynamic types.
type JsonConfig struct {
	Hops            []JsonTraceHop `json:"hops"`
	Static          []string       `json:"static"`
	Hosts           []string       `json:"hosts"`
	ResolveInterval string         `json:"resolve-interval"`
	PingInterval    string         `json:"ping-interval"`
}

type JsonTraceHop struct {
	Destination string `json:"destination"`
	Hop         int    `json:"hop"`
}

func ParseConfig(r io.Reader) (*Config, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var j JsonConfig
	err := decoder.Decode(&j)
	if err != nil {
		return nil, err
	}

	c := &Config{
		Targets:         make([]LatencyTarget, 0, len(j.Hops)+len(j.Static)+len(j.Hosts)),
		ResolveInterval: 15 * time.Minute,
		PingInterval:    1 * time.Second,
	}

	if len(j.ResolveInterval) > 0 {
		if d, err := time.ParseDuration(j.ResolveInterval); err != nil {
			return nil, fmt.Errorf("failed to parse 'resolve-interval': %w", err)
		} else {
			c.ResolveInterval = d
		}
	}

	if len(j.PingInterval) > 0 {
		if d, err := time.ParseDuration(j.PingInterval); err != nil {
			return nil, fmt.Errorf("failed to parse 'ping-interval': %w", err)
		} else {
			c.PingInterval = d
		}
	}

	for index, th := range j.Hops {
		dest, err := netip.ParseAddr(th.Destination)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'hops[%d]': %w", index, err)
		}
		c.Targets = append(c.Targets, &TraceHops{
			Dest: dest,
			Hop:  th.Hop,
		})
	}

	for index, ip := range j.Static {
		dest, err := netip.ParseAddr(ip)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'static[%d]': %w", index, err)
		}
		c.Targets = append(c.Targets, &StaticIP{
			IP: dest,
		})
	}

	for _, h := range j.Hosts {
		c.Targets = append(c.Targets, &HostnameTarget{
			Name: h,
		})
	}

	return c, nil
}
