# Magnet Linux

A small, manually administered system assembled with magpkg. The initial target
is x86-64 UEFI, with console installation, sinit and perp, and an ext4 or Btrfs
root. The optional Hyprland profile provides graphical login after installation.

For the ISO/USB installer, start with [INSTALL.txt](INSTALL.txt). It installs
into already mounted filesystems and initializes ordinary editable `/etc`
files. The Btrfs layout and snapshot workflows below are optional.

`system.jsonnet` selects packages. `skeleton/` contains initial `/etc` files.
`setup-root.sh` copies missing configuration and applies explicit permissions;
`setup-boot.sh` prepares GRUB. `setup-tree.sh` installs the editable package
tree and bootstrap seed. Edit `/etc` normally after installation; service
configuration is administrator-owned. The CA certificate bundle is a package.

## Base and services

The installed system uses GNU coreutils, grep, sed, gawk, findutils, diffutils,
tar and compression tools; util-linux for system administration; shadow for
accounts, login, su and passwd; procps-ng for process tools; iproute2 and
dhcpcd for networking; and Bash as `/bin/bash`, with `/bin/sh` pointing to it.
Root and new interactive accounts use `/bin/bash`. Nano is the default editor,
mandoc provides `man` with the less pager, curl handles transfers, and
inetutils provides hostname, ping and traceroute. BusyBox remains in the
isolated build bootstrap, but is not in the installed runtime package set.

Read installed manuals with `man bash`, `man ls` or `man 5 doas.conf`. In the
pager, `/` searches and `q` quits. Installation builds the initial keyword
index. Run `makewhatis /share/man` as root after package changes to build or refresh the keyword index used by `apropos`
and `man -k`; ordinary page lookup works without an index.

Like Oasis, sinit is PID 1 and perp supervises foreground services.
`/etc/rc.init` mounts filesystems and starts perp; `/etc/rc.shutdown` stops
services before the final shutdown helper. Service definitions are ordinary
scripts under `/etc/perp`, enabled by the directory sticky bit. Runtime locks
and sockets are under `/run/perp`; supervisor logs rotate in `/var/log/perp`.
`/etc/rc.local` remains available for administrator startup commands.

## Headless installation test

From the repository root:

```sh
nix-shell magnet-linux/shell.nix
sudo -v
./magnet-linux/tests/install-boot.sh
```

Nix supplies host tools only. All installed package payloads are built by
magpkg using the bootstrap seed. Without Nix, provide the commands checked by
the test and set `OVMF_CODE` and `OVMF_VARS` to a matching pair of x86-64 OVMF
firmware files. Secure Boot is not enabled.

The test runs `prepare.py` to download the package-tree release, static magpkg,
and bootstrap archive pinned in the root `dependencies.json`. Git, Go and sibling
source checkouts are not required. The seed is imported into the host source
cache with `seed-store.jsonnet`, then installed separately at
`/var/lib/magpkg/bootstrap.tar.zst`
in the guest. Recipes use that installed path, with no reference to the host
checkout. Deleting the guest's fetch cache does not delete its seed.

The test creates a fresh sparse 16 GiB disk image, partitions and mounts only that
image, and executes the installation steps below. Sudo is needed for its loop
device and mounts. Builds and QEMU run as the invoking user. The disk is fully
unmounted and detached before each QEMU boot. KVM is used when available,
otherwise QEMU uses software emulation.

Three firmware boots exercise:

1. GRUB loading the installed kernel from Btrfs, sinit and perp, mounted data
   subvolumes, device nodes, account creation, and permissions.
2. A system rebuilt and staged by the guest itself, adding `hello` and preserving
   administrator edits to `/etc` and the package tree.
3. Offline restoration of a manual snapshot, recovering both the original
   package set and `/etc`, while `/var` retains data from all three boots.

On its first boot the guest starts with an empty package store. It downloads
hash-pinned sources, rebuilds magpkg and compares the binary with the installed
one, then rebuilds the full system including the kernel. No host checkout,
compiler, package cache, or shared filesystem is available to QEMU. It syncs
the edited package selection into a `root-next` snapshot and selects that root
in GRUB for the second boot. The third boot restores the pre-update snapshot.

Each boot also obtains a DHCP lease, starts sshd, accepts an ordinary user's
public key, rejects an unrelated key, and transfers a file using SFTP. The host
pins the installation's Ed25519 host key; the same key must work after updating
and restoring. QEMU forwards only `127.0.0.1:22222` to guest SSH and allows
outbound networking for source downloads. Set `SSH_PORT` to another unused
unprivileged port if necessary.

