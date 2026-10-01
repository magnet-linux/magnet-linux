#!/usr/bin/env bash
# Exercise a fresh installation and real Hyprland login using the public creator.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
mkdir -p "$repo/magnet-linux/out"
work=$(mktemp -d "$repo/magnet-linux/out/desktop-test.XXXXXXXX")
chmod 0700 "$work"
vm=$work/vm
"$repo/magnet-linux/create-desktop-vm.sh" "$vm"
SSH_PORT=${SSH_PORT:-22223} "$repo/magnet-linux/tests/session-boot.sh" "$vm"
echo "PASS: fresh Hyprland installation and graphical boot; VM retained at $vm"
