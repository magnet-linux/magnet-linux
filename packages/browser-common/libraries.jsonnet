local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local compiler = import './toolchain.jsonnet';
// Shared Linux audio and MIDI library.
local alsa = b.package('alsa', |||
  ./configure --prefix=/ --libdir=/lib --sysconfdir=/etc \
    --disable-static --disable-python
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);
// The existing Cairo is sufficient for Sway but omits these GTK bindings.
// Install only the additional files, preserving the shared Cairo package.
local cairo_gobject = b.meson('cairo',
  '-Dtests=disabled -Dgtk_doc=false -Dxlib=disabled -Dxcb=disabled -Dglib=enabled -Dzlib=enabled -Dfontconfig=enabled -Dfreetype=enabled',
  [l.cairo, l.glib], [], '', |||
    mkdir -p /build/bindings/lib/pkgconfig /build/bindings/include/cairo
    cp -a /out/lib/libcairo-gobject.so* /build/bindings/lib/
    cp /out/lib/pkgconfig/cairo-gobject.pc /build/bindings/lib/pkgconfig/
    cp /out/include/cairo/cairo-gobject.h /build/bindings/include/cairo/
    # The shared Cairo's feature header predates this separately installed
    # binding. Advertise it in its own header without replacing that header.
    python3 - <<'PY'
    from pathlib import Path
    header = Path('/build/bindings/include/cairo/cairo-gobject.h')
    header.write_text(header.read_text().replace(
        '#if CAIRO_HAS_GOBJECT_FUNCTIONS',
        '#ifndef CAIRO_HAS_GOBJECT_FUNCTIONS\n#define CAIRO_HAS_GOBJECT_FUNCTIONS 1\n#endif\n#if CAIRO_HAS_GOBJECT_FUNCTIONS'))
    PY
    find /out -mindepth 1 -delete
    cp -a /build/bindings/. /out/
  |||) { name: 'cairo-gobject-1.18.4' };
local xml = b.package('libxml2', |||
  ./configure --prefix=/ --libdir=/lib --without-python --without-lzma \
    --without-zlib --without-readline --disable-static
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);
local dbus = b.meson('dbus',
  '-Dapparmor=disabled -Dselinux=disabled -Dsystemd=disabled -Dx11_autolaunch=disabled -Dmodular_tests=disabled -Ddoxygen_docs=disabled -Dducktype_docs=disabled -Dxml_docs=disabled -Dqt_help=disabled -Druntime_dir=/run',
  [l.expat]);
local atspi = b.meson('atspi',
  '-Dx11=disabled -Dintrospection=disabled -Ddocs=false -Duse_systemd=false -Dgtk2_atk_adaptor=false -Ddbus_daemon=/bin/dbus-daemon',
  [l.glib, dbus, xml]);
local glvnd = b.meson('libglvnd', '-Dx11=disabled -Dglx=disabled -Dhgl=false -Dgles1=false');
local epoxy = b.meson('epoxy', '-Dx11=false -Dglx=no -Degl=yes -Dtests=false -Ddocs=false',
  [glvnd]);
local jpeg = b.package('jpeg', |||
  cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX=/ -DCMAKE_INSTALL_LIBDIR=/lib \
    -DCMAKE_INSTALL_INCLUDEDIR=/include -DCMAKE_INSTALL_BINDIR=/bin \
    -DCMAKE_INSTALL_DATAROOTDIR=/share -DENABLE_STATIC=OFF
  ninja -C build -j"$BUILD_PARALLELISM"
  DESTDIR=/out cmake --install build
|||, [], [t.cmake, t.ninja, compiler.nasm]);
local pixbuf = b.meson('gdk-pixbuf',
  '-Dintrospection=disabled -Dman=false -Dtests=false -Dinstalled_tests=false -Dtiff=disabled -Dbuiltin_loaders=all -Dgio_sniffing=false',
  [l.glib, l.libpng, jpeg]);
local curl = b.package('curl', |||
  ./configure --prefix=/ --libdir=/lib --enable-shared --disable-static \
    --with-openssl=/ --with-zlib=/ --without-brotli --without-zstd \
    --without-libpsl --without-libidn2 --without-libssh --without-libssh2 \
    --without-nghttp2 --without-nghttp3 --without-ngtcp2 --without-quiche \
    --disable-ldap --disable-ldaps --disable-docs --disable-manual \
    --with-ca-bundle=/etc/ssl/certs/ca-certificates.crt --without-ca-path
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  install -Dm644 COPYING /out/share/licenses/curl/COPYING
  LD_LIBRARY_PATH=/out/lib /out/bin/curl --version
  printf 'curl transfer check\n' > /build/curl-check.txt
  LD_LIBRARY_PATH=/out/lib /out/bin/curl --fail --silent \
    file:///build/curl-check.txt > /build/curl-result.txt
  cmp /build/curl-check.txt /build/curl-result.txt
|||, [l.zlib, (import '../openssl.jsonnet').openssl,
      (import '../ca-certificates.jsonnet').ca_certificates]);
local pci = b.package('pciutils', |||
  make -j"$BUILD_PARALLELISM" PREFIX=/ IDSDIR=/share/hwdata \
    SHARED=yes ZLIB=no DNS=no LIBKMOD=no HWDB=no lspci setpci
  make PREFIX=/ IDSDIR=/share/hwdata SHARED=yes ZLIB=no DNS=no \
    LIBKMOD=no HWDB=no DESTDIR=/out install-lib
  install -Dm755 lspci /out/bin/lspci
  install -Dm755 setpci /out/bin/setpci
  install -Dm644 COPYING /out/share/licenses/pciutils/COPYING
  LD_LIBRARY_PATH=/out/lib /out/bin/lspci --version
|||, [l.hwdata]);
local gtk = b.meson('gtk',
  '-Dx11_backend=false -Dwayland_backend=true -Dbroadway_backend=false -Dprint_backends=file -Dintrospection=false -Dgtk_doc=false -Dman=false -Ddemos=false -Dexamples=false -Dtests=false -Dcolord=no',
  [l.glib, l.pango, cairo_gobject, l.fribidi, l.wayland, l.xkbcommon, pixbuf, atspi, epoxy],
  [l.protocols], '', |||
    # Meson skips cache generation under DESTDIR. Include the cache in the
    # package so a fresh installation does not need an imperative post-install.
    glib-compile-schemas /out/share/glib-2.0/schemas
  |||);
{ dbus: dbus, atspi: atspi, glvnd: glvnd, epoxy: epoxy, xml: xml, alsa: alsa,
  jpeg: jpeg, pixbuf: pixbuf, gtk: gtk, pci: pci, curl: curl }