The test also checks that perp restarts a crashed service, honors stop/start
and persistent disable commands, and that sinit's poweroff and reboot paths work.

The test installs a temporary `/etc/rc.local` assertion script which powers off
the VM after the build and SSH checks. This hook is **only in the test image**,
not the ordinary skeleton. Root remains locked. The test user's randomly generated
password is discarded; SSH password authentication is disabled. A fresh test
client key is kept in the private test output directory. Each run retains its
disk, serial logs, and SSH logs under `out/vm-test.*`; build state is under
`_work/`.

Set `BUILD_PARALLELISM` to control host compilation and `QEMU_TIMEOUT` to change
the per-boot timeout in seconds (default 5400). The guest has four CPUs and
8 GiB RAM by default; `QEMU_MEMORY` changes RAM in MiB. A full rebuild takes
tens of minutes with KVM; software emulation may need a longer timeout.
`MAGPKG_STORE` can select another host store. Use separate output directories and SSH ports for concurrent test instances.

If GNU's download server is unavailable, set
`GNU_MIRROR=https://mirrors.kernel.org/gnu/` for the test. The guest downloads
the same hash-verified source archives from that mirror; package definitions
and identities remain unchanged.

## Optional Btrfs layout

The EFI system partition contains a standalone GRUB executable at the UEFI
fallback path `EFI/BOOT/BOOTX64.EFI`. The Btrfs top level contains:

```text
root/          mounted at /; includes packages, /etc, kernel, and GRUB config
home/          mounted at /home
var/           mounted at /var
snapshots/     manually named snapshots of root
```

`root`, `home`, and `var` are sibling subvolumes. The root snapshot includes
`/etc` and the magpkg ownership manifest, but excludes the separately mounted
`/home`, `/var`, and EFI filesystem. `/run` and `/tmp` are tmpfs mounts.

The kernel has the VM's root/storage drivers built in; this profile uses no
separate initramfs and no loadable modules. GRUB selects the root partition by PARTUUID
and the `root` subvolume explicitly. Keeping `/boot/vmlinuz` in that subvolume
means restoring it also restores the kernel. The EFI executable searches by
filesystem UUID and loads `/root/boot/grub/grub.cfg`.

`/etc/magnet-linux/system.jsonnet` selects the system packages and imports the
editable recipes in `/etc/magnet-linux/tree`. These roll back with `/etc`.
`/var/lib/magpkg/store` contains disposable build/cache state; the separately
kept `/var/lib/magpkg/bootstrap.tar.zst` is the pinned compiler seed. Root's
login profile sets `MAGPKG_STORE` to this persistent store. Ordinary users keep
magpkg's default store in their own home directories.

## Manual installation

These commands run on an existing x86-64 Linux installation host. Start in the
repository root with the host tools above. Build packages as your ordinary
user before doing privileged installation work:

```sh
python3 magnet-linux/prepare.py
magpkg=$PWD/magnet-linux/_work/magpkg
store=$PWD/magnet-linux/_work/store
"$magpkg" init --store "$store"
"$magpkg" build --store "$store" \
  --ext-str "bootstrap-url=file://$PWD/magnet-linux/_work/bootstrap.tar.zst" \
  magnet-linux/seed-store.jsonnet seed
"$magpkg" build --store "$store" --jobs 3 --parallelism 8 \
  magnet-linux/system.jsonnet system
as_root() { sudo env "PATH=$PATH" "$@"; }
```

Partition the intended disk yourself: GPT, an EFI system partition (256 MiB is
sufficient for this setup), and a Linux system partition. Set `esp` and
`system` to those **target partitions**, and choose unused mount directories:

```sh
# Example device names only; select the actual target partitions yourself.
esp=/dev/vda1
system=/dev/vda2
top=/mnt/magnet-top
target=/mnt/magnet
```

Formatting destroys the contents of those two partitions. The automated test
does this only to its newly created image.

```sh
as_root mkfs.vfat -F 32 "$esp"
as_root mkfs.btrfs -f -O ^free-space-tree,^block-group-tree "$system"
as_root mkdir -p "$top" "$target"
as_root mount "$system" "$top"
for name in root home var; do
  as_root btrfs subvolume create "$top/$name"
done
as_root mkdir "$top/snapshots"
as_root mount -o subvol=root "$system" "$target"

as_root "$magpkg" sync --store "$store" --no-build --no-binary-cache \
  --dest "$target" magnet-linux/system.jsonnet system

root_uuid=$(as_root blkid -s UUID -o value "$system")
root_partuuid=$(as_root blkid -s PARTUUID -o value "$system")
esp_uuid=$(as_root blkid -s UUID -o value "$esp")
as_root mount -o subvol=home "$system" "$target/home"
as_root mount -o subvol=var "$system" "$target/var"
as_root mkdir -p "$target/boot/efi"
as_root mount "$esp" "$target/boot/efi"
as_root ./magnet-linux/setup-root.sh "$target" magnet "$root_uuid" "$esp_uuid"
as_root ./magnet-linux/setup-tree.sh "$target"
```

