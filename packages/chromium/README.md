# Chromium

`chromium.jsonnet` builds Chromium 152.0.7977.82 for x86_64 Linux musl and
installs `/bin/chromium`, its private resources under `/lib/chromium`, and a
desktop entry. The recipes use the shared GTK/Wayland libraries from
`../browser-common/` and `../desktop-libs.jsonnet`.

The initial configuration targets native Wayland and software rendering.
CUPS printing, Kerberos single sign-on, GPU acceleration, WebGPU and PipeWire
screen capture are disabled. Audio uses ALSA and the desktop's PipeWire ALSA
plugin; the PulseAudio backend is disabled in this build.
The browser sandbox remains enabled. Chromium must run as an unprivileged
desktop user; the launcher rejects root instead of adding `--no-sandbox`.
No Google API credentials or proprietary codecs are included.
The Hyprland desktop provides D-Bus and Mesa graphics; Chromium currently
uses its software rendering path.

## Build

```sh
nix-shell magnet-linux/shell.nix
magnet-linux/_work/magpkg build --store magnet-linux/_work/store \
  --jobs 1 --parallelism 16 packages/chromium/chromium.jsonnet chromium
```

LLVM/Clang/LLD 22.1.8, compiler builtins, Rust/Cargo 1.97.0, GN, bindgen and
esbuild are built from source. Rust uses upstream musl Rust 1.96.1 only as
its bootstrap. The existing Go bootstrap builds esbuild. Chromium builds its
bundled native libc++ and most internal libraries; NSS/NSPR and Mesa GBM are
separate runtime packages. Compiler tools are build-only dependencies.

The default 24 GiB disk / 4 GiB RAM desktop VM is sized for running browsers.
A full compiler and browser source rebuild needs substantially more disk and
RAM; use a larger guest or build on the host.

Archives and additional Cargo/Go dependencies are SHA-256 pinned; builds run
without network access. The Chromium Linux source archive is the Gentoo
maintainer's release archive also used by Alpine, checked against Alpine's
published SHA-512 before recording its SHA-256. Rollup's pinned portable WASM
distribution replaces the bundled glibc addon for the DevTools build. Musl
patch provenance is recorded in [patches/README.md](patches/README.md).

## Install and test

Select `desktop-chromium` or `desktop-browsers` from
`magnet-linux/desktop-hyprland.jsonnet`. Use the complete system
selection when syncing; syncing just the browser into an existing system would
remove other managed packages. Follow the staged update instructions in the
[distro guide](../../magnet-linux/README.md).

To create a new VM with an unprivileged desktop account:

```sh
WITH_CHROMIUM=1 ./magnet-linux/create-desktop-vm.sh magnet-linux/out/chromium-vm browser
./magnet-linux/run-vm.sh magnet-linux/out/chromium-vm
```

Read the private `credentials.txt`, log in as `browser`, and launch Chromium
from Fuzzel or a terminal. Add `WITH_FIREFOX=1` to install both browsers.
Shut down with `doas poweroff`, using your own login password.

The graphical test uses a private Xvfb display, QEMU, scrot and xdotool:

```sh
nix-shell magnet-linux/shell.nix --run \
  'TEST_BROWSERS=chromium magnet-linux/tests/session-boot.sh magnet-linux/out/chromium-vm'
```

It loads an HTTPS page, exercises JavaScript/fetch/Canvas 2D, checks for sandbox
violations in its log, and powers off the VM. Artifacts are saved under
`magnet-linux/out/session-test.*`. Stop the VM before running the test.

Browser updates come through the package tree. Refresh the version, hashes and
matching patches regularly; profiles and downloads remain ordinary user files.
