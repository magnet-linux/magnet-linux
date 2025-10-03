local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";

local perl = {
  name: "perl-5.42.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export LD="x86_64-linux-musl-gcc"
    export CFLAGS="-O2 -pipe"
    export LDFLAGS=""

    tar -xzf /fetch/perl-5.42.0.tar.gz
    cd perl-5.42.0

    sh Configure \
        -des \
        -Dprefix=/usr \
        -Dvendorprefix=/usr \
        -Dsiteprefix=/usr \
        -Dman1dir=none \
        -Dman3dir=none \
        -Dusethreads \
        -Duseshrplib \
        -Duse64bitall \
        -Dcc="${CC}" \
        -Dld="${LD}"

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install

    mkdir -p /out/bin
    ln -sf ../usr/bin/perl /out/bin/perl
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
      filename: "perl-5.42.0.tar.gz",
      sha256: "e093ef184d7f9a1b9797e2465296f55510adb6dab8842b0c3ed53329663096dc",
      urls: [
        "https://www.cpan.org/src/5.0/perl-5.42.0.tar.gz",
      ],
    },
  ],
};

{
  perl: perl,
}