The mkfs feature options keep the filesystem readable by the selected GRUB
implementation instead of inheriting new filesystem feature defaults.

Set passwords and create accounts explicitly. Nothing ships with a shared or
empty password. These commands execute the target's shadow account tools:

```sh
as_root chroot "$target" /bin/passwd root
as_root chroot "$target" /bin/useradd -m -u 1000 -g users -s /bin/bash alice
as_root chroot "$target" /bin/passwd alice
as_root ./magnet-linux/setup-doas.sh "$target" alice
as_root ./magnet-linux/setup-boot.sh "$target" "$root_uuid" "$root_partuuid"
```

Review `/etc/hostname`, `/etc/fstab`, `/etc/rc.init` and `/etc/perp`, and `/etc/rc.local` inside
the target. Networking and SSH startup are opt-in; see below. The serial console is
`ttyS0` at 115200 baud, with a local text login on tty1. The skeleton does not configure a desktop or
passwordless privilege escalation. The selected administrator can use
`doas COMMAND` with their own password, or `doas -s` for a root shell.
`su -` with the root password is also available.

OpenDoas is part of the base system. `/etc/doas.conf` initially contains
`permit :wheel as root`; PAM authenticates against the invoking account's
password and checks account validity using `/etc/pam.d/doas`. Password caching
and empty-password authentication are disabled. `setup-root.sh` creates an
empty `wheel` group; `setup-doas.sh TARGET USER` explicitly enrolls an
administrator. Existing policy and PAM files are preserved. To grant another
account access on a running system, an administrator can use
`doas usermod -a -G wheel USER`, then have that user log in again.

```sh
as_root umount "$target/boot/efi" "$target/var" "$target/home"
as_root umount "$target" "$top"
```

Boot the disk in a UEFI machine with Secure Boot disabled and a serial console.
This first kernel configuration is a development/VM profile, not a general
hardware configuration. AHCI and NVMe are included, but arbitrary hardware,
encrypted root, firmware loading, and physical network adapters have not been
validated by the installation test. Virtio networking and DHCP are tested.

## SSH access

The system includes OpenSSH clients, the server, and SFTP. They are statically
linked against OpenSSL. `setup-root.sh` creates the unprivileged `sshd` account
and generates missing host keys in the target's `/etc/ssh`. It preserves keys
on subsequent runs. No private keys are in the package archive or skeleton.
Generate host keys separately for each installation; cloning an already
initialized disk also clones its SSH identity.

The initial `/etc/ssh/sshd_config` permits only public-key authentication and
disables root login, password authentication, and keyboard-interactive login.
Install your **public** key for an ordinary user after creating that account
and mounting its home subvolume. For the `alice` account above:

```sh
as_root install -d -m 0700 -o 1000 -g 100 "$target/home/alice/.ssh"
as_root install -m 0600 -o 1000 -g 100 "$HOME/.ssh/id_ed25519.pub" \
  "$target/home/alice/.ssh/authorized_keys"
```

Use the account's actual UID and GID if different. Set its shell explicitly to
`/bin/bash` when creating it. `/etc/default/useradd` sets the same default.
Set an account password as shown above: this build uses no PAM, and OpenSSH
rejects locked accounts even when a valid public key is supplied. Setting a
local password does not enable SSH password authentication.

On a headless installation, edit `/etc/dhcpcd.conf` for your interfaces.
Enable networking and SSH with:

```sh
perpctl -b /etc/perp A dhcpcd sshd
```

For an offline target, set the service directories' sticky bits instead:

```sh
as_root chmod +t "$target/etc/perp/dhcpcd" "$target/etc/perp/sshd"
```

Dhcpcd maintains addresses, routes, lease renewal and `/etc/resolv.conf`.
For static networking, leave dhcpcd disabled and put your `ip` commands in
`/etc/rc.local`. The base skeleton enables the serial console and chronyd;
`create-desktop-vm.sh` also enables networking and SSH in its VM image.

The Hyprland profile uses **NetworkManager** instead of the standalone dhcpcd
service. Click the Ethernet/Wi-Fi indicator in Waybar to open `nmtui` in Foot,
or run `nmtui` from a terminal. `nmcli` provides the command-line interface:

