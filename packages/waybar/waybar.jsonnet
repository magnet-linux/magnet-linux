local b = (import '../hyprland/build.libsonnet') { sources+: import './sources.libsonnet' };
local d = import './dependencies.jsonnet';
local l = import '../desktop-libs.jsonnet';
{
  waybar: b.meson('Waybar',
    '-Dauto_features=disabled -Dlibudev=enabled -Dpulseaudio=enabled -Dlibnl=enabled -Dniri=false -Dlogin-proxy=false',
    [d.gtkmm, d.fmt, d.spdlog, d.jsoncpp, d.layer_shell, d.xkbregistry,
     l.wayland, l.eudev, (import '../tzdata.jsonnet').tzdata,
     (import '../audio/libraries.jsonnet').pulse,
     (import '../network/libraries.jsonnet').libnl],
    [l.protocols],
    // The 0.15 release predates the Lua IPC used by our Hyprland package.
    "patch -p1 <<'MAGNET_PATCH'\n" + (importstr './hyprland-lua.patch') + "\nMAGNET_PATCH\n", |||
      # setup-login.sh provisions /etc once; package updates preserve edits.
      mkdir -p /out/share/waybar
      mv /out/etc/xdg/waybar /out/share/waybar/defaults
      rmdir /out/etc/xdg /out/etc
      install -Dm644 LICENSE /out/share/licenses/waybar/LICENSE
      /out/bin/waybar --version
    |||),
}
