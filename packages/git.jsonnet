local bootstrap = import "./bootstrap.jsonnet";
local core = import "./core.jsonnet";
local curl_pkg = import "./curl.jsonnet";
local openssl_pkg = import "./openssl.jsonnet";
local zlib_pkg = import "./zlib.jsonnet";
local perl_pkg = import "./perl.jsonnet";

local curl = curl_pkg.curl;
local openssl = openssl_pkg.openssl;
local zlib = zlib_pkg.zlib;
local perl = perl_pkg.perl;

local git = {
  name: "git-2.51.0",
  build: |||
    #!/bin/sh
    set -euxo pipefail

    export PATH="/usr/bin:$PATH"
    export LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LIBRARY_PATH:-}"
    export LD_LIBRARY_PATH="/usr/lib64:/usr/lib:/lib:${LD_LIBRARY_PATH:-}"
    export PKG_CONFIG_PATH="/usr/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
    export CC="x86_64-linux-musl-gcc"
    export CXX="x86_64-linux-musl-g++"
    export AR="x86_64-linux-musl-gcc-ar"
    export RANLIB="x86_64-linux-musl-gcc-ranlib"
    export CFLAGS="-O2 -pipe"
    export CXXFLAGS="-O2 -pipe"
    export LDFLAGS=""
    export PERL_PATH="/usr/bin/perl"

    tar -xJf /fetch/git-2.51.0.tar.xz
    cd git-2.51.0

    make_args="prefix=/usr CC=x86_64-linux-musl-gcc AR=x86_64-linux-musl-gcc-ar RANLIB=x86_64-linux-musl-gcc-ranlib NO_TCLTK=YesPlease NO_GETTEXT=YesPlease NO_INSTALL_HARDLINKS=YesPlease NO_LIBPCRE2=YesPlease NO_REGEX=NeedsStartEnd NO_EXPAT=YesPlease"
    make -j"${BUILD_PARALLELISM}" ${make_args}
    make DESTDIR=/out ${make_args} install

    mkdir -p /out/bin
    for tool in git git-shell git-upload-pack git-receive-pack; do
        if [ -f "/out/usr/bin/$tool" ]; then
            ln -sf ../usr/bin/$tool "/out/bin/$tool"
        fi
    done
  |||,
  runDeps: [core.musl_rt, core.libgcc_rt, curl, openssl, zlib, perl],
  buildDeps: [
    core.make,
    core.binutils,
    core.gcc,
    core.musl,
    core.coreutils,
    core.tar,
    core.xz,
    core.gzip,
    core.gawk,
    core.sed,
    core.grep,
    core.bash,
    core.pkgconfig,
    core.findutils,
    core.diffutils,
    openssl,
    zlib,
    curl,
    perl,
  ],
  fetch: [
    {
      filename: "git-2.51.0.tar.xz",
      sha256: "60a7c2251cc2e588d5cd87bae567260617c6de0c22dca9cdbfc4c7d2b8990b62",
      urls: [
        "https://mirrors.edge.kernel.org/pub/software/scm/git/git-2.51.0.tar.xz",
        "https://www.kernel.org/pub/software/scm/git/git-2.51.0.tar.xz",
      ],
    },
  ],
};

{
  git: git,
}
