local core = import "./core.jsonnet";
local m4_pkg = import "./m4.jsonnet";

local m4 = m4_pkg.m4;

local bison = {
  name: "bison-3.8.2",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CPP="x86_64-linux-musl-gcc -E"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"

    tar -xJf /fetch/bison-3.8.2.tar.xz
    cd bison-3.8.2

    ./configure --build=x86_64-linux-musl --host=x86_64-linux-musl --prefix=/usr --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
  |||,
  runDeps: [core.musl_rt, core.libgcc_rt, m4],
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
    m4,
  ],
  fetch: [
    {
      filename: "bison-3.8.2.tar.xz",
      sha256: "9bba0214ccf7f1079c5d59210045227bcf619519840ebfa80cd3849cff5a5bf2",
      urls: [
        "https://ftpmirror.gnu.org/bison/bison-3.8.2.tar.xz",
        "https://ftp.gnu.org/gnu/bison/bison-3.8.2.tar.xz",
      ],
    },
  ],
};

{
  bison: bison,
}
