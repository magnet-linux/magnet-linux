local bootstrap_packages = import './bootstrap.jsonnet';
local bootstrap = bootstrap_packages.bootstrap;
local root_layout = bootstrap_packages.root_layout;
local runtime = function(dependencies) [root_layout] + dependencies;
local build_policy = {
  buildEnv: {
    PATH: '/bin',
  },
};

local make = build_policy + {
  name: 'make',
  build: |||
    tar -xzf /fetch/make-4.4.1.tar.gz
    cd make-4.4.1

    CC='gcc -static' \
    CFLAGS='-std=gnu17 -O2 -pipe -fno-ident' \
    LDFLAGS='-static -s' \
      ./configure \
        --prefix=/ \
        --disable-nls \
        --disable-dependency-tracking

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install

    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([]),
  buildDeps: [bootstrap],
  fetch: [
    {
      filename: 'make-4.4.1.tar.gz',
      sha256: 'dd16fb1d67bfab79a72f5e8390735c49e3e8e70b4945a15ab1f81ddb78658fb3',
      urls: ['https://ftp.gnu.org/gnu/make/make-4.4.1.tar.gz'],
    },
  ],
};

local musl = build_policy + {
  name: 'musl',
  build: |||
    tar -xzf /fetch/musl-1.2.6.tar.gz
    cd musl-1.2.6

    CC=gcc \
    CFLAGS='-O2 -pipe -fno-ident' \
      ./configure \
        --prefix=/ \
        --syslibdir=/lib \
        --target=x86_64-linux-musl

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
  |||,
  runDeps: runtime([]),
  buildDeps: [bootstrap, make],
  fetch: [
    {
      filename: 'musl-1.2.6.tar.gz',
      sha256: 'd585fd3b613c66151fc3249e8ed44f77020cb5e6c1e635a616d3f9f82460512a',
      urls: ['https://musl.libc.org/releases/musl-1.2.6.tar.gz'],
    },
  ],
};

local musl_rt = build_policy + {
  name: 'musl-rt',
  build: |||
    mkdir -p /out/lib
    cp -a /lib/libc.so /out/lib/
    cp -a /lib/ld-musl-x86_64.so.1 /out/lib/
  |||,
  runDeps: runtime([]),
  buildDeps: [bootstrap, musl],
  fetch: [],
};

local binutils = build_policy + {
  name: 'binutils',
  build: |||
    tar -xzf /fetch/binutils-2.44.tar.gz
    cd binutils-2.44
    mkdir build
    cd build

    ../configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --target=x86_64-linux-musl \
      --prefix=/ \
      --libdir=/lib \
      --disable-multilib \
      --disable-nls \
      --disable-werror

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: [bootstrap, make, musl],
  fetch: [
    {
      filename: 'binutils-2.44.tar.gz',
      sha256: '0cdd76777a0dfd3dd3a63f215f030208ddb91c2361d2bcc02acec0f1c16b6a2e',
      urls: ['https://ftp.gnu.org/gnu/binutils/binutils-2.44.tar.gz'],
    },
  ],
};

local gcc = build_policy + {
  name: 'gcc',
  build: |||
    tar -xJf /fetch/gcc-15.1.0.tar.xz
    cd gcc-15.1.0

    tar -xJf /fetch/gmp-6.3.0.tar.xz
    mv gmp-6.3.0 gmp
    tar -xzf /fetch/mpc-1.3.1.tar.gz
    mv mpc-1.3.1 mpc
    tar -xJf /fetch/mpfr-4.2.2.tar.xz
    mv mpfr-4.2.2 mpfr

    # This package tree deliberately uses one native library directory. GCC's
    # x86-64 target otherwise selects ../lib64 even with multilib disabled.
    sed -i \
      's|^MULTILIB_OSDIRNAMES = m64=.*|MULTILIB_OSDIRNAMES = m64=../lib|' \
      gcc/config/i386/t-linux64

    mkdir build
    cd build

    ../configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --target=x86_64-linux-musl \
      --prefix=/ \
      --libdir=/lib \
      --with-slibdir=/lib \
      --with-toolexeclibdir=/lib \
      --with-sysroot=/ \
      --disable-bootstrap \
      --disable-multilib \
      --disable-nls \
      --without-system-zlib \
      --enable-languages=c,c++ \
      --disable-libsanitizer \
      --disable-libvtv \
      --disable-libquadmath \
      --disable-libitm

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([binutils, musl]),
  buildDeps: [bootstrap, make, binutils, musl],
  fetch: [
    {
      filename: 'gcc-15.1.0.tar.xz',
      sha256: 'e2b09ec21660f01fecffb715e0120265216943f038d0e48a9868713e54f06cea',
      urls: ['https://ftp.gnu.org/gnu/gcc/gcc-15.1.0/gcc-15.1.0.tar.xz'],
    },
    {
      filename: 'gmp-6.3.0.tar.xz',
      sha256: 'a3c2b80201b89e68616f4ad30bc66aee4927c3ce50e33929ca819d5c43538898',
      urls: ['https://ftp.gnu.org/gnu/gmp/gmp-6.3.0.tar.xz'],
    },
    {
      filename: 'mpc-1.3.1.tar.gz',
      sha256: 'ab642492f5cf882b74aa0cb730cd410a81edcdbec895183ce930e706c1c759b8',
      urls: ['https://ftp.gnu.org/gnu/mpc/mpc-1.3.1.tar.gz'],
    },
    {
      filename: 'mpfr-4.2.2.tar.xz',
      sha256: 'b67ba0383ef7e8a8563734e2e889ef5ec3c3b898a01d00fa0a6869ad81c6ce01',
      urls: ['https://ftp.gnu.org/gnu/mpfr/mpfr-4.2.2.tar.xz'],
    },
  ],
};

