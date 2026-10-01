#!/usr/bin/env bash
# Builds unprivileged; sudo is used only for a fresh image's loop device/mounts.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
distro=$repo/magnet-linux
for tool in python3 bwrap qemu-system-x86_64 btrfs mkfs.btrfs mkfs.vfat \
  sfdisk losetup mount umount mountpoint blkid grub-mkstandalone timeout sudo ssh ssh-keygen scp; do
  command -v "$tool" >/dev/null || { echo "missing host tool: $tool" >&2; exit 1; }
done
: "${OVMF_CODE:?set OVMF_CODE to OVMF_CODE.fd (or use shell.nix)}"
: "${OVMF_VARS:?set OVMF_VARS to OVMF_VARS.fd (or use shell.nix)}"
test -r "$OVMF_CODE" && test -r "$OVMF_VARS"
sudo -n true || { echo 'The image test needs sudo for loop mounts.' >&2; exit 1; }
mkdir -p "$distro/_work" "$distro/out"
store=${MAGPKG_STORE:-$distro/_work/store}
store=$(realpath -m "$store")
python3 "$distro/prepare.py"
magpkg=$(command -v "${MAGPKG:-$distro/_work/magpkg}")
magpkg=$(realpath "$magpkg")
seed=$distro/_work/bootstrap.tar.zst
"$magpkg" init --store "$store"
"$magpkg" build --store "$store" \
  --ext-str "bootstrap-url=file://$seed" \
  "$distro/seed-store.jsonnet" seed
"$magpkg" build --store "$store" --jobs 3 --parallelism "${BUILD_PARALLELISM:-8}" \
  "$distro/system.jsonnet" system

