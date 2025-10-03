local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local python_pkg = import "./python3.jsonnet";

local python3 = python_pkg.python3;

local meson = {
  name: "meson-1.6.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export PYTHONDONTWRITEBYTECODE="1"

    tar -xzf /fetch/meson-1.6.0.tar.gz
    cd meson-1.6.0

    python3 -m compileall mesonbuild

    install -d /out/usr/lib/meson
    cp -R mesonbuild /out/usr/lib/meson/mesonbuild
    install -Dm755 meson.py /out/usr/lib/meson/meson.py

    install -d /out/usr/bin
    printf '#!/usr/bin/env sh\nexec python3 /usr/lib/meson/meson.py "$@"\n' > /out/usr/bin/meson
    chmod 0755 /out/usr/bin/meson

    if [ -f man/meson.1 ]; then
        install -Dm644 man/meson.1 /out/usr/share/man/man1/meson.1
    fi
  |||,
  runDeps: [core.musl_rt, python3],
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
    python3,
  ],
  fetch: [
    {
      filename: "meson-1.6.0.tar.gz",
      sha256: "999b65f21c03541cf11365489c1fad22e2418bb0c3d50ca61139f2eec09d5496",
      urls: [
        "https://github.com/mesonbuild/meson/releases/download/1.6.0/meson-1.6.0.tar.gz",
        "https://files.pythonhosted.org/packages/source/m/meson/meson-1.6.0.tar.gz",
      ],
    },
  ],
};

{
  meson: meson,
}
