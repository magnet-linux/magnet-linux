(import '../browser-common/build.libsonnet') {
  sources+: (import '../chromium/sources.libsonnet') + (import './sources.libsonnet'),
  cmake(name, options='', runDeps=[], buildDeps=[], before='', after=''):
    self.package(name, before + '\n' + |||
      cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/ -DCMAKE_INSTALL_LIBDIR=/lib \
        -DCMAKE_INSTALL_INCLUDEDIR=/include -DCMAKE_INSTALL_BINDIR=/bin \
        -DCMAKE_INSTALL_DATAROOTDIR=/share -DCMAKE_INSTALL_LIBEXECDIR=/libexec \
        -DBUILD_TESTING=OFF -DBUILD_SHARED_LIBS=ON -DFETCHCONTENT_FULLY_DISCONNECTED=ON \
    ||| + '  ' + options + '\n' + |||
      ninja -C build -j"$BUILD_PARALLELISM"
      DESTDIR=/out cmake --install build
    ||| + after, [(import '../desktop-tools.jsonnet').cxx_runtime] + runDeps,
      [(import '../desktop-tools.jsonnet').cmake, (import '../desktop-tools.jsonnet').ninja] + buildDeps),
  autotools(name, options='', runDeps=[], buildDeps=[], before='', after=''):
    self.package(name, before + '\n' +
      './configure --prefix=/ --libdir=/lib --sysconfdir=/etc --disable-static ' + options + '\n' + |||
      make -j"$BUILD_PARALLELISM"
      make DESTDIR=/out install
      rm -f /out/share/info/dir
    ||| + after, runDeps, buildDeps),
}
