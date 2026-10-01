#!/bin/sh
# Provision desktop networking in a mounted offline installation, preserving /etc.
set -eu
umask 022
if test "$#" != 2; then echo "usage: $0 TARGET USER" >&2; exit 2; fi
target=$(realpath "$1")
user=$2
case "$user" in ''|-*|*[!a-zA-Z0-9_-]*) echo 'Invalid username.' >&2; exit 2;; esac
test "$target" != / || { echo 'Use a mounted offline target.' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'Run setup-network as root.' >&2; exit 1; }
test "$(chroot "$target" /bin/id -u "$user")" != 0
distro=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
for program in NetworkManager nmcli nmtui dbus-daemon wpa_supplicant; do
  test -x "$target/bin/$program"
done
test -x "$target/lib/polkit-1/polkitd"
# Refuse to silently disable an administrator's replacement DHCP service.
if test -d "$target/etc/perp/dhcpcd"; then
  if ! cmp -s "$target/etc/perp/dhcpcd/rc.main" "$distro/skeleton/etc/perp/dhcpcd/rc.main"; then
    echo 'Custom dhcpcd service: arrange the NetworkManager handover manually.' >&2
    exit 1
  fi
fi
for relative in etc etc/NetworkManager etc/NetworkManager/conf.d \
  etc/NetworkManager/system-connections etc/dbus-1 etc/dbus-1/system.d \
  etc/polkit-1 etc/polkit-1/rules.d etc/perp var/lib var/lib/dbus \
  var/lib/NetworkManager var/lib/polkit-1; do
  test ! -L "$target/$relative"
  mkdir -p "$target/$relative"
done
chmod 0700 "$target/etc/NetworkManager/system-connections" "$target/var/lib/NetworkManager"
for account in messagebus polkitd; do
  if ! chroot "$target" /bin/id "$account" >/dev/null 2>&1; then
    chroot "$target" /bin/groupadd -r -f "$account"
    chroot "$target" /bin/useradd -r -M -d / -s /bin/false -g "$account" "$account"
  fi
  test "$(chroot "$target" /bin/id -u "$account")" != 0
done
chown "$(chroot "$target" /bin/id -u polkitd):$(chroot "$target" /bin/id -g polkitd)" "$target/var/lib/polkit-1"
chmod 0700 "$target/var/lib/polkit-1"
chroot "$target" /bin/groupadd -r -f netdev
chroot "$target" /bin/usermod -a -G netdev "$user"
for service in dbus polkitd wpa_supplicant nm-dispatcher NetworkManager; do
  test ! -L "$target/etc/perp/$service"
  # Preserve a service the administrator deliberately disabled on later runs.
  if ! test -d "$target/etc/perp/$service"; then
    install -d -m 1755 "$target/etc/perp/$service"
  fi
done
skeleton=$distro/graphical-skeleton
for relative in etc/NetworkManager/NetworkManager.conf \
  etc/NetworkManager/system-connections/wired.nmconnection etc/polkit-1/rules.d/50-network.rules \
  etc/perp/dbus/rc.main etc/perp/polkitd/rc.main \
  etc/perp/wpa_supplicant/rc.main etc/perp/nm-dispatcher/rc.main etc/perp/NetworkManager/rc.main; do
  destination=$target/$relative
  if test -e "$destination" || test -L "$destination"; then continue; fi
  mode=0644
  case "$relative" in etc/perp/*) mode=0755;; *.nmconnection) mode=0600;; esac
  install -o 0 -g 0 -m "$mode" "$skeleton/$relative" "$destination"
done
if test -d "$target/etc/perp/dhcpcd"; then chmod -t "$target/etc/perp/dhcpcd"; fi
echo 'NetworkManager configured; the desktop account can run nmtui or nmcli.'
