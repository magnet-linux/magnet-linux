local bootstrap = import "./bootstrap.jsonnet";

local bootstrap_root = bootstrap.bootstrap;
local bootstrap_busybox = bootstrap.busybox;
local bootstrap_make = bootstrap.make;

local make = bootstrap_make;

local muslTarball = {
  name: "musl-tarball",
  build: |||
    #!/bin/sh
    set -euo pipefail

    export CC="x86_64-linux-musl-gcc"
    export AR="x86_64-linux-musl-ar"
    export RANLIB="x86_64-linux-musl-ranlib"
    export CFLAGS="-O2 -pipe"
    export LDFLAGS=""

    tar -xzf "/fetch/musl-1.2.5.tar.gz"
    cd musl-*

    JOBS=${BUILD_PARALLELISM}

    ./configure --prefix=/usr --syslibdir=/lib --host=x86_64-linux-musl

    make -j"$JOBS"
    make DESTDIR=/out install

    headers_tmp=$(mktemp -d)
    tar -xJf "/fetch/linux-headers-4.19.88-2.tar.xz" -C "$headers_tmp"
    copy_headers_dir() {
        src="$1"
        if [ -d "$src" ]; then
            mkdir -p /out/usr/include
            for entry in "$src"/*; do
                [ -e "$entry" ] || continue
                name=$(basename "$entry")
                rm -rf "/out/usr/include/$name"
                if [ -d "$entry" ]; then
                    mkdir -p "/out/usr/include/$name"
                    (cd "$entry" && tar -cf - .) | tar -xf - -C "/out/usr/include/$name"
                else
                    cp -a "$entry" /out/usr/include/
                fi
            done
        fi
    }
    generic_dir=$(find "$headers_tmp" -maxdepth 3 -type d -path '*/generic/include' | head -n1 || true)
    arch_dir=$(find "$headers_tmp" -maxdepth 3 -type d -path '*/x86/include' | head -n1 || true)
    if [ -n "$generic_dir" ]; then
        copy_headers_dir "$generic_dir"
    fi
    if [ -n "$arch_dir" ]; then
        copy_headers_dir "$arch_dir"
    fi
    rm -rf "$headers_tmp"

    tar -czf /tmp/musl.tar.gz -C /out .
    rm -rf /out/*
    mv /tmp/musl.tar.gz /out/musl.tar.gz
  |||,
  runDeps: [],
  buildDeps: [bootstrap_root, make],
  fetch: [
    {
      filename: "musl-1.2.5.tar.gz",
      sha256: "a9a118bbe84d8764da0ea0d28b3ab3fae8477fc7e4085d90102b8596fc7c75e4",
      urls: [
        "https://musl.libc.org/releases/musl-1.2.5.tar.gz",
      ],
    },
    {
      filename: "linux-headers-4.19.88-2.tar.xz",
      sha256: "dc7abf734487553644258a3822cfd429d74656749e309f2b25f09f4282e05588",
      urls: [
        "https://ftp.barfooze.de/pub/sabotage/tarballs/linux-headers-4.19.88-2.tar.xz",
      ],
    },
  ],
};

local musl = {
  name: "musl",
  build: |||
    #!/bin/sh
    set -euo pipefail

    mkdir -p /out
    tar -xzf /musl.tar.gz -C /out
  |||,
  runDeps: [],
  buildDeps: [bootstrap_root, muslTarball],
  fetch: [],
};

local musl_rt = {
  name: "musl-rt",
  build: |||
    #!/bin/sh
    set -euo pipefail

    tmpdir=$(mktemp -d)
    tar -xzf /musl.tar.gz -C "$tmpdir"

    rm -rf "$tmpdir"/usr/include "$tmpdir"/include "$tmpdir"/usr/share
    find "$tmpdir" -type f -name '*.a' -delete || true

    mkdir -p /out
    cp -a "$tmpdir"/. /out/
  |||,
  runDeps: [],
  buildDeps: [bootstrap_root, muslTarball],
  fetch: [],
};

local binutils = {
  name: "binutils",
  build: |||
    #!/bin/sh
    set -euo pipefail

    export PATH="/toolchain/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export AR="x86_64-linux-musl-ar"
    export RANLIB="x86_64-linux-musl-ranlib"
    export LD="x86_64-linux-musl-ld"
    export STRIP="x86_64-linux-musl-strip"

    tar -xJf "/fetch/binutils-2.33.1.tar.xz"
    cd binutils-*

    mkdir build
    cd build

    ../configure \
        --host=x86_64-linux-musl \
        --target=x86_64-linux-musl \
        --prefix=/usr \
        --disable-nls \
        --disable-multilib

    JOBS=${BUILD_PARALLELISM}
    make AR="$AR" RANLIB="$RANLIB" -j"$JOBS"
    make AR="$AR" RANLIB="$RANLIB" DESTDIR=/out install

    rm -f /out/usr/share/info/dir || true
  |||,
  runDeps: [musl_rt],
  buildDeps: [bootstrap_root, make],
  fetch: [
    {
      filename: "binutils-2.33.1.tar.xz",
      sha256: "ab66fc2d1c3ec0359b8e08843c9f33b63e8707efdff5e4cc5c200eae24722cbf",
      urls: [
        "https://ftpmirror.gnu.org/binutils/binutils-2.33.1.tar.xz",
      ],
    },
  ],
};

local gcc = {
  name: "gcc",
  build: |||
    #!/bin/sh
    set -euo pipefail

    export PATH="/toolchain/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export AR="x86_64-linux-musl-ar"
    export RANLIB="x86_64-linux-musl-ranlib"
    export LD="x86_64-linux-musl-ld"
    export STRIP="x86_64-linux-musl-strip"

    tar -xzf /musl.tar.gz -C /

    tar -xJf "/fetch/gcc-9.4.0.tar.xz"
    cd gcc-*

    tar -xjf "/fetch/gmp-6.1.2.tar.bz2"
    mv gmp-* gmp
    tar -xjf "/fetch/mpfr-4.0.2.tar.bz2"
    mv mpfr-* mpfr
    tar -xzf "/fetch/mpc-1.1.0.tar.gz"
    mv mpc-* mpc

    mkdir build
    cd build

    ../configure --build=x86_64-linux-musl --host=x86_64-linux-musl --target=x86_64-linux-musl \
        --prefix=/usr --disable-bootstrap --disable-multilib --disable-nls --without-system-zlib \
        --enable-languages=c,c++ --disable-libsanitizer --disable-libvtv --disable-libquadmath \
        --disable-libgomp --disable-libatomic --disable-libitm

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -f /out/usr/share/info/dir || true
  |||,
  runDeps: [binutils, musl],
  buildDeps: [bootstrap_root, make, binutils, muslTarball],
  fetch: [
    {
      filename: "gcc-9.4.0.tar.xz",
      sha256: "c95da32f440378d7751dd95533186f7fc05ceb4fb65eb5b85234e6299eb9838e",
      urls: [
        "https://ftpmirror.gnu.org/gcc/gcc-9.4.0/gcc-9.4.0.tar.xz",
      ],
    },
    {
      filename: "gmp-6.1.2.tar.bz2",
      sha256: "5275bb04f4863a13516b2f39392ac5e272f5e1bb8057b18aec1c9b79d73d8fb2",
      urls: [
        "https://ftpmirror.gnu.org/gmp/gmp-6.1.2.tar.bz2",
      ],
    },
    {
      filename: "mpfr-4.0.2.tar.bz2",
      sha256: "c05e3f02d09e0e9019384cdd58e0f19c64e6db1fd6f5ecf77b4b1c61ca253acc",
      urls: [
        "https://ftpmirror.gnu.org/mpfr/mpfr-4.0.2.tar.bz2",
      ],
    },
    {
      filename: "mpc-1.1.0.tar.gz",
      sha256: "6985c538143c1208dcb1ac42cedad6ff52e267b47e5f970183a3e75125b43c2e",
      urls: [
        "https://ftpmirror.gnu.org/mpc/mpc-1.1.0.tar.gz",
      ],
    },
  ],
};

local coreutils = {
  name: "coreutils",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PKG_CONFIG=
    export CC="gcc"
    export CXX="g++"
    export AR="ar"
    export RANLIB="ranlib"
    export LD="ld"
    export STRIP="strip"
    export CPP="$CC -E"

    tar -xJf "/fetch/coreutils-9.4.tar.xz"
    cd coreutils-*

    FORCE_UNSAFE_CONFIGURE=1 ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls --without-gmp

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl_rt],
  buildDeps: [bootstrap_root, bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "coreutils-9.4.tar.xz",
      sha256: "ea613a4cf44612326e917201bbbcdfbd301de21ffc3b59b6e5c07e040b275e52",
      urls: [
        "https://ftpmirror.gnu.org/coreutils/coreutils-9.4.tar.xz",
      ],
    },
  ],
};

local gawk = {
  name: "gawk",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    tar -xJf "/fetch/gawk-5.2.2.tar.xz"
    cd gawk-*

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls --without-libsigsegv --without-readline

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl_rt],
  buildDeps: [bootstrap_root, bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "gawk-5.2.2.tar.xz",
      sha256: "3c1fce1446b4cbee1cd273bd7ec64bc87d89f61537471cd3e05e33a965a250e9",
      urls: [
        "https://ftpmirror.gnu.org/gawk/gawk-5.2.2.tar.xz",
      ],
    },
  ],
};

local sed = {
  name: "sed",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PKG_CONFIG=

    tar -xJf "/fetch/sed-4.9.tar.xz"
    cd sed-*

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [bootstrap_root, bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "sed-4.9.tar.xz",
      sha256: "6e226b732e1cd739464ad6862bd1a1aba42d7982922da7a53519631d24975181",
      urls: [
        "https://ftpmirror.gnu.org/sed/sed-4.9.tar.xz",
      ],
    },
  ],
};

local findutils = {
  name: "findutils",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"

    tar -xJf "/fetch/findutils-4.10.0.tar.xz"
    cd findutils-4.10.0

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls --localstatedir=/var

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    make,
    binutils,
    gcc,
    musl,
    coreutils,
    gawk,
    sed,
  ],
  fetch: [
    {
      filename: "findutils-4.10.0.tar.xz",
      sha256: "1387e0b67ff247d2abde998f90dfbf70c1491391a59ddfecb8ae698789f0a4f5",
      urls: [
        "https://ftp.gnu.org/gnu/findutils/findutils-4.10.0.tar.xz",
        "https://ftpmirror.gnu.org/findutils/findutils-4.10.0.tar.xz",
      ],
    },
  ],
};

local diffutils = {
  name: "diffutils",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"

    tar -xJf "/fetch/diffutils-3.12.tar.xz"
    cd diffutils-3.12

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    make,
    binutils,
    gcc,
    musl,
    coreutils,
    gawk,
    sed,
  ],
  fetch: [
    {
      filename: "diffutils-3.12.tar.xz",
      sha256: "7c8b7f9fc8609141fdea9cece85249d308624391ff61dedaf528fcb337727dfd",
      urls: [
        "https://ftp.gnu.org/gnu/diffutils/diffutils-3.12.tar.xz",
        "https://ftpmirror.gnu.org/diffutils/diffutils-3.12.tar.xz",
      ],
    },
  ],
};

local pkgconfig = {
  name: "pkgconfig",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PKG_CONFIG=

    tar -xzf "/fetch/pkg-config-0.29.2.tar.gz"
    cd pkg-config-*

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls --with-internal-glib

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [bootstrap_root, bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "pkg-config-0.29.2.tar.gz",
      sha256: "6fc69c01688c9458a57eb9a1664c9aba372ccda420a02bf4429fe610e7e7d591",
      urls: [
        "https://pkgconfig.freedesktop.org/releases/pkg-config-0.29.2.tar.gz",
      ],
    },
  ],
};

local bash = {
  name: "bash",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export MAKEINFO=true

    tar -xzf "/fetch/bash-5.2.37.tar.gz"
    cd bash-*

    ./configure --prefix=/usr --without-bash-malloc --disable-nls

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
    mkdir -p /out/bin
    ln -sf ../usr/bin/bash /out/bin/bash
    ln -sf bash /out/bin/sh
  |||,
  runDeps: [musl],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    make,
    binutils,
    gcc,
    musl,
    coreutils,
    gawk,
    sed,
  ],
  fetch: [
    {
      filename: "bash-5.2.37.tar.gz",
      sha256: "9599b22ecd1d5787ad7d3b7bf0c59f312b3396d1e281175dd1f8a4014da621ff",
      urls: [
        "https://ftpmirror.gnu.org/bash/bash-5.2.37.tar.gz",
      ],
    },
  ],
};

local gzip = {
  name: "gzip",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    tar -xJf "/fetch/gzip-1.13.tar.xz"
    cd gzip-*

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-nls

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "gzip-1.13.tar.xz",
      sha256: "7454eb6935db17c6655576c2e1b0fabefd38b4d0936e0f87f48cd062ce91a057",
      urls: [
        "https://ftpmirror.gnu.org/gzip/gzip-1.13.tar.xz",
      ],
    },
  ],
};

local xz = {
  name: "xz",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PKG_CONFIG=
    export CC="gcc"
    export CXX="g++"
    export AR="ar"
    export RANLIB="ranlib"
    export LD="ld"
    export STRIP="strip"
    export CPP="$CC -E"

    for tool in ar ranlib nm strip; do
        prefixed="/usr/bin/x86_64-linux-musl-$tool"
        if [ ! -e "$prefixed" ] && command -v "$tool" >/dev/null 2>&1; then
            ln -sf "$tool" "$prefixed"
        fi
    done

    tar -xJf "/fetch/xz-5.4.6.tar.xz"
    cd xz-*

    ./configure --build=x86_64-linux-musl --host=x86_64-linux-musl --prefix=/usr --disable-nls --disable-doc

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl_rt],
  buildDeps: [bootstrap_busybox, make, binutils, gcc, musl],
  fetch: [
    {
      filename: "xz-5.4.6.tar.xz",
      sha256: "b92d4e3a438affcf13362a1305cd9d94ed47ddda22e456a42791e630a5644f5c",
      urls: [
        "https://tukaani.org/xz/xz-5.4.6.tar.xz",
      ],
    },
  ],
};

local tar = {
  name: "tar",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PKG_CONFIG="/usr/bin/pkg-config"
    export PKG_CONFIG_LIBDIR="/usr/lib/pkgconfig:/usr/share/pkgconfig"
    export PKG_CONFIG_SYSROOT_DIR="/"
    export CC="gcc"
    export CXX="g++"
    export AR="ar"
    export RANLIB="ranlib"
    export LD="ld"
    export STRIP="strip"
    export CPP="$CC -E"

    for tool in ar ranlib nm strip; do
        prefixed="/usr/bin/x86_64-linux-musl-$tool"
        if [ ! -e "$prefixed" ] && command -v "$tool" >/dev/null 2>&1; then
            ln -sf "$tool" "$prefixed"
        fi
    done

    tar -xJf "/fetch/tar-1.35.tar.xz"
    cd tar-*

    ./configure --build=x86_64-linux-musl --host=x86_64-linux-musl --prefix=/usr --disable-nls --without-selinux

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
    mkdir -p /out/bin
    ln -sf ../usr/bin/tar /out/bin/tar
  |||,
  runDeps: [musl_rt, xz, gzip],
  buildDeps: [
    bootstrap_busybox,
    make,
    binutils,
    gcc,
    musl,
    pkgconfig,
    xz,
  ],
  fetch: [
    {
      filename: "tar-1.35.tar.xz",
      sha256: "4d62ff37342ec7aed748535323930c7cf94acf71c3591882b26a7ea50f3edc16",
      urls: [
        "https://ftpmirror.gnu.org/tar/tar-1.35.tar.xz",
      ],
    },
  ],
};

local grep = {
  name: "grep",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"

    tar -xJf "/fetch/grep-3.11.tar.xz"
    cd grep-*

    ./configure --host=x86_64-linux-musl --prefix=/usr --disable-perl-regexp --disable-nls

    JOBS=${BUILD_PARALLELISM}
    make -j"$JOBS"
    make DESTDIR=/out install

    rm -vf /out/usr/share/info/dir
  |||,
  runDeps: [musl],
  buildDeps: [bootstrap_busybox, make, binutils, gcc, musl, tar, xz],
  fetch: [
    {
      filename: "grep-3.11.tar.xz",
      sha256: "1db2aedde89d0dea42b16d9528f894c8d15dae4e190b59aecc78f5a951276eab",
      urls: [
        "https://ftpmirror.gnu.org/grep/grep-3.11.tar.xz",
      ],
    },
  ],
};

local libgcc_rt = {
  name: "libgcc-rt",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    mkdir -p /out/usr/lib64
    cp -a /usr/lib64/libgcc_s.so* /out/usr/lib64/
  |||,
  runDeps: [],
  buildDeps: [bootstrap_busybox, musl_rt, gcc, coreutils, bash],
  fetch: [],
};

local libstdcpp_rt = {
  name: "libstdcpp-rt",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    mkdir -p /out/usr/lib64
    cp -a /usr/lib64/libstdc++.so* /out/usr/lib64/
    if ls /usr/lib64/libsupc++.so* >/dev/null 2>&1; then
        cp -a /usr/lib64/libsupc++.so* /out/usr/lib64/
    fi
  |||,
  runDeps: [libgcc_rt],
  buildDeps: [bootstrap_busybox, musl_rt, gcc, coreutils, bash],
  fetch: [],
};

{
  make: make,
  muslTarball: muslTarball,
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
