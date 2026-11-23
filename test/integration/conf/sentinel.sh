#!/bin/sh
# Starts a Sentinel. Its configuration is written once; Sentinel rewrites it.
#
# The primary it monitors is the data node of its own instance of the stack: the instances keep every port 100 apart
# (test/integration/instance.sh), so the instance's offset is the distance of this Sentinel's port from 26381 (instance 1's
# first Sentinel), rounded down to the hundred, and the primary's port is 16379 plus that offset. Deriving it here keeps the
# compose file's environment, and so instance 1's configuration hash, as it was.
set -eu
if [ ! -f /data/sentinel.conf ]; then
  primary=$((16379 + (SENTINEL_PORT - 26381) / 100 * 100))
  sed "s/^sentinel monitor quader 127.0.0.1 16379 /sentinel monitor quader 127.0.0.1 ${primary} /" /conf/sentinel.conf > /data/sentinel.conf.new
  grep -q "^sentinel monitor quader 127.0.0.1 ${primary} " /data/sentinel.conf.new
  {
    echo "tls-port ${SENTINEL_PORT}"
    echo "sentinel announce-port ${SENTINEL_PORT}"
  } >> /data/sentinel.conf.new
  mv /data/sentinel.conf.new /data/sentinel.conf
fi
exec valkey-server /data/sentinel.conf --sentinel
