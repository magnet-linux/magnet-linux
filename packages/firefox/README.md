# Firefox

This directory contains Firefox ESR and its browser-specific build tools.
Shared libraries and native toolchains live in [browser-common](../browser-common/README.md).
They install globally into the normal `/bin`, `/lib`, and `/share` layout.
Existing Wayland, font, GLib, Cairo, Pango, compression, and C++ runtime packages
are reused. The headless and basic desktop selections do not include Firefox.

`firefox.jsonnet` builds Firefox 140.17.0esr. `../browser-common/libraries.jsonnet`
supplies GTK 3 and supporting libraries; `toolchain.jsonnet` adds Firefox's
cbindgen, Autoconf 2.13 and WASI tools to the shared native toolchain.
`sources.libsonnet` pins archives by SHA-256, and `cbindgen-crates.libsonnet`
pins the registry archives from cbindgen's Cargo.lock. Builds run without
network access. The musl patches retain their upstream references in `patches`.

The compiler chain uses upstream Rust 1.88.0 binaries for x86_64 Linux musl
only as a bootstrap. Rust 1.89.0 and Cargo are then built from source against
source-built LLVM/Clang 20.1.8. Firefox and cbindgen use that rebuilt compiler.
The native Rust target defaults to shared musl, matching the installed system.
WASI libc, libc++, and compiler builtins are also source-built for Firefox's RLBox
library sandboxing. Build tools are not Firefox runtime dependencies.

Build on a host with substantial disk space and memory; the default 24 GiB VM
disk is intended for installing the result, not for this full compiler build:

```sh
nix-shell magnet-linux/shell.nix
magnet-linux/_work/magpkg build --store magnet-linux/_work/store \
  --jobs 1 --parallelism 16 packages/firefox/firefox.jsonnet firefox
```

Use the `desktop-browser` root in `magnet-linux/desktop-hyprland.jsonnet` when
syncing an installation. A sync of just `firefox.jsonnet` against an existing
system root would remove the other magpkg-managed packages. Follow the staged
root update procedure in `magnet-linux/README.md` with the browser selection.

The first version targets native Wayland and software rendering. Audio uses
the PulseAudio client backend, served by PipeWire in the Hyprland profile.
WebRTC remains disabled. Browser
and library sandboxing remain enabled. Firefox's built-in updater is disabled;
security updates must be delivered by updating the package version and hash.
Profiles and downloads remain ordinary user files.

To create a VM with Firefox and graphical login:

```sh
WITH_FIREFOX=1 ./magnet-linux/create-desktop-vm.sh magnet-linux/out/browser-vm browser
./magnet-linux/run-vm.sh magnet-linux/out/browser-vm
```

Read the private `credentials.txt`, log in as `browser`, and launch Firefox
from Fuzzel or a terminal. Shut down with `doas poweroff`.

The graphical test boots the VM in a private Xvfb/QEMU display, loads
`https://example.com`, and exercises JavaScript, fetch, and Canvas 2D:

```sh
nix-shell magnet-linux/shell.nix --run \
  'TEST_BROWSERS=firefox magnet-linux/tests/session-boot.sh magnet-linux/out/browser-vm'
```

Stop the VM before running the test. Screenshots and logs are retained under
`magnet-linux/out/session-test.*`. Add `TEST_AUDIO=1` to verify playback in
QEMU's recorded output. GPU acceleration and WebRTC remain disabled.
