#!/bin/bash
export PATH=/bin
mkdir -p /proc /sys /dev /run /tmp
mount -t devtmpfs devtmpfs /dev
exec /bin/sinit
