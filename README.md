# Network Monitor

A small linux only utility for probing the status of your network.

Useful to quickly check how your network is behaving.

# Running

```
> latency-monitor.sh | tee --append stats.csv
2021/12/01,19:24:21,eth0,gateway 10.0.0.1,1.85
2021/12/01,19:24:21,eth0,gateway 10.0.0.1,2.94
2021/12/01,19:24:21,eth0,gateway 10.0.0.1,1.94
2021/12/01,19:24:19,eth0,first-hop 99.250.28.1,11.5
2021/12/01,19:24:19,eth0,first-hop 99.250.28.1,11.1
2021/12/01,19:24:19,eth0,first-hop 99.250.28.1,12.2
```

# Disclaimer

Sure. You __can__ use this to look at your network, but should you?
You probably want a better tool to report network latency and to
collect it into a single spot. This works as a stop-gap hack before
you self host a better more production ready tool on all the machines
in your home network.

Also, `latency-summary.py` depends on private monorepo tooling.
