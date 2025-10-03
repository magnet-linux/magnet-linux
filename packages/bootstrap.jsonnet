local bootstrap = {
  name: "bootstrap-rootfs",
  build: "untar",
  runDeps: [],
  buildDeps: [],
  fetch: [
    {
      filename: "bootstrap.tar.zst",
      sha256: "ba9faafb7ab5a5b23c251da5e4f5a9d4eca80639257eca1d8b9e72316df0ffc9",
      urls: [
        "magnet:?xt=urn:btih:44e646ad4d4a935ee64df404bcd334bd30898f5f&dn=bootstrap.tar.zst",
      ],
    },
  ],
};

local make = {
  name: "make",
  build: |||
    #!/bin/sh
    set -eux
    export CFLAGS="-O2 -pipe -static"
    export LDFLAGS="-static"
    tar --strip-components=1 -xzf "/fetch/make-4.4.1.tar.gz"
    ./configure --prefix=/usr --disable-nls --disable-dependency-tracking
    ./build.sh
    ./make
    ./make install DESTDIR="/tmp/"
    install -Dm755 /tmp/usr/bin/make /out/bin/make
    strip /out/bin/make
  |||,
  runDeps: [],
  buildDeps: [bootstrap],
  fetch: [
    {
      filename: "make-4.4.1.tar.gz",
      urls: [
        "https://ftpmirror.gnu.org/make/make-4.4.1.tar.gz",
      ],
      sha256: "dd16fb1d67bfab79a72f5e8390735c49e3e8e70b4945a15ab1f81ddb78658fb3",
    },
  ],
};

local busybox = {
  name: "busybox",
  build: |||
    #!/bin/sh
    set -eux
    export CFLAGS="-O2 -pipe -static"
    export LDFLAGS="-static"
    : "${BUILD_PARALLELISM:=1}"
    tar -xjf "/fetch/busybox-1.36.1.tar.bz2"
    cd busybox-*
    make distclean >/dev/null 2>&1 || true
    make defconfig
    printf '\nCONFIG_STATIC=y\n' >> .config
    make -j"${BUILD_PARALLELISM}" busybox
    strip busybox
    install -Dm755 busybox /out/bin/busybox
    cd /out/bin
    for applet in $(./busybox --list); do
        [ "$applet" = "busybox" ] && continue
        ln -sf busybox "$applet"
    done
  |||,
  runDeps: [],
  buildDeps: [bootstrap, make],
  fetch: [
    {
      filename: "busybox-1.36.1.tar.bz2",
      urls: [
        "https://busybox.net/downloads/busybox-1.36.1.tar.bz2",
        "https://sources.buildroot.net/busybox/busybox-1.36.1.tar.bz2",
      ],
      sha256: "b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314",
    },
  ],
};

