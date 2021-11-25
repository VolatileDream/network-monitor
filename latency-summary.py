#!/usr/bin/env python-mr

from _.command_line.app import APP
from _.command_line.flags import Flag
from _.data.formatting.blocks import Block
from _.sketch.t_digest.tdigest import TDigest

FLAG_lost_latency = Flag.float("lost-latency", default=None, description="Latency value to use for lost packets, by default they are ignored.")

def _lines(filename):
  with open(filename) as f:
    for line in f:
      line = line.rstrip()
      _date, _time, interface, ping, latency = line.split(",")
      yield (interface, ping, latency)


def aggregate_block(td, percentiles):
  ns = [str(p) for p in percentiles]
  ps = [str(td.percentile(p/100.0)) for p in percentiles]
  cs = (Block(), Block().vjoin(ns), Block().vjoin(["="] * len(ns)), Block().vjoin(ps))
  return Block.space().hjoin(cs)


def main(filename):
  interfaces = {}
  pingpairs = {}

  def digest():
    return TDigest(1000)

  l = FLAG_lost_latency.value
  for (interface, ping, latency) in _lines(filename):
    if latency == "lost":
      if l is None:
        continue
      latency = l
    else:
      latency = float(latency)

    #if interface not in interfaces:
    #  interfaces[interface] = digest()
    #interfaces[interface].add(latency)

    if (interface, ping) not in pingpairs:
      pingpairs[(interface, ping)] = digest()
    pingpairs[(interface, ping)].add(latency)

  percentiles = [50, 90.0, 95.0, 99.0, 99.9]

  for i in interfaces:
    #print(Block(i) | aggregate_block(interfaces[i], percentiles))
    pass

  for i in pingpairs:
    print(Block("%s => %s" % i) | aggregate_block(pingpairs[i], percentiles))


if __name__ == "__main__":
  APP.run(main)
