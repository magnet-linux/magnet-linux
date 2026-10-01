#!/bin/sh
# Initialize time synchronization in a mounted offline target, preserving /etc.
set -eu
umask 022
if test "$#" != 1; then echo "usage: $0 TARGET" >&2; exit 2; fi
target=$(realpath "$1")
test "$target" != / || { echo 'Use a mounted offline target.' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'Run setup-time as root.' >&2; exit 1; }
test -x "$target/bin/chronyd"
distro=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
for relative in etc etc/perp etc/perp/chronyd var var/lib var/lib/chrony; do
  test ! -L "$target/$relative"
done
mkdir -p "$target/etc/perp" "$target/var/lib/chrony"
if ! chroot "$target" /bin/id chrony >/dev/null 2>&1; then
  chroot "$target" /bin/groupadd -r -f chrony
  chroot "$target" /bin/useradd -r -M -d /var/lib/chrony -s /bin/false -g chrony chrony
fi
uid=$(chroot "$target" /bin/id -u chrony)
gid=$(chroot "$target" /bin/id -g chrony)
test "$uid" != 0
chown "$uid:$gid" "$target/var/lib/chrony"
chmod 0750 "$target/var/lib/chrony"
if ! test -d "$target/etc/perp/chronyd"; then
  install -d -m 1755 "$target/etc/perp/chronyd"
fi
for relative in etc/chrony.conf etc/perp/chronyd/rc.main; do
  destination=$target/$relative
  if test -e "$destination" || test -L "$destination"; then continue; fi
  mode=0644
  case "$relative" in */rc.main) mode=0755;; esac
  install -o 0 -g 0 -m "$mode" "$distro/skeleton/$relative" "$destination"
done
echo 'Chrony configured for automatic time synchronization.'
