local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local openssl_pkg = import "./openssl.jsonnet";
local zlib_pkg = import "./zlib.jsonnet";

local openssl = openssl_pkg.openssl;
local zlib = zlib_pkg.zlib;

local curl = {
  name: "curl-8.16.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"
    export LDFLAGS=""
    export PKG_CONFIG_PATH="/usr/lib/pkgconfig"

    tar -xJf /fetch/curl-8.16.0.tar.xz
    cd curl-8.16.0

    ./configure \
        --prefix=/usr \
        --host=x86_64-linux-musl \
        --with-openssl=/usr \
        --with-zlib=/usr \
        --enable-ipv6 \
        --disable-ldap \
        --disable-ldaps \
        --without-brotli \
        --without-zstd \
        --without-libidn2 \
        --without-nghttp2 \
        --without-nghttp3 \
        --without-ngtcp2 \
        --without-libssh2 \
        --without-libpsl \
        --disable-manual

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install

    mkdir -p /out/bin
    if [ -f /out/usr/bin/curl ]; then
        ln -sf ../usr/bin/curl /out/bin/curl
    fi
  |||,
  runDeps: [core.musl_rt, core.libgcc_rt, openssl, zlib],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.coreutils,
    core.tar,
    core.xz,
    core.gzip,
    core.gawk,
    core.sed,
    core.grep,
    core.bash,
    core.pkgconfig,
    openssl,
    zlib,
  ],
  fetch: [
    {
      filename: "curl-8.16.0.tar.xz",
      sha256: "40c8cddbcb6cc6251c03dea423a472a6cea4037be654ba5cf5dec6eb2d22ff1d",
      urls: [
        "https://curl.se/download/curl-8.16.0.tar.xz",
      ],
    },
  ],
};

{
  curl: curl,
}
