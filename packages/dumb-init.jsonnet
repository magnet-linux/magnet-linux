local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";

local dumb_init = {
  name: "dumb-init-1.2.5",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export CC="gcc"

    tar -xzf /fetch/dumb-init-1.2.5.tar.gz
    cd dumb-init-*
    make build
    install -Dm755 dumb-init /out/bin/dumb-init
  |||,
  runDeps: [],
  buildDeps: [
    bootstrap.busybox,
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.bash,
  ],
  fetch: [
    {
      filename: "dumb-init-1.2.5.tar.gz",
      sha256: "3eda470d8a4a89123f4516d26877a727c0945006c8830b7e3bad717a5f6efc4e",
      urls: [
        "https://github.com/Yelp/dumb-init/archive/refs/tags/v1.2.5.tar.gz",
      ],
    },
  ],
};

{
  dumb_init: dumb_init,
}
