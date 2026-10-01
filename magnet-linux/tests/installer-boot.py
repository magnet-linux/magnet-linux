#!/usr/bin/env python3
"""Boot real installation media, install offline, then boot without the media."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import selectors
import shutil
import socket
import subprocess
import tempfile
import time

class VM:
    def __init__(self, command, logfile):
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)
        self.log = logfile.open("wb")
        self.buffer = b""

    def wait_for(self, needle, timeout=180):
        end = time.monotonic() + timeout
        while needle not in self.buffer:
            if b'\nMAGNET_INSTALL_TEST_FAILED' in self.buffer or b'\nMAGNET_INSTALLED_FAILED' in self.buffer:
                raise RuntimeError(f"Guest check failed; see {self.log.name}")
            if time.monotonic() > end:
                raise RuntimeError(f"Timed out waiting for {needle!r}; see {self.log.name}")
            if not self.selector.select(1):
                continue
            data = os.read(self.process.stdout.fileno(), 65536)
            if not data:
                raise RuntimeError(f"QEMU exited before {needle!r}; see {self.log.name}")
            self.log.write(data)
            self.log.flush()
            self.buffer += data
        before, _, self.buffer = self.buffer.partition(needle)
        return before

    def send(self, text):
        self.process.stdin.write(text.encode())
        self.process.stdin.flush()

    def screenshot(self, monitor, destination):
        with socket.socket(socket.AF_UNIX) as client:
            client.settimeout(10)
            client.connect(str(monitor))
            with client.makefile("rwb") as stream:
                json.loads(stream.readline())  # QMP greeting
                for command in [{"execute": "qmp_capabilities"},
                                {"execute": "screendump", "arguments": {"filename": str(destination)}}]:
                    stream.write(json.dumps(command).encode() + b"\n")
                    stream.flush()
                    while True:
                        response = json.loads(stream.readline())
                        if "error" in response:
                            raise RuntimeError(response["error"])
                        if "return" in response:
                            break

    def script(self, text):
        self.send("stty -echo\n")
        self.wait_for(b"magnet-live# ")
        encoded = base64.b64encode(text.encode()).decode()
        self.send("base64 -d > /tmp/install-test.sh <<'MAGNET_SCRIPT'\n")
        for start in range(0, len(encoded), 120):
            self.send(encoded[start:start + 120] + "\n")
        self.send("MAGNET_SCRIPT\nbash /tmp/install-test.sh\n")

    def wait_exit(self, timeout=60):
        end = time.monotonic() + timeout
        while self.process.poll() is None:
            if time.monotonic() > end:
                raise RuntimeError("QEMU did not power off")
            if self.selector.select(1):
                data = os.read(self.process.stdout.fileno(), 65536)
                self.log.write(data)
        if self.process.returncode:
            raise RuntimeError(f"QEMU exited with {self.process.returncode}")
        self.close()

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.selector.close()
        self.log.close()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("iso", type=Path)
    parser.add_argument("--filesystem", choices=["ext4", "btrfs"], default="ext4")
    parser.add_argument("--media-mode", choices=["cdrom", "usb"], default="cdrom")
    parser.add_argument("--profile", choices=["base", "hyprland"], default="base")
    parser.add_argument("--interactive", action="store_true", help="answer the real console prompts")
    parser.add_argument("--separate-boot", action="store_true", help="mount a separate ext4 /boot partition")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    iso = args.iso.resolve(strict=True)
    distro = Path(__file__).resolve().parents[1]
    os.umask(0o077)
    if args.output:
        work = args.output.resolve()
        work.mkdir(parents=True, exist_ok=False)
    else:
        work = Path(tempfile.mkdtemp(prefix="installer-test.", dir=distro / "out"))
    print(f"Test artifacts: {work}", flush=True)
    for name, variable in [("code.fd", "OVMF_CODE"), ("vars.fd", "OVMF_VARS")]:
        shutil.copyfile(os.environ[variable], work / name)
    with (work / "disk.raw").open("wb") as disk:
        disk.truncate(16 * 1024**3)
    password = secrets.token_hex(12)
    root_password = secrets.token_hex(16)
    (work / "credentials.txt").write_text(f"User: browser\nPassword: {password}\nRoot password: {root_password}\n")
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(work / "client-key")], check=True)
    public_key = (work / "client-key.pub").read_text().strip()
    partitions = "size=512M,type=U\nsize=14G,type=L\ntype=L" if args.separate_boot else "size=512M,type=U\ntype=L"
    boot_setup = ""
    boot_check = ""
    if args.separate_boot:
        boot_setup = "mkfs.ext4 -F /dev/vda3\nmkdir -p /mnt/boot\nmount /dev/vda3 /mnt/boot"
        boot_check = 'test "$(findmnt -n -o FSTYPE --mountpoint /boot)" = ext4'
    kvm = os.access("/dev/kvm", os.R_OK | os.W_OK)
    common = [
        "qemu-system-x86_64", "-name", "magnet-installer-test",
        "-machine", "q35,accel=" + ("kvm" if kvm else "tcg"),
        "-cpu", "host" if kvm else "max", "-m", "4096", "-smp", "4",
        "-drive", f"if=pflash,format=raw,readonly=on,file={work}/code.fd",
        "-drive", f"if=pflash,format=raw,file={work}/vars.fd",
        "-drive", f"if=none,format=raw,id=target,file={work}/disk.raw",
        "-device", "virtio-blk-pci,drive=target,bootindex=1",
        "-device", "virtio-rng-pci", "-device", "virtio-vga",
        "-nic", "none", "-display", "none", "-monitor", "none",
        "-qmp", f"unix:{work}/qmp.sock,server=on,wait=off",
        "-serial", "stdio", "-no-reboot",
    ]
    if args.media_mode == "cdrom":
        media = ["-drive", f"file={iso},format=raw,media=cdrom,readonly=on", "-boot", "order=d"]
    else:
        media = ["-device", "qemu-xhci", "-drive", f"if=none,id=media,file={iso},format=raw,readonly=on",
                 "-device", "usb-storage,drive=media,bootindex=0"]
    if args.filesystem == "ext4":
        formatting = "mkfs.ext4 -F /dev/vda2\nmount /dev/vda2 /mnt"
        rootcheck = ""
    else:
        formatting = """mkfs.btrfs -f -O ^free-space-tree,^block-group-tree /dev/vda2
