local bootstrap = import './bootstrap.jsonnet';
local package(name, source, script, deps=[]) = {
  name: name,
  buildEnv: { PATH: '/bin', PKG_CONFIG_LIBDIR: '/lib/pkgconfig' },
  build: { kind: 'script', script: script },
  buildDeps: [bootstrap.bootstrap] + deps,
  runDeps: [],
  fetch: [source],
};

local pkgconf = package('pkgconf-bootstrap', {
  filename: 'pkgconf.tar.xz',
  sha256: '3a9080ac51d03615e7c1910a0a2a8df08424892b5f13b0628a204d3fcce0ea8b',
  urls: ['https://distfiles.ariadne.space/pkgconf/pkgconf-2.3.0.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/pkgconf.tar.xz
  cd pkgconf-2.3.0
  CC='gcc -static' ./configure --prefix=/ --disable-shared
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  ln -s pkgconf /out/bin/pkg-config
|||);

local zlib = (import './zlib.jsonnet').zlib;

local disk_libs = package('disk-libs', {
  filename: 'util-linux.tar.xz',
  sha256: '81ee93b3cfdfeb7d7c4090cedeba1d7bbce9141fd0b501b686b3fe475ddca4c6',
  urls: ['https://www.kernel.org/pub/linux/utils/util-linux/v2.41/util-linux-2.41.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/util-linux.tar.xz
  cd util-linux-2.41
  ./configure --prefix=/ --libdir=/lib --disable-all-programs \
    --enable-libuuid --enable-libblkid --disable-shared --enable-static \
    --disable-nls --without-python --without-systemd --without-udev
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||, [pkgconf]);

local btrfs = package('btrfs-progs', {
  filename: 'btrfs.tar.xz',
  sha256: '57da428dd2199fd88d83ecf1cad05678ce78640ef7e52d7633be9887cef674bb',
  urls: ['https://www.kernel.org/pub/linux/kernel/people/kdave/btrfs-progs/btrfs-progs-v6.15.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/btrfs.tar.xz
  cd btrfs-progs-v6.15
  # libblkid's static archive also exports parse_range. Namespace the btrfs
  # helper so static linking does not collide with util-linux's implementation.
  find . -name '*.[ch]' -exec sed -i 's/\bparse_range\b/btrfs_parse_range/g' {} +
  CC='gcc -static' ./configure --prefix=/ --disable-documentation \
    --disable-convert --disable-python --disable-libudev --disable-backtrace \
    --disable-zstd --disable-lzo --disable-shared --with-crypto=builtin
  make -j"$BUILD_PARALLELISM" btrfs mkfs.btrfs
  mkdir -p /out/bin
  cp btrfs mkfs.btrfs /out/bin/
  strip /out/bin/*
|||, [pkgconf, zlib, disk_libs]) + { runDeps: [bootstrap.root_layout] };

{ btrfs: btrfs }
