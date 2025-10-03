local core = import "./core.jsonnet";

local make = core.make;
local binutils = core.binutils;
local gcc = core.gcc;
local musl = core.musl;
local coreutils = core.coreutils;
local tar = core.tar;
local gzip = core.gzip;
local bash = core.bash;
local sed = core.sed;
local grep = core.grep;

local zlib = {
  name: "zlib-1.3.1",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CFLAGS="-O2 -pipe"

    tar -xzf /fetch/zlib-1.3.1.tar.gz
    cd zlib-1.3.1

    ./configure --prefix=/usr
    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
  |||,
  runDeps: [core.musl_rt],
  buildDeps: [
    make,
    binutils,
    gcc,
    musl,
    coreutils,
    tar,
    gzip,
    bash,
    sed,
    grep,
  ],
  fetch: [
    {
      filename: "zlib-1.3.1.tar.gz",
      sha256: "9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23",
      urls: [
        "https://zlib.net/zlib-1.3.1.tar.gz",
        "https://downloads.sourceforge.net/project/libpng/zlib/1.3.1/zlib-1.3.1.tar.gz",
      ],
    },
  ],
};

{
  zlib: zlib,
}
