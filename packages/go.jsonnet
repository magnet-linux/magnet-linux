local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";

local bootstrap_root = bootstrap.bootstrap;
local bootstrap_busybox = bootstrap.busybox;
local bootstrap_make = bootstrap.make;

local binutils = core.binutils;
local gcc = core.gcc;
local musl = core.musl;
local coreutils = core.coreutils;
local gawk = core.gawk;
local sed = core.sed;
local bash = core.bash;
local pkgconfig = core.pkgconfig;

local bootstrap14 = {
  name: "go-1.4.3",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export CGO_ENABLED=0
    export GOROOT_FINAL=/usr/lib/go-1.4.3

    mkdir -p /var/tmp

    tar -xzf "/fetch/go1.4.3.src.tar.gz"
    cd go/src
    ./make.bash
    cd ../..

    mkdir -p /out/usr/lib/go-1.4.3
    cp -a go/. /out/usr/lib/go-1.4.3/
    rm -rf /out/usr/lib/go-1.4.3/pkg/bootstrap

    mkdir -p /out/usr/bin
    ln -sf ../lib/go-1.4.3/bin/go /out/usr/bin/go-1.4
    ln -sf ../lib/go-1.4.3/bin/gofmt /out/usr/bin/gofmt-1.4
  |||,
  runDeps: [],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    bootstrap_make,
    binutils,
    gcc,
    musl,
    coreutils,
    gawk,
    sed,
    bash,
  ],
  fetch: [
    {
      filename: "go1.4.3.src.tar.gz",
      sha256: "9947fc705b0b841b5938c48b22dc33e9647ec0752bae66e50278df4f23f64959",
      urls: [
        "https://go.dev/dl/go1.4.3.src.tar.gz",
      ],
    },
  ],
};

local toolchain117 = {
  name: "go-1.17.13",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export GOROOT_BOOTSTRAP=/usr/lib/go-1.4.3
    export GOROOT_FINAL=/usr/lib/go-1.17.13

    mkdir -p /var/tmp

    tar -xzf "/fetch/go1.17.13.src.tar.gz"
    cd go/src
    ./make.bash
    cd ../..

    mkdir -p /out/usr/lib/go-1.17.13
    cp -a go/. /out/usr/lib/go-1.17.13/
    rm -rf /out/usr/lib/go-1.17.13/pkg/bootstrap

    mkdir -p /out/usr/bin
    ln -sf ../lib/go-1.17.13/bin/go /out/usr/bin/go-1.17
    ln -sf ../lib/go-1.17.13/bin/gofmt /out/usr/bin/gofmt-1.17
  |||,
  runDeps: [],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    bootstrap_make,
    binutils,
    gcc,
    musl,
    coreutils,
    pkgconfig,
    gawk,
    sed,
    bash,
    bootstrap14,
  ],
  fetch: [
    {
      filename: "go1.17.13.src.tar.gz",
      sha256: "a1a48b23afb206f95e7bbaa9b898d965f90826f6f1d1fc0c1d784ada0cd300fd",
      urls: [
        "https://go.dev/dl/go1.17.13.src.tar.gz",
      ],
    },
  ],
};

local toolchain120 = {
  name: "go-1.20.6",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export GOROOT_BOOTSTRAP=/usr/lib/go-1.17.13
    export GOROOT_FINAL=/usr/lib/go-1.20.6

    mkdir -p /var/tmp

    tar -xzf "/fetch/go1.20.6.src.tar.gz"
    cd go/src
    ./make.bash
    cd ../..

    mkdir -p /out/usr/lib/go-1.20.6
    cp -a go/. /out/usr/lib/go-1.20.6/
    rm -rf /out/usr/lib/go-1.20.6/pkg/bootstrap

    mkdir -p /out/usr/bin
    ln -sf ../lib/go-1.20.6/bin/go /out/usr/bin/go-1.20
    ln -sf ../lib/go-1.20.6/bin/gofmt /out/usr/bin/gofmt-1.20
  |||,
  runDeps: [],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    bootstrap_make,
    binutils,
    gcc,
    musl,
    coreutils,
    pkgconfig,
    gawk,
    sed,
    bash,
    toolchain117,
  ],
  fetch: [
    {
      filename: "go1.20.6.src.tar.gz",
      sha256: "62ee5bc6fb55b8bae8f705e0cb8df86d6453626b4ecf93279e2867092e0b7f70",
      urls: [
        "https://go.dev/dl/go1.20.6.src.tar.gz",
      ],
    },
  ],
};

local latest = {
  name: "go-1.23.1",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export GOROOT_BOOTSTRAP=/usr/lib/go-1.20.6
    export GOROOT_FINAL=/usr/lib/go-1.23.1

    mkdir -p /var/tmp

    tar -xzf "/fetch/go1.23.1.src.tar.gz"
    cd go/src
    ./make.bash
    cd ../..

    mkdir -p /out/usr/lib/go-1.23.1
    cp -a go/. /out/usr/lib/go-1.23.1/
    rm -rf /out/usr/lib/go-1.23.1/pkg/bootstrap

    ln -sf go-1.23.1 /out/usr/lib/go

    mkdir -p /out/usr/bin
    ln -sf ../lib/go/bin/go /out/usr/bin/go
    ln -sf ../lib/go/bin/gofmt /out/usr/bin/gofmt
  |||,
  runDeps: [binutils, gcc, musl, pkgconfig],
  buildDeps: [
    bootstrap_root,
    bootstrap_busybox,
    bootstrap_make,
    binutils,
    gcc,
    musl,
    coreutils,
    pkgconfig,
    gawk,
    sed,
    bash,
    toolchain120,
  ],
  fetch: [
    {
      filename: "go1.23.1.src.tar.gz",
      sha256: "6ee44e298379d146a5e5aa6b1c5b5d5f5d0a3365eabdd70741e6e21340ec3b0d",
      urls: [
        "https://go.dev/dl/go1.23.1.src.tar.gz",
      ],
    },
  ],
};

{
  bootstrap14: bootstrap14,
  toolchain117: toolchain117,
  toolchain120: toolchain120,
  latest: latest,
}
