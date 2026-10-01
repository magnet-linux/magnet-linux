local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local tools = import '../desktop-tools.jsonnet';
local nspr = b.package('nspr',
  "patch -p1 <<'MAGNET_PATCH'\n" + (importstr './patches/nspr-lfs64.patch') + "\nMAGNET_PATCH\n" + |||
  mkdir build
  cd build
  export CFLAGS='-O2 -D_PR_POLL_AVAILABLE -D_PR_HAVE_OFF64_T -D_PR_INET6 -D_PR_HAVE_INET_NTOP -D_PR_HAVE_GETHOSTBYNAME2 -D_PR_HAVE_GETADDRINFO -D_PR_INET6_PROBE'
  ../nspr/configure --prefix=/ --libdir=/lib --includedir=/include/nspr \
    --disable-debug --enable-optimize --enable-ipv6 --enable-64bit
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  install -Dm644 config/nspr.pc /out/lib/pkgconfig/nspr.pc
  rm -f /out/lib/*.a
  install -Dm644 ../nspr/LICENSE /out/share/licenses/nspr/LICENSE
|||);
local nss = b.package('nss', |||
  nss_jobs=$BUILD_PARALLELISM
  if test "$nss_jobs" -gt 8; then nss_jobs=8; fi
  make -C nss -j"$nss_jobs" BUILD_OPT=1 USE_64=1 \
    NSS_DISABLE_GTESTS=1 NSS_ENABLE_WERROR=0 \
    NSPR_INCLUDE_DIR=/include/nspr NSPR_LIB_DIR=/lib
  mkdir -p /out/lib /out/include/nss /out/lib/pkgconfig
  find -L dist -type f -name '*.so' -exec cp -L -p '{}' /out/lib/ \;
  cp -L dist/public/nss/*.h /out/include/nss/
  install -Dm644 nss/COPYING /out/share/licenses/nss/COPYING
  cat > /out/lib/pkgconfig/nss.pc <<'PC'
  prefix=/
  libdir=/lib
  includedir=/include/nss
  Name: NSS
  Description: Network Security Services
  Version: 3.128
  Requires: nspr
  Libs: -L${libdir} -lnss3 -lnssutil3 -lsmime3 -lssl3
  Cflags: -I${includedir}
  PC
  cat > /build/nss-check.c <<'C'
  #include <nss.h>
  int main(void) {
    if (!NSS_VersionCheck("3.128") || NSS_NoDB_Init(0) != SECSuccess) return 1;
    return NSS_Shutdown() != SECSuccess;
  }
  C
  gcc -I/out/include/nss -I/include/nspr /build/nss-check.c \
    -L/out/lib -Wl,-rpath-link,/out/lib -lnss3 -lnssutil3 -o /build/nss-check
  LD_LIBRARY_PATH=/out/lib /build/nss-check
|||, [nspr, l.zlib], [(import '../kernel-tools.jsonnet').perl]);
local markupsafe = b.package('markupsafe', |||
  mkdir -p /out/lib/python3.13/site-packages
  cp -a src/markupsafe /out/lib/python3.13/site-packages/
|||, [tools.python]);
local packaging = b.package('packaging', |||
  mkdir -p /out/lib/python3.13/site-packages
  cp -a src/packaging /out/lib/python3.13/site-packages/
|||, [tools.python]);
local mako = b.package('mako', |||
  mkdir -p /out/lib/python3.13/site-packages
  cp -a mako /out/lib/python3.13/site-packages/
|||, [markupsafe, packaging]);
local yaml = b.package('pyyaml', |||
  mkdir -p /out/lib/python3.13/site-packages
  cp -a lib/yaml /out/lib/python3.13/site-packages/
|||, [tools.python]);
// Chromium's software Wayland compositor still links to the GBM interface.
// This small package supplies GBM without introducing hardware GL drivers.
local gbm = b.meson('mesa',
  '-Dplatforms=wayland -Dgallium-drivers= -Dvulkan-drivers= -Dgbm=enabled -Degl=disabled -Dglx=disabled -Dopengl=false -Dgles1=disabled -Dgles2=disabled -Dllvm=disabled -Dshared-glapi=disabled -Dvideo-codecs= -Dgallium-rusticl=false -Dbuild-tests=false',
  [l.libdrm, l.wayland, l.expat, l.zlib], [mako, yaml, l.protocols], '', |||
    mkdir -p /out/share/licenses/mesa
    cp -a docs/license.rst licenses /out/share/licenses/mesa/
  |||);
{ nspr: nspr, nss: nss, gbm: gbm, markupsafe: markupsafe }
