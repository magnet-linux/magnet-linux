local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local meson_pkg = import "./meson.jsonnet";
local ninja_pkg = import "./ninja.jsonnet";
local python_pkg = import "./python3.jsonnet";
local zlib_pkg = import "./zlib.jsonnet";

local meson = meson_pkg.meson;
local ninja = ninja_pkg.ninja;
local python3 = python_pkg.python3;
local zlib = zlib_pkg.zlib;

local libcap = {
  name: "libcap-2.70",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export CC="x86_64-linux-musl-gcc"
    export CFLAGS="-O2 -pipe -fPIC"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"

    tar -xJf /fetch/libcap-2.70.tar.xz
    cd libcap-2.70

    make -C libcap CC="${CC}" CFLAGS="${CFLAGS}" AR="${AR}" RANLIB="${RANLIB}" LDFLAGS="" -j"${BUILD_PARALLELISM}"
    make -C libcap DESTDIR=/out prefix=/usr lib=lib install
  |||,
  runDeps: [core.musl_rt],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.coreutils,
    core.tar,
    core.xz,
    core.gzip,
    core.grep,
    core.sed,
    core.bash,
    meson,
    ninja,
    python3,
    zlib,
  ],
  fetch: [
    {
      filename: "libcap-2.70.tar.xz",
      sha256: "23a6ef8aadaf1e3e875f633bb2d116cfef8952dba7bc7c569b13458e1952b30f",
      urls: [
        "https://www.kernel.org/pub/linux/libs/security/linux-privs/libcap2/libcap-2.70.tar.xz",
        "https://mirrors.edge.kernel.org/pub/linux/libs/security/linux-privs/libcap2/libcap-2.70.tar.xz",
      ],
    },
  ],
};

{
  libcap: libcap,
}
