local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local native = import '../chromium/toolchain.jsonnet';
local gbm = (import '../chromium/dependencies.jsonnet').gbm;
local protocols = b.meson('wayland-protocols', '-Dtests=false', [], [l.wayland]);
// Supplement the existing GBM-only package, keeping its identity (and browser
// artifacts) unchanged. GLVND owns the public GL entry points and headers.
local mesa = b.meson('mesa',
  '-Dplatforms=wayland -Dgallium-drivers=virgl,softpipe -Dvulkan-drivers= -Dgbm=enabled -Degl=enabled -Dglx=disabled -Dopengl=true -Dgles1=disabled -Dgles2=enabled -Dglvnd=enabled -Dllvm=disabled -Dvideo-codecs= -Dgallium-rusticl=false -Dbuild-tests=false',
  [gbm, common.glvnd, t.cxx_runtime], gbm.buildDeps + [protocols,
    (import '../kernel-tools.jsonnet').bison, (import '../kernel-tools.jsonnet').flex,
    (import '../kernel-tools.jsonnet').m4], '', |||
    rm -f /out/lib/libgbm.so* /out/lib/pkgconfig/gbm.pc /out/include/gbm.h
    install -Dm644 docs/license.rst /out/share/licenses/mesa-graphics/license.rst
  |||) { name: 'mesa-graphics-26.2.2' };
local pugixml = b.cmake('pugixml', '-DPUGIXML_BUILD_TESTS=OFF');
local toml = b.meson('tomlplusplus', '-Dbuild_tests=false -Dbuild_examples=false', [t.cxx_runtime], [t.cmake]);
local glaze = b.cmake('glaze', '-Dglaze_DEVELOPER_MODE=OFF -Dglaze_BUILD_EXAMPLES=OFF');
local glslang = b.cmake('glslang', '-DENABLE_OPT=OFF -DENABLE_GLSLANG_BINARIES=OFF -DGLSLANG_TESTS=OFF');
local abseil = b.cmake('abseil', '-DABSL_BUILD_TESTING=OFF -DABSL_ENABLE_INSTALL=ON -DCMAKE_CXX_STANDARD=17');
local re2 = b.cmake('re2', '-DRE2_BUILD_TESTING=OFF', [abseil]);
local muparser = b.cmake('muparser', '-DENABLE_OPENMP=OFF -DENABLE_SAMPLES=OFF');
local lcms = b.autotools('lcms2', '--without-jpeg --without-tiff');
local libzip = b.cmake('libzip', '-DBUILD_TOOLS=OFF -DBUILD_REGRESS=OFF -DBUILD_EXAMPLES=OFF -DBUILD_DOC=OFF -DENABLE_BZIP2=OFF -DENABLE_LZMA=OFF -DENABLE_ZSTD=OFF -DENABLE_OPENSSL=OFF -DENABLE_GNUTLS=OFF -DENABLE_MBEDTLS=OFF', [l.zlib]);
local magic = b.autotools('file', '--disable-bzlib --disable-xzlib --disable-zstdlib --disable-lzlib --disable-libseccomp', [l.zlib]);
local webp = b.cmake('libwebp', '-DWEBP_BUILD_ANIM_UTILS=OFF -DWEBP_BUILD_CWEBP=OFF -DWEBP_BUILD_DWEBP=OFF -DWEBP_BUILD_GIF2WEBP=OFF -DWEBP_BUILD_IMG2WEBP=OFF -DWEBP_BUILD_VWEBP=OFF -DWEBP_BUILD_WEBPINFO=OFF -DWEBP_BUILD_LIBWEBPMUX=OFF -DWEBP_BUILD_EXTRAS=OFF');
local uuid = (import '../util-linux.jsonnet').uuid;
local readline = b.autotools('readline', '--with-curses --with-shared-termcap-library', [l.ncurses]);
local lua = b.package('lua', |||
  cd src
  make -j"$BUILD_PARALLELISM" liblua.a MYCFLAGS='-O2 -fPIC -DLUA_USE_LINUX'
  gcc -shared -Wl,-soname,liblua5.5.so.0 -o liblua5.5.so.0 \
    -Wl,--whole-archive liblua.a -Wl,--no-whole-archive -lm -ldl
  install -Dm755 liblua5.5.so.0 /out/lib/liblua5.5.so.0
  ln -s liblua5.5.so.0 /out/lib/liblua5.5.so
  mkdir -p /out/include/lua5.5 /out/lib/pkgconfig
  cp lua.h luaconf.h lualib.h lauxlib.h lua.hpp /out/include/lua5.5/
  cat > /out/lib/pkgconfig/lua5.5.pc <<'EOF'
  Name: Lua
  Description: Lua language library
  Version: 5.5.1
  Libs: -L/lib -llua5.5
  Libs.private: -lm -ldl
  Cflags: -I/include/lua5.5
  EOF
|||);
local cargo_c = b.package('cargo-c', (importstr './vendor.sh') + |||
  export OPENSSL_DIR=/ OPENSSL_STATIC=1
  cargo build --release --locked --offline -j"$BUILD_PARALLELISM"
  for binary in cargo-cbuild cargo-cinstall cargo-ctest; do
    install -Dm755 "target/release/$binary" "/out/bin/$binary"
  done
|||, [t.cxx_runtime], [native.rust, t.python, t.cmake, (import '../openssl.jsonnet').openssl]) {
  fetch+: import './cargo-c-crates.libsonnet',
};
local rsvg = b.meson('librsvg', '-Dintrospection=disabled -Dpixbuf=enabled -Dpixbuf-loader=disabled -Drsvg-convert=disabled -Ddocs=disabled -Dvala=disabled -Dtests=false -Davif=disabled',
  [common.gtk, common.xml], [native.rust, cargo_c], importstr './vendor.sh') {
  fetch+: import './librsvg-crates.libsonnet',
};
local jinja = b.package('jinja2', |||
  mkdir -p /out/lib/python3.13/site-packages
  cp -a src/jinja2 /out/lib/python3.13/site-packages/
|||, [(import '../chromium/dependencies.jsonnet').markupsafe]);
local eis = b.meson('libeis', '-Dtests=disabled -Dliboeffis=disabled -Ddocumentation=[]',
  [l.libevdev, l.xkbcommon], [t.python, jinja]);
