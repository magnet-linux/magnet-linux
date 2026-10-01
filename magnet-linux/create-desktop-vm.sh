#!/usr/bin/env bash
# Create a persistent VM. Existing destinations are never overwritten.
set -euo pipefail
umask 077
distro=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(dirname "$distro")
if test "$#" -gt 2 || test "${1:-}" = --help; then
  echo "usage: $0 [NEW-VM-DIRECTORY [USERNAME]]"
  echo 'Run inside nix-shell magnet-linux/shell.nix.'
  echo 'Set WITH_FIREFOX=1 to include the optional Firefox browser.'
  echo 'Set WITH_CHROMIUM=1 to include Chromium.'
  echo 'Hyprland and graphical login are included. USERNAME defaults to browser.'
  exit 0
fi
vm=$(realpath -m "${1:-$distro/out/desktop-vm}")
user=${2:-browser}
case "${WITH_FIREFOX:-0}:${WITH_CHROMIUM:-0}" in
  0:0) profile=desktop;;
  1:0) profile=desktop-browser;;
  0:1) profile=desktop-chromium;;
  1:1) profile=desktop-browsers;;
  *) echo 'WITH_FIREFOX and WITH_CHROMIUM must be 0 or 1.' >&2; exit 2;;
esac
# Accept the old opt-in for existing commands; the desktop is now Hyprland-only.
case "${WITH_HYPRLAND:-1}" in
  1) ;;
  *) echo 'Hyprland is the desktop; use system.jsonnet for a headless installation.' >&2; exit 2;;
esac
if test "$user" = root; then
  echo 'The Hyprland desktop needs an unprivileged USERNAME.' >&2
  exit 2
fi
[[ "$user" =~ ^[a-z_][a-z0-9_-]*$ ]] || {
  echo 'Choose a lowercase user name.' >&2; exit 1;
}
selection=$distro/desktop-hyprland.jsonnet
selected_root=$profile
test ! -e "$vm" && test ! -L "$vm" || { echo "Destination already exists: $vm" >&2; exit 1; }
for tool in go bwrap btrfs mkfs.btrfs mkfs.vfat sfdisk losetup mount umount \
  blkid grub-mkstandalone sudo ssh-keygen; do
  command -v "$tool" >/dev/null || { echo "missing host tool: $tool" >&2; exit 1; }
done
: "${OVMF_CODE:?use magnet-linux/shell.nix}"
: "${OVMF_VARS:?use magnet-linux/shell.nix}"
sudo -v
mkdir -p "$distro/_work"
store=$(realpath -m "${MAGPKG_STORE:-$distro/_work/store}")
magpkg=$distro/_work/magpkg
go -C "$repo/magpkg" build -o "$magpkg" ./cmd/magpkg
"$magpkg" init --store "$store"
"$magpkg" build --store "$store" \
  --ext-str "bootstrap-url=file://$repo/bootstrap/out/bootstrap.tar.zst" \
  "$distro/seed-store.jsonnet" seed
# Build outputs must be readable by ordinary guest accounts. Keep the private
# umask for the image's credentials and keys, but do not pass it to builds.
(
  umask 022
  "$magpkg" build --store "$store" --jobs 3 --parallelism "${BUILD_PARALLELISM:-8}" \
    "$selection" "$selected_root"
)

