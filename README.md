# Magnet Linux

Magnet Linux is an experimental x86-64 musl distribution with a Btrfs root,
manual snapshots, sinit/perp, editable `/etc`, and an optional Hyprland desktop.
It currently targets UEFI QEMU machines. Installation and system maintenance
are explicit operations; package synchronization is performed on offline roots.

The project has three independently released repositories:

| Repository | Responsibility |
| --- | --- |
| [magpkg](https://github.com/magnet-linux/magpkg) | Package manager, Go libraries, tests, manuals, and static executable releases. |
| [packages](https://github.com/magnet-linux/packages) | Software recipes, patches, build helpers, and the bootstrap toolchain. |
| [magnet-linux](https://github.com/magnet-linux/magnet-linux) | This distro: profiles, kernel configuration, session policy, installer, `/etc` skeletons, and QEMU tests. |

## Start a desktop VM

Download and extract a distro source release, or clone this repository. A
release archive works without Git or a neighbouring checkout. Enter the host
tool environment and create a VM:

```sh
nix-shell magnet-linux/shell.nix
./magnet-linux/create-desktop-vm.sh
./magnet-linux/run-vm.sh
```

The creator prepares the exact release inputs in `dependencies.json`: a
package-tree archive, a static magpkg executable, and the bootstrap seed. It
verifies SHA-256 before extracting or using each download. Go and Git are not
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
nix-shell magnet-linux/shell.nix --run 'SSH_PORT=22228 magnet-linux/tests/install-boot.sh'
nix-shell magnet-linux/shell.nix --run \
  'WITH_FIREFOX=1 WITH_CHROMIUM=1 TEST_BROWSERS="firefox chromium" TEST_DOAS=1 SSH_PORT=22228 magnet-linux/tests/desktop-boot.sh'
```

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
