local core = import "./core.jsonnet";

local m4 = {
  name: "m4-1.4.19",
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

    tar -xzf /fetch/m4-1.4.19.tar.gz
    cd m4-1.4.19

    ./configure --build=x86_64-linux-musl --host=x86_64-linux-musl --prefix=/usr --disable-nls

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install
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
    core.gawk,
    core.sed,
    core.grep,
    core.bash,
  ],
  fetch: [
    {
      filename: "m4-1.4.19.tar.gz",
      sha256: "3be4a26d825ffdfda52a56fc43246456989a3630093cced3fbddf4771ee58a70",
      urls: [
        "https://ftpmirror.gnu.org/m4/m4-1.4.19.tar.gz",
        "https://ftp.gnu.org/gnu/m4/m4-1.4.19.tar.gz",
      ],
    },
  ],
};

{
  m4: m4,
}
