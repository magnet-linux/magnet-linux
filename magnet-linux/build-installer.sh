#!/usr/bin/env bash
# Build an offline x86-64 UEFI ISO, usable as a CD or USB disk.
set -euo pipefail
umask 022
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
distro=$repo/magnet-linux
output=$(realpath -m "${1:-$distro/out/magnet-linux-installer.iso}")
test "$#" -le 1 || { echo 'usage: build-installer.sh [NEW-ISO]' >&2; exit 2; }
test ! -e "$output" && test ! -L "$output" || { echo "Output exists: $output" >&2; exit 1; }
for tool in python3 cpio gzip grub-mkrescue xorriso sudo bwrap; do command -v "$tool" >/dev/null; done
sudo -n true || { echo 'The image build needs sudo for package ownership and device nodes.' >&2; exit 1; }
python3 "$distro/prepare.py"
magpkg=$(realpath "$(command -v "${MAGPKG:-$distro/_work/magpkg}")")
store=$(realpath -m "${MAGPKG_STORE:-$distro/_work/store}")
seed=$distro/_work/bootstrap.tar.zst
"$magpkg" init --store "$store"
"$magpkg" build --store "$store" --ext-str "bootstrap-url=file://$seed" "$distro/seed-store.jsonnet" seed
read -r -a profiles <<< "${INSTALLER_PROFILES:-base hyprland}"
mkdir -p "$distro/_work" "$(dirname "$output")"
work_base=${INSTALLER_WORKDIR:-$distro/_work}
mkdir -p "$work_base"
work=$(mktemp -d "$work_base/installer-build.XXXXXXXX")
iso=$work/iso live=$work/live
mkdir -p "$iso/boot/grub" "$live"
root_cmd() { sudo -n env "PATH=$PATH" "$@"; }
cleanup() { root_cmd rm -rf -- "$work"; }
trap cleanup EXIT
for profile in "${profiles[@]}"; do
  case "$profile" in
    base) selection=system.jsonnet; selected=system;;
    hyprland) selection=desktop-hyprland.jsonnet; selected=desktop;;
    hyprland-firefox) selection=desktop-hyprland.jsonnet; selected=desktop-browser;;
    hyprland-chromium) selection=desktop-hyprland.jsonnet; selected=desktop-chromium;;
    hyprland-browsers) selection=desktop-hyprland.jsonnet; selected=desktop-browsers;;
    *) echo "Unknown profile: $profile" >&2; exit 2;;
  esac
  "$magpkg" build --store "$store" --jobs 3 --parallelism "${BUILD_PARALLELISM:-8}" "$distro/$selection" "$selected"
  root_cmd "$magpkg" sync --no-build --no-binary-cache --store "$store" \
    --dest "$work/target" "$distro/$selection" "$selected" > "$work/sync.log"
  root_cmd python3 "$distro/installer/bundle.py" profile "$work/target" "$store" "$iso" "$profile"
  root_cmd rm -rf "$work/target"
done
"$magpkg" build --store "$store" --jobs 3 --parallelism "${BUILD_PARALLELISM:-8}" "$distro/installer.jsonnet" live
root_cmd "$magpkg" sync --no-build --no-binary-cache --store "$store" \
  --dest "$live" "$distro/installer.jsonnet" live > "$work/live-sync.log"
root_cmd cp "$live/boot/vmlinuz" "$iso/boot/vmlinuz"
root_cmd rm -rf "$live/.magpkg" "$live/boot"
root_cmd cp -a "$distro/skeleton/." "$live/"
root_cmd find "$live/etc/perp" -type f -name 'rc.*' -exec chmod 0755 '{}' +
root_cmd chmod 0755 "$live/etc/rc.shutdown"
# The ISO has no SSH service or pre-generated machine identity.
root_cmd rm -rf "$live/etc/perp/sshd" "$live/etc/perp/chronyd"
root_cmd install -m 0755 "$distro/installer/live-init.sh" "$live/init"
root_cmd install -m 0755 "$distro/installer/live-rc.sh" "$live/etc/rc.init"
root_cmd install -m 0644 "$distro/installer/profile" "$live/etc/profile"
root_cmd ln -s /run/perp "$live/etc/perp/.control"
root_cmd ln -s /proc/self/mounts "$live/etc/mtab"
root_cmd ln -s /run/media/magnet/source/magnet-linux/magnet-linux.sh "$live/bin/magnet-linux"
root_cmd install -m 0644 "$live/share/e2fsprogs/mke2fs.conf" "$live/etc/mke2fs.conf"
# shellcheck disable=SC2016
root_cmd bash -c 'printf "root::19700:0:99999:7:::\n" > "$1/etc/shadow"
  printf "dhcpcd:x:491:491:DHCP client:/var/empty:/bin/false\n" >> "$1/etc/passwd"
  printf "dhcpcd:x:491:\n" >> "$1/etc/group"
  chmod 0600 "$1/etc/shadow"
  mkdir -p "$1/var/empty" "$1/etc/perp/getty-tty1"
  for service in getty-tty1 getty-ttyS0; do
    tty=${service#getty-}
    printf "#!/bin/sh\ntest \"\$1\" = start || exit 0\nexec agetty --autologin root --noclear %s 115200 vt100\n" "$tty" > "$1/etc/perp/$service/rc.main"
    chmod 0755 "$1/etc/perp/$service/rc.main"
    chmod +t "$1/etc/perp/$service"
  done
  chmod +t "$1/etc/perp/dhcpcd"' bash "$live"
root_cmd mknod -m 0600 "$live/dev/console" c 5 1
root_cmd mknod -m 0666 "$live/dev/null" c 1 3
root_cmd python3 "$distro/installer/bundle.py" source "$repo" "$seed" "$iso"
cp "$distro/INSTALL.txt" "$iso/INSTALL.txt"
printf 'Magnet Linux installation media, format 1\n' > "$iso/MAGNET_INSTALL"
# shellcheck disable=SC2016
root_cmd bash -c 'cd "$1"; find . -print0 | LC_ALL=C sort -z | cpio --null -o -H newc --owner=0:0 --reproducible' bash "$live" |
  gzip -n -1 > "$iso/boot/initramfs.gz"
cat > "$iso/boot/grub/grub.cfg" <<'GRUB'
serial --unit=0 --speed=115200
terminal_input console serial
terminal_output console serial
set timeout=3
set default=0
menuentry 'Magnet Linux installation / recovery' {
  linux /boot/vmlinuz console=tty0 console=ttyS0,115200n8 panic=10
  initrd /boot/initramfs.gz
}
GRUB
temporary=$output.partial
test ! -e "$temporary"
grub-mkrescue -o "$temporary" "$iso" -volid MAGNET_INSTALL
mv "$temporary" "$output"
sha256sum "$output" > "$output.sha256"
echo "Installer image: $output"
