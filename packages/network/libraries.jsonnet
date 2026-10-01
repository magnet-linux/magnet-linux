local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local kt = import '../kernel-tools.jsonnet';
local slang = b.package('slang', |||
  ./configure --prefix=/ --libdir=/lib --without-png --without-onig --without-pcre
  # S-Lang otherwise leaves termcap references unresolved in the shared object.
  sed -i 's/-ltermcap/-lncursesw/g' slang.pc
  make -C src -j"$BUILD_PARALLELISM" ELF_DEP_LIBS='-lncursesw -lm -ldl' elf
  make -C src DESTDIR=/out ELF_DEP_LIBS='-lncursesw -lm -ldl' install-elf
  make DESTDIR=/out install-pkgconfig
  install -Dm644 COPYING /out/share/licenses/slang/COPYING
|||, [l.ncurses]);
local newt = b.package('newt', |||
  ./configure --prefix=/ --libdir=/lib --without-python --without-tcl --disable-nls
  make -j"$BUILD_PARALLELISM" sharedlib
  make DESTDIR=/out install-sh
  install -Dm644 COPYING /out/share/licenses/newt/COPYING
|||, [slang]);
local duktape = b.package('duktape', |||
  make -f Makefile.sharedlibrary -j"$BUILD_PARALLELISM" INSTALL_PREFIX=/ LIBDIR=lib
  make -f Makefile.sharedlibrary INSTALL_PREFIX=/ LIBDIR=lib DESTDIR=/out install
  rm -f /out/lib/libduktaped.so*
  install -Dm644 LICENSE.txt /out/share/licenses/duktape/LICENSE.txt
|||);
local libnl = b.autotools('libnl', '--disable-cli', [], [kt.bison, kt.flex, kt.m4], '', |||
  # Library defaults live under /share; /etc belongs to the administrator.
  mkdir -p /out/share/libnl
  if test -d /out/etc/libnl; then mv /out/etc/libnl /out/share/libnl/defaults; fi
  rm -rf /out/etc
  install -Dm644 COPYING /out/share/licenses/libnl/COPYING
|||);
local libndp = b.autotools('libndp', '', [], [],
  "patch -p1 <<'MAGNET_PATCH'\n" + (importstr './libndp-musl.patch') + "\nMAGNET_PATCH\n", |||
  install -Dm644 COPYING /out/share/licenses/libndp/COPYING
|||);
local gettext = b.autotools('gettext',
  '--disable-nls --disable-java --disable-csharp --disable-emacs --disable-openmp --disable-libasprintf --without-libxml2 --without-libunistring',
  [], [], '', |||
    install -Dm644 COPYING /out/share/licenses/gettext/COPYING
  |||);
{ slang: slang, newt: newt, duktape: duktape, libnl: libnl, libndp: libndp, gettext: gettext }