local toolchain = [bootstrap, make, binutils, gcc, musl];

local coreutils = build_policy + {
  name: 'coreutils',
  build: |||
    tar -xJf /fetch/coreutils-9.4.tar.xz
    cd coreutils-9.4

    FORCE_UNSAFE_CONFIGURE=1 \
      ./configure \
        --build=x86_64-linux-musl \
        --host=x86_64-linux-musl \
        --prefix=/ \
        --disable-nls \
        --without-gmp

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'coreutils-9.4.tar.xz',
      sha256: 'ea613a4cf44612326e917201bbbcdfbd301de21ffc3b59b6e5c07e040b275e52',
      urls: ['https://ftp.gnu.org/gnu/coreutils/coreutils-9.4.tar.xz'],
    },
  ],
};

local gawk = build_policy + {
  name: 'gawk',
  build: |||
    tar -xJf /fetch/gawk-5.3.2.tar.xz
    cd gawk-5.3.2

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls \
      --without-libsigsegv \
      --without-readline

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'gawk-5.3.2.tar.xz',
      sha256: 'f8c3486509de705192138b00ef2c00bbbdd0e84c30d5c07d23fc73a9dc4cc9cc',
      urls: ['https://ftp.gnu.org/gnu/gawk/gawk-5.3.2.tar.xz'],
    },
  ],
};

local sed = build_policy + {
  name: 'sed',
  build: |||
    tar -xJf /fetch/sed-4.9.tar.xz
    cd sed-4.9

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'sed-4.9.tar.xz',
      sha256: '6e226b732e1cd739464ad6862bd1a1aba42d7982922da7a53519631d24975181',
      urls: ['https://ftp.gnu.org/gnu/sed/sed-4.9.tar.xz'],
    },
  ],
};

local stage2Tools = toolchain + [coreutils, gawk, sed];

local findutils = build_policy + {
  name: 'findutils',
  build: |||
    tar -xJf /fetch/findutils-4.10.0.tar.xz
    cd findutils-4.10.0

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --localstatedir=/var \
      --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: stage2Tools,
  fetch: [
    {
      filename: 'findutils-4.10.0.tar.xz',
      sha256: '1387e0b67ff247d2abde998f90dfbf70c1491391a59ddfecb8ae698789f0a4f5',
      urls: ['https://ftp.gnu.org/gnu/findutils/findutils-4.10.0.tar.xz'],
    },
  ],
};

local diffutils = build_policy + {
  name: 'diffutils',
  build: |||
    tar -xJf /fetch/diffutils-3.12.tar.xz
    cd diffutils-3.12

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: stage2Tools,
  fetch: [
    {
      filename: 'diffutils-3.12.tar.xz',
      sha256: '7c8b7f9fc8609141fdea9cece85249d308624391ff61dedaf528fcb337727dfd',
      urls: ['https://ftp.gnu.org/gnu/diffutils/diffutils-3.12.tar.xz'],
    },
  ],
};

local pkgconfig = build_policy + {
  name: 'pkgconfig',
  build: |||
    tar -xzf /fetch/pkg-config-0.29.2.tar.gz
    cd pkg-config-0.29.2

    CFLAGS='-std=gnu17 -O2 -pipe -fno-ident' \
      ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls \
      --with-internal-glib

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'pkg-config-0.29.2.tar.gz',
      sha256: '6fc69c01688c9458a57eb9a1664c9aba372ccda420a02bf4429fe610e7e7d591',
      urls: ['https://pkgconfig.freedesktop.org/releases/pkg-config-0.29.2.tar.gz'],
    },
  ],
};

