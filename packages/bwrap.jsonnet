local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local meson_pkg = import "./meson.jsonnet";
local ninja_pkg = import "./ninja.jsonnet";
local python_pkg = import "./python3.jsonnet";
local zlib_pkg = import "./zlib.jsonnet";
local libcap_pkg = import "./libcap.jsonnet";

local meson = meson_pkg.meson;
local ninja = ninja_pkg.ninja;
local python3 = python_pkg.python3;
local zlib = zlib_pkg.zlib;
local libcap = libcap_pkg.libcap;

local bwrap = {
  name: "bwrap-0.9.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export PKG_CONFIG_STATIC=1
    export CC="x86_64-linux-musl-gcc"
    export CFLAGS="-O2 -pipe"
    export LDFLAGS="-static ${LDFLAGS:-}"
    export PKG_CONFIG_PATH="/usr/lib/pkgconfig"

    tar -xJf /fetch/bubblewrap-0.9.0.tar.xz
    cd bubblewrap-0.9.0

    meson setup build --prefix=/usr --buildtype=release -Dselinux=disabled -Dman=disabled -Dtests=false -Ddefault_library=static -Dprefer_static=true
    ninja -C build -j"${BUILD_PARALLELISM}"
    DESTDIR=/out ninja -C build install

    mkdir -p /out/bin
    if [ -f /out/usr/bin/bwrap ]; then
        ln -sf ../usr/bin/bwrap /out/bin/bwrap
    fi
  |||,
  runDeps: [core.musl_rt, libcap],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.coreutils,
    core.tar,
    core.xz,
    core.gawk,
    core.sed,
    core.grep,
    core.bash,
    core.pkgconfig,
    meson,
    ninja,
    python3,
    zlib,
    libcap,
  ],
  fetch: [
    {
      filename: "bubblewrap-0.9.0.tar.xz",
      sha256: "c6347eaced49ac0141996f46bba3b089e5e6ea4408bc1c43bab9f2d05dd094e1",
      urls: [
        "https://github.com/containers/bubblewrap/releases/download/v0.9.0/bubblewrap-0.9.0.tar.xz",
        "https://gitlab.com/bubblewrap/bubblewrap/-/archive/v0.9.0/bubblewrap-0.9.0.tar.xz",
      ],
    },
  ],
};

{
  bwrap: bwrap,
}
