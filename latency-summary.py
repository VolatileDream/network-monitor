#!/usr/bin/env python-mr

from collections import defaultdict

from _.command_line.app import APP
from _.command_line.flags import Flag
from _.command_line.flags_ext import ListFlag
from _.data.formatting.blocks import Block
from _.sketch.t_digest.tdigest import TDigest

import logging
logger = logging.getLogger(__name__)


FLAG_lost_latency_mod = Flag.float("lost-latency-mod", default=1.1, description=
                                  ("Multiplier modifier for attributing latency "
                                   "to lost packets. Maximum latency seen so far is "
                                   "multiplied by this modifier when lost packets "
                                   "are encountered."))
FLAG_percentiles = ListFlag("percentiles", float, short="p",
                            default=[50, 90.0, 95.0, 99.0, 99.9])


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
  percentiles = list(FLAG_percentiles.value)
  percentiles.sort()
  for i in interfaces:
    #print(Block(i) | aggregate_block(interfaces[i], percentiles))
    pass

  for i in pingpairs:
    interface, dest = i
    digest = pingpairs[i]
    header = Block("%s => %s (lost: %s / %s)" % (interface, dest, lost[i], digest.count()))
    print(header | aggregate_block(digest, percentiles))


def main(filename):
  mod = FLAG_lost_latency_mod.value
  if mod < 1.0:
    logger.error("--lost-latency-mod should be set greater than or equal to one.")

  def digest():
    return TDigest(10000)

  interfaces = defaultdict(digest)
  pingpairs = defaultdict(digest)

  lost = defaultdict(int)

  maximum_latency = defaultdict(int)
  for (interface, ping, latency) in _lines(filename):
    dest = (interface, ping)
    if latency == "lost":
      lost[dest] += 1
      latency = maximum_latency[dest] * mod
    else:
      latency = float(latency)
      maximum_latency[dest] = max(latency, maximum_latency[dest])

    #interfaces[interface].add(latency)
    pingpairs[dest].add(latency)

  dump(interfaces, pingpairs, lost)


def m():
  APP.run(main)


if __name__ == "__main__": m()
