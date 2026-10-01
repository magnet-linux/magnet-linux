#!/bin/bash
set -eu
export PATH=/bin
mount -t proc proc /proc
ln -sfn /proc/self/fd /dev/fd
ln -sfn fd/0 /dev/stdin
ln -sfn fd/1 /dev/stdout
ln -sfn fd/2 /dev/stderr
mount -t sysfs sysfs /sys
mount -t tmpfs -o mode=0755,nosuid,nodev tmpfs /run
mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs /tmp
mkdir -p /dev/pts /dev/shm /run/perp /var/log /run/media/magnet
mount -t devpts -o mode=0620,gid=5 devpts /dev/pts
mount -t tmpfs -o mode=1777,nosuid,nodev tmpfs /dev/shm
chmod 0700 /run/perp
echo magnet-live > /proc/sys/kernel/hostname
ip link set lo up
for _attempt in $(seq 1 40); do
  while IFS= read -r media; do
    # Some hybrid layouts label both the whole USB disk and an overlapping
    # partition. The ISO filesystem begins at the whole-disk offset.
    parent=$(lsblk -dn -o PKNAME "$media")
    candidates=("$media")
    if test -n "$parent"; then candidates=("/dev/$parent" "$media"); fi
    for candidate in "${candidates[@]}"; do
      if mount -t iso9660 -o ro "$candidate" /run/media/magnet 2>/dev/null; then
        if test -f /run/media/magnet/MAGNET_INSTALL; then break 2; fi
        umount /run/media/magnet
      fi
    done
  done < <(blkid -t LABEL=MAGNET_INSTALL -o device 2>/dev/null || true)
  test ! -f /run/media/magnet/MAGNET_INSTALL || break
  sleep 1
done
if ! test -f /run/media/magnet/MAGNET_INSTALL; then
  echo 'Installation media could not be mounted. Use the shell to diagnose it.'
fi
perpboot /etc/perp &
echo "$!" > /run/perpboot.pid
