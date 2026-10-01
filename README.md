# Magnet Linux

Magnet Linux is an experimental x86-64 musl distribution with sinit/perp,
editable `/etc`, and an optional Hyprland desktop. Its text installer supports
ext4 and Btrfs on UEFI machines, with optional manual Btrfs snapshots.
Package synchronization is performed on offline roots.

The project has three independently released repositories:

| Repository | Responsibility |
| --- | --- |
| [magpkg](https://github.com/magnet-linux/magpkg) | Package manager, Go libraries, tests, manuals, and static executable releases. |
| [packages](https://github.com/magnet-linux/packages) | Software recipes, patches, build helpers, and the bootstrap toolchain. |
| [magnet-linux](https://github.com/magnet-linux/magnet-linux) | This distro: profiles, kernel configuration, session policy, installer, `/etc` skeletons, and QEMU tests. |

## Installation media

Build an offline ISO containing the base and Hyprland profiles:

```sh
nix-shell magnet-linux/shell.nix
sudo -v
./magnet-linux/build-installer.sh
```

The resulting `magnet-linux/out/magnet-linux-installer.iso` boots as a UEFI
CD or USB disk. It starts a text console with disk tools, magpkg, and a local
root shell. Prepare and mount the destination, then run:

```sh
magnet-linux install --root /mnt
```

The installer asks for a profile, hostname and root password. It
shows the target mounts before installing prebuilt packages, configuration,
the editable package tree, bootstrap seed, and GRUB. The destination root
must be mounted at `/mnt`, with its FAT EFI partition at `/mnt/boot/efi`.
Optional separate filesystems must already be mounted. Formatting remains
manual; installation itself needs no network connection.

Root is the only initial login account. Add users afterward with `useradd`
and `passwd`, either through `chroot /mnt` before rebooting or from the root
console. All profiles start with text login and Ethernet DHCP. A Hyprland
installation includes the desktop packages; configure graphical login after
adding an ordinary user, as described in `INSTALL.txt`.

See [INSTALL.txt](magnet-linux/INSTALL.txt), also included on the media, for
ext4 and Btrfs examples. `INSTALLER_PROFILES='base'` builds smaller media;
additional profile names include `hyprland-firefox`, `hyprland-chromium`, and
`hyprland-browsers`. The live environment always uses a text console.
`INSTALLER_WORKDIR` can select fast temporary storage for image staging;
the default is `magnet-linux/_work`.

The first hardware target remains QEMU. Secure Boot, legacy BIOS installation,
encrypted root, and hardware-specific Wi-Fi/GPU firmware are not included.

To try the installer yourself, run these commands inside the Nix shell above.
They create a new disk and private firmware state; choose an unused directory:

```sh
mkdir magnet-linux/out/manual-install
cp "$OVMF_VARS" magnet-linux/out/manual-install/vars.fd
chmod u+w magnet-linux/out/manual-install/vars.fd
qemu-img create -f raw magnet-linux/out/manual-install/disk.raw 16G
qemu-system-x86_64 -machine q35,accel=kvm -cpu host -m 4096 -smp 4 \
  -drive if=pflash,format=raw,readonly=on,file="$OVMF_CODE" \
  -drive if=pflash,format=raw,file=magnet-linux/out/manual-install/vars.fd \
  -drive if=virtio,format=raw,file=magnet-linux/out/manual-install/disk.raw \
  -cdrom magnet-linux/out/magnet-linux-installer.iso -boot order=d \
  -vga none -device virtio-vga-gl -display sdl,gl=on \
  -nic user,model=virtio-net-pci -serial mon:stdio
```

Follow `INSTALL.txt` at the console. After installation, rerun QEMU without
`-cdrom ... -boot order=d` to boot the disk. The ISO can also be written as a
whole-disk image to a USB key; doing so replaces that key's contents.

## Create a desktop VM directly

Download and extract a distro source release, or clone this repository. A
release archive works without Git or a neighbouring checkout. Enter the host
tool environment and create a VM:

```sh
nix-shell magnet-linux/shell.nix
./magnet-linux/create-desktop-vm.sh
./magnet-linux/run-vm.sh
```

Both builders prepare the exact release inputs in `dependencies.json`: a
package-tree archive, a static magpkg executable, and the bootstrap seed. They
verify SHA-256 before extracting or using each download. Go and Git are not
installation prerequisites. `make prepare` downloads the inputs separately.
The host needs Python 3.12+, bubblewrap, QEMU and the disk/boot tools provided
by the Nix shell; Nix itself is optional if these tools are already installed.

Packages are extracted into the ignored `packages/` directory. Downloaded
tools and the seed are in `magnet-linux/_work/`. Existing package-tree edits
are retained. A changed tree version requires explicitly moving the old tree
aside first; preparation does not merge or overwrite recipe changes.

Set `WITH_FIREFOX=1` and/or `WITH_CHROMIUM=1` when creating a VM to include
browsers. Read `credentials.txt` in the new VM directory for the private login
details. Existing VM data stays under `magnet-linux/out/`.
This desktop demo helper explicitly adds its `browser` user after installation.

See [the installation and maintenance guide](magnet-linux/README.md) and
[the desktop guide](magnet-linux/DESKTOP.md) for configuration and commands.

## Development and testing

Repositories can live anywhere. For an initial preparation, explicitly select
development inputs when needed:

```sh
PACKAGES_DIR=/path/to/packages MAGPKG=/path/to/magpkg \
  BOOTSTRAP_ARCHIVE=/path/to/bootstrap.tar.zst make prepare
```

The package checkout is copied, including local edits and excluding Git/build
state. It is independent of its original location afterward. The seed still
has to match the pinned hash. An existing prepared tree is always preserved;
move it aside to copy a new development tree. `MAGPKG` overrides the host tool,
while the installed magpkg version remains selected by the package recipe.

```sh
make test
nix-shell magnet-linux/shell.nix --run \
  'python3 magnet-linux/tests/installer-boot.py magnet-linux/out/magnet-linux-installer.iso'
nix-shell magnet-linux/shell.nix --run \
  'python3 magnet-linux/tests/installer-boot.py magnet-linux/out/magnet-linux-installer.iso --filesystem btrfs --media-mode usb'
nix-shell magnet-linux/shell.nix --run 'SSH_PORT=22228 magnet-linux/tests/install-boot.sh'
nix-shell magnet-linux/shell.nix --run \
  'WITH_FIREFOX=1 WITH_CHROMIUM=1 TEST_BROWSERS="firefox chromium" TEST_DOAS=1 SSH_PORT=22228 magnet-linux/tests/desktop-boot.sh'
```

The installer tests boot the media, prepare a blank disk inside QEMU, and
install with networking disabled. They then detach the media, boot the new
system, verify mounts and accounts, and build a package from its retained
bootstrap seed, and authenticate at the root console. Add `--interactive` to
exercise the actual console prompts. `--profile hyprland --with-user` also
tests adding an ordinary user and configuring graphical login after installation.

The headless test builds packages inside the guest and verifies snapshot
updates and recovery. The graphical test creates a fresh disk, logs in on a
private Xvfb display, exercises the desktop and browsers, and shuts down.

The installed tree includes both the selected package definitions and this
distro's profiles/scripts, plus the dependency lock and an exact file manifest.
It remains self-contained for maintenance and rolls back with the root.
Uncached package sources still require their download servers.

## Releases

Pin published package-tree, magpkg, and bootstrap assets in `dependencies.json`,
run the integration tests, commit, and run `./scripts/release.sh VERSION HEAD`.
The script produces a deterministic source archive and SHA256SUMS in `out/`.
It includes the dependency lock, with no package tree, Git metadata, VM disks,
credentials, or build output. Publish those exact files as GitHub release
assets; keep existing versions unchanged. Source releases are usable with
`make prepare` and the same commands above.

The original monorepo history remains in this repository. The package manager
and tree repositories retain their corresponding extracted histories.
