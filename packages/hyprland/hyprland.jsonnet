local b = import './build.libsonnet';
local d = import './dependencies.jsonnet';
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local utils = b.cmake('hyprutils', '', [l.pixman]);
local lang = b.cmake('hyprlang', '', [utils]);
local scanner = b.cmake('hyprwayland-scanner', '', [d.pugixml]);
local wire = b.cmake('hyprwire', '', [utils, l.libffi, d.pugixml]);
local protocols = b.cmake('hyprland-protocols');
local graphics = b.cmake('hyprgraphics', '',
  [utils, d.mesa, l.cairo, l.pango, common.jpeg, d.webp, d.magic, l.libpng, d.rsvg]);
local cursor = b.cmake('hyprcursor', '', [lang, d.libzip, l.cairo, d.rsvg, d.toml]);
local aquamarine = b.cmake('aquamarine', '',
  [utils, d.mesa, l.seatd, l.libinput, l.wayland, l.pixman, l.libdrm, l.eudev, l.displayinfo, l.hwdata],
  [scanner, d.protocols]);
local hyprland = b.cmake('Hyprland', '-DNO_XWAYLAND=ON -DNO_SYSTEMD=ON -DNO_UWSM=ON -DNO_HYPRPM=ON',
  [aquamarine, lang, cursor, graphics, wire, l.xkbcommon, d.uuid, l.wayland,
   l.cairo, l.pango, l.pixman, d.xcursor, l.libdrm, l.libinput, d.eis, l.glib,
   d.re2, d.muparser, d.lcms, d.lua, d.readline, d.glslang],
  [d.glaze, scanner, protocols, d.protocols, d.xproto, t.python,
   (import '../browser-common/toolchain.jsonnet').bash],
  // GCC 15's libstdc++ lacks ranges::starts_with; preserve prefix semantics.
  "patch -p1 <<'MAGNET_PATCH'\n" + (importstr './gcc15-ranges.patch') + "\nMAGNET_PATCH\n", |||
    # Keep machine configuration outside the package manifest.
    rm -rf /out/etc
    install -Dm644 LICENSE /out/share/licenses/hyprland/LICENSE
  |||);
{ hyprland: hyprland, utils: utils, aquamarine: aquamarine }
