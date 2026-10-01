#!/bin/sh
# Initialize a mounted installation after magpkg sync. Never replace /etc files.
set -eu
umask 022

if test "$#" -ne 4; then
  echo "usage: $0 TARGET HOSTNAME ROOT-FS-UUID ESP-FS-UUID" >&2
  exit 2
fi
target=$(realpath "$1")
hostname=$2
root_uuid=$3
esp_uuid=$4
test "$target" != / || { echo 'refusing to initialize the host root' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'run setup-root as root' >&2; exit 1; }
case "$hostname" in ''|*[!a-zA-Z0-9.-]*) echo 'invalid hostname' >&2; exit 1;; esac
for uuid in "$root_uuid" "$esp_uuid"; do
  case "$uuid" in ''|*[!a-fA-F0-9-]*) echo 'invalid filesystem UUID' >&2; exit 1;; esac
done
for program in init perpd sh bash useradd groupadd mount makewhatis; do
  test -x "$target/bin/$program" || { echo 'sync the system packages first' >&2; exit 1; }
done
skeleton=$(CDPATH='' cd -- "$(dirname -- "$0")/skeleton" && pwd)

# Check the administrator-owned destinations before copying or changing modes.
for relative in etc etc/init.d etc/ssh root home var var/mail run tmp sys boot boot/efi; do
  test ! -L "$target/$relative" || { echo "symlink at $relative" >&2; exit 1; }
  if test -e "$target/$relative"; then
    test -d "$target/$relative" || { echo "not a directory: $relative" >&2; exit 1; }
  fi
done
mkdir -p "$target/etc/init.d" "$target/etc/ssh" "$target/boot/efi" "$target/run" "$target/sys" "$target/home" "$target/var"
mkdir -p "$target/var/mail"

new_serial_service=false
test -d "$target/etc/perp/getty-ttyS0" || new_serial_service=true
new_console_service=false
test -d "$target/etc/perp/getty-tty1" || new_console_service=true
new_time_service=false
test -d "$target/etc/perp/chronyd" || new_time_service=true
find "$skeleton" -type d | while IFS= read -r source; do
  relative=${source#"$skeleton"}
  test ! -L "$target$relative" || { echo "symlink at $relative" >&2; exit 1; }
  mkdir -p "$target$relative"
done

find "$skeleton" -type f | while IFS= read -r source; do
  relative=${source#"$skeleton/"}
  destination=$target/$relative
  if test -e "$destination" || test -L "$destination"; then
    continue
  fi
  mode=0644
  case "$relative" in
    etc/shadow|etc/doas.conf) mode=0600;;
    etc/init.d/*|etc/rc.*|etc/perp/*/rc.*) mode=0755;;
  esac
  install -o 0 -g 0 -m "$mode" "$source" "$destination"
done
if "$new_serial_service"; then chmod +t "$target/etc/perp/getty-ttyS0"; fi
if "$new_console_service"; then chmod +t "$target/etc/perp/getty-tty1"; fi
# Perp keeps locks and its control socket in tmpfs, not in the root snapshot.
if ! test -e "$target/etc/perp/.control" && ! test -L "$target/etc/perp/.control"; then
  ln -s /run/perp "$target/etc/perp/.control"
fi
if ! test -e "$target/etc/mtab" && ! test -L "$target/etc/mtab"; then
  ln -s /proc/self/mounts "$target/etc/mtab"
fi

if ! test -e "$target/etc/hostname" && ! test -L "$target/etc/hostname"; then
  printf '%s\n' "$hostname" > "$target/etc/hostname"
fi
"$(dirname -- "$skeleton")/setup-fstab.sh" "$target"
if test -f "$target/share/e2fsprogs/mke2fs.conf" && ! test -e "$target/etc/mke2fs.conf"; then
  install -m 0644 "$target/share/e2fsprogs/mke2fs.conf" "$target/etc/mke2fs.conf"
fi

# Accounts are machine state, not package contents. This also initializes SSH
# when re-running setup on an older installation, without replacing passwd.
if ! chroot "$target" /bin/id sshd >/dev/null 2>&1; then
  if ! grep -q '^sshd:' "$target/etc/group"; then
    chroot "$target" /bin/groupadd -r sshd
  fi
  chroot "$target" /bin/useradd -r -M -d /run/sshd -s /bin/false -g sshd sshd
fi
test "$(chroot "$target" /bin/id -u sshd)" != 0
if ! chroot "$target" /bin/id dhcpcd >/dev/null 2>&1; then
  chroot "$target" /bin/groupadd -r -f dhcpcd
  chroot "$target" /bin/useradd -r -M -d /var/empty -s /bin/false -g dhcpcd dhcpcd
fi
# Use the target's static keygen with a path prefix, so host entropy works
# without mounting /dev in the offline target. -A preserves existing keys.
"$target/bin/ssh-keygen" -A -f "$target"
"$(dirname -- "$skeleton")/setup-time.sh" "$target"
if "$new_time_service"; then chmod +t "$target/etc/perp/chronyd"; fi
"$(dirname -- "$skeleton")/setup-doas.sh" "$target"
# The keyword index is derived from the installed manuals, not configuration.
chroot "$target" /bin/makewhatis /share/man
echo 'Initial configuration installed. Set passwords with chroot TARGET passwd.'