```sh
nmcli device status
nmcli connection show
nmcli device wifi list
nmcli --ask device wifi connect 'your SSID'
```

System settings live in `/etc/NetworkManager/NetworkManager.conf` and
`/etc/NetworkManager/conf.d/`. Connection profiles are ordinary INI files under
`/etc/NetworkManager/system-connections/`, owned by root with mode 0600. After
editing a profile manually, run `nmcli connection reload` as root, then
reactivate that connection. NetworkManager maintains `/etc/resolv.conf` and
uses its internal DHCP client. No external connectivity-check URL is enabled.
The initial `wired.nmconnection` profile enables DHCP and IPv6 autoconfiguration
on Ethernet adapters, including after reboot. Edit it or disable its
autoconnect setting if you want only explicitly configured wired connections.

`setup-login.sh` calls `setup-network.sh TARGET USER`, which creates the system
D-Bus, polkit, supplicant and NetworkManager perp services and disables the
stock dhcpcd service. Existing `/etc` edits are retained; an administrator's
custom dhcpcd service is rejected rather than silently replaced. The desktop
account receives `netdev` membership and permission to control system-wide
connections, including from SSH. The greeter receives no network permissions.
Other polkit actions and password prompts are not enabled.

QEMU provides an Ethernet adapter. Wi-Fi support is packaged, but association
requires real hardware, its kernel driver, firmware and regulatory data; it
cannot be validated using the VM's virtual Ethernet device.

```sh
perpls -b /etc/perp             # service status
perpctl -b /etc/perp d sshd     # stop until brought up or rebooted
perpctl -b /etc/perp u sshd     # start an already active service
perpctl -b /etc/perp X sshd     # disable persistently
```

