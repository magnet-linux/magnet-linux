#!/bin/sh
# Provision an offline graphical login installation without replacing /etc edits.
set -eu
umask 022
if test "$#" != 2; then echo "usage: $0 TARGET USER" >&2; exit 2; fi
target=$(realpath "$1")
user=$2
test "$target" != / || { echo 'Use a mounted offline target.' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'Run setup-login as root.' >&2; exit 1; }
distro=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
for program in greetd gtkgreet magnet-session magnet-greetd Hyprland waybar fuzzel mako notify-send; do
  test -x "$target/bin/$program"
done
for relative in etc etc/greetd etc/pam.d etc/xdg etc/xdg/hypr etc/xdg/waybar etc/xdg/fuzzel etc/xdg/mako \
  etc/magnet-linux etc/magnet-linux/audio etc/magnet-linux/audio/pipewire \
  etc/magnet-linux/audio/pipewire-pulse etc/magnet-linux/audio/wireplumber \
  etc/security etc/security/limits.d etc/wireplumber etc/wireplumber/wireplumber.conf.d \
  var/lib var/lib/greetd; do
  test ! -L "$target/$relative"
  mkdir -p "$target/$relative"
done
if ! chroot "$target" /bin/id greeter >/dev/null 2>&1; then
  chroot "$target" /bin/useradd -r -U -M -d /var/lib/greetd -s /bin/sh greeter
fi
greeter_uid=$(chroot "$target" /bin/id -u greeter)
greeter_gid=$(chroot "$target" /bin/id -g greeter)
test "$greeter_uid" != 0
chown "$greeter_uid:$greeter_gid" "$target/var/lib/greetd"
chmod 0700 "$target/var/lib/greetd"
"$distro/setup-desktop.sh" "$target" "$user"
"$distro/setup-desktop.sh" "$target" greeter
# Audio belongs to the desktop account; the login screen needs no sound access.
chroot "$target" /bin/usermod -a -G audio "$user"
"$distro/setup-network.sh" "$target" "$user"
"$distro/setup-doas.sh" "$target" "$user"
skeleton=$distro/graphical-skeleton
for service in greetd getty-tty2; do
  test ! -L "$target/etc/perp/$service"
  if ! test -d "$target/etc/perp/$service"; then
    install -d -m 1755 "$target/etc/perp/$service"
  fi
done
find "$skeleton" -type f | while IFS= read -r source; do
  destination=$target/${source#"$skeleton/"}
  if test -e "$destination" || test -L "$destination"; then continue; fi
  mode=0644
  case "$source" in */etc/perp/*/rc.*|*/etc/magnet-linux/audio/*/rc.main) mode=0755;; esac
  install -o 0 -g 0 -m "$mode" "$source" "$destination"
done
if ! test -e "$target/etc/localtime" && ! test -L "$target/etc/localtime"; then
  test -f "$target/share/zoneinfo/Etc/UTC"
  ln -s /share/zoneinfo/Etc/UTC "$target/etc/localtime"
fi
# A stock tty1 getty is replaced by greetd. Preserve custom service scripts.
if ! cmp -s "$target/etc/perp/getty-tty1/rc.main" \
    "$distro/desktop-skeleton/etc/perp/getty-tty1/rc.main"; then
  echo 'tty1 has a custom perp service; configure its login manager manually.' >&2
  exit 1
fi
chmod -t "$target/etc/perp/getty-tty1"
echo 'Graphical login configured: log in to Hyprland.'
