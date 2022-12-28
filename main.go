package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web/network-monitor/config"
	"web/network-monitor/ping"
	"web/network-monitor/resolve"
	"web/network-monitor/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/unit"
)

var (
	bindFlag = flag.String("bind",
		"127.0.0.1:9090",
		"Host and port to bind to for prometheus metrics export.")
)

var meter metric.Meter = metric.NewNoopMeter()

func main() {
	flag.Parse()
	cleanup, err := telemetry.Setup()
	defer cleanup()

	if err != nil {
		fmt.Printf("failed to setup telemetry: %v\n", err)
		os.Exit(1)
	}

	meter = global.Meter("netmon")

	// Kill the app on sigint
	appCtx, appCancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer appCancel()

	firstCfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("could not load config: %v\n", err)
	}

	cfgCh := make(chan config.Config, 1)
	cfgCh <- *firstCfg

	go signalHandler(appCtx, appCancel, cfgCh)

	resolver, resultCh := resolve.NewService(cfgCh, resolve.DefaultResolver())
	go resolver.Run(appCtx)

	manager, results := ping.NewManager(100)
	go manager.Run(appCtx)
	go printResults(appCtx, results)

	go glue(appCtx, resultCh, manager)

	server := &http.Server{
		Addr:    *bindFlag,
		Handler: http.DefaultServeMux,
		BaseContext: func(_ net.Listener) context.Context {
			// Use appCtx to auto shutdown.
			return appCtx
		},
	}
	go killserver(appCtx, server)

	fmt.Printf("running...\n")
	log.Fatal(server.ListenAndServe())
}

func signalHandler(appCtx context.Context, cancel func(), cfgCh chan config.Config) {
	// this lives for the life of the application.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGHUP)

signal_loop:
	for {
		var sig os.Signal

		select {
		case <-appCtx.Done():
			break signal_loop
		case sig = <-signals:
		}

		log.Printf("got signal: %s\n", sig)

		if sig == syscall.SIGHUP {
			// reload cfg
			log.Printf("reloading config...\n")
			c, err := config.LoadConfig()
			if err != nil {
				log.Printf("failed to load config: %v", err)
			} else {
				cfgCh <- *c
			}
		} else if sig == syscall.SIGINT {
			// tear down.
			break signal_loop
		}
	}

	cancel()
}

func glue(ctx context.Context, resolveCh <-chan resolve.Result, m *ping.Manager) {
	ips := make(map[netip.Addr]struct{})

	for {
		var r resolve.Result
		select {
		case <-ctx.Done():
			return
		case r = <-resolveCh:
		}

		log.Printf("config resolved: %v\n", r)

		newIps := make(map[netip.Addr]struct{})
		for _, resolution := range r.Resolved {
			if resolution.Error != nil {
				log.Printf("failed to resolve '%s': %v", resolution.Target, resolution.Error)
			} else {
				for _, addr := range resolution.Addrs {
					if addr.IsValid() {
						newIps[addr] = struct{}{}
					}
				}
			}
		}

		remove := 0
		for ip, _ := range ips {
			if _, ok := newIps[ip]; !ok {
				remove += 1
				pr := prFromIp(ip)
				m.Remove(ctx, pr)
			}
		}
		add := 0
		for ip, _ := range newIps {
			if _, ok := ips[ip]; !ok {
				add += 1
				pr := prFromIp(ip)
				m.Add(ctx, pr)
			}
		}
		ips = newIps // overwrite

		log.Printf("updated %d probe endpoints\n", remove+add)
	}
}

func prFromIp(ip netip.Addr) ping.ProbeRequest {
	pr := ping.ProbeRequest{
		Source:      netip.IPv6Unspecified(),
		Destination: ip,
	}
	if ip.Is4() {
		pr.Source = netip.IPv4Unspecified()
	}
	return pr
}

func killserver(ctx context.Context, s *http.Server) {
	select {
	case <-ctx.Done():
	}

	fmt.Println("server teardown...")
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s.Shutdown(c)
	s.Close()
}

const (
	hostKey = attribute.Key("remote")
)

func printResults(ctx context.Context, r <-chan *ping.PingResult) {
	latency, err := meter.SyncFloat64().Histogram(
		"network/latency",
		instrument.WithUnit(unit.Milliseconds),
		instrument.WithDescription("Latency from this host to the specified target."))

	if err != nil {
		log.Printf("Failed to create metric: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case result := <-r:
			millis := float64(result.Elapsed().Microseconds()) / 1000.0
			//log.Printf("ping result %s: %f\n", result.Dest, millis)
			if latency != nil {
				latency.Record(ctx, millis, hostKey.String(result.Dest.String()))
			}
		}
	}
}
