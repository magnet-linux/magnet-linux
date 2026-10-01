# Hyprland

`hyprland.jsonnet` builds Hyprland 0.56.2 for x86_64 musl, with Aquamarine,
the Hyprland libraries, Lua configuration, and native Wayland applications.
Sources and Cargo crates are pinned by SHA-256; package builds are offline.
The upstream release bundle includes the required submodules. A small patch
replaces `ranges::starts_with` with equivalent prefix comparison for GCC 15's
standard library.

```sh
nix-shell magnet-linux/shell.nix --run \
  'magnet-linux/_work/magpkg build --store magnet-linux/_work/store --jobs 3 --parallelism 8 packages/hyprland/hyprland.jsonnet hyprland'
```

The additional libraries live in `dependencies.jsonnet`. Existing desktop
and browser libraries are reused. Mesa supplements Chromium's existing GBM
package with EGL, GLES, VirGL and a software driver, preserving that package's
identity. GLVND owns the public GL libraries and headers. Xcursor libraries
support cursor themes; no X server is installed.

Librsvg and cargo-c reuse the existing source-built Rust toolchain. The
compiler, Cargo dependencies, protocol generators and development build tools
are build dependencies rather than desktop applications.

The initial profile omits Xwayland, systemd integration, UWSM and the plugin
manager. It supplies no panel, audio services, screen locker or desktop portal.
See [the desktop guide](../../magnet-linux/DESKTOP.md) for the complete system
selection, QEMU setup, manual configuration and graphical session test.
