#!/usr/bin/env bash
# Shared validation for installation into explicitly mounted filesystems.
layout_error() { echo "magnet-linux: $*" >&2; return 1; }
mount_value() { findmnt -n -r -o "$2" --mountpoint "$1"; }
valid_uuid() { [[ "$1" =~ ^[a-fA-F0-9-]+$ ]]; }
read_layout() {
  target=$(realpath -e "$1")
  [[ "$target" != / && "$target" != *[[:space:]]* ]] ||
    { layout_error 'Choose a mounted target other than /, without whitespace in its path.'; return 1; }
  mountpoint -q "$target" || { layout_error "$target is not a mountpoint."; return 1; }
  root_type=$(mount_value "$target" FSTYPE)
  case "$root_type" in ext4|btrfs) ;; *) layout_error "Unsupported root filesystem: $root_type"; return 1;; esac
  root_uuid=$(mount_value "$target" UUID)
  valid_uuid "$root_uuid" || { layout_error 'The root filesystem has no usable UUID.'; return 1; }
  root_fsroot=$(mount_value "$target" FSROOT)
  [[ "$root_fsroot" =~ ^/[a-zA-Z0-9_@./-]*$ && "$root_fsroot" != *..* ]] ||
    { layout_error 'Unsupported root subvolume path.'; return 1; }
  root_device=$(mount_value "$target" SOURCE)
  root_device=${root_device%%\[*}
  test -b "$root_device" || { layout_error 'Root must be backed by a block device.'; return 1; }
  root_partuuid=$(blkid -s PARTUUID -o value "$root_device")
  valid_uuid "$root_partuuid" || { layout_error 'Root needs a partition UUID.'; return 1; }
  case ",$(mount_value "$target" OPTIONS)," in
    *,rw,*) ;; *) layout_error 'Root is mounted read-only.'; return 1;;
  esac
  if test "$(mount_value "$target" MAJ:MIN)" = "$(mount_value / MAJ:MIN)" &&
     test "$root_fsroot" = "$(mount_value / FSROOT)"; then
    layout_error 'The target is the running root filesystem.'; return 1
  fi
  for name in etc home var boot boot/efi; do
    test ! -L "$target/$name" || { layout_error "Symlink at target/$name."; return 1; }
  done
  esp=$target/boot/efi
  mountpoint -q "$esp" || { layout_error 'Mount the EFI system partition at TARGET/boot/efi.'; return 1; }
  test "$(mount_value "$esp" FSTYPE)" = vfat ||
    { layout_error 'The EFI system partition must be FAT.'; return 1; }
  esp_uuid=$(mount_value "$esp" UUID)
  valid_uuid "$esp_uuid" || { layout_error 'The EFI partition has no usable UUID.'; return 1; }
  case ",$(mount_value "$esp" OPTIONS)," in
    *,rw,*) ;; *) layout_error 'The EFI partition is mounted read-only.'; return 1;;
  esac
}
write_fstab() {
  local path relative type uuid fsroot options pass
  printf '# Generated once from the mounted installation target; edit normally.\n'
  while IFS= read -r path; do
    [[ "$path" == "$target" || "$path" == "$target/"* ]] || return 1
    [[ "$path" =~ ^/[a-zA-Z0-9_./-]+$ ]] ||
      { layout_error "Unsupported mount path: $path"; return 1; }
    relative=${path#"$target"}
    relative=${relative:-/}
    type=$(mount_value "$path" FSTYPE)
    uuid=$(mount_value "$path" UUID)
    valid_uuid "$uuid" || { layout_error "Missing UUID for $path"; return 1; }
    pass=0
    case "$type" in
      ext4) options=defaults; pass=2; test "$relative" != / || pass=1;;
      btrfs)
        fsroot=$(mount_value "$path" FSROOT)
        [[ "$fsroot" =~ ^/[a-zA-Z0-9_@./-]*$ && "$fsroot" != *..* ]] || return 1
        options="defaults,subvol=${fsroot#/}"
        test "$fsroot" != / || options=defaults
        ;;
      vfat) options=umask=0077;;
      *) layout_error "Unsupported filesystem at $relative: $type"; return 1;;
    esac
    printf 'UUID=%s %s %s %s 0 %s\n' "$uuid" "$relative" "$type" "$options" "$pass"
  done < <(findmnt -Rrn -o TARGET --mountpoint "$target")
}
