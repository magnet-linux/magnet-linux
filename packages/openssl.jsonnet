local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local perl_pkg = import "./perl.jsonnet";

local perl = perl_pkg.perl;

local openssl = {
  name: "openssl-3.3.1",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export CC="x86_64-linux-musl-gcc"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CFLAGS="-O2 -pipe"
    export LDFLAGS=""
    export PERL="/usr/bin/perl"

    tar -xzf /fetch/openssl-3.3.1.tar.gz
    cd openssl-3.3.1

    ./Configure \
        linux-x86_64 \
        --prefix=/usr \
        --openssldir=/etc/ssl \
        --libdir=lib \
        shared \
        threads \
        no-tests

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install_sw

    mkdir -p /out/bin
    if [ -f "/out/usr/bin/openssl" ]; then
        ln -sf ../usr/bin/openssl /out/bin/openssl
    fi
  |||,
  runDeps: [core.musl_rt, core.libgcc_rt],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.coreutils,
    core.tar,
    core.gzip,
    core.grep,
    core.sed,
    core.bash,
    perl,
  ],
  fetch: [
    {
      filename: "openssl-3.3.1.tar.gz",
      sha256: "777cd596284c883375a2a7a11bf5d2786fc5413255efab20c50d6ffe6d020b7e",
      urls: [
        "https://www.openssl.org/source/openssl-3.3.1.tar.gz",
        "https://ftp.openssl.org/source/openssl-3.3.1.tar.gz",
      ],
    },
  ],
};

{
  openssl: openssl,
}
