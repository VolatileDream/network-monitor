package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"time"

	"web/network-monitor/config"
	//"web/network-monitor/icmp"
	"web/network-monitor/ping"
	"web/network-monitor/resolve"
	"web/network-monitor/trace"
	//"web/network-monitor/telemetry"
)

var (
	_ = trace.TraceResult{}
	_ = config.Config{}
	_ = resolve.ResolverService{}
)

func main() {
	/*
		cleanup, err := telemetry.Setup()
		defer cleanup()

		if err != nil {
			fmt.Printf("failed to setup telemetry: %v\n", err)
			os.Exit(1)
		}
	*/

	nets, _ := net.Interfaces()
	for _, iface := range nets {
		fmt.Println(iface)
		addrs, _ := iface.Addrs()
		fmt.Println("  ", addrs)
	}

	cfg(context.Background())

	res, err := trace.TraceRoute(
		context.Background(),
		//netip.MustParseAddr("192.168.100.1"),
		netip.MustParseAddr("8.8.8.8"),
		trace.TraceRouteOptions{
			MaxHops: 32,
			Retries: 1,
			//Interface: netip.MustParseAddr("192.168.1.117"),
		})
	fmt.Println(res)
	fmt.Println(err)

	names, err := trace.ResolveHops(context.Background(), res.Hops, 2*time.Second)
	fmt.Println(names)
	fmt.Println(err)

	return
}

func cfg(ctx context.Context) {
	// TODO: load from file regularily
	c := config.Config{
		Targets: []config.LatencyTarget{
			&config.TraceHops{
				Dest: netip.MustParseAddr("8.8.8.8"),
				Hop:  2, // not the gateway, but the ISP machine.
			},
			&config.StaticIPs{
				netip.MustParseAddr("192.168.1.1"),
			},
		},
		ResolveInterval: 15 * time.Minute,
		PingInterval:    time.Second,
	}

	cfgChan := make(chan config.Config, 1)
	cfgChan <- c

	resolver, resultChan := resolve.NewService(cfgChan, resolve.DefaultResolver())

	go resolver.Run(ctx)

	for results := range resultChan {
		fmt.Println("another config resolution")
		for _, res := range results.Resolved {
			fmt.Printf("%+v\n", res)
		}
	}
}

func run() {
	// Kill the app on sigint
	appCtx, appCancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer appCancel()

	manager, results := ping.NewManager(100)
	go manager.Run(appCtx)
	go printResults(appCtx, results)

	manager.Add(appCtx, ping.ProbeRequest{
		Source:      netip.MustParseAddr("192.168.1.117"),
		Destination: netip.MustParseAddr("192.168.1.1"),
	})
	manager.Add(appCtx, ping.ProbeRequest{
		Source:      netip.MustParseAddr("192.168.1.117"),
		Destination: netip.MustParseAddr("192.168.100.1"),
	})

	s := &http.Server{
		Addr:    "127.0.0.1:9090",
		Handler: http.DefaultServeMux,
		BaseContext: func(_ net.Listener) context.Context {
			fmt.Printf("setup http context\n")
			// Use appCtx to auto shutdown.
			return appCtx
		},
	}
	go killserver(appCtx, s)

	// Build up application
	t := time.AfterFunc(30*time.Second, func() {
		appCancel()
	})
	defer t.Stop()

	fmt.Printf("running...\n")
	s.ListenAndServe()
	fmt.Printf("server exit\n")
}

func killserver(ctx context.Context, s *http.Server) {
	select {
	case <-ctx.Done():
	}
	fmt.Println("running cancel...")
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s.Shutdown(c)
	s.Close()
}

func printResults(ctx context.Context, r <-chan *ping.PingResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-r:
			fmt.Printf("ping result %s: %s\n", result.Dest, result.Elapsed())
		}
	}
}
