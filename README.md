# Network Monitor

A go utility for probing the status of your network.

Useful to quickly check how your network is behaving.

# Running

Requires `CAP_NET_RAW` or running as a priviliged user to function.

Unlike the previous iteration, this one exposes metrics via prometheus
(address configured via `--bind`) instead of standard output. Configuration
file can be passed via `--config`.

