// Shared package helpers. Browser directories can extend the source manifest.
local bootstrap = (import '../bootstrap.jsonnet').bootstrap;
local core = import '../core.jsonnet';
local tools = import '../desktop-tools.jsonnet';
local defaults = (import '../desktop-sources.libsonnet') + (import './sources.libsonnet');
{
  sources:: defaults,
  package(name, script, runDeps=[], buildDeps=[]):
    local sources = self.sources;
    {
    name: name + '-' + sources[name].version,
    buildEnv: {
      PATH: '/bin', CC: 'gcc', CXX: 'g++',
      CFLAGS: '-O2 -std=gnu17', CXXFLAGS: '-O2',
      PKG_CONFIG_LIBDIR: '/lib/pkgconfig:/share/pkgconfig',
      PYTHONHASHSEED: '0',
    },
    build: { kind: 'script', script:
      'tar --no-same-owner -xf /fetch/' + sources[name].archive.filename + '\n' +
      'cd ' + sources[name].directory + '\n' + script + '\n' + |||
        mkdir -p /out/share/licenses
        find /out -name '*.la' -delete
      |||,
    },
    buildDeps: [bootstrap, core.musl, tools.pkgconf] + runDeps + buildDeps,
    runDeps: [core.musl_rt] + runDeps,
    fetch: [sources[name].archive],
  },
  meson(name, options='', runDeps=[], buildDeps=[], before='', after=''):
    self.package(name, before + '\n' + |||
      export CFLAGS=-O2
      meson setup build --prefix=/ --libdir=lib --libexecdir=libexec \
        --sysconfdir=/etc --localstatedir=/var --buildtype=release \
        --wrap-mode=nodownload -Ddefault_library=shared -Dwerror=false \
    ||| + '  ' + options + '\n' + |||
      ninja -C build -j"$BUILD_PARALLELISM"
      DESTDIR=/out meson install -C build --no-rebuild
    ||| + after, runDeps, [tools.meson] + buildDeps),
}
