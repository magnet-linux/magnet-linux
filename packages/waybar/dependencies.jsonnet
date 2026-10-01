local b = (import '../hyprland/build.libsonnet') { sources+: import './sources.libsonnet' };
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local mmOptions = '-Dmaintainer-mode=false -Dbuild-documentation=false';
local sigc = b.meson('libsigc++', mmOptions + ' -Dbuild-examples=false -Dbuild-tests=false', [t.cxx_runtime]);
local glibmm = b.meson('glibmm', mmOptions + ' -Dbuild-examples=false', [sigc, l.glib]);
local cairomm = b.meson('cairomm', mmOptions + ' -Dbuild-examples=false -Dbuild-tests=false', [sigc, l.cairo]);
local pangomm = b.meson('pangomm', mmOptions, [glibmm, cairomm, l.pango]);
local atkmm = b.meson('atkmm', mmOptions, [glibmm, common.atspi]);
local gtkmm = b.meson('gtkmm', mmOptions + ' -Dbuild-x11-api=false -Dbuild-demos=false -Dbuild-tests=false',
  [glibmm, cairomm, pangomm, atkmm, common.gtk]);
local fmt = b.cmake('fmt', '-DFMT_DOC=OFF -DFMT_TEST=OFF');
local spdlog = b.cmake('spdlog', '-DSPDLOG_FMT_EXTERNAL=ON -DSPDLOG_BUILD_EXAMPLE=OFF -DSPDLOG_BUILD_TESTS=OFF', [fmt]);
local jsoncpp = b.cmake('jsoncpp', '-DJSONCPP_WITH_TESTS=OFF -DJSONCPP_WITH_POST_BUILD_UNITTEST=OFF -DBUILD_STATIC_LIBS=OFF -DBUILD_OBJECT_LIBS=OFF');
local layer = b.meson('gtk-layer-shell', '-Dintrospection=false -Dvapi=false -Ddocs=false -Dexamples=false -Dtests=false',
  [common.gtk, l.wayland], [l.protocols]);
// Supplement libxkbcommon without changing the shared keyboard library's ID.
local registry = b.meson('libxkbcommon',
  '-Denable-x11=false -Denable-tools=false -Denable-docs=false -Denable-xkbregistry=true -Dxkb-config-root=/share/X11/xkb',
  [l.xkbcommon, common.xml], [(import '../kernel-tools.jsonnet').bison, (import '../kernel-tools.jsonnet').m4], '', |||
    mkdir -p /build/registry/lib/pkgconfig /build/registry/include/xkbcommon
    cp -a /out/lib/libxkbregistry.so* /build/registry/lib/
    cp /out/lib/pkgconfig/xkbregistry.pc /build/registry/lib/pkgconfig/
    cp /out/include/xkbcommon/xkbregistry.h /build/registry/include/xkbcommon/
    find /out -mindepth 1 -delete
    cp -a /build/registry/. /out/
  |||) { name: 'xkbregistry-' + (import '../desktop-sources.libsonnet').libxkbcommon.version };
{
  gtkmm: gtkmm, fmt: fmt, spdlog: spdlog, jsoncpp: jsoncpp,
  layer_shell: layer, xkbregistry: registry,
}
