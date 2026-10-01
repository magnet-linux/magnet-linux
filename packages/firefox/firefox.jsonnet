local b = import './build.libsonnet';
local t = import './toolchain.jsonnet';
local d = import '../browser-common/libraries.jsonnet';
local tools = import '../desktop-tools.jsonnet';
local libs = import '../desktop-libs.jsonnet';
local kt = import '../kernel-tools.jsonnet';
local patches = [
  importstr './patches/abseil-cpp.patch',
  importstr './patches/bmo-1952657-no-execinfo.patch',
  importstr './patches/fix-rust-target.patch',
  importstr './patches/lfs64.patch',
  importstr './patches/nspr-poll.patch',
];
local firefox_build = b.package('firefox',
  std.join('\n', ['patch -p1 <<\'MAGNET_PATCH\'\n' + p + '\nMAGNET_PATCH\n' for p in patches]) +
  '\ncat > .mozconfig <<\'MAGNET_MOZCONFIG\'\n' + (importstr './mozconfig') + '\nMAGNET_MOZCONFIG\n' + |||
    export CC=clang CXX=clang++ AR=llvm-ar RANLIB=llvm-ranlib
    export CFLAGS=-O2 CXXFLAGS=-O2
    export RUST_TARGET=x86_64-unknown-linux-musl
    export RUSTFLAGS='-C target-feature=-crt-static'
    export MOZBUILD_STATE_PATH=/build/mozbuild MOZ_NOSPAM=1
    export HOME=/build/home
    mkdir -p "$HOME"
    export MOZ_BUILD_DATE=20260929000000
    export MACH_BUILD_PYTHON_NATIVE_PACKAGE_SOURCE=none
    export CARGO_NET_OFFLINE=true
    export AUTOCONF=/bin/autoconf2.13
    export MOZ_MAKE_FLAGS="-j$BUILD_PARALLELISM"
    python3 mach build
    DESTDIR=/out python3 mach install
    install -Dm644 LICENSE /out/share/licenses/firefox/LICENSE
    mkdir -p /out/share/applications
    cat > /out/share/applications/firefox.desktop <<'EOF'
    [Desktop Entry]
    Type=Application
    Name=Firefox
    Comment=Web browser
    Exec=firefox %u
    Icon=firefox
    Terminal=false
    Categories=Network;WebBrowser;
    MimeType=text/html;x-scheme-handler/http;x-scheme-handler/https;
    StartupWMClass=firefox
    EOF
    install -Dm644 browser/branding/official/default128.png \
      /out/share/icons/hicolor/128x128/apps/firefox.png
    # Updates come from the pinned package graph, including ESR security fixes.
    mkdir -p /out/lib/firefox/distribution
    cat > /out/lib/firefox/distribution/policies.json <<'EOF'
    {"policies":{"DisableAppUpdate":true,"DontCheckDefaultBrowser":true}}
    EOF
  |||,
  [d.gtk, d.dbus, d.alsa, (import '../audio/libraries.jsonnet').pulse,
   libs.zlib, libs.libdrm, libs.eudev, tools.cxx_runtime,
   (import '../ca-certificates.jsonnet').ca_certificates],
  [t.llvm, t.rust, t.cbindgen, t.node, t.nasm, t.autoconf, t.zip, t.wasi,
   t.python_bz2, t.python_browser, kt.perl, tools.ninja]);
// Keep installation details separate from the expensive source build. musl
// needs an explicit search path for Firefox's bundled NSS/NSPR libraries.
local firefox = {
  name: firefox_build.name,
  buildEnv: { PATH: '/bin' },
  buildDeps: [(import '../bootstrap.jsonnet').bootstrap, firefox_build],
  runDeps: firefox_build.runDeps + [(import '../base/tools.jsonnet').shell_tools],
  fetch: [],
  build: { kind: 'script', script: |||
    mkdir -p /out/bin /out/lib /out/share/applications /out/share/licenses \
      /out/share/icons/hicolor/128x128/apps
    cp -a /lib/firefox /out/lib/
    cp /share/applications/firefox.desktop /out/share/applications/
    cp -a /share/licenses/firefox /out/share/licenses/
    cp /share/icons/hicolor/128x128/apps/firefox.png /out/share/icons/hicolor/128x128/apps/
    cat > /out/bin/firefox <<'EOF'
    #!/bin/sh
    export LD_LIBRARY_PATH=/lib/firefox${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}
    exec /lib/firefox/firefox "$@"
    EOF
    chmod 0755 /out/bin/firefox
    /out/bin/firefox --version
  ||| },
};
{ firefox: firefox }
