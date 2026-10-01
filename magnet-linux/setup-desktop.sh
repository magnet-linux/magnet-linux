#!/bin/sh
# Initialize an offline desktop installation. Preserve administrator files.
set -eu
umask 022
if test "$#" != 2; then
  echo "usage: $0 TARGET USER" >&2
  exit 2
fi
target=$(realpath "$1")
user=$2
test "$target" != / || { echo 'Use a mounted offline target.' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'Run setup-desktop as root.' >&2; exit 1; }
case "$user" in ''|*[!a-zA-Z0-9_-]*) echo 'Invalid user name.' >&2; exit 1;; esac
chroot "$target" /bin/id -u "$user" >/dev/null
for program in foot seatd udevd Hyprland; do test -x "$target/bin/$program"; done
for relative in etc etc/init.d etc/xdg etc/xdg/foot etc/fonts etc/fonts/conf.d etc/magnet-linux; do
  test ! -L "$target/$relative"
  mkdir -p "$target/$relative"
done
for group in seat video render input dialout kmem sgx audio lp disk cdrom tape kvm; do
  if ! grep -q "^$group:" "$target/etc/group"; then
    chroot "$target" /bin/groupadd -r "$group"
  fi
done
for group in seat video render; do
  if ! chroot "$target" /bin/id -nG "$user" | tr ' ' '\n' | grep -Fxq "$group"; then
    chroot "$target" /bin/usermod -a -G "$group" "$user"
  fi
done
users=$target/etc/magnet-linux/desktop-users
test ! -L "$users"
touch "$users"
if ! grep -Fxq "$user" "$users"; then printf '%s\n' "$user" >> "$users"; fi

skeleton=$(CDPATH='' cd -- "$(dirname -- "$0")/desktop-skeleton" && pwd)
for service in udevd seatd getty-tty1; do
  test ! -L "$target/etc/perp/$service"
  if ! test -d "$target/etc/perp/$service"; then
    install -d -m 1755 "$target/etc/perp/$service"
  fi
done
find "$skeleton" -type f | while IFS= read -r source; do
  relative=${source#"$skeleton/"}
  destination=$target/$relative
  if test -e "$destination" || test -L "$destination"; then continue; fi
  mode=0644
  case "$relative" in etc/init.d/*|etc/perp/*/rc.*) mode=0755;; esac
  install -o 0 -g 0 -m "$mode" "$source" "$destination"
done
cp -a --update=none "$target/share/fontconfig/defaults/." "$target/etc/fonts/"
# Change only the untouched selection created by setup-tree; leave custom ones.
selection=$target/etc/magnet-linux/system.jsonnet
if test -f "$selection" && test "$(cat "$selection")" = "import './tree/magnet-linux/system.jsonnet'"; then
  echo "{ system: (import './tree/magnet-linux/desktop-hyprland.jsonnet').desktop }" > "$selection"
fi
echo "Desktop devices and runtime directory configured for $user."
