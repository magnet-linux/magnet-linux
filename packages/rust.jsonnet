local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";

local rust = {
  name: "rust-1.90.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"

    tar -xJf /fetch/rust-1.90.0-x86_64-unknown-linux-musl.tar.xz
    cd rust-1.90.0-x86_64-unknown-linux-musl

    ./install.sh --prefix=/usr --destdir=/out

    mkdir -p /out/bin
    for tool in cargo rustc rustfmt rust-gdb rust-lldb; do
        if [ -f "/out/usr/bin/$tool" ]; then
            ln -sf ../usr/bin/$tool "/out/bin/$tool"
        fi
    done
  |||,
  runDeps: [core.binutils, core.gcc, core.musl],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl_rt,
    core.tar,
    core.xz,
    core.bash,
    core.coreutils,
    core.grep,
    core.sed,
  ],
  fetch: [
    {
      filename: "rust-1.90.0-x86_64-unknown-linux-musl.tar.xz",
      sha256: "ea531dbeb35d390e692e3ab02fb14f0771b4d3e301ae2023eb90d99ab726a5e8",
      urls: [
        "https://static.rust-lang.org/dist/rust-1.90.0-x86_64-unknown-linux-musl.tar.xz",
      ],
    },
  ],
};

{
  rust: rust,
}
