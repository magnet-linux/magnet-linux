#!/usr/bin/env bash
set -euo pipefail
distro=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR
source "$distro/mount-layout.sh"
test "$#" = 1 || { echo 'usage: setup-fstab.sh TARGET' >&2; exit 2; }
read_layout "$1"
mkdir -p "$target/etc"
test ! -L "$target/etc/fstab"
if test ! -e "$target/etc/fstab"; then
  temporary=$(mktemp "$target/etc/.fstab.XXXXXXXX")
  trap 'rm -f "$temporary"' EXIT
  write_fstab > "$temporary"
  chmod 0644 "$temporary"
  mv "$temporary" "$target/etc/fstab"
fi
