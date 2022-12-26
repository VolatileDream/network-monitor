# Network Monitor

A go utility for probing the status of your network.

Useful to quickly check how your network is behaving.

# Running

Requires `CAP_NET_RAW` or running as a priviliged user to function.

Unlike the previous iteration, this one exposese metrics via prometheus
(address configured via `--bind`) instead of standard output.

## TODO

 * Add loadable configuration file