local bash = build_policy + {
  name: 'bash',
  build: |||
    tar -xzf /fetch/bash-5.2.37.tar.gz
    cd bash-5.2.37

    CFLAGS='-std=gnu17 -O2 -pipe -fno-ident -Wno-error=implicit-function-declaration' \
    MAKEINFO=true \
      ./configure \
        --build=x86_64-linux-musl \
        --host=x86_64-linux-musl \
        --prefix=/ \
        --without-bash-malloc \
        --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
    ln -s bash /out/bin/sh
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: stage2Tools,
  fetch: [
    {
      filename: 'bash-5.2.37.tar.gz',
      sha256: '9599b22ecd1d5787ad7d3b7bf0c59f312b3396d1e281175dd1f8a4014da621ff',
      urls: ['https://ftp.gnu.org/gnu/bash/bash-5.2.37.tar.gz'],
    },
  ],
};

local gzip = build_policy + {
  name: 'gzip',
  build: |||
    tar -xJf /fetch/gzip-1.13.tar.xz
    cd gzip-1.13

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'gzip-1.13.tar.xz',
      sha256: '7454eb6935db17c6655576c2e1b0fabefd38b4d0936e0f87f48cd062ce91a057',
      urls: ['https://ftp.gnu.org/gnu/gzip/gzip-1.13.tar.xz'],
    },
  ],
};

local xz = build_policy + {
  name: 'xz',
  build: |||
    tar -xJf /fetch/xz-5.4.6.tar.xz
    cd xz-5.4.6

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --libdir=/lib \
      --disable-nls \
      --disable-doc

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain,
  fetch: [
    {
      filename: 'xz-5.4.6.tar.xz',
      sha256: 'b92d4e3a438affcf13362a1305cd9d94ed47ddda22e456a42791e630a5644f5c',
      urls: [
        'https://github.com/tukaani-project/xz/releases/download/v5.4.6/xz-5.4.6.tar.xz',
        'https://tukaani.org/xz/xz-5.4.6.tar.xz',
      ],
    },
  ],
};

local tar = build_policy + {
  name: 'tar',
  build: |||
    tar -xJf /fetch/tar-1.35.tar.xz
    cd tar-1.35

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-nls \
      --without-selinux

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt, xz, gzip]),
  buildDeps: toolchain + [pkgconfig, xz],
  fetch: [
    {
      filename: 'tar-1.35.tar.xz',
      sha256: '4d62ff37342ec7aed748535323930c7cf94acf71c3591882b26a7ea50f3edc16',
      urls: ['https://ftp.gnu.org/gnu/tar/tar-1.35.tar.xz'],
    },
  ],
};

local grep = build_policy + {
  name: 'grep',
  build: |||
    tar -xJf /fetch/grep-3.11.tar.xz
    cd grep-3.11

    ./configure \
      --build=x86_64-linux-musl \
      --host=x86_64-linux-musl \
      --prefix=/ \
      --disable-perl-regexp \
      --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
    rm -f /out/share/info/dir
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: toolchain + [tar, xz],
  fetch: [
    {
      filename: 'grep-3.11.tar.xz',
      sha256: '1db2aedde89d0dea42b16d9528f894c8d15dae4e190b59aecc78f5a951276eab',
      urls: ['https://ftp.gnu.org/gnu/grep/grep-3.11.tar.xz'],
    },
  ],
};

local libgcc_rt = build_policy + {
  name: 'libgcc-rt',
  build: |||
    library="$(gcc -print-file-name=libgcc_s.so.1)"
    test -f "$library"
    mkdir -p /out/lib
    cp -a "$(dirname "$library")"/libgcc_s.so* /out/lib/
  |||,
  runDeps: runtime([musl_rt]),
  buildDeps: [bootstrap, gcc],
  fetch: [],
};

local libstdcpp_rt = build_policy + {
  name: 'libstdcpp-rt',
  build: |||
    library="$(g++ -print-file-name=libstdc++.so.6)"
    test -f "$library"
    mkdir -p /out/lib
    cp -a "$(dirname "$library")"/libstdc++.so* /out/lib/
  |||,
  runDeps: runtime([musl_rt, libgcc_rt]),
  buildDeps: [bootstrap, gcc],
  fetch: [],
};

{
  make: make,
  musl: musl,
  musl_rt: musl_rt,
  binutils: binutils,
  gcc: gcc,
  coreutils: coreutils,
  gawk: gawk,
  sed: sed,
  findutils: findutils,
  diffutils: diffutils,
  pkgconfig: pkgconfig,
  bash: bash,
  gzip: gzip,
  xz: xz,
  tar: tar,
  grep: grep,
  libgcc_rt: libgcc_rt,
  libstdcpp_rt: libstdcpp_rt,
}
