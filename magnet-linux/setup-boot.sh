#!/bin/sh
# Run from the installation host with TARGET/boot/efi mounted.
set -eu
umask 022
if test "$#" -ne 3; then
  echo "usage: $0 TARGET ROOT-FS-UUID ROOT-PARTUUID" >&2
  exit 2
fi
target=$(realpath "$1")
root_uuid=$2
root_partuuid=$3
test "$target" != / || { echo 'use a mounted offline target' >&2; exit 1; }
for uuid in "$root_uuid" "$root_partuuid"; do
  case "$uuid" in ''|*[!a-fA-F0-9-]*) echo 'invalid UUID' >&2; exit 1;; esac
done
test -s "$target/boot/vmlinuz"
mountpoint -q "$target/boot/efi" || { echo 'mount the EFI system partition at TARGET/boot/efi first' >&2; exit 1; }
for path in boot boot/grub boot/efi/EFI boot/efi/EFI/BOOT; do
  test ! -L "$target/$path" || { echo "symlink at $path" >&2; exit 1; }
done
mkdir -p "$target/boot/grub" "$target/boot/efi/EFI/BOOT"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
cat > "$scratch/grub.cfg" <<EOF
search --no-floppy --fs-uuid --set=root $root_uuid
configfile /root/boot/grub/grub.cfg
EOF
cat > "$scratch/system.cfg" <<EOF
serial --unit=0 --speed=115200
terminal_input serial console
terminal_output serial console
set timeout=1
set default=0
menuentry 'Magnet Linux' {
  search --no-floppy --fs-uuid --set=root $root_uuid
  linux /root/boot/vmlinuz root=PARTUUID=$root_partuuid rootfstype=btrfs rootflags=subvol=root rw console=ttyS0,115200n8 panic=10
}
# A manually staged root can be selected at the serial GRUB menu. The default
# remains the original root until the administrator edits set default above.
if [ -f /root-next/boot/vmlinuz ]; then
  menuentry 'Magnet Linux (root-next)' --id next {
    search --no-floppy --fs-uuid --set=root $root_uuid
    linux /root-next/boot/vmlinuz root=PARTUUID=$root_partuuid rootfstype=btrfs rootflags=subvol=root-next rw console=ttyS0,115200n8 panic=10
  }
fi
EOF
grub-mkstandalone -O x86_64-efi --locales='' --fonts='' \
  --modules='part_gpt fat btrfs normal linux search search_fs_uuid serial' \
  -o "$scratch/BOOTX64.EFI" "boot/grub/grub.cfg=$scratch/grub.cfg"
install -m 0644 "$scratch/system.cfg" "$target/boot/grub/grub.cfg.new"
mv -f "$target/boot/grub/grub.cfg.new" "$target/boot/grub/grub.cfg"
install -m 0644 "$scratch/BOOTX64.EFI" "$target/boot/efi/EFI/BOOT/BOOTX64.EFI.new"
mv -f "$target/boot/efi/EFI/BOOT/BOOTX64.EFI.new" "$target/boot/efi/EFI/BOOT/BOOTX64.EFI"
sync
