package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
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

func main() {
	flag.Parse()
	cleanup, err := telemetry.Setup()
	defer cleanup()

	if err != nil {
		fmt.Printf("failed to setup telemetry: %v\n", err)
		os.Exit(1)
	}

	initMeter()

	// Kill the app on sigint
	appCtx, appCancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer appCancel()

	firstCfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("could not load config: %v\n", err)
	}

	// Split the configuration channel in two:
	// one for the Resolver, and another for the ping manager.
	cfgCh := make(chan config.Config, 1)
	cfgCh <- *firstCfg
	c1, c2 := split(appCtx, cfgCh)

	go signalHandler(appCtx, appCancel, cfgCh)

	resolver, resultCh := resolve.NewService(c1, resolve.DefaultResolver())
	go resolver.Run(appCtx)

	manager, results := ping.NewManager(100, c2, resultCh)
	go manager.Run(appCtx)
	go printResults(appCtx, results)

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

func split(ctx context.Context, c <-chan config.Config) (<-chan config.Config, <-chan config.Config) {
	one := make(chan config.Config, 1)
	two := make(chan config.Config, 1)

	go func() {
		select {
		case <-ctx.Done():
			return
		case cfg := <-c:
			one <- cfg
			two <- cfg
		}
	}()

	return one, two
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

var meter metric.Meter = metric.NewNoopMeter()

const (
	addrKey = attribute.Key("remote")
	nameKey = attribute.Key("name")
)

func initMeter() {
	meter = global.Meter("netmon")
}

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
				latency.Record(ctx,
					millis,
					addrKey.String(result.Dest.String()),
					nameKey.String(result.Target.MetricName()))
			}
		}
	}
}
