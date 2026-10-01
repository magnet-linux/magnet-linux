#!/bin/sh
# Installed as /etc/rc.local only in the disposable test image.
set -eu
# Let sysinit finish so the test also observes a running serial getty.
if test "${1-}" != --check; then
  perpctl -b /etc/perp A dhcpcd sshd
  for _ in $(seq 1 60); do
    if ip -4 route show default | grep -q default &&
       grep -q '^nameserver ' /etc/resolv.conf && test -f /var/log/sshd.log; then break; fi
    sleep 1
  done
  ip -4 route show default | grep -q default
  grep -q '^nameserver ' /etc/resolv.conf
  test -f /var/log/sshd.log
  tail -f /var/log/sshd.log > /dev/console &
  "$0" --check &
  exit 0
fi
set -x
trap 'echo MAGNET_TEST_FAILED; poweroff' EXIT

for _ in 1 2 3 4 5 6 7 8 9 10; do
  if pidof agetty >/dev/null; then break; fi
  sleep 1
done
pidof agetty >/dev/null

test "$(cat /proc/1/comm)" = init
test "$(readlink /proc/1/exe)" = /bin/sinit
test ! -e /bin/busybox
test "$(readlink /bin/sh)" = bash
test ! -e /bin/oksh && test ! -e /bin/ksh
bash -c 'test -n "$BASH_VERSION"; values=(one two); test "${values[1]}" = two'
MANPAGER='cat' man bash | sed 's/.\x08//g' > /tmp/bash-man.txt
grep -q SYNOPSIS /tmp/bash-man.txt
MANPAGER='cat' man 5 doas.conf | sed 's/.\x08//g' > /tmp/doas-man.txt
grep -q DESCRIPTION /tmp/doas-man.txt
less --version
test "$(readlink -f /var/run)" = /run
test "$(readlink -f /var/lock)" = /run/lock
cp --version | grep -q 'GNU coreutils'
mount --version | grep -q util-linux
perpok -b /etc/perp -u 1 getty-ttyS0
perpok -b /etc/perp -u 1 sshd
/etc/guest-supervision.sh
grep -q ' / btrfs ' /proc/mounts
test "$(stat -c %a /etc/shadow)" = 600
test "$(stat -c %a /root)" = 700
test "$(stat -c %a /tmp)" = 1777
test "$(id -u alice)" = 1000
grep -q '^alice:.*:/bin/bash$' /etc/passwd
grep -q '^root:.*:/bin/bash$' /etc/passwd
test "$(stat -c %u /home/alice)" = 1000
grep -q '^root:!:' /etc/shadow
grep -q ' /home btrfs ' /proc/mounts
grep -q ' /var btrfs ' /proc/mounts
grep -q ' /boot/efi vfat ' /proc/mounts
test -c /dev/console
test -c /dev/ttyS0
test "$(cat /etc/hostname)" = magnet-test
btrfs subvolume show / >/dev/null
test "$(id -u sshd)" != 0
test "$(stat -c %a /etc/ssh/ssh_host_ed25519_key)" = 600
test "$(stat -c %u /run/sshd)" = 0
sshd -T > /run/sshd-config
grep -qi '^PasswordAuthentication no$' /run/sshd-config
grep -qi '^KbdInteractiveAuthentication no$' /run/sshd-config
grep -qi '^PermitRootLogin no$' /run/sshd-config
grep -qi '^AuthenticationMethods publickey$' /run/sshd-config
ssh -V
test -x /bin/magpkg
test -x /bin/bwrap
test -f /etc/magnet-linux/tree/packages/magpkg.jsonnet
test -f /etc/magnet-linux/system.jsonnet
test -s /etc/ssl/certs/ca-certificates.crt
# Login shells must find the persistent system store too.
test "$(/bin/sh -lc 'echo "$MAGPKG_STORE"')" = /var/lib/magpkg/store

if ! test -f /var/selfhost-complete; then
  if ! /etc/guest-selfhost.sh > /var/log/magpkg-selfhost.log 2>&1; then
    tail -100 /var/log/magpkg-selfhost.log
    exit 1
  fi
  grep '^MAGNET_TEST_' /var/log/magpkg-selfhost.log
fi

case "$(cat /etc/test-state)" in
  original)
    test ! -e /bin/hello
    test "$(cat /etc/magnet-linux/tree/LOCAL-NOTE)" = original
    echo MAGNET_TEST_ORIGINAL_OK
    ;;
  updated)
    test "$(/bin/hello)" = 'hello world'
    test "$(cat /etc/magnet-linux/tree/LOCAL-NOTE)" = updated
    grep -q 'hello' /etc/magnet-linux/system.jsonnet
    echo MAGNET_TEST_UPDATE_OK
    ;;
  *) exit 1;;
esac
if test -e /var/test-boots; then
  count=$(cat /var/test-boots)
else
  count=0
fi
echo "$((count + 1))" > /var/test-boots
sync
# Let the host test exercise a real authenticated session before shutdown.
for _ in $(seq 1 120); do
  if test -f /home/alice/ssh-test-complete; then break; fi
  sleep 1
done
test -f /home/alice/ssh-test-complete
rm /home/alice/ssh-test-complete
echo MAGNET_TEST_SSH_OK
trap - EXIT
if test "$(cat /etc/test-state)" = updated; then
  # QEMU's -no-reboot turns this into an exit for the host's next phase.
  reboot
else
  poweroff
fi
