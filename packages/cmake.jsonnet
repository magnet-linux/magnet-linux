local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local openssl_pkg = import "./openssl.jsonnet";

local openssl = openssl_pkg.openssl;

local cmake = {
  name: "cmake-4.1.2",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export PKG_CONFIG_PATH="/usr/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export CPP="x86_64-linux-musl-gcc -E"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"
    export LDFLAGS=""

    tar -xzf /fetch/cmake-4.1.2.tar.gz
    cd cmake-4.1.2

    if ! ./bootstrap --prefix=/usr --parallel="${BUILD_PARALLELISM}" -- -DCMAKE_BUILD_TYPE=Release -DCMAKE_USE_OPENSSL=ON -DOPENSSL_ROOT_DIR=/usr -DOPENSSL_CRYPTO_LIBRARY=/usr/lib/libcrypto.so -DOPENSSL_SSL_LIBRARY=/usr/lib/libssl.so; then
        grep -n "[Ee]rror" Bootstrap.cmk/cmake_bootstrap.log || true
        exit 1
    fi

    make -j"${BUILD_PARALLELISM}"
    make DESTDIR=/out install

    mkdir -p /out/bin
    for tool in cmake ctest cpack; do
        if [ -f "/out/usr/bin/$tool" ]; then
            ln -sf ../usr/bin/$tool "/out/bin/$tool"
        fi
    done
  |||,
  runDeps: [core.musl_rt, core.libstdcpp_rt, openssl],
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
    openssl,
  ],
  fetch: [
    {
      filename: "cmake-4.1.2.tar.gz",
      sha256: "643f04182b7ba323ab31f526f785134fb79cba3188a852206ef0473fee282a15",
      urls: [
        "https://cmake.org/files/v4.1/cmake-4.1.2.tar.gz",
      ],
    },
  ],
};

{
  cmake: cmake,
}