mkdir -p "$(dirname "$vm")"
mkdir "$vm"
target=$vm/root
top=$vm/top
disk=$vm/disk.raw
loop=
root_cmd() { sudo env "PATH=$PATH" "$@"; }
cleanup() {
  set +e
  for path in "$target/boot/efi" "$target/var" "$target/home" "$target" "$top"; do
    if mountpoint -q "$path"; then root_cmd umount "$path"; fi
  done
  if test -n "$loop"; then root_cmd losetup -d "$loop"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir "$target" "$top"
truncate -s 24G "$disk"
sfdisk "$disk" <<'EOF'
label: gpt
size=256M,type=U
type=L
EOF
loop=$(root_cmd losetup --find --show --partscan "$disk")
esp=${loop}p1
system=${loop}p2
root_cmd mkfs.vfat -F 32 "$esp"
root_cmd mkfs.btrfs -f -O ^free-space-tree,^block-group-tree "$system"
root_uuid=$(root_cmd blkid -s UUID -o value "$system")
root_partuuid=$(root_cmd blkid -s PARTUUID -o value "$system")
esp_uuid=$(root_cmd blkid -s UUID -o value "$esp")
root_cmd mount "$system" "$top"
for subvol in root home var; do
  root_cmd btrfs subvolume create "$top/$subvol"
  root_cmd chmod 0755 "$top/$subvol"
done
root_cmd mount -o subvol=root "$system" "$target"
root_cmd mkdir -p "$target/home" "$target/var"
root_cmd chmod 0755 "$target/home" "$target/var"
root_cmd mount -o subvol=home "$system" "$target/home"
root_cmd mount -o subvol=var "$system" "$target/var"
root_cmd "$magpkg" sync --store "$store" --no-build --no-binary-cache \
  --dest "$target" "$selection" "$selected_root" > "$vm/install.log"
root_cmd "$distro/setup-root.sh" "$target" magnet-desktop "$root_uuid" "$esp_uuid"
root_cmd mount "$esp" "$target/boot/efi"
root_cmd "$distro/setup-tree.sh" "$target"
guest_home=/home/$user guest_uid=1000 guest_gid=100
root_cmd chroot "$target" /bin/useradd -m -u 1000 -g users -s /bin/bash "$user"
password=$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
root_password=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
printf '%s:%s\nroot:%s\n' "$user" "$password" "$root_password" |
  root_cmd chroot "$target" /bin/chpasswd -c SHA512
printf 'User: %s\nPassword: %s\nRoot password: %s\n' "$user" "$password" "$root_password" > "$vm/credentials.txt"
unset password root_password
printf '%s\n' "$user" > "$vm/login-user"
ssh-keygen -q -t ed25519 -N '' -C magnet-desktop-vm -f "$vm/client-key"
root_cmd install -d -m 0700 -o "$guest_uid" -g "$guest_gid" "$target$guest_home/.ssh"
root_cmd install -m 0600 -o "$guest_uid" -g "$guest_gid" "$vm/client-key.pub" "$target$guest_home/.ssh/authorized_keys"
host_key=$(root_cmd cat "$target/etc/ssh/ssh_host_ed25519_key.pub")
printf 'magnet-desktop-vm %s\n' "$host_key" > "$vm/known_hosts"
root_cmd "$distro/setup-login.sh" "$target" "$user"
printf "{ system: (import './tree/magnet-linux/desktop-hyprland.jsonnet')['%s'] }\n" "$profile" |
  root_cmd tee "$target/etc/magnet-linux/system.jsonnet" >/dev/null
root_cmd chmod +t "$target/etc/perp/sshd"
root_cmd tee "$target/etc/motd" >/dev/null <<'EOF'
Welcome to Magnet Linux.
Log in to Hyprland on tty1's login screen. Alt+Enter opens a terminal.
From a local text console: magnet-session hyprland.
Use doas poweroff to shut down. Use passwd to change your password.
EOF
root_cmd chmod 0644 "$target/etc/motd"
root_cmd "$distro/setup-boot.sh" "$target" "$root_uuid" "$root_partuuid"
for path in "$target/boot/efi" "$target/var" "$target/home" "$target" "$top"; do root_cmd umount "$path"; done
root_cmd losetup -d "$loop"
loop=
rmdir "$target" "$top"
# Preserve the matching firmware pair across host nixpkgs upgrades.
cp "$OVMF_CODE" "$vm/code.fd"
cp "$OVMF_VARS" "$vm/vars.fd"
chmod 0600 "$vm/code.fd" "$vm/vars.fd"
touch "$vm/ready"
printf 'virgl\n' > "$vm/graphics"
touch "$vm/graphical-login"
echo "VM installed in $vm"
echo "Private login details: $vm/credentials.txt"
echo "Launch with: $distro/run-vm.sh $vm"
