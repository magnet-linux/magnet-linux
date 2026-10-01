local seed = (import './bootstrap.jsonnet').bootstrap;
local package(name, source, script, deps=[]) = {
  name: name,
  buildEnv: { PATH: '/bin' },
  build: { kind: 'script', script: script },
  buildDeps: [seed] + deps,
  runDeps: [],
  fetch: [source],
};

local m4 = package('m4', {
  filename: 'm4.tar.xz',
  sha256: '63aede5c6d33b6d9b13511cd0be2cac046f2e70fd0a07aa9573a04a82783af96',
  urls: ['https://ftp.gnu.org/gnu/m4/m4-1.4.19.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/m4.tar.xz
  cd m4-1.4.19
  CC='gcc -static' CFLAGS='-O2 -std=gnu17' ./configure --prefix=/ --disable-nls
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);

local flex = package('flex', {
  filename: 'flex.tar.gz',
  sha256: 'e87aae032bf07c26f85ac0ed3250998c37621d95f8bd748b31f15b33c45ee995',
  urls: ['https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz'],
}, |||
  tar --no-same-owner -xzf /fetch/flex.tar.gz
  cd flex-2.6.4
  CC='gcc -static' CFLAGS='-O2 -std=gnu17' ./configure --prefix=/ --disable-nls --disable-shared
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||, [m4]);

local bison = package('bison', {
  filename: 'bison.tar.xz',
  sha256: '9bba0214ccf7f1079c5d59210045227bcf619519840ebfa80cd3849cff5a5bf2',
  urls: ['https://ftp.gnu.org/gnu/bison/bison-3.8.2.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/bison.tar.xz
  cd bison-3.8.2
  CC='gcc -static' CFLAGS='-O2 -std=gnu17' ./configure --prefix=/ --disable-nls
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||, [m4]);

local perl = package('perl', {
  filename: 'perl.tar.xz',
  sha256: '0551c717458e703ef7972307ab19385edfa231198d88998df74e12226abf563b',
  urls: ['https://www.cpan.org/src/5.0/perl-5.40.2.tar.xz'],
}, |||
  tar --no-same-owner -xJf /fetch/perl.tar.xz
  cd perl-5.40.2
  sh Configure -des -Dprefix=/ -Dcc=gcc -Dccflags='-O2 -std=gnu17 -D_GNU_SOURCE' \
    -Dldflags=-static -Uusedl -Uuseshrplib -Dman1dir=none -Dman3dir=none
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);

{ m4: m4, flex: flex, bison: bison, perl: perl }