mount /dev/vda2 /mnt
for name in system home var snapshots; do btrfs subvolume create /mnt/$name; done
umount /mnt
mount -o subvol=system /dev/vda2 /mnt
mkdir -p /mnt/home /mnt/var
mount -o subvol=home /dev/vda2 /mnt/home
mount -o subvol=var /dev/vda2 /mnt/var"""
        rootcheck = """test "$(findmnt -n -o FSROOT /)" = /system
test "$(findmnt -n -o FSROOT /home)" = /home
test "$(findmnt -n -o FSROOT /var)" = /var
mkdir -p /run/snapshots
mount -o subvolid=5 /dev/vda2 /run/snapshots
btrfs subvolume snapshot -r / /run/snapshots/snapshots/installed
test -f /run/snapshots/snapshots/installed/etc/magnet-linux/installation
umount /run/snapshots"""
    installed_check = f"""#!/bin/bash
set -eu
exec > /var/log/installer-check.log 2>&1
trap '{{ echo; echo MAGNET_INSTALLED_FAILED; }} > /dev/console; poweroff' ERR
test "$(findmnt -n -o FSTYPE /)" = {args.filesystem}
{boot_check}
mountpoint -q /boot/efi
test -s /boot/efi/EFI/BOOT/BOOTX64.EFI
test "$(hostname)" = magnet-installed
test "$(id -u browser)" = 1000
test "$(stat -c %a /bin/doas)" = 4755
test -x /bin/magnet-linux
test -f /etc/magnet-linux/tree/packages/storage.jsonnet
(cd /etc/magnet-linux/tree; sha256sum -c FILES.sha256)
{rootcheck}
export MAGPKG_STORE=/var/lib/magpkg/store
magpkg build --no-binary-cache /etc/magnet-linux/tree/packages/hello.jsonnet hello
magpkg sync --no-build --no-binary-cache --dest /var/tmp/hello /etc/magnet-linux/tree/packages/hello.jsonnet hello
test "$(/var/tmp/hello/bin/hello)" = 'hello world'
rm /etc/rc.local
{{ echo; echo MAGNET_INSTALLED_OK; }} > /dev/console
poweroff
"""
    check64 = base64.b64encode(installed_check.encode()).decode()
    install_command = f"""magnet-linux install --root /mnt --profile {args.profile} --hostname magnet-installed --user browser \\
  --password-file /tmp/password --root-password-file /tmp/root-password --ssh-key /tmp/public-key --enable-ssh --yes"""
    script = f"""#!/bin/bash
