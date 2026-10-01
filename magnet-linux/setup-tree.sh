#!/bin/sh
# Seed the installed recipes and package store; preserve administrator edits.
set -eu
umask 022
if test "$#" -lt 1 || test "$#" -gt 2; then
  echo "usage: $0 TARGET [BOOTSTRAP-ARCHIVE]" >&2
  exit 2
fi
target=$(realpath "$1")
repo=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
default_seed=$repo/bootstrap/out/bootstrap.tar.zst
if ! test -f "$default_seed"; then default_seed=/var/lib/magpkg/bootstrap.tar.zst; fi
seed=${2:-$default_seed}
seed_hash=2703bd4a9bd9fbdddb00cca71cbcced18b769e338df099c8809f28070d50316a
test "$target" != / || { echo 'use a mounted offline target' >&2; exit 1; }
test "$(id -u)" = 0 || { echo 'run setup-tree as root' >&2; exit 1; }
mountpoint -q "$target/var" || { echo 'mount the target /var subvolume first' >&2; exit 1; }
for relative in etc etc/magnet-linux etc/magnet-linux/tree var/lib var/lib/magpkg var/lib/magpkg/store; do
  test ! -L "$target/$relative" || { echo "symlink at $relative" >&2; exit 1; }
  if test -e "$target/$relative"; then test -d "$target/$relative"; fi
done
test "$(sha256sum "$seed" | cut -d ' ' -f 1)" = "$seed_hash" || {
  echo 'bootstrap archive has the wrong SHA-256' >&2; exit 1;
}
mkdir -p "$target/etc/magnet-linux" "$target/var/lib/magpkg"
installed_seed=$target/var/lib/magpkg/bootstrap.tar.zst
test ! -L "$installed_seed"
if test -e "$installed_seed"; then
  test "$(sha256sum "$installed_seed" | cut -d ' ' -f 1)" = "$seed_hash"
else
  seed_tmp=$(mktemp "$target/var/lib/magpkg/.bootstrap.XXXXXXXX")
  trap 'rm -f "$seed_tmp"' EXIT HUP INT TERM
  install -m 0644 "$seed" "$seed_tmp"
  mv "$seed_tmp" "$installed_seed"
  trap - EXIT HUP INT TERM
fi

tree=$target/etc/magnet-linux/tree
if ! test -e "$tree"; then
  scratch=$(mktemp -d "$target/etc/magnet-linux/.tree.XXXXXXXX")
  trap 'rm -rf "$scratch"' EXIT HUP INT TERM
  cp -R "$repo/packages" "$scratch/packages"
  mkdir "$scratch/magnet-linux"
  cp "$repo"/magnet-linux/*.sh "$repo"/magnet-linux/*.jsonnet \
    "$repo"/magnet-linux/*.config "$repo"/magnet-linux/*.md "$scratch/magnet-linux/"
  cp -R "$repo/magnet-linux/skeleton" "$scratch/magnet-linux/skeleton"
  cp -R "$repo/magnet-linux/desktop-skeleton" "$scratch/magnet-linux/desktop-skeleton"
  cp -R "$repo/magnet-linux/graphical-skeleton" "$scratch/magnet-linux/graphical-skeleton"
  # The working tree includes local recipes which are not in the magpkg source
  # pin. Record its base revision and the exact installed files separately.
  if command -v git >/dev/null 2>&1 && git -C "$repo" rev-parse HEAD >/dev/null 2>&1; then
    git -C "$repo" rev-parse HEAD > "$scratch/BASE-COMMIT"
  elif test -f "$repo/BASE-COMMIT"; then
    cp "$repo/BASE-COMMIT" "$scratch/BASE-COMMIT"
  fi
  (cd "$scratch"; find packages magnet-linux -type f | LC_ALL=C sort | xargs sha256sum) \
    > "$scratch/FILES.sha256"
  chmod 0755 "$scratch"
  mv "$scratch" "$tree"
  trap - EXIT HUP INT TERM
fi
selection=$target/etc/magnet-linux/system.jsonnet
if ! test -e "$selection" && ! test -L "$selection"; then
  echo "import './tree/magnet-linux/system.jsonnet'" > "$selection"
fi
chroot "$target" /bin/magpkg init --store /var/lib/magpkg/store
echo 'Package tree, bootstrap seed, and persistent store installed.'
