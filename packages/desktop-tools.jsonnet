// Native build tools and the matching bootstrap C++ runtime. Only the runtime
// is needed in the installed desktop; build tools stay in build closures.
local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local core = import './core.jsonnet';
local sources = import './desktop-sources.libsonnet';
// The desktop is compiled with the pinned bootstrap GCC. Install its matching
// runtime without making a complete second compiler build a prerequisite.
// The independently rebuilt compiler/runtime remain available in core.jsonnet.
local cxx_runtime = {
  name: 'bootstrap-cxx-runtime',
  buildEnv: { PATH: '/bin' },
  build: { kind: 'script', script: |||
    mkdir -p /out/lib /out/share/licenses/gcc-runtime
    for library in libstdc++.so.6 libgcc_s.so.1; do
      cp -L "$(g++ -print-file-name="$library")" "/out/lib/$library"
    done
    tar --no-same-owner -xJf /fetch/gcc-15.1.0.tar.xz \
      gcc-15.1.0/COPYING3 gcc-15.1.0/COPYING.RUNTIME
    cp gcc-15.1.0/COPYING3 gcc-15.1.0/COPYING.RUNTIME /out/share/licenses/gcc-runtime/
  ||| },
  buildDeps: [bootstrap],
  runDeps: [core.musl_rt],
  fetch: [core.gcc.fetch[0]],
};
local package(name, script, buildDeps=[], runDeps=[]) = {
  name: name + '-' + sources[name].version,
  buildEnv: {
    PATH: '/bin', CC: 'gcc', CXX: 'g++',
    CFLAGS: '-O2 -std=gnu17', CXXFLAGS: '-O2',
    PKG_CONFIG_LIBDIR: '/lib/pkgconfig:/share/pkgconfig',
  },
  build: { kind: 'script', script:
    'tar --no-same-owner -xf /fetch/' + sources[name].archive.filename + '\n' +
    'cd ' + sources[name].directory + '\n' + script,
  },
  buildDeps: [bootstrap, core.musl] + runDeps + buildDeps,
  runDeps: [core.musl_rt] + runDeps,
  fetch: [sources[name].archive],
};
local python = package('python', |||
  ./configure --prefix=/ --without-ensurepip --disable-test-modules
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  /out/bin/python3 -c 'import hashlib, subprocess, xml.parsers.expat, zlib'
|||, [], [(import './zlib.jsonnet').shared]);
local ninja = package('ninja', |||
  CXX='g++ -static-libstdc++ -static-libgcc' python3 configure.py --bootstrap
  install -Dm755 ninja /out/bin/ninja
  install -Dm644 COPYING /out/share/licenses/ninja/COPYING
|||, [python]);
local meson = package('meson', |||
  mkdir -p /out/lib/meson /out/bin
  cp -a mesonbuild /out/lib/meson/
  cp meson.py /out/lib/meson/meson.py
  cat > /out/bin/meson <<'EOF'
  #!/bin/sh
  exec /bin/python3 /lib/meson/meson.py "$@"
  EOF
  chmod 0755 /out/bin/meson
  install -Dm644 COPYING /out/share/licenses/meson/COPYING
|||, [], [python, ninja]);
local pkgconf = package('pkgconf', |||
  ./configure --prefix=/ --disable-shared --enable-static
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  ln -s pkgconf /out/bin/pkg-config
|||);
local cmake = package('cmake', |||
  ./bootstrap --prefix=/ --parallel="$BUILD_PARALLELISM" -- \
    -DCMAKE_USE_OPENSSL=OFF -DBUILD_TESTING=OFF
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||, [], [cxx_runtime]);
local gperf = package('gperf', |||
  CXX='g++ -static-libstdc++ -static-libgcc' ./configure --prefix=/
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);
{
  python: python,
  ninja: ninja,
  meson: meson,
  pkgconf: pkgconf,
  cmake: cmake,
  gperf: gperf,
  cxx_runtime: cxx_runtime,
}
