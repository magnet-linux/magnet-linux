local core = import "./core.jsonnet";
local perl_pkg = import "./perl.jsonnet";
local m4_pkg = import "./m4.jsonnet";
local bison_pkg = import "./bison.jsonnet";
local flex_pkg = import "./flex.jsonnet";

local perl = perl_pkg.perl;
local m4 = m4_pkg.m4;
local bison = bison_pkg.bison;
local flex = flex_pkg.flex;

local postgres_minimal = {
  name: "postgresql-17.0-minimal",
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
    export LDFLAGS=""

    tar -xzf /fetch/postgresql-17.0.tar.gz
    cd postgresql-17.0

    ./configure \
        --build=x86_64-linux-musl \
        --host=x86_64-linux-musl \
        --prefix=/usr \
        --disable-nls \
        --without-icu \
        --without-readline \
        --without-zlib \
        --without-libxml \
        --without-libxslt \
        --without-gssapi \
        --without-pam \
        --without-bsd-auth \
        --without-ldap \
        --without-bonjour \
        --without-selinux \
        --without-systemd \
        --without-lz4 \
        --without-zstd \
        --without-perl \
        --without-python \
        --without-tcl \
        --without-llvm

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
    core.pkgconfig,
    perl,
    m4,
    bison,
    flex,
  ],
  fetch: [
    {
      filename: "postgresql-17.0.tar.gz",
      sha256: "bf81c0c5161e456a886ede5f1f4133f43af000637e377156a02e7e83569081ad",
      urls: [
        "https://ftp.postgresql.org/pub/source/v17.0/postgresql-17.0.tar.gz",
        "https://ftp.us.debian.org/debian/pool/main/p/postgresql-17/postgresql-17_17.0.orig.tar.gz",
      ],
    },
  ],
};

{
  postgres_minimal: postgres_minimal,
}
