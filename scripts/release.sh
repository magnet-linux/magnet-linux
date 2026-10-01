#!/usr/bin/env bash
# Distro releases contain policy and a dependency lock, with no embedded trees.
set -euo pipefail
umask 022
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${1:?usage: release.sh VERSION [REF [OUTPUT-DIRECTORY]]}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || exit 2
revision=$(git -C "$repo" rev-parse --verify "${2:-HEAD}^{commit}")
epoch=$(git -C "$repo" show -s --format=%ct "$revision")
out=$(realpath -m "${3:-$repo/out/release-$version}")
test ! -e "$out" || { echo "Output already exists: $out" >&2; exit 1; }
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
prefix=magnet-linux-$version
mkdir "$scratch/$prefix"
git -C "$repo" archive "$revision" | tar -xf - -C "$scratch/$prefix"
test -s "$scratch/$prefix/dependencies.json"
test ! -e "$scratch/$prefix/packages"
test ! -e "$scratch/$prefix/magpkg"
printf 'version=%s\nrevision=%s\n' "$version" "$revision" > "$scratch/$prefix/RELEASE"
mkdir -p "$out"
tar --format=gnu --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
  -C "$scratch" -cf - "$prefix" | gzip -n > "$out/$prefix.tar.gz"
(cd "$out"; sha256sum ./*.tar.gz > SHA256SUMS)
printf 'Release assets: %s\n' "$out"