The startup script validates configuration and recreates `/run/sshd` for
privilege separation. Logs go to `/var/log/sshd.log`. The kernel includes
seccomp support for OpenSSH's pre-authentication sandbox. Confirm the host key
fingerprint at the console (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`)
before trusting a new server, then connect with `ssh alice@HOST`.

The sinit/perp base is intended for fresh installations. Existing BusyBox
images retain their old `/etc` and boot system. A package sync alone does not
migrate their init, service scripts, or account-tool assumptions; create a new
VM to try this base. Initialization deliberately preserves administrator files.

## Packages and configuration

Chrony synchronizes the system clock on both headless and desktop installs.
`setup-root.sh` calls `setup-time.sh TARGET` to create its locked service
account and enable its perp service. Run `setup-time.sh` separately when adding
chrony to an existing mounted installation. Existing settings and an explicitly
disabled service are preserved. `/etc/chrony.conf` initially uses `pool.ntp.org`;
edit it to select other NTP sources. This build uses ordinary NTP, without NTS.

`chronyc tracking` and `chronyc -n sources` are available to ordinary users.
Administrative commands use the private Unix socket and require root. No NTP
server is exposed; the read-only UDP monitoring interface is loopback-only.
Chronyd drops privileges to `chrony`, keeps drift data in `/var/lib/chrony`,
and logs through perp. It retries sources if networking is initially absent.
After editing the configuration, root can restart it with
`perpctl -b /etc/perp t chronyd`. To disable it persistently, use
`perpctl -b /etc/perp X chronyd`.

The installed runtime includes the base tools above, Btrfs tools, OpenSSH,
magpkg, bubblewrap, CA certificates, and the kernel. The bootstrap compiler and Go toolchain are
materialized in build sandboxes as needed. Go currently comes from a pinned
official binary release; magpkg and the rest of the system are built from their
pinned inputs. Btrfs tools
are statically linked; their optional userspace zstd/lzo restore/receive support
is disabled in this initial recipe.

The installed system can build its own packages and kernel. Enable networking
and set a correct clock for HTTPS, then, as root:

```sh
export MAGPKG_STORE=/var/lib/magpkg/store
magpkg build --jobs 2 --parallelism 4 /etc/magnet-linux/system.jsonnet system
```

The seed is retained locally; other uncached sources need their configured
download servers. This is self-contained for system maintenance, not an offline
mirror of every source. `setup-tree.sh` records the source checkout's base Git
commit and a `FILES.sha256` manifest of the installed recipes, including local
changes. It initializes the tree and selection only when absent. To update
recipes later, edit or copy reviewed files into the tree yourself, preferably
in a staged root as below. The Go magpkg source pin is independent of the tree.

Do not pass
`--delete-unmanaged`: `/etc`, home directories, and bootloader configuration
are intentionally outside package ownership. Re-run `setup-boot.sh` if changing
the initial EFI installation or installing onto a different partition; that
provisioning step still requires the installation host's GRUB tools. Normal
package and kernel updates use the existing EFI executable and run in the guest.

`setup-root.sh` is initialization, not configuration management. Existing files
and their modes are preserved, including passwords, hostname, and mount
configuration. Skeleton changes require manual application to existing systems.
For an older oksh installation, sync packages while the target is offline,
then use `chroot TARGET usermod -s /bin/bash root` and the same command for
each interactive user. Update `/etc/shells` to list `/bin/sh` and `/bin/bash`,
and set `SHELL=/bin/bash` in `/etc/default/useradd`. Start a new login session
to pick up the account's shell; system scripts continue to use `/bin/sh`.
The repository stores normal files; special permissions are explicit in the
setup script and `root-layout` package. `/dev` is populated by devtmpfs at boot.

Package sync is not transactional. Build first and sync an inactive root
snapshot rather than the running root. There is no automatic update service.

## Staging an update in the installed system

For the first update, starting from the original `root` subvolume, mount the
Btrfs top level and choose unused snapshot names:

```sh
export MAGPKG_STORE=/var/lib/magpkg/store
mkdir -p /run/magnet-top
mount -t btrfs -o subvolid=5 UUID=YOUR-ROOT-FS-UUID /run/magnet-top
sync
btrfs subvolume snapshot -r / /run/magnet-top/snapshots/before-update
btrfs subvolume snapshot / /run/magnet-top/root-next
next=/run/magnet-top/root-next
mount --bind /var "$next/var"
```

Edit `$next/etc/magnet-linux/system.jsonnet` and recipes under
`$next/etc/magnet-linux/tree`, then build and install that selection:

```sh
magpkg build --jobs 2 --parallelism 4 "$next/etc/magnet-linux/system.jsonnet" system
magpkg sync --no-build --no-binary-cache --dest "$next" \
  "$next/etc/magnet-linux/system.jsonnet" system
sync
umount "$next/var"
umount /run/magnet-top
reboot
```

Select **Magnet Linux (root-next)** in the serial GRUB menu. The installed
configuration shows that entry when `/root-next/boot/vmlinuz` exists. To make
it the default, explicitly change `set default=0` to `set default=next` in the
original root's `/boot/grub/grub.cfg` before rebooting. The EFI executable
always reads that original root's config, even when running `root-next`.

Both roots share `/home` and `/var`, but each has its own packages, kernel,
selection, recipes, and `/etc`. Retain the original root while trying an update.
The `/var` bind mount lets sync see package-owned directories on that shared
subvolume, such as the font cache and D-Bus state directories. It also means
any package changes under `/var` apply to the shared state immediately.
For later stages, choose an unused subvolume name and add a corresponding GRUB
entry with matching kernel path and `rootflags=subvol=...`; this is deliberately
manual. Never sync a target which is the currently running subvolume.

## Manual snapshots and recovery

From the installed system, mount the Btrfs top level at a temporary location.
Use the filesystem UUID shown in `/etc/fstab`:

```sh
mkdir -p /run/magnet-top
mount -t btrfs -o subvolid=5 UUID=YOUR-ROOT-FS-UUID /run/magnet-top
sync
btrfs subvolume snapshot -r / /run/magnet-top/snapshots/before-update
umount /run/magnet-top
```

Snapshots taken while running are filesystem checkpoints, not application
quiescence or backups. Persistent application data is deliberately outside
this root snapshot.

To restore, boot another Linux environment and mount only the Btrfs top level;
do not keep the installed root mounted while replacing it:

```sh
mount -t btrfs -o subvolid=5 UUID=YOUR-ROOT-FS-UUID /mnt
mv /mnt/root /mnt/root-before-recovery
btrfs subvolume snapshot /mnt/snapshots/before-update /mnt/root
umount /mnt
```

Use an unused name for the saved root. The restored snapshot is cloned writable.
The existing GRUB entry still selects `root`, now containing the old kernel,
packages, manifest, and configuration. `/home` and `/var` remain current. The
EFI partition is separate: root snapshots do not undo EFI executable changes;
repair it with `setup-boot.sh` from the installation host if necessary.

## Optional desktop

For Hyprland, graphical login, Foot, and graphical QEMU boot tests, see
[DESKTOP.md](DESKTOP.md). The default `system.jsonnet` remains the console/SSH
installation; `desktop-hyprland.jsonnet` selects the additional desktop packages and
kernel drivers.
