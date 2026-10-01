#!/usr/bin/env bash
# Configure UEFI boot for the actual mounted root and optional separate /boot.
set -euo pipefail
distro=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR
source "$distro/mount-layout.sh"
if test "$#" != 3; then
  echo 'usage: setup-boot.sh TARGET ROOT-FS-UUID ROOT-PARTUUID' >&2; exit 2
fi
expected_uuid=$2 expected_partuuid=$3
read_layout "$1"
test "$root_uuid" = "$expected_uuid" && test "$root_partuuid" = "$expected_partuuid"
test -s "$target/boot/vmlinuz"
for path in boot/grub boot/efi/EFI boot/efi/EFI/BOOT; do
  test ! -L "$target/$path"
done
boot_mount=$(findmnt -nr -o TARGET -T "$target/boot")
boot_uuid=$(mount_value "$boot_mount" UUID)
valid_uuid "$boot_uuid"
boot_fsroot=$(mount_value "$boot_mount" FSROOT)
[[ "$boot_fsroot" =~ ^/[a-zA-Z0-9_@./-]*$ && "$boot_fsroot" != *..* ]]
case "$(mount_value "$boot_mount" FSTYPE)" in ext4|btrfs) ;; *) echo 'Unsupported /boot filesystem.' >&2; exit 1;; esac
boot_path="${boot_fsroot%/}${target#"$boot_mount"}/boot"
# When /boot itself is a mountpoint, the path inside it is its filesystem root.
if test "$boot_mount" = "$target/boot"; then boot_path=${boot_fsroot%/}; fi
rootflags=
if test "$root_type" = btrfs; then
  rootflags="rootflags=subvol=${root_fsroot#/}"
  test "$root_fsroot" != / || rootflags=rootflags=subvolid=5
fi
mkdir -p "$target/boot/grub" "$esp/EFI/BOOT"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cat > "$scratch/grub.cfg" <<EOF
search --no-floppy --fs-uuid --set=root $boot_uuid
configfile $boot_path/grub/grub.cfg
EOF
cat > "$scratch/system.cfg" <<EOF
serial --unit=0 --speed=115200
terminal_input serial console
terminal_output serial console
set timeout=3
set default=0
menuentry 'Magnet Linux' {
  search --no-floppy --fs-uuid --set=root $boot_uuid
  linux $boot_path/vmlinuz root=PARTUUID=$root_partuuid rootfstype=$root_type $rootflags rw console=tty0 console=ttyS0,115200n8 panic=10
}
EOF
if test "$root_type" = btrfs && test "$root_fsroot" = /root && test "$boot_mount" = "$target"; then
  cat >> "$scratch/system.cfg" <<EOF
if [ -f /root-next/boot/vmlinuz ]; then
  menuentry 'Magnet Linux (root-next)' --id next {
    search --no-floppy --fs-uuid --set=root $root_uuid
    linux /root-next/boot/vmlinuz root=PARTUUID=$root_partuuid rootfstype=btrfs rootflags=subvol=root-next rw console=tty0 console=ttyS0,115200n8 panic=10
  }
fi
EOF
fi
grub-mkstandalone -O x86_64-efi --locales='' --fonts='' \
  --modules='part_gpt fat ext2 btrfs normal linux search search_fs_uuid serial' \
  -o "$scratch/BOOTX64.EFI" "boot/grub/grub.cfg=$scratch/grub.cfg"
install -m 0644 "$scratch/system.cfg" "$target/boot/grub/grub.cfg.new"
mv -f "$target/boot/grub/grub.cfg.new" "$target/boot/grub/grub.cfg"
install -m 0644 "$scratch/BOOTX64.EFI" "$esp/EFI/BOOT/BOOTX64.EFI.new"
mv -f "$esp/EFI/BOOT/BOOTX64.EFI.new" "$esp/EFI/BOOT/BOOTX64.EFI"
sync
