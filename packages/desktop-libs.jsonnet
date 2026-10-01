local b = import './desktop-build.libsonnet';
local tools = import './desktop-tools.jsonnet';
local kt = import './kernel-tools.jsonnet';

// Keep the existing static zlib used by the base packages unchanged.
local zlib = (import './zlib.jsonnet').shared;
local expat = b.autotools('expat', '--without-docbook --without-examples --without-tests --disable-static');
local libffi = b.autotools('libffi', '--disable-docs --disable-static');
local pcre2 = b.autotools('pcre2', '--enable-jit --disable-static');
local libpng = b.autotools('libpng', '--disable-static', [zlib]);
local freetype = b.autotools('freetype',
  '--disable-static --with-harfbuzz=no --with-brotli=no --with-bzip2=no', [zlib, libpng]);
local fontconfig = b.meson('fontconfig',
  '-Ddoc=disabled -Dnls=disabled -Dtests=disabled -Dtools=enabled -Dcache-build=disabled -Dxml-backend=expat',
  [freetype, expat], [tools.gperf], '', |||
    mkdir -p /out/share/fontconfig
    mv /out/etc/fonts /out/share/fontconfig/defaults
    rmdir /out/etc
  |||);
local pixman = b.meson('pixman', '-Dtests=disabled -Ddemos=disabled -Dgtk=disabled -Dlibpng=disabled');
local wayland = b.meson('wayland', '-Dtests=false -Ddocumentation=false -Ddtd_validation=false', [libffi, expat]);
local protocols = b.meson('wayland-protocols', '-Dtests=false', [], [wayland]);
local xkeyboard = b.meson('xkeyboard-config', '-Dnls=false', [], [kt.perl]);
// libxkbcommon uses these Compose tables even for native Wayland applications.
// Install just the locale data from libX11, without the X11 libraries/server.
local compose = b.package('xlocale', |||
  destination=/out/share/X11/locale
  mkdir -p "$destination"
  preprocess() {
    cpp -P -traditional-cpp -undef -DWCHAR32=1 "$1" |
      sed -e 's/^[[:space:]]*XCOMM/#/' -e 's/^[[:space:]]*XHASH/#/' \
        -e 's,X11_LOCALEDATADIR,/share/X11/locale,g' -e '/^$/d'
  }
  for index in locale.alias compose.dir; do
    preprocess "nls/$index.pre" > "$index.preprocessed"
    # Upstream writes both modern and legacy colon-separated index forms.
    sed -e '/^[^#][^[:space:]]*:/s/://' -e '/^[^#].*[[:space:]].*:/d' \
      "$index.preprocessed" > "$destination/$index"
    cat "$index.preprocessed" >> "$destination/$index"
  done
  find nls -name Compose.pre -type f | while IFS= read -r source; do
    directory=${source%/Compose.pre}
    directory=${directory#nls/}
    mkdir -p "$destination/$directory"
    preprocess "$source" > "$destination/$directory/Compose"
  done
|||) { name: 'xcompose-data-1.8.12' };
local xkbcommon = b.meson('libxkbcommon',
  '-Denable-x11=false -Denable-tools=false -Denable-docs=false -Denable-xkbregistry=false -Dxkb-config-root=/share/X11/xkb',
  [xkeyboard, compose], [kt.bison, kt.m4]);
local libdrm = b.meson('libdrm', '-Dauto_features=disabled -Dtests=false');
local hwdata = b.package('hwdata', |||
  ./configure --prefix=/ --datadir=/share
  make hwdata.pc
  mkdir -p /out/share/hwdata /out/share/pkgconfig
  cp pci.ids usb.ids pnp.ids oui.txt iab.txt /out/share/hwdata/
  cp hwdata.pc /out/share/pkgconfig/
|||);
local displayinfo = b.meson('libdisplay-info', '', [], [hwdata]);
local seatd = b.meson('seatd', '-Dlibseat-logind=disabled -Dlibseat-builtin=disabled -Dman-pages=disabled');
local eudev = b.autotools('eudev',
  // Spell both paths identically: eudev's install hook compares their strings
  // before linking udevadm, and /bin vs //bin would replace it with a self-link.
  '--bindir=/bin --sbindir=/bin --disable-introspection --disable-manpages --disable-selinux --disable-hwdb --disable-static',
  [], [tools.gperf], '', |||
    test ! -L /out/bin/udevadm
    /out/bin/udevadm --version
  |||);
local libevdev = b.meson('libevdev', '-Dtests=disabled -Ddocumentation=disabled');
local mtdev = b.autotools('mtdev', '--disable-static');
local libinput = b.meson('libinput',
  '-Dlibwacom=false -Ddebug-gui=false -Dtests=false -Ddocumentation=false',
  [eudev, libevdev, mtdev]);
local glib = b.meson('glib',
  '-Dtests=false -Dinstalled_tests=false -Dintrospection=disabled -Dnls=disabled -Dlibmount=disabled -Dselinux=disabled -Dlibelf=disabled -Dman-pages=disabled -Dglib_debug=disabled',
  [libffi, pcre2, zlib]);
local harfbuzz = b.meson('harfbuzz',
  '-Dtests=disabled -Dutilities=disabled -Ddocs=disabled -Dintrospection=disabled -Dglib=disabled -Dgobject=disabled -Dcairo=disabled -Dicu=disabled -Dfreetype=enabled',
  [freetype]);
local fribidi = b.meson('fribidi', '-Ddocs=false -Dtests=false');
local cairo = b.meson('cairo',
  '-Dtests=disabled -Dgtk_doc=false -Dxlib=disabled -Dxcb=disabled -Dzlib=enabled -Dfontconfig=enabled -Dfreetype=enabled',
  [pixman, fontconfig, freetype, libpng, zlib]);
local pango = b.meson('pango',
  '-Dintrospection=disabled -Dbuild-testsuite=false -Dbuild-examples=false -Dlibthai=disabled -Dxft=disabled -Dfontconfig=enabled -Dfreetype=enabled -Dcairo=enabled',
  [glib, harfbuzz, fribidi, cairo, fontconfig, tools.cxx_runtime]);
local jsonc = b.package('json-c', |||
  # GNUInstallDirs otherwise rewrites a root prefix to /usr for these paths.
  cmake -S . -B build -DCMAKE_INSTALL_PREFIX=/ -DCMAKE_INSTALL_LIBDIR=/lib \
    -DCMAKE_INSTALL_INCLUDEDIR=/include -DCMAKE_INSTALL_BINDIR=/bin \
    -DCMAKE_INSTALL_DATAROOTDIR=/share \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_STATIC_LIBS=OFF -DBUILD_TESTING=OFF
  cmake --build build -j"$BUILD_PARALLELISM"
  DESTDIR=/out cmake --install build
|||, [], [tools.cmake]);
local tllist = b.meson('tllist');
local utf8proc = b.package('utf8proc', |||
  make -j"$BUILD_PARALLELISM" prefix=/ libdir=/lib
  make prefix=/ libdir=/lib DESTDIR=/out install
|||);
local fcft = b.meson('fcft', '-Ddocs=disabled -Dgrapheme-shaping=enabled -Drun-shaping=enabled',
  [freetype, fontconfig, pixman, harfbuzz, utf8proc], [tllist]);
local ncurses = b.autotools('ncurses',
  '--with-shared --without-debug --without-ada --without-cxx --without-cxx-binding --enable-widec --enable-pc-files --with-pkg-config-libdir=/lib/pkgconfig',
  [], [], '', |||
    # Our runtime has no /usr/share indirection beyond /usr -> /.
    test -f /out/share/terminfo/x/xterm-256color
  |||);
local fonts = b.package('fonts', |||
  mkdir -p /out/share/fonts/truetype/dejavu
  cp ttf/*.ttf /out/share/fonts/truetype/dejavu/
|||);
{
  zlib: zlib, expat: expat, libffi: libffi, pcre2: pcre2, libpng: libpng,
  freetype: freetype, fontconfig: fontconfig, pixman: pixman,
  wayland: wayland, protocols: protocols, xkeyboard: xkeyboard, compose: compose, xkbcommon: xkbcommon,
  libdrm: libdrm, displayinfo: displayinfo, hwdata: hwdata,
  seatd: seatd, eudev: eudev, libevdev: libevdev, mtdev: mtdev, libinput: libinput,
  glib: glib, harfbuzz: harfbuzz, fribidi: fribidi, cairo: cairo, pango: pango, jsonc: jsonc,
  tllist: tllist, utf8proc: utf8proc, fcft: fcft, ncurses: ncurses, fonts: fonts,
}
