local core = import "./core.jsonnet";
local m4_pkg = import "./m4.jsonnet";

local m4 = m4_pkg.m4;

local flex = {
  name: "flex-2.6.4",
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

    tar -xzf /fetch/flex-2.6.4.tar.gz
    cd flex-2.6.4

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
    core.gzip,
    core.gawk,
    core.sed,
    core.grep,
    core.bash,
    m4,
  ],
  fetch: [
    {
      filename: "flex-2.6.4.tar.gz",
      sha256: "e87aae032bf07c26f85ac0ed3250998c37621d95f8bd748b31f15b33c45ee995",
      urls: [
        "https://github.com/westes/flex/releases/download/v2.6.4/flex-2.6.4.tar.gz",
        "https://downloads.sourceforge.net/project/flex/flex-2.6.4.tar.gz",
      ],
    },
  ],
};

{
  flex: flex,
}
