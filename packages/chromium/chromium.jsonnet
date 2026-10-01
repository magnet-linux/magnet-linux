local b = import './build.libsonnet';
local t = import './toolchain.jsonnet';
local d = import './dependencies.jsonnet';
local shared = import '../browser-common/libraries.jsonnet';
local tools = import '../desktop-tools.jsonnet';
local libs = import '../desktop-libs.jsonnet';
local kt = import '../kernel-tools.jsonnet';
local patches = import './patches.libsonnet';
local chromium_build = b.package('chromium',
  std.join('\n', ["/opt/build-patch/bin/patch -p1 <<'MAGNET_PATCH'\n" + p + '\nMAGNET_PATCH\n' for p in patches]) +
  '\n' + (importstr './prepare.sh') + '\n' + |||
    export CC=clang CXX=clang++ AR=llvm-ar NM=llvm-nm
    export CFLAGS='-O2 -Wno-unknown-warning-option'
    export CXXFLAGS='-O2 -Wno-unknown-warning-option'
    export RUSTC_BOOTSTRAP=1 LIBCLANG_PATH=/lib
    export HOME=/build/home GOTOOLCHAIN=local GOPROXY=off
    export GOCACHE=/build/go-cache GOPATH=/build/go-path CGO_ENABLED=0
    mkdir -p "$HOME" out/magnet
  ||| + "cat > out/magnet/args.gn <<'MAGNET_ARGS'\n" + (importstr './args.gn') + '\nMAGNET_ARGS\n' + |||
    gn gen out/magnet
    ninja -C out/magnet -j"$BUILD_PARALLELISM" chrome chrome_crashpad_handler
  ||| + (importstr './install.sh'),
  [shared.gtk, shared.dbus, shared.alsa, shared.pci, shared.curl, d.nss, d.gbm,
   libs.libdrm, libs.eudev, libs.hwdata, libs.fontconfig, tools.cxx_runtime,
   (import '../ca-certificates.jsonnet').ca_certificates],
  [t.llvm, t.rust, t.builtins, t.bindgen, t.gn, t.node, t.esbuild,
   t.python_bz2, t.python_browser, t.nasm, t.bash, t.patch, t.xz,
   tools.ninja, tools.gperf, kt.perl, kt.bison, kt.flex,
   (import '../browser-common/bsd-compat-headers.jsonnet').headers,
   (import '../go.jsonnet').go]) {
  fetch+: [(import './sources.libsonnet')['rollup-wasm'].archive],
  build: super.build {
    script: std.strReplace(super.script, 'tar --no-same-owner -xf',
      'XZ_DEFAULTS=-T4 tar --no-same-owner -I /opt/build-xz/bin/xz -xf'),
  },
};
// Keep the installed launcher's shell tools separate from the compiler's
// build environment so runtime policy does not trigger a browser source build.
local chromium = {
  name: chromium_build.name,
  buildEnv: { PATH: '/bin' },
  buildDeps: [(import '../bootstrap.jsonnet').bootstrap, chromium_build],
  runDeps: chromium_build.runDeps + [(import '../base/tools.jsonnet').shell_tools],
  fetch: [],
  build: { kind: 'script', script: |||
    mkdir -p /out/bin /out/lib /out/share/applications /out/share/licenses \
      /out/share/icons/hicolor/128x128/apps
    # Refresh session guidance separately from the expensive source build.
    sed 's/ and run start-sway//' /bin/chromium > /out/bin/chromium
    chmod 0755 /out/bin/chromium
    cp -a /lib/chromium /out/lib/
    cp /share/applications/chromium.desktop /out/share/applications/
    cp -a /share/licenses/chromium /out/share/licenses/
    cp /share/icons/hicolor/128x128/apps/chromium.png /out/share/icons/hicolor/128x128/apps/
    /out/lib/chromium/chrome --version
  ||| },
};
{ chromium: chromium }
