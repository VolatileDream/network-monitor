package config

import (
	"bytes"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func Test_Parse(t *testing.T) {
	tests := []struct {
		name string
		json string
		cfg  Config
		err  bool
	}{
		{
			name: "empty struct",
			json: "{}",
			cfg: Config{
				Targets:         []LatencyTarget{},
				ResolveInterval: defaultResolveInterval,
				PingInterval:    defaultPingInterval,
			},
			err: false,
		},
		{
			name: "bad hop id",
			json: `{"hops":[{"name": "abc", "destination":"abc", "hop":3}]}`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "bad static id",
			json: `{"static":[{"ip":"abc"}]}`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "bad resolve time",
			json: `{"resolve-interval":"abc"}`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "bad ping time",
			json: `{"ping-interval":"abc"}`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "bad json",
			json: `{"`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "unknown field",
			json: `{"abc":1}`,
			cfg:  Config{},
			err:  true,
		},
		{
			name: "correct parsing everything",
			json: `{
  "hops":[{"name":"isp-hop", "destination":"8.8.8.8", "hop":2}],
  "static":[{"name":"router", "ip":"192.168.1.1"}, {"ip":"1.1.1.1"}],
  "hosts":[{"host":"pkg.go.dev"}, {"name": "mysite", "host":"example.com"}],
  "resolve-interval":"10m",
  "ping-interval":"5s"
}`,
			cfg: Config{
				Targets: []LatencyTarget{
					&TraceHops{
						Name: "isp-hop",
						Dest: netip.MustParseAddr("8.8.8.8"),
						Hop:  2,
					},
					&StaticIP{
						Name: "router",
						IP:   netip.MustParseAddr("192.168.1.1"),
					},
					&StaticIP{
						Name: "static-ip:1.1.1.1",
						IP:   netip.MustParseAddr("1.1.1.1"),
					},
					&HostnameTarget{
						Name: "host:pkg.go.dev",
						Host: "pkg.go.dev",
					},
					&HostnameTarget{
						Name: "mysite",
						Host: "example.com",
					},
				},
				ResolveInterval: 10 * time.Minute,
				PingInterval:    5 * time.Second,
			},
			err: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := ParseConfig(bytes.NewBufferString(test.json))
			if test.err {
				if err == nil {
					t.Errorf("expected an error when parsing: %s", test.json)
				}
			} else if err != nil {
				t.Errorf("did not expect error: %v", err)
			} else if !reflect.DeepEqual(c, &test.cfg) {
				t.Errorf("got: %v", c)
				t.Errorf("want: %v", test.cfg)
			}
		})
	}
}
