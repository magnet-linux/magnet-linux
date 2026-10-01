#!/bin/sh
set -eu
export PATH=/bin
umask 077
exec /bin/greetd --config /etc/greetd/config.toml >> /var/log/greetd.log 2>&1
