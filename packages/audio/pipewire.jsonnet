local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local d = import './libraries.jsonnet';
local pipewire = b.meson('pipewire',
  '-Dauto_features=disabled -Dalsa=enabled -Dudev=enabled -Dudevrulesdir=/lib/udev/rules.d -Ddbus=enabled -Dsndfile=enabled -Dpw-cat=enabled -Dpipewire-alsa=enabled -Dpipewire-jack=disabled -Dpipewire-v4l2=disabled -Dexamples=disabled -Dtests=disabled -Dflatpak=disabled -Dsession-managers=[] -Drlimits-install=false -Dlegacy-rtkit=false',
  [common.alsa, common.dbus, l.eudev, d.sndfile], [], '', |||
    install -Dm644 COPYING /out/share/licenses/pipewire/COPYING
  |||);
local wireplumber = b.meson('wireplumber',
  '-Dauto_features=disabled -Dsystem-lua=true -Dsystem-lua-version=5.5 -Dtests=false -Ddbus-tests=false -Dsystemd-user-service=false',
  [pipewire, l.glib, (import '../hyprland/dependencies.jsonnet').lua], [], '', |||
    install -Dm644 LICENSE /out/share/licenses/wireplumber/LICENSE
  |||);
{ pipewire: pipewire, wireplumber: wireplumber }
