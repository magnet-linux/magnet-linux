local b = import './desktop-build.libsonnet';
local l = import './desktop-libs.jsonnet';
{
  foot: b.meson('foot',
    '-Ddocs=disabled -Dtests=true -Dterminfo=enabled -Dutmp-backend=none -Dgrapheme-clustering=enabled',
    [l.wayland, l.xkbcommon, l.pixman, l.fontconfig, l.fcft, l.utf8proc],
    [l.protocols, l.tllist, l.ncurses], '', |||
      meson test -C build --print-errorlogs
      # Ship the example without making manually edited configuration owned.
      mkdir -p /out/share/foot
      mv /out/etc/xdg/foot/foot.ini /out/share/foot/foot.ini.example
      rmdir /out/etc/xdg/foot /out/etc/xdg /out/etc
    |||),
}
