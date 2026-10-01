// Base packages share the existing musl build environment.
(import '../browser-common/build.libsonnet') {
  sources+: import './sources.libsonnet',
  autotools(name, options='', runDeps=[], buildDeps=[], before='', after=''):
    self.package(name, before + '\n' +
      './configure --prefix=/ --sbindir=/bin --libdir=/lib --sysconfdir=/etc --localstatedir=/var ' + options + '\n' + |||
        make -j"$BUILD_PARALLELISM"
        make DESTDIR=/out install
        rm -f /out/share/info/dir
      ||| + after, runDeps, buildDeps),
}
