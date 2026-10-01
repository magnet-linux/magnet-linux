local b = import './desktop-build.libsonnet';
local l = import './desktop-libs.jsonnet';
{
  fuzzel: b.meson('fuzzel',
    '-Denable-cairo=disabled -Dpng-backend=libpng -Dsvg-backend=nanosvg -Dsystem-nanosvg=disabled',
    [l.wayland, l.xkbcommon, l.pixman, l.fontconfig, l.fcft, l.libpng],
    [l.protocols, l.tllist], |||
      # Upstream requires scdoc with no docs option; ship the manual sources.
      sed -i "/^subdir('doc')$/d" meson.build
    |||, |||
      # Provision /etc separately so package sync preserves manual edits.
      mkdir -p /out/share/fuzzel
      mv /out/etc/xdg/fuzzel/fuzzel.ini /out/share/fuzzel/fuzzel.ini.example
      rmdir /out/etc/xdg/fuzzel /out/etc/xdg /out/etc
      install -m644 doc/*.scd /out/share/doc/fuzzel/
      install -Dm644 3rd-party/nanosvg/LICENSE.txt /out/share/licenses/fuzzel/nanosvg.txt
      /out/bin/fuzzel --version
    |||),
}
