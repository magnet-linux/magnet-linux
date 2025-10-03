local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local python_pkg = import "./python3.jsonnet";

local python3 = python_pkg.python3;

local ninja = {
  name: "ninja-1.12.1",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"

    tar -xzf /fetch/ninja-1.12.1.tar.gz
    cd ninja-1.12.1

    python3 configure.py --bootstrap

    install -Dm755 ninja /out/usr/bin/ninja
    install -Dm644 misc/ninja.vim /out/usr/share/vim/vimfiles/syntax/ninja.vim
    install -Dm644 misc/bash-completion /out/usr/share/bash-completion/completions/ninja
  |||,
  runDeps: [core.musl_rt, core.libstdcpp_rt, core.libgcc_rt],
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
    core.libstdcpp_rt,
    core.libgcc_rt,
    python3,
  ],
  fetch: [
    {
      filename: "ninja-1.12.1.tar.gz",
      sha256: "821bdff48a3f683bc4bb3b6f0b5fe7b2d647cf65d52aeb63328c91a6c6df285a",
      urls: [
        "https://github.com/ninja-build/ninja/archive/refs/tags/v1.12.1.tar.gz",
      ],
    },
  ],
};

{
  ninja: ninja,
}
