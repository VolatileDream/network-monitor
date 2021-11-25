#!/usr/bin/env python-mr

from collections import defaultdict

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


def dump(interfaces, pingpairs, lost):
  percentiles = [50, 90.0, 95.0, 99.0, 99.9]
  for i in interfaces:
    #print(Block(i) | aggregate_block(interfaces[i], percentiles))
    pass

  for i in pingpairs:
    interface, dest = i
    digest = pingpairs[i]
    header = Block("%s => %s (lost: %s / %s)" % (interface, dest, lost[i], digest.count()))
    print(header | aggregate_block(digest, percentiles))


def main(filename):
  def digest():
    return TDigest(10000)

  interfaces = defaultdict(digest)
  pingpairs = defaultdict(digest)

  lost = defaultdict(int)

  l = FLAG_lost_latency.value
  for (interface, ping, latency) in _lines(filename):
    if latency == "lost":
      lost[(interface, ping)] += 1
      if l is None:
        continue
      latency = l
    else:
      latency = float(latency)

    #interfaces[interface].add(latency)
    pingpairs[(interface, ping)].add(latency)

  dump(interfaces, pingpairs, lost)


if __name__ == "__main__":
  APP.run(main)