work=$(mktemp -d "$distro/out/vm-test.XXXXXXXX")
target=$work/root
top=$work/top
disk=$work/disk.raw
loop=
qemu_pid=
ssh_port=${SSH_PORT:-22222}
case "$ssh_port" in ''|*[!0-9]*) echo 'SSH_PORT must be an integer' >&2; exit 1;; esac
test "$ssh_port" -ge 1024 && test "$ssh_port" -le 65535
mkdir "$target" "$top"
root_cmd() { sudo -n env "PATH=$PATH" "$@"; }
cleanup() {
  set +e
  if test -n "$qemu_pid"; then
    kill "$qemu_pid" 2>/dev/null
    wait "$qemu_pid" 2>/dev/null
  fi
  for path in "$target/boot/efi" "$target/var" "$target/home" "$target" "$top"; do
    if mountpoint -q "$path"; then root_cmd umount "$path"; fi
  done
  if test -n "$loop"; then root_cmd losetup -d "$loop"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
echo "Test artifacts: $work"
truncate -s 16G "$disk"
sfdisk "$disk" <<'EOF'
label: gpt
size=256M,type=U
type=L
EOF
loop=$(root_cmd losetup --find --show --partscan "$disk")
esp=${loop}p1
system=${loop}p2
root_cmd mkfs.vfat -F 32 "$esp"
# GRUB must be able to read this filesystem. Pin its feature profile rather
# than inheriting changing mkfs defaults (e.g. new extent-tree formats).
root_cmd mkfs.btrfs -f -O ^free-space-tree,^block-group-tree "$system"
root_uuid=$(root_cmd blkid -s UUID -o value "$system")
root_partuuid=$(root_cmd blkid -s PARTUUID -o value "$system")
esp_uuid=$(root_cmd blkid -s UUID -o value "$esp")
root_cmd mount "$system" "$top"
for subvol in root home var; do root_cmd btrfs subvolume create "$top/$subvol"; done
root_cmd mkdir "$top/snapshots"
mount_target() {
  root_cmd mount -o subvol=root "$system" "$target"
  root_cmd mkdir -p "$target/boot/efi"
  root_cmd mount "$esp" "$target/boot/efi"
}
sync_target() {
  root_cmd "$magpkg" sync --store "$store" --no-build --no-binary-cache \
    --dest "$target" "$1" system > "$work/sync-$2.log"
}
unmount_disk() {
  root_cmd umount "$target/boot/efi"
  root_cmd umount "$target"
  root_cmd umount "$top"
  root_cmd losetup -d "$loop"
  loop=
}
remount_disk() {
  loop=$(root_cmd losetup --find --show --partscan "$disk")
  esp=${loop}p1
  system=${loop}p2
  root_cmd mount "$system" "$top"
  mount_target
}
boot() {
  local phase=$1 expected=$2
  cp "$OVMF_VARS" "$work/vars-$phase.fd"
  chmod u+w "$work/vars-$phase.fd"
  local accel=tcg cpu=max
  if test -r /dev/kvm && test -w /dev/kvm; then accel=kvm; cpu=host; fi
  echo "Booting $phase ($accel)"
  timeout "${QEMU_TIMEOUT:-5400}" qemu-system-x86_64 \
    -machine "q35,accel=$accel" -cpu "$cpu" -m "${QEMU_MEMORY:-8192}" -smp 4 \
    -drive "if=pflash,format=raw,readonly=on,file=$OVMF_CODE" \
    -drive "if=pflash,format=raw,file=$work/vars-$phase.fd" \
    -drive "if=virtio,format=raw,file=$disk" \
    -device virtio-rng-pci \
    -nic "user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$ssh_port-:22" \
    -display none -monitor none -serial stdio -no-reboot \
    < /dev/null > "$work/boot-$phase.log" 2>&1 &
  qemu_pid=$!
  local ssh_options=(
    -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes
    -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$work/known_hosts"
    -o GlobalKnownHostsFile=/dev/null -o HostKeyAlgorithms=ssh-ed25519
    -o UpdateHostKeys=no -o ConnectTimeout=2 -o ConnectionAttempts=1
  )
  local connected=false
  for ((attempt=0; attempt<60; attempt++)); do
    if ! kill -0 "$qemu_pid" 2>/dev/null; then break; fi
    if ssh "${ssh_options[@]}" -i "$work/client-key" -p "$ssh_port" \
        alice@127.0.0.1 'test "$(id -u)" = 1000' 2> "$work/ssh-$phase.log"; then
      connected=true
      break
    fi
    sleep 1
  done
  if ! "$connected"; then
    cat "$work/ssh-$phase.log" 2>/dev/null || true
    tail -80 "$work/boot-$phase.log"
    return 1
  fi
  # Unknown keys must fail even though the account itself is usable.
  if ssh "${ssh_options[@]}" -i "$work/wrong-key" -p "$ssh_port" \
      alice@127.0.0.1 true >> "$work/ssh-$phase.log" 2>&1; then
    echo 'sshd accepted an unauthorized key' >&2
    return 1
  fi
  # scp uses SFTP by default and exercises the configured subsystem too.
  scp "${ssh_options[@]}" -i "$work/client-key" -P "$ssh_port" \
    "$work/ssh-payload" alice@127.0.0.1:ssh-payload >> "$work/ssh-$phase.log" 2>&1
  ssh "${ssh_options[@]}" -i "$work/client-key" -p "$ssh_port" alice@127.0.0.1 \
    'test "$(cat ssh-payload)" = magnet-ssh-test && rm ssh-payload && touch ssh-test-complete' \
    >> "$work/ssh-$phase.log" 2>&1
  if ! wait "$qemu_pid"; then
    qemu_pid=
      tail -80 "$work/boot-$phase.log"
      return 1
  fi
  qemu_pid=
  if ! grep -q "^$expected" "$work/boot-$phase.log" || \
      ! grep -q '^MAGNET_TEST_SSH_OK' "$work/boot-$phase.log" || \
      grep -q '^MAGNET_TEST_FAILED' "$work/boot-$phase.log"; then
    tail -80 "$work/boot-$phase.log"
    return 1
  fi
}

mount_target
sync_target "$distro/system.jsonnet" install
root_cmd "$distro/setup-root.sh" "$target" magnet-test "$root_uuid" "$esp_uuid"
root_cmd mount -o subvol=var "$system" "$target/var"
root_cmd "$distro/setup-tree.sh" "$target" "$seed"
echo original | root_cmd tee "$target/etc/magnet-linux/tree/LOCAL-NOTE" >/dev/null
root_cmd "$distro/setup-tree.sh" "$target" "$seed"
test "$(cat "$target/etc/magnet-linux/tree/LOCAL-NOTE")" = original
test ! -d "$target/var/lib/magpkg/store/packages"
root_cmd umount "$target/var"
root_cmd mount -o subvol=home "$system" "$target/home"
root_cmd chroot "$target" /bin/useradd -m -u 1000 -g users -s /bin/bash alice
# Without PAM, OpenSSH rejects locked accounts even with a valid key. Give
# the test account a random, discarded password; password SSH remains off.
test_password=$(head -c 32 /dev/urandom | base64)
printf 'alice:%s\n' "$test_password" | root_cmd chroot "$target" /bin/chpasswd -c SHA512
unset test_password
ssh-keygen -q -t ed25519 -N '' -f "$work/client-key"
ssh-keygen -q -t ed25519 -N '' -f "$work/wrong-key"
root_cmd install -d -m 0700 -o 1000 -g 100 "$target/home/alice/.ssh"
root_cmd install -m 0600 -o 1000 -g 100 "$work/client-key.pub" "$target/home/alice/.ssh/authorized_keys"
printf '[127.0.0.1]:%s %s\n' "$ssh_port" "$(cat "$target/etc/ssh/ssh_host_ed25519_key.pub")" > "$work/known_hosts"
printf 'magnet-ssh-test\n' > "$work/ssh-payload"
host_key=$(cat "$target/etc/ssh/ssh_host_ed25519_key.pub")
root_cmd umount "$target/home"
root_cmd install -m 0755 "$distro/tests/guest-check.sh" "$target/etc/rc.local"
root_cmd install -m 0755 "$distro/tests/guest-supervision.sh" "$target/etc/guest-supervision.sh"
root_cmd install -m 0755 "$distro/tests/guest-selfhost.sh" "$target/etc/guest-selfhost.sh"
if test -n "${GNU_MIRROR:-}"; then
  printf '%s\n' "${GNU_MIRROR%/}/" | root_cmd tee "$target/etc/guest-gnu-mirror" >/dev/null
fi
echo original | root_cmd tee "$target/etc/test-state" >/dev/null
root_cmd "$distro/setup-boot.sh" "$target" "$root_uuid" "$root_partuuid"

# Re-running initialization must preserve administrator content and modes.
root_cmd "$distro/setup-root.sh" "$target" ignored-hostname "$root_uuid" "$esp_uuid"
test "$(cat "$target/etc/hostname")" = magnet-test
test "$(cat "$target/etc/ssh/ssh_host_ed25519_key.pub")" = "$host_key"
root_cmd btrfs subvolume snapshot -r "$target" "$top/snapshots/before-update"
unmount_disk
boot initial MAGNET_TEST_ORIGINAL_OK
# The guest rebuilt the system, synced root-next, and selected it in GRUB.
grep -q '^MAGNET_TEST_SELFHOST_OK' "$work/boot-initial.log"
boot updated MAGNET_TEST_UPDATE_OK

# Offline recovery: retain the failed/new root and clone the chosen snapshot.
remount_disk
root_cmd umount "$target/boot/efi"
root_cmd umount "$target"
root_cmd mv "$top/root" "$top/root-updated"
root_cmd btrfs subvolume snapshot "$top/snapshots/before-update" "$top/root"
mount_target
unmount_disk
boot restored MAGNET_TEST_ORIGINAL_OK

remount_disk
test "$(cat "$top/var/test-boots")" = 3
test "$(cat "$target/etc/test-state")" = original
test "$(cat "$target/etc/magnet-linux/tree/LOCAL-NOTE")" = original
test ! -e "$target/bin/hello"
root_cmd cp "$top/var/log/magpkg-selfhost.log" "$work/magpkg-selfhost.log"
root_cmd chown "$(id -u):$(id -g)" "$work/magpkg-selfhost.log"
unmount_disk
echo "PASS: UEFI install, SSH/SFTP, guest magpkg/system/kernel rebuild, staged update, /etc and tree recovery, persistent /var"
echo "Disk and serial logs retained in $work"
