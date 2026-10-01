#!/bin/sh
# Provision an offline target; optionally enroll one explicitly selected admin.
set -eu
umask 022
if test "$#" -lt 1 || test "$#" -gt 2; then
  echo "usage: $0 TARGET [ADMIN-USER]" >&2
  exit 2
fi
target=$(realpath "$1")
test "$target" != / || { echo 'Use a mounted offline target.' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'Run setup-doas as root.' >&2; exit 1; }
test -x "$target/bin/doas"
test -f "$target/lib/security/pam_unix.so"
if test "$#" = 2; then
  user=$2
  case "$user" in ''|-*|*[!a-zA-Z0-9_-]*) echo 'Invalid username.' >&2; exit 2;; esac
  uid=$(chroot "$target" /bin/id -u "$user")
  test "$uid" != 0
fi
distro=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
for relative in etc etc/pam.d; do
  test ! -L "$target/$relative"
  mkdir -p "$target/$relative"
done
chroot "$target" /bin/groupadd -r -f wheel
for relative in etc/doas.conf etc/pam.d/doas; do
  destination=$target/$relative
  if test -e "$destination" || test -L "$destination"; then continue; fi
  mode=0644
  case "$relative" in etc/doas.conf) mode=0600;; esac
  install -o 0 -g 0 -m "$mode" "$distro/skeleton/$relative" "$destination"
done
chroot "$target" /bin/doas -C /etc/doas.conf
if test "$#" = 2; then
  chroot "$target" /bin/usermod -a -G wheel "$user"
  echo "Doas configured; $user is a member of wheel."
else
  echo 'Doas configured; add administrators to wheel explicitly.'
fi
