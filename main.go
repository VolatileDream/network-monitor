package main

import (
	"context"
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

	// Kill the app on sigint
	appCtx, appCancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer appCancel()

	firstCfg, err := loadConfig(appCtx)
	if err != nil {
		log.Fatal("could not load config: %v\n", err)
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
		Addr:    "127.0.0.1:9090",
		Handler: http.DefaultServeMux,
		BaseContext: func(_ net.Listener) context.Context {
			fmt.Printf("setup http context\n")
			// Use appCtx to auto shutdown.
			return appCtx
		},
	}
	go killserver(appCtx, server)

	t := time.AfterFunc(30*time.Second, func() {
		appCancel()
	})
	defer t.Stop()

	fmt.Printf("running...\n")
	server.ListenAndServe()
	fmt.Printf("server exit\n")
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
			_, err := loadConfig(appCtx)
			if err != nil {
				log.Printf("failed to load config: %v", err)
			} else {
				// TODO
				//cfgCh <- c
			}
		} else if sig == syscall.SIGINT {
			// tear down.
			break signal_loop
		}
	}

	cancel()
}

func a() {
	nets, _ := net.Interfaces()
	for _, iface := range nets {
		fmt.Println(iface)
		addrs, _ := iface.Addrs()
		fmt.Println("  ", addrs)
	}

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

func loadConfig(ctx context.Context) (*config.Config, error) {
	// TODO: load from file regularily
	return &config.Config{
		Targets: []config.LatencyTarget{
			//*
			&config.TraceHops{
				Dest: netip.MustParseAddr("8.8.8.8"),
				Hop:  3, // not the gateway, but the ISP machine.
			},
			&config.TraceHops{
				Dest: netip.MustParseAddr("8.8.8.8"),
				Hop:  2, // not the gateway, but the ISP machine.
			},
			//*/
			&config.TraceHops{
				Dest: netip.MustParseAddr("8.8.8.8"),
				Hop:  1, // gateway
			},
			/*
				&config.StaticIPs{
					netip.MustParseAddr("192.168.1.1"),
				},
			*/
		},
		ResolveInterval: 15 * time.Minute,
		PingInterval:    time.Second,
	}, nil
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
