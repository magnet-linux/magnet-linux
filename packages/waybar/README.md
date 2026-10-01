# Waybar

`waybar.jsonnet` builds Waybar 0.15.0 for the Hyprland desktop. The dependencies
reuse the shared GTK, Wayland, font and graphics packages. New libraries live
in `dependencies.jsonnet`: GTK's C++ bindings, fmt, spdlog, jsoncpp,
gtk-layer-shell, and an xkbregistry supplement to the existing libxkbcommon.
All upstream archives are pinned by SHA-256 and builds run without network
access. The clock uses GCC's standard C++ timezone support and the source-built
IANA database from `packages/tzdata.jsonnet`.
The default clock explicitly uses the `C` locale: the musl toolchain's generic
C++ locale implementation does not accept the desktop's `C.UTF-8` locale name.

`hyprland-lua.patch` adapts the release's workspace click commands to the Lua
dispatcher API used by our pinned Hyprland 0.56.2. It quotes workspace names
before putting them in Lua expressions. This package targets that API; it is
not intended for older Hyprland versions. See upstream's corresponding
[IPC change](https://github.com/Alexays/Waybar/commit/05945748dccce28bf96d26d8f64a9e69a8dd49ba).

PulseAudio volume support connects to pipewire-pulse. Optional tray, network,
Bluetooth and systemd integrations are disabled
in this initial build. The configured modules are Hyprland workspaces, window
title, output volume and clock. No Sway runtime is required.

```sh
nix-shell magnet-linux/shell.nix --run \
  'magnet-linux/_work/magpkg build --store magnet-linux/_work/store --jobs 3 --parallelism 8 packages/waybar/waybar.jsonnet waybar'
```

`setup-login.sh` provisions `/etc/xdg/waybar/config.jsonc` and `style.css` only
when absent. User files in `~/.config/waybar/` take precedence. Hyprland starts
Waybar once on session startup; logging out terminates it with the session.
Its log is `~/.local/state/magnet-linux/waybar.log` (under `$XDG_STATE_HOME`
instead when set).
See [DESKTOP.md](../../magnet-linux/DESKTOP.md) for installation and the real
QEMU test, including clicking a workspace button and checking reserved space.
