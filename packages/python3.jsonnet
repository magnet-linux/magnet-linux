local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local openssl_pkg = import "./openssl.jsonnet";
local zlib_pkg = import "./zlib.jsonnet";

local openssl = openssl_pkg.openssl;
local zlib = zlib_pkg.zlib;

local python3 = {
  name: "python-3.12.5",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export CPPFLAGS="-I/usr/include ${CPPFLAGS:-}"
    export LDFLAGS="-L/usr/lib ${LDFLAGS:-}"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export CPP="x86_64-linux-musl-gcc -E"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"
    export PKG_CONFIG_PATH="/usr/lib/pkgconfig"

    tar -xzf /fetch/Python-3.12.5.tgz
    cd Python-3.12.5

    ./configure --build=x86_64-linux-musl --host=x86_64-linux-musl --prefix=/usr --enable-shared --with-ensurepip=install --with-openssl=/usr --without-static-libpython

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install

    mkdir -p /out/bin
    if [ -f /out/usr/bin/python3 ]; then
        ln -sf ../usr/bin/python3 /out/bin/python3
    fi
    if [ -f /out/usr/bin/pip3 ]; then
        ln -sf ../usr/bin/pip3 /out/bin/pip3
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
      filename: "Python-3.12.5.tgz",
      sha256: "38dc4e2c261d49c661196066edbfb70fdb16be4a79cc8220c224dfeb5636d405",
      urls: [
        "https://www.python.org/ftp/python/3.12.5/Python-3.12.5.tgz",
        "https://www.mirrorservice.org/sites/www.python.org/ftp/python/3.12.5/Python-3.12.5.tgz",
      ],
    },
  ],
};

{
  python3: python3,
}