set -eu
trap 'echo MAGNET_INSTALL_TEST_FAILED' ERR
mkdir -p /mnt
if magnet-linux install --root /mnt --yes >/tmp/rejected 2>&1; then exit 1; fi
grep -q 'is not a mountpoint' /tmp/rejected
if magnet-linux install --root / --yes >/tmp/rejected 2>&1; then exit 1; fi
grep -q 'other than /' /tmp/rejected
test ! -e /mnt/etc
sfdisk /dev/vda <<'PARTITIONS'
label: gpt
{partitions}
PARTITIONS
mkfs.fat -F 32 /dev/vda1
{formatting}
{boot_setup}
mkdir -p /mnt/boot/efi
if magnet-linux install --root /mnt --yes >/tmp/rejected 2>&1; then exit 1; fi
grep -q 'Mount the EFI system partition' /tmp/rejected
mount /dev/vda1 /mnt/boot/efi
umask 077
printf '%s\\n' '{password}' > /tmp/password
printf '%s\\n' '{root_password}' > /tmp/root-password
printf '%s\\n' '{public_key}' > /tmp/public-key
mkdir -p /tmp/bad-media/profiles
touch /tmp/bad-media/MAGNET_INSTALL /tmp/bad-media/profiles/{args.profile}.sha256
echo bad > /tmp/bad-media/corrupt
printf '%064d  corrupt\\n' 0 > /tmp/bad-media/source.sha256
if magnet-linux install --root /mnt --media /tmp/bad-media --profile {args.profile} --hostname test --user browser --password-file /tmp/password --yes >/tmp/rejected 2>&1; then exit 1; fi
test ! -e /mnt/etc/passwd
test ! -e /mnt/.magpkg
{install_command}
if magnet-linux install --root /mnt --yes >/tmp/rejected 2>&1; then exit 1; fi
grep -q 'already contains a system' /tmp/rejected
test "$(cat /mnt/etc/hostname)" = magnet-installed
printf '%s' '{check64}' | base64 -d > /mnt/etc/rc.local
chmod 0755 /mnt/etc/rc.local
printf 'HOSTKEY_START\\n'
cat /mnt/etc/ssh/ssh_host_ed25519_key.pub
printf 'HOSTKEY_END\\n'
sync
umount -R /mnt
echo MAGNET_INSTALL_TEST_OK
poweroff
"""
    vm = VM(common + media, work / "install-serial.log")
    try:
        vm.wait_for(b"magnet-live# ", timeout=240)
        time.sleep(2)  # Allow the other getty to finish its local console login.
        vm.screenshot(work / "qmp.sock", work / "live-console.ppm")
        print(f"Live {args.media_mode} boot succeeded; installing {args.profile} on {args.filesystem} offline.", flush=True)
        if args.interactive:
            before, after = script.split(install_command, 1)
            vm.script(before + '\necho MAGNET_PREPARED\n')
            vm.wait_for(b'\nMAGNET_PREPARED')
            vm.wait_for(b'magnet-live# ')
            vm.send('magnet-linux install --root /mnt --root-password-file /tmp/root-password --ssh-key /tmp/public-key --enable-ssh || echo MAGNET_INSTALL_TEST_FAILED\n')
            for prompt, answer in [
                (b'System profile? [base] ', args.profile),
                (b'Hostname? [magnet] ', 'magnet-installed'),
                (b'Administrator username? ', 'browser'),
                (b'Administrator password: ', password),
                (b'Retype password: ', password),
                (b'Proceed with installation? [no] ', 'yes'),
            ]:
                vm.wait_for(prompt)
                vm.send(answer + '\n')
            vm.wait_for(b'Installation complete.', timeout=900)
            vm.wait_for(b'magnet-live# ')
            vm.script("set -eu\ntrap 'echo MAGNET_INSTALL_TEST_FAILED' ERR\n" + after)
        else:
            vm.script(script)
        output = vm.wait_for(b"\nMAGNET_INSTALL_TEST_OK", timeout=900)
        if b"MAGNET_INSTALL_TEST_FAILED" in output:
            raise RuntimeError("Installation failed")
        hostkey = output.split(b"HOSTKEY_START\r\n", 1)[1].split(b"\r\nHOSTKEY_END", 1)[0].decode().strip()
        (work / "known_hosts").write_text(f"magnet-desktop-vm {hostkey}\n")
        vm.wait_exit()
    finally:
        vm.close()
    print("Booting installed disk with no installation media or network.", flush=True)
    vm = VM(common, work / "installed-serial.log")
    try:
        vm.wait_for(b"\nMAGNET_INSTALLED_OK", timeout=600)
        vm.wait_exit()
    finally:
        vm.close()
    (work / "ready").touch()
    (work / "login-user").write_text("browser\n")
    (work / "graphics").write_text("virgl\n")
    if args.profile == "hyprland":
        (work / "graphical-login").touch()
    print(f"PASS: {args.media_mode} boot, {args.filesystem} installation, target guards, independent boot, installed recipes and offline package build.", flush=True)
    print(f"VM retained: {work}", flush=True)

if __name__ == "__main__":
    main()
