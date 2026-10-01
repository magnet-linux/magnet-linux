#!/usr/bin/env bash
# Install into an explicitly prepared target. This command never formats disks.
set -euo pipefail
umask 022
distro=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR
source "$distro/mount-layout.sh"
usage() {
  cat <<'HELP'
usage: magnet-linux install --root MOUNTPOINT [options]
Mount an ext4 or Btrfs root, optional /home and /var, and a FAT EFI partition
at MOUNTPOINT/boot/efi first. Partitioning and formatting are manual.

  --profile base|hyprland|hyprland-firefox|hyprland-chromium|hyprland-browsers
  --hostname NAME          --user NAME
  --password-file FILE     Administrator password (one line, private file)
  --root-password-file FILE  Optional root password; otherwise root stays locked
  --ssh-key FILE           Install an administrator's public SSH key
  --enable-ssh             Enable the SSH server (public-key authentication)
  --yes                    Noninteractive; requires profile, hostname, user,
                           and password-file
  --media PATH             Installation media (default /run/media/magnet)
  --source PATH --store PATH --bootstrap FILE
                           Use an already-built source tree/store on a host
HELP
}
root='' profile='' hostname='' user='' password_file='' root_password_file='' ssh_key=''
media=/run/media/magnet source_tree='' store='' seed='' noninteractive=false enable_ssh=false
while test "$#" -gt 0; do
  case "$1" in
    --help|-h) usage; exit 0;;
    --yes) noninteractive=true; shift;;
    --enable-ssh) enable_ssh=true; shift;;
    --root|--profile|--hostname|--user|--password-file|--root-password-file|--ssh-key|--media|--source|--store|--bootstrap)
      test "$#" -ge 2 || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in
        --root) root=$2;; --profile) profile=$2;; --hostname) hostname=$2;;
        --user) user=$2;; --password-file) password_file=$2;;
        --root-password-file) root_password_file=$2;; --ssh-key) ssh_key=$2;;
        --media) media=$2;; --source) source_tree=$2;; --store) store=$2;; --bootstrap) seed=$2;;
      esac
      shift 2;;
    *) echo "Unknown option: $1" >&2; exit 2;;
  esac
done
test "$(id -u)" = 0 || { echo 'Run the installer as root.' >&2; exit 1; }
test -n "$root" || { usage >&2; exit 2; }
read_layout "$root"
for path in etc/passwd .magpkg; do
  if test -e "$target/$path" || test -L "$target/$path"; then
    layout_error 'The target already contains a system. Use magpkg for updates.'; exit 1
  fi
done
for path in var/lib var/lib/magpkg var/lib/magpkg/store var/log; do
  test ! -L "$target/$path" || { layout_error "Symlink at target/$path."; exit 1; }
done
if "$noninteractive"; then
  test -n "$profile" && test -n "$hostname" && test -n "$user" && test -n "$password_file" ||
    { echo '--yes requires profile, hostname, user, and password-file.' >&2; exit 2; }
else
  test -n "$profile" || { read -r -p 'System profile? [base] ' profile; profile=${profile:-base}; }
  test -n "$hostname" || { read -r -p 'Hostname? [magnet] ' hostname; hostname=${hostname:-magnet}; }
  test -n "$user" || read -r -p 'Administrator username? ' user
fi
[[ "$hostname" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*$ ]] || { echo 'Invalid hostname.' >&2; exit 2; }
[[ "$user" =~ ^[a-z_][a-z0-9_-]*$ && "$user" != root && "$user" != nobody ]] ||
  { echo 'Choose an ordinary lowercase user name.' >&2; exit 2; }
case "$user" in
  sshd|dhcpcd|chrony|greeter|messagebus|polkitd)
    echo 'That name is reserved for a system account.' >&2; exit 2;;
esac
case "$profile" in
  base) selection=system.jsonnet; selected=system;;
  hyprland) selection=desktop-hyprland.jsonnet; selected=desktop;;
  hyprland-firefox) selection=desktop-hyprland.jsonnet; selected=desktop-browser;;
  hyprland-chromium) selection=desktop-hyprland.jsonnet; selected=desktop-chromium;;
  hyprland-browsers) selection=desktop-hyprland.jsonnet; selected=desktop-browsers;;
  *) echo "Unknown profile: $profile" >&2; exit 2;;