// Xcursor is used for cursor themes even in a native Wayland-only session.
local xproto = b.autotools('xorgproto', '--disable-specs');
local xau = b.autotools('libXau', '', [], [xproto]);
local xdmcp = b.autotools('libXdmcp', '', [], [xproto]);
local xcbproto = b.autotools('xcb-proto', '', [], [t.python]);
local xcb = b.autotools('libxcb', '--disable-devel-docs', [xau, xdmcp], [xcbproto, xproto, t.python]);
local xtrans = b.autotools('xtrans');
local x11 = b.autotools('libX11', '--disable-specs --disable-xf86bigfont', [xcb], [xproto, xtrans], '', |||
  # Native Wayland packages already provide Compose/locale data.
  rm -rf /out/share/X11/locale
|||);
local xrender = b.autotools('libXrender', '', [x11], [xproto]);
local xfixes = b.autotools('libXfixes', '', [x11], [xproto]);
local xcursor = b.autotools('libXcursor', '', [xrender, xfixes], [xproto]);
{
  protocols: protocols, mesa: mesa, pugixml: pugixml, toml: toml, glaze: glaze,
  glslang: glslang, re2: re2, muparser: muparser, lcms: lcms, libzip: libzip,
  magic: magic, webp: webp, uuid: uuid, readline: readline, lua: lua,
  cargo_c: cargo_c, rsvg: rsvg, eis: eis, xcursor: xcursor, xproto: xproto,
}
