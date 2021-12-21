#!/usr/bin/env bash
set -o pipefail -o errexit -o errtrace -o nounset

debug() {
  echo "Trapped ERR, func trace:" >> /dev/stderr
  local i=${#BASH_LINENO[@]}
  while [[ $i -gt 0 ]]; do
    i=$((i-1))
    echo " ${FUNCNAME[$i]} at ${BASH_SOURCE[$i]}:${BASH_LINENO[$i]}" >> /dev/stderr
  done
  exit
}
trap debug ERR


interfaces() {
  ls /sys/class/net/
}

state() {
  local iface="$1" ; shift
  # Sometimes interfaces disappear. When that happens, treat it as though
  # we don't know what their state it.
  cat "/sys/class/net/${iface}/operstate" 2> /dev/null || echo unknown
}

test-ping() {
  local -r iface="$1"; shift
  local -r hop="$1"; shift
  ping -4 -c 1 -W 1 -I "$iface" "$hop" > /dev/null && echo "$hop" || true
}

gateway() {
  local -r iface="$1" ; shift
  local -r gateway=`ip route list dev "$iface" | awk '/^default/ { print $3 }'`
  echo "Testing reachability of $gateway via $iface" >> /dev/stderr
  test-ping "$iface" "$gateway"
}

gateway-next-hop() {
  local -r iface="$1" ; shift
  # Each of these is a public ip address that responds to pings.
  # They are also well known DNS providers, and can take a little more
  # traffic from our script (BECAUSE IT IS GOING TO BE INFREQUENT).
  #
  # But also, traceroute traffic shouldn't reach them because of the way
  # it gets used below.
  local -a options=("1.1.1.1" "1.0.0.1" "8.8.8.8" "8.8.4.4" "9.9.9.9" "speedtest.net")
  # Pick one to use.
  local host=$(shuf --head-count 1 --echo "${options[@]}")

  # Run traceroute to find out the hop after the gateway server.
  # Traceroute exits with non-zero because it doesn't succesfully ping the
  # server we told it to. We don't want it to do that, so we must `|| true`.
  local -r hop=`(traceroute --icmp --wait=1 --max-hop=2 "$host" || true) |\
                 awk '$1 == 2 { print $2 }'`

  # Test the value from traceroute. If it can not be pinged, it's no good.
  echo "Using $host for first-hop => got $hop" >> /dev/stderr
  test-ping "$iface" "$hop"
}

prefix-timestamp() {
  # Need the flush to ensure that when piped to something this still looks nice.
  awk '{ print strftime("%Y/%m/%d,%H:%M:%S") "," $0 ; fflush(); }'
}

do-ping() {
  # Outputs "iface, label dest, ("lost" | ping)"
  local -r count=3
  local -r iface="$1" ; shift
  local -r label="$1" ; shift
  local -r dest="$1" ; shift
  # If no packets arrive ping will exit with non-zero status.
  # We want to keep running when that happens.
  (ping -4 -c $count -I "$iface" "$dest" -O -W 1 || true) | \
    awk ' /no answer yet/ { print "lost" } /time=/ { print(substr($7, 6)) }' |\
    awk "
      BEGIN { c = 0 ; }
      { print ; c += 1 ; }
      END {
        for (; c < $count ; c++) {
          print(\"lost\");
        }
      }
    " |\
    awk "{ print \"${iface},${label} ${dest},\" \$0 }"
}

hilite-lost() {
  # Hilites lost pings.
  awk '/lost/ { print("\033[31m" $0 "\033[0m"); } !/lost/ { print }'
}

monitor() {
  local -r reset_time=$1 ; shift
  local -r -a ifaces=(`interfaces`)
  local -A gateways=()
  local -A nexts=()

  echo "Setting up gateway & next hop across: ${ifaces[@]}" >> /dev/stderr

  for i in "${ifaces[@]}"; do
    gateways["$i"]=`gateway "$i"`
    nexts["$i"]=`gateway-next-hop "$i"`
  done

  echo "Monitoring..." >> /dev/stderr

  while [[ $SECONDS -lt $reset_time ]]; do
    for i in "${ifaces[@]}"; do
      if [[ "$(state "$i")" != "up" ]]; then
        # Downed interfaces get no output.
        continue
      fi
      local g="${gateways[$i]}"
      local n="${nexts[$i]}"
      [ -n "$g" ] && do-ping "$i" gateway "$g"
      [ -n "$n" ] && do-ping "$i" first-hop "$n"
    done
  done  
}

run() {
  # Reset attributes every 15m, mostly to reset first-hop data.
  # Not having first-hop data for 15m is tolerable, since the initialization
  # of the gateways & first-hop addresses takes a few seconds, it's better
  # not to repeatedly perform that setup.
  local -r period=$((60 * 60 * 1 / 4))
  # Jitter window has width of 10% of the monitoring period.
  local -r jitter=$((period / 10))

  while true; do
    local duration=$((period + RANDOM % jitter))
    monitor $((SECONDS + duration)) | prefix-timestamp
  done
}

if [[ $# -le 0 ]]; then
  run
else
  case "$1" in
    hilite) run | hilite-lost ;;
    *) echo "unrecognized command: $1" 2>> /dev/stderr ; exit 1 ;;
  esac
fi