esac
read_password() {
  local file=$1 label=$2 first second mode
  if test -n "$file"; then
    test -f "$file" && test ! -L "$file" || { echo 'Password file must be a regular file.' >&2; return 1; }
    mode=$(stat -c %a "$file")
    test "$(( 8#$mode & 077 ))" = 0 ||
      { echo 'Password files must not be readable by group or others.' >&2; return 1; }
    first=$(cat "$file")
  else
    read -r -s -p "$label password: " first; echo >&2
    read -r -s -p 'Retype password: ' second; echo >&2
    test "$first" = "$second" || { echo 'Passwords do not match.' >&2; return 1; }
  fi
  [[ -n "$first" && "$first" != *:* && "$first" != *$'\n'* && "$first" != *$'\r'* ]] ||
    { echo 'Password must be nonempty and contain no colons or line breaks.' >&2; return 1; }
  password=$first
}
read_password "$password_file" Administrator
administrator_password=$password
root_password=
if test -n "$root_password_file"; then read_password "$root_password_file" Root; root_password=$password; fi
unset password
if test -n "$ssh_key"; then
  ssh-keygen -l -f "$ssh_key" >/dev/null || { echo 'Invalid SSH public key.' >&2; exit 2; }
fi
magpkg=${MAGPKG:-magpkg}
if test -z "$source_tree"; then
  test -f "$media/MAGNET_INSTALL" && test -f "$media/profiles/$profile.sha256" ||
    { echo "Profile $profile is not included on this installation media." >&2; exit 1; }
  echo 'Verifying installation media...'
  (cd "$media"; sha256sum -c source.sha256; sha256sum -c "profiles/$profile.sha256") >/dev/null
  source_tree=$media/source
  seed=$media/bootstrap.tar.zst
  store=$target/var/lib/magpkg/store
  media_install=true
else
  source_tree=$(realpath -e "$source_tree")
  test -n "$store" && test -n "$seed" ||
    { echo 'Host installation requires --store and --bootstrap.' >&2; exit 2; }
  store=$(realpath -e "$store")
  media_install=false
fi
test -f "$source_tree/magnet-linux/$selection" && test -f "$seed"
echo "Install $profile into $target ($root_type), hostname $hostname, administrator $user."
echo 'The installer will populate these mounts:'
findmnt -R --mountpoint "$target" -o TARGET,SOURCE,FSTYPE
if ! "$noninteractive"; then
  read -r -p 'Proceed with installation? [no] ' answer
  case "$answer" in yes|y) ;; *) echo 'Installation cancelled.'; exit 1;; esac
fi
# Validate every mount before making the first change.
fstab=$(mktemp)
trap 'rm -f "$fstab"; unset administrator_password root_password' EXIT
write_fstab > "$fstab"
mkdir -p "$target/var/log" "$target/etc"
echo 'Installing packages...'
if "$media_install"; then
  "$magpkg" init --store "$store"
  mkdir -p "$store/packages"
  while read -r _digest filename; do
    [[ "$filename" =~ ^packages/[a-zA-Z0-9_.+-]+\.tar\.zst$ ]] ||
      { echo 'Malformed package manifest.' >&2; exit 1; }
    install -m 0644 "$media/$filename" "$store/packages/${filename##*/}"
  done < "$media/profiles/$profile.sha256"
fi
"$magpkg" sync --no-build --no-binary-cache --store "$store" --dest "$target" \
  "$source_tree/magnet-linux/$selection" "$selected" > "$target/var/log/magnet-install.log"
echo 'Setting up configuration and accounts...'
install -m 0644 "$fstab" "$target/etc/fstab"
"$source_tree/magnet-linux/setup-root.sh" "$target" "$hostname" "$root_uuid" "$esp_uuid"
"$source_tree/magnet-linux/setup-tree.sh" "$target" "$seed"
chroot "$target" /bin/useradd -m -u 1000 -g users -s /bin/bash "$user"
printf '%s:%s\n' "$user" "$administrator_password" | chroot "$target" /bin/chpasswd -c SHA512
unset administrator_password
if test -n "$root_password"; then
  printf 'root:%s\n' "$root_password" | chroot "$target" /bin/chpasswd -c SHA512
fi
unset root_password
"$source_tree/magnet-linux/setup-doas.sh" "$target" "$user"
if test "$profile" = base; then
  chmod +t "$target/etc/perp/dhcpcd"
else
  "$source_tree/magnet-linux/setup-login.sh" "$target" "$user"
fi
printf "{ system: (import './tree/magnet-linux/%s')['%s'] }\n" "$selection" "$selected" \
  > "$target/etc/magnet-linux/system.jsonnet"
if test -n "$ssh_key"; then
  install -d -m 0700 -o 1000 -g 100 "$target/home/$user/.ssh"
  install -m 0600 -o 1000 -g 100 "$ssh_key" "$target/home/$user/.ssh/authorized_keys"
fi
if "$enable_ssh"; then chmod +t "$target/etc/perp/sshd"; fi
echo 'Installing the UEFI bootloader...'
"$source_tree/magnet-linux/setup-boot.sh" "$target" "$root_uuid" "$root_partuuid"
printf 'profile=%s\nhostname=%s\nfilesystem=%s\n' "$profile" "$hostname" "$root_type" \
  > "$target/etc/magnet-linux/installation"
sync
echo 'Installation complete. Unmount the target, remove the installation media, and reboot.'
