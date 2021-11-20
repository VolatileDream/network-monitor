#!/usr/bin/env bash

interfaces() {
  ls /sys/class/net/
}

gateway() {
  local iface="$1" ; shift
  ip route list dev "$iface" | awk '/^default/ { print $3 }'
}

state() {
  local iface="$1" ; shift
  cat "/sys/class/net/${iface}/operstate"
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
  traceroute --icmp --wait=1 --max-hop=2 "$host" | awk '$1 == 2 { print $2 }'
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
  ping -4 -c $count -I $iface "$dest" -O -W 1 | \
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

  echo "status,reinit"

  for i in "${ifaces[@]}"; do
    gateways["$i"]=`gateway "$i"`
    nexts["$i"]=`gateway-next-hop "$i"`
  done

  echo "status,ready"

  while [[ $SECONDS -lt $reset_time ]]; do
    for i in "${ifaces[@]}"; do
      if [[ "$(state "$i")" != "up" ]]; then
        # Downed interfaces get no output.
        #echo "${i},not-up"
        continue
      fi
      local g="${gateways[$i]}"
      local n="${nexts[$i]}"
      do-ping "$i" gateway "$g"
      do-ping "$i" first-hop "$n"
    done
  done  
}

run() {
  # Reset attributes every hour
  local -r period=$((60 * 60 * 1))
  # Jitter window 10m wide
  local -r jitter=$((60 * 10))

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
    *) echo "unrecognized command: $1" ; exit 1 ;;
  esac
fi
