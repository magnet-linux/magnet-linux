# Packages

The minimal bootable system, configuration skeleton, manual installation
procedure, and headless QEMU test live in [magnet-linux](../magnet-linux/README.md).

`doas.jsonnet` builds OpenDoas 6.8.2 with the shared Linux-PAM package. The base
system installs `/bin/doas` as root-owned mode 4755. Editable authorization and
PAM files are provisioned separately; the initial rule requires the invoking
`wheel` member's password to run a command as root. Timestamp caching is not
compiled in.

`fuzzel.jsonnet` builds the native Wayland application launcher for the Hyprland
profile. It shares Foot's libraries, supports PNG and bundled NanoSVG icons,
and also provides `fuzzel --dmenu` for piped choices. The upstream example and
manual sources are installed under `/share`; the distro skeleton provisions
editable `/etc/xdg/fuzzel/fuzzel.ini` defaults separately.

`adwaita-cursors.jsonnet` installs the Xcursor assets and compatibility aliases
from GNOME's hash-pinned Adwaita 48.1 release, without the application icons.
The graphical session selects Adwaita for both the desktop and login screen.

The [audio recipes](audio/README.md) provide PipeWire, WirePlumber, PulseAudio
client compatibility and playback/recording tools for the graphical session.

The [network recipes](network/README.md) provide NetworkManager, nmcli, nmtui,
wpa_supplicant and polkit for desktop networking under perp.

`chrony.jsonnet` provides chronyd and chronyc for both the headless and desktop
systems, with `libcap.jsonnet` for dropping privileges. This initial build is
an ordinary NTP client; NTS is not enabled. The skeleton provisions an editable
`/etc/chrony.conf` and a perp service, with persistent state in `/var/lib/chrony`.

`notifications.jsonnet` provides Mako, its standalone basu D-Bus library, and
libnotify (including `notify-send`). It shares the existing Wayland, Pango,
Cairo and GdkPixbuf libraries and requires no systemd or elogind daemon.
Hyprland starts it in the user's D-Bus session, with activation available for
recovery. `/etc/xdg/mako/config` supplies the initial appearance and timeouts.

## Bootstrap hello

`hello.jsonnet` builds a statically linked hello-world executable using the
reproducible bootstrap root:

```sh
go -C magpkg run ./cmd/magpkg init
go -C magpkg run ./cmd/magpkg build \
  --ext-str "bootstrap-url=file://$PWD/bootstrap/out/bootstrap.tar.zst" \
  ../magnet-linux/seed-store.jsonnet seed
go -C magpkg run ./cmd/magpkg build ../packages/hello.jsonnet
```

The command prints the resulting package archive path. The executable is
stored as `bin/hello` inside that archive.

## Magpkg

`magpkg.jsonnet` builds the Go implementation from public commit
`245ac58457491cb9721edfeb3e06402605ba1a20` (on the `gowip` branch):

```sh
go -C magpkg run ./cmd/magpkg build ../packages/magpkg.jsonnet magpkg
```

The result is a static `/bin/magpkg`. Its runtime dependencies include Bash and GNU shell utilities,
bubblewrap, and Mozilla CA certificates for HTTPS downloads. The distro selects
it in its base system; `magnet-linux/setup-tree.sh` separately installs editable
recipes, a system selection, and the bootstrap seed.
The running kernel must support the namespaces required by bubblewrap.

The GitHub commit tarball is pinned by SHA-256. GitHub can change its archive
compression without changing the source; we accept that limitation for now and
leave a TODO beside the fetch to mirror the exact bytes or publish a source
release asset. A changed archive fails verification.

`go.jsonnet` currently packages the official Go 1.26.4 Linux/amd64 binary
toolchain as a build dependency; rebuilding Go from source is a follow-up.
`magpkg-modules.libsonnet` pins the raw `.mod` and `.zip` downloads for the
commit's module graph. The recipe assembles a local file proxy, checks the
modules against the source's `go.sum`, runs the Go tests, and builds without
network access. Updating the source pin requires refreshing the module manifest
from that commit's `go mod download -json all` output and hashing the referenced
`GoMod` and `Zip` files. Uppercase module path characters use Go's `!lowercase`
proxy escaping.

## Core

`core.jsonnet` rebuilds the core toolchain and userland from source, using the
bootstrap only as the seed environment. In particular, musl, binutils, and GCC
are normal packages rather than permanent parts of the seed.

The package tree uses a root-prefix filesystem layout. Executables, libraries,
headers, and architecture-independent data are installed in `/bin`, `/lib`,
`/include`, and `/share`. The `root-layout` package supplies the filesystem
skeleton and compatibility links `/usr -> .` and `/sbin -> bin`. The original
bootstrap archive remains unchanged; `bootstrap-tools` normalizes it into this
layout before it is used by regular builds.

Packages also apply this tree's build-environment policy explicitly, overriding
the default `PATH` with `/bin`. Other trees can use the default environment or
override any value through a package's `buildEnv` object.

Source archives are extracted with `--no-same-owner`. This also permits builds
invoked as root: bubblewrap maps only the build user's UID/GID, so ownership
IDs recorded by upstream release archives need not exist in the sandbox.

Each exported field is a build root. For example, this rebuilds GCC and all of
its prerequisites:

```sh
go -C magpkg run ./cmd/magpkg build \
  --jobs 1 \
  --parallelism "$(nproc)" \
  ../packages/core.jsonnet gcc
```

The other roots are `make`, `musl`, `musl_rt`, `binutils`, `coreutils`,
`gawk`, `sed`, `findutils`, `diffutils`, `pkgconfig`, `bash`, `gzip`, `xz`,
`tar`, `grep`, `libgcc_rt`, and `libstdcpp_rt`. Separate build invocations can
share the same store: package locks prevent duplicate publication while an
executor continues with any other ready branch.

## Shell environment

`shell.jsonnet` demonstrates a package-defined development shell. Its runtime
closure supplies the bootstrap tools and an executable launcher at
`/libexec/magpkg-shell`. Magpkg materializes and leases the closure, while the
launcher controls bubblewrap mounts, isolation, and the default interactive
shell:

```sh
go -C magpkg run ./cmd/magpkg shell ../packages/shell.jsonnet shell
```

A command after `--` is passed unchanged to the launcher:

```sh
go -C magpkg run ./cmd/magpkg shell \
  ../packages/shell.jsonnet shell -- cc --version
```

Package-provided launchers execute on the host with the invoking user's
permissions before creating their sandbox. Only trusted packages and binary
caches should be used for shell environments.

## Wayland desktop

The desktop is selected by `magnet-linux/desktop-hyprland.jsonnet`.
[Hyprland](hyprland/README.md) supplies the compositor and Mesa graphics;
[login](login/README.md) supplies greetd and its GTK greeter. Foot, Waybar,
Fuzzel, notifications, audio, and NetworkManager complete the desktop.
Shared musl libraries are defined in `desktop-libs.jsonnet`; native build
tools live in `desktop-tools.jsonnet`. Sources are pinned by SHA-256.

See [the desktop guide](../magnet-linux/DESKTOP.md) for installation,
configuration, and graphical QEMU tests.

The optional [Firefox recipes](firefox/README.md) and
[Chromium recipes](chromium/README.md) live in their own directories.
[browser-common](browser-common/README.md) supplies shared GTK libraries and
native Rust/LLVM build recipes. Browser-specific patches and tools remain
with their browser. Everything installs into the normal global layout.
The desktop roots are `desktop` (no browser), `desktop-browser` (Firefox),
`desktop-chromium`, and `desktop-browsers` (both browsers).
