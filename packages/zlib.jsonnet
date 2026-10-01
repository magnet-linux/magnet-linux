local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local zlib = {
  zlib: {
    name: 'zlib',
    buildEnv: { PATH: '/bin', PKG_CONFIG_LIBDIR: '/lib/pkgconfig' },
    build: { kind: 'script', script: |||
      tar --no-same-owner -xzf /fetch/zlib.tar.gz
      cd zlib-1.3.1
      ./configure --prefix=/ --static
      make -j"$BUILD_PARALLELISM"
      make DESTDIR=/out install
    ||| },
    buildDeps: [bootstrap],
    runDeps: [],
    fetch: [{
      filename: 'zlib.tar.gz',
      sha256: '9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23',
      urls: ['https://zlib.net/fossils/zlib-1.3.1.tar.gz'],
    }],
  },
};
zlib + {
  // Shared, PIC variant for the optional desktop and its native build tools.
  shared: zlib.zlib {
    name: 'zlib-shared',
    build: { kind: 'script', script: |||
      tar --no-same-owner -xzf /fetch/zlib.tar.gz
      cd zlib-1.3.1
      CFLAGS='-O2 -fPIC' ./configure --prefix=/
      make -j"$BUILD_PARALLELISM"
      make DESTDIR=/out install
    ||| },
    runDeps: [(import './core.jsonnet').musl_rt],
  },
}
