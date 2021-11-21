#!/usr/bin/env python-mr

from _.command_line.app import APP
from _.command_line.flags import Flag
from _.data.formatting.blocks import Block
from _.sketch.t_digest.tdigest import TDigest

FLAG_lost_latency = Flag.float("lost-latency", default=None, description="Latency value to use for lost packets, by default they are ignored.")

def _lines(filename):
  with open(filename) as f:
    for line in f:
      date, time, interface, ping, latency = line.rstrip("\n\r").split(",")
      yield (date, time, interface, ping, latency)


def detect(filename):
  interfaces = set()
  ips = set()
  
  for data in _lines(filename):
    date, time, interface, ping, latency = data
    interfaces.add(interface)
    ips.add((interface, ping))

  return interfaces, ips


def aggregate_block(td, percentiles):
  ns = [str(p) for p in percentiles]
  ps = [str(td.percentile(p/100.0)) for p in percentiles]
  cs = (Block(), Block().vjoin(ns), Block().vjoin(["="] * len(ns)), Block().vjoin(ps))
  return Block.space().hjoin(cs)


def main(filename):
  interfaces, pingpairs = detect(filename)

  interfaces = { i: TDigest(1000) for i in interfaces }
  pingpairs = { i: TDigest(1000) for i in pingpairs }


  l = FLAG_lost_latency.value
  for data in _lines(filename):
    date, time, interface, ping, latency = data
    if latency == "lost":
      if l is not None:
       latency = l
      else:
        continue
    latency = float(latency)
    interfaces[interface].add(latency)
    pingpairs[(interface, ping)].add(latency)

  percentiles = [50, 90.0, 95.0, 99.0, 99.9]

  for i in interfaces:
    #print(Block(i) | aggregate_block(interfaces[i], percentiles))
    pass

  for i in pingpairs:
    print(Block("%s => %s" % i) | aggregate_block(pingpairs[i], percentiles))


if __name__ == "__main__":
  APP.run(main)