local bootstrap_out = {
  name: "bootstrap",
  build: |||
    #!/bin/sh
    set -eux
    : "${BUILD_PARALLELISM:=1}"
    tar --strip-components=1 -xzf "/fetch/musl-cross-make-v0.9.11.tar.gz"
    rm -rf sources
    mkdir -p sources
    cp "/fetch/binutils-2.33.1.tar.xz" sources/binutils-2.33.1.tar.xz
    cp "/fetch/gcc-9.4.0.tar.xz" sources/gcc-9.4.0.tar.xz
    cp "/fetch/gmp-6.1.2.tar.bz2" sources/gmp-6.1.2.tar.bz2
    cp "/fetch/mpc-1.1.0.tar.gz" sources/mpc-1.1.0.tar.gz
    cp "/fetch/mpfr-4.0.2.tar.bz2" sources/mpfr-4.0.2.tar.bz2
    cp "/fetch/musl-1.2.5.tar.gz" sources/musl-1.2.5.tar.gz
    cp "/fetch/linux-headers-4.19.88-2.tar.xz" sources/linux-headers-4.19.88-2.tar.xz
    cp "/fetch/config.sub" sources/config.sub
    install_root=/tmp/musl-toolchain
    rm -rf "$install_root"
    {
        printf 'TARGET = x86_64-linux-musl\n'
        printf 'OUTPUT = %s\n' "$install_root"
        printf 'COMMON_CONFIG += --disable-nls\n'
        printf 'GCC_CONFIG += --disable-libquadmath --disable-decimal-float\n'
        printf 'GCC_CONFIG += --disable-libitm --disable-fixed-point --disable-lto\n'
    } > config.mak
    make -j"${BUILD_PARALLELISM}"
    make -j"${BUILD_PARALLELISM}" install
    rm -rf /out/toolchain
    mkdir -p /out/bin /out/dev /out/proc /out/sys /out/tmp
    chmod 1777 /out/tmp
    cp -a "$install_root" /out/toolchain
    mkdir -p /out/toolchain/usr
    ln -sfn ../include /out/toolchain/usr/include
    if [ -d /out/toolchain/x86_64-linux-musl/lib ]; then
        cp -a /out/toolchain/x86_64-linux-musl/lib/. /out/toolchain/lib/
        ln -sfn ../x86_64-linux-musl/lib/ld-musl-x86_64.so.1 /out/toolchain/lib/ld-musl-x86_64.so.1
        ln -sfn ../x86_64-linux-musl/lib/libc.so /out/toolchain/lib/libc.so
    fi
    for tool in addr2line ar as c++ c++filt cpp g++ gcc ld nm objcopy objdump ranlib strip strings; do
        prefixed="/out/toolchain/bin/x86_64-linux-musl-$tool"
        if [ -e "$prefixed" ]; then
            ln -sfn "x86_64-linux-musl-$tool" "/out/toolchain/bin/$tool"
        fi
    done
    install -Dm755 /bin/busybox /out/bin/busybox
    (
        cd /out/bin
        for applet in $(./busybox --list); do
            [ "$applet" = "busybox" ] && continue
            ln -sf busybox "$applet"
        done
    )
    for tool_path in /out/toolchain/bin/*; do
        tool_name=$(basename "$tool_path")
        ln -sf "/toolchain/bin/$tool_name" "/out/bin/$tool_name"
    done
    rm -f /out/lib
    ln -s /toolchain/lib /out/lib
  |||,
  runDeps: [],
  buildDeps: [bootstrap, make, busybox],
  fetch: [
    {
      filename: "musl-cross-make-v0.9.11.tar.gz",
      urls: [
        "https://github.com/richfelker/musl-cross-make/archive/refs/tags/v0.9.11.tar.gz",
      ],
      sha256: "306a66dd175d1065e6075deea02300d02e17806fb0a4d6f5e5829cf07c16eb51",
    },
    {
      filename: "binutils-2.33.1.tar.xz",
      urls: [
        "https://ftpmirror.gnu.org/binutils/binutils-2.33.1.tar.xz",
      ],
      sha256: "ab66fc2d1c3ec0359b8e08843c9f33b63e8707efdff5e4cc5c200eae24722cbf",
    },
    {
      filename: "gcc-9.4.0.tar.xz",
      urls: [
        "https://ftpmirror.gnu.org/gcc/gcc-9.4.0/gcc-9.4.0.tar.xz",
      ],
      sha256: "c95da32f440378d7751dd95533186f7fc05ceb4fb65eb5b85234e6299eb9838e",
    },
    {
      filename: "gmp-6.1.2.tar.bz2",
      urls: [
        "https://ftpmirror.gnu.org/gmp/gmp-6.1.2.tar.bz2",
        "https://ftp.gnu.org/gnu/gmp/gmp-6.1.2.tar.bz2",
        "https://mirrors.kernel.org/gnu/gmp/gmp-6.1.2.tar.bz2",
      ],
      sha256: "5275bb04f4863a13516b2f39392ac5e272f5e1bb8057b18aec1c9b79d73d8fb2",
    },
    {
      filename: "mpc-1.1.0.tar.gz",
      urls: [
        "https://ftpmirror.gnu.org/mpc/mpc-1.1.0.tar.gz",
      ],
      sha256: "6985c538143c1208dcb1ac42cedad6ff52e267b47e5f970183a3e75125b43c2e",
    },
    {
      filename: "mpfr-4.0.2.tar.bz2",
      urls: [
        "https://ftpmirror.gnu.org/mpfr/mpfr-4.0.2.tar.bz2",
      ],
      sha256: "c05e3f02d09e0e9019384cdd58e0f19c64e6db1fd6f5ecf77b4b1c61ca253acc",
    },
    {
      filename: "musl-1.2.5.tar.gz",
      urls: [
        "https://musl.libc.org/releases/musl-1.2.5.tar.gz",
      ],
      sha256: "a9a118bbe84d8764da0ea0d28b3ab3fae8477fc7e4085d90102b8596fc7c75e4",
    },
    {
      filename: "linux-headers-4.19.88-2.tar.xz",
      urls: [
        "https://ftp.barfooze.de/pub/sabotage/tarballs/linux-headers-4.19.88-2.tar.xz",
      ],
      sha256: "dc7abf734487553644258a3822cfd429d74656749e309f2b25f09f4282e05588",
    },
    {
      filename: "config.sub",
      urls: [
        "https://git.savannah.gnu.org/gitweb/?p=config.git;a=blob_plain;f=config.sub;hb=3d5db9ebe860",
      ],
      sha256: "75d5d255a2a273b6e651f82eecfabf6cbcd8eaeae70e86b417384c8f4a58d8d3",
    },
  ],
};

{
  bootstrap: bootstrap,
  make: make,
  busybox: busybox,
  bootstrap_out: bootstrap_out,
}
