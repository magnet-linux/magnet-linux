#!/bin/sh
# Runs on the first boot of the disposable test disk, never on the host.
set -eux
export MAGPKG_STORE=/var/lib/magpkg/store
tree=/etc/magnet-linux/tree
test -f /var/lib/magpkg/bootstrap.tar.zst
test ! -d "$MAGPKG_STORE/packages"
(cd "$tree"; sha256sum -c FILES.sha256)
magpkg --help
bwrap --version
for option in USER_NS UTS_NS PID_NS IPC_NS NET_NS CGROUPS; do
  grep -qx "CONFIG_$option=y" /boot/kernel.config
done

# Nothing from the host's store or toolchain is installed. Fetch through the
# guest's HTTPS trust store, then rebuild magpkg and all its build dependencies.
if test -s /etc/guest-gnu-mirror; then
  magpkg build --no-binary-cache --ext-str "gnu-mirror=$(cat /etc/guest-gnu-mirror)" \
    "$tree/magnet-linux/gnu-source-mirror.jsonnet" seed
fi
magpkg build --no-binary-cache --parallelism 4 "$tree/packages/magpkg.jsonnet" magpkg
magpkg sync --no-build --no-binary-cache --dest /var/tmp/magpkg-rebuilt \
  "$tree/packages/magpkg.jsonnet" magpkg
cmp /bin/magpkg /var/tmp/magpkg-rebuilt/bin/magpkg
/var/tmp/magpkg-rebuilt/bin/magpkg --help
echo MAGNET_TEST_MAGPKG_REBUILT

# Prove the installed recipes can rebuild the whole selected system (including
# its kernel), then install an edited selection into an inactive root snapshot.
mkdir -p /run/magnet-top
root_uuid=$(awk '$2 == "/var" {print $1}' /etc/fstab)
mount -t btrfs -o subvolid=5 "$root_uuid" /run/magnet-top
btrfs subvolume snapshot / /run/magnet-top/root-next
next=/run/magnet-top/root-next
cat > "$next/etc/magnet-linux/system.jsonnet" <<'EOF'
local base = import './tree/magnet-linux/system.jsonnet';
local hello = (import './tree/packages/hello.jsonnet').hello;
{ system: base.system + { runDeps+: [hello] } }
EOF
echo updated > "$next/etc/test-state"
echo updated > "$next/etc/magnet-linux/tree/LOCAL-NOTE"
magpkg build --no-binary-cache --jobs 2 --parallelism 4 "$next/etc/magnet-linux/system.jsonnet" system
magpkg sync --no-build --no-binary-cache --dest "$next" "$next/etc/magnet-linux/system.jsonnet" system
test "$("$next/bin/hello")" = 'hello world'
test "$(cat "$next/etc/test-state")" = updated
test "$(cat "$next/etc/magnet-linux/tree/LOCAL-NOTE")" = updated
cmp /boot/vmlinuz "$next/boot/vmlinuz"
echo MAGNET_TEST_SYSTEM_REBUILT
# The EFI image reads this original root's GRUB config; select the staged root
# for the next boot. A snapshot restore recovers the original default too.
sed -i 's/^set default=0$/set default=next/' /boot/grub/grub.cfg
sync
umount /run/magnet-top
touch /var/selfhost-complete
echo MAGNET_TEST_SELFHOST_OK
