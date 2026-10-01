#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Launch a persistent desktop VM, or connect to its SSH server.
set -euo pipefail
umask 077
distro=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
mode=${MAGNET_VM_MODE:-desktop}
case "${1:-}" in
  --headless) mode=headless; shift;;
  --ssh) mode=ssh; shift;;
  --help)
    echo "usage: $0 [--headless|--ssh] [VM-DIRECTORY]"
    echo 'Defaults: out/desktop-vm, 4 GiB RAM, 4 CPUs, SSH on localhost:22222.'
    echo 'Overrides: QEMU_MEMORY, QEMU_CPUS, SSH_PORT, QEMU_GRAPHICS (software or virgl).'
    echo 'QEMU_DISPLAY overrides the window backend (gtk or sdl); virgl defaults to sdl.'
    echo 'QEMU_AUDIO selects auto, pipewire, pa, alsa, sdl, none or wav (with QEMU_AUDIO_FILE).'
    exit 0;;
esac
test "$#" -le 1 || { echo 'Too many arguments; see --help.' >&2; exit 2; }
vm=$(realpath -m "${1:-${MAGNET_VM_DIR:-$distro/out/desktop-vm}}")
if ! test -f "$vm/ready"; then
  echo "No completed VM at $vm." >&2
  echo 'Relative VM paths are resolved from your current directory.' >&2
  found=false
  for marker in "$distro"/out/*/ready; do
    test -f "$marker" || continue
    if ! "$found"; then echo 'Available completed VMs:' >&2; found=true; fi
    printf '  %s\n' "${marker%/ready}" >&2
  done
  if ! "$found"; then echo 'Run create-desktop-vm.sh first.' >&2; fi
  exit 1
fi
if ! command -v qemu-system-x86_64 >/dev/null || ! command -v ssh >/dev/null || ! command -v flock >/dev/null; then
  test "${MAGNET_VM_NIX:-0}" != 1 || { echo 'Required host tools are missing.' >&2; exit 1; }
  command -v nix-shell >/dev/null || { echo 'Enter the host shell described in DESKTOP.md.' >&2; exit 1; }
  export MAGNET_VM_DIR=$vm MAGNET_VM_MODE=$mode MAGNET_VM_NIX=1
  export MAGNET_VM_LAUNCHER=$distro/run-vm.sh
  # Expand the exported path inside the Nix shell, preserving spaces.
  # shellcheck disable=SC2016
  exec nix-shell "$distro/shell.nix" --run 'exec "$MAGNET_VM_LAUNCHER"'
fi
port=${SSH_PORT:-22222}
[[ "$port" =~ ^[0-9]+$ ]] && test "$port" -ge 1024 && test "$port" -le 65535 || {
  echo 'SSH_PORT must be between 1024 and 65535.' >&2; exit 1;
}
if test "$mode" = ssh; then
  exec ssh -F /dev/null -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
    -o HostKeyAlias=magnet-desktop-vm -o "UserKnownHostsFile=$vm/known_hosts" \
    -o GlobalKnownHostsFile=/dev/null -i "$vm/client-key" -p "$port" \
    "$(cat "$vm/login-user")@127.0.0.1"
fi
accel=tcg cpu=max
if test -r /dev/kvm && test -w /dev/kvm; then accel=kvm; cpu=host; fi
graphics=${QEMU_GRAPHICS:-$(cat "$vm/graphics" 2>/dev/null || echo software)}
gpu=virtio-vga,xres=1280,yres=768
backend=${QEMU_DISPLAY:-gtk}
gl=off
case "$graphics" in
  software) ;;
  virgl) gpu=virtio-vga-gl,xres=1280,yres=768
    # GTK's GL output can stay black with NVIDIA on X11 even when the guest
    # is rendering correctly. SDL works with the same accelerated guest GPU.
    backend=${QEMU_DISPLAY:-sdl}
    gl=on;;
  *) echo 'QEMU_GRAPHICS must be software or virgl.' >&2; exit 2;;
esac
case "$backend" in gtk|sdl) ;; *) echo 'QEMU_DISPLAY must be gtk or sdl.' >&2; exit 2;; esac
display=(-display "$backend,gl=$gl" -serial "file:$vm/serial.log")
if test "$mode" = headless; then
  display=(-display none -serial stdio)
  # Keep console-only boots independent of host OpenGL support.
  gpu=virtio-vga,xres=1280,yres=768
fi
# shellcheck source=qemu-audio.sh
source "$distro/qemu-audio.sh"
echo "Starting $vm with $accel. Changes are saved to disk."
echo "Audio backend: $audio_backend"
if test -f "$vm/graphical-login"; then
  if test "$mode" = headless; then
    echo 'Console-only boot; use a graphical launch for Hyprland.'
  else
    echo 'Log in to Hyprland on the login screen.'
  fi
  echo "Login details: $vm/credentials.txt"
else
  echo "Login details: $vm/credentials.txt"
  echo 'This older image uses its installed console/desktop configuration.'
fi
exec flock -n "$vm/run.lock" qemu-system-x86_64 -name magnet-linux-desktop \
  -pidfile "$vm/qemu.pid" \
  -machine "q35,accel=$accel" -cpu "$cpu" -m "${QEMU_MEMORY:-4096}" -smp "${QEMU_CPUS:-4}" \
  -drive "if=pflash,format=raw,readonly=on,file=$vm/code.fd" \
  -drive "if=pflash,format=raw,file=$vm/vars.fd" \
  -drive "if=virtio,format=raw,file=$vm/disk.raw" \
  -device virtio-rng-pci -vga none -device "$gpu" \
  -device virtio-keyboard-pci -device virtio-tablet-pci \
  -nic "user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$port-:22" \
  -monitor none "${display[@]}" "${qemu_audio[@]}"
