// Versioned native toolchains share build recipes; callers override source pins.
function(pins={}, llvmVersion='20.1.8', llvmMajor='20', largeXZ=false)
local core = import '../core.jsonnet';
local base = (import './build.libsonnet') { sources+: pins };
// BusyBox's small XZ decoder cannot read the 128 MiB dictionary in newer
// upstream Rust archives. Keep a full decoder private to these builds.
local xz = core.xz {
  name: 'build-xz-5.4.6',
  buildDeps: [(import '../bootstrap.jsonnet').bootstrap, core.musl],
  build: super.build {
    script: std.strReplace(std.strReplace(std.strReplace(super.script,
      '--prefix=/', '--prefix=/opt/build-xz'),
      '--libdir=/lib', '--libdir=/opt/build-xz/lib'),
      '--disable-nls', '--disable-shared --enable-static --disable-nls'),
  },
};
local b = base {
  package(name, script, runDeps=[], buildDeps=[]):
    local p = super.package(name, script, runDeps, buildDeps);
    if largeXZ && std.startsWith(name, 'rust') then p {
      buildDeps+: [xz],
      build: super.build {
        script: std.strReplace(super.script, 'tar --no-same-owner -xf',
          'tar --no-same-owner -I /opt/build-xz/bin/xz -xf'),
      },
    } else p,
};
local tools = import '../desktop-tools.jsonnet';
local libs = import '../desktop-libs.jsonnet';
local kt = import '../kernel-tools.jsonnet';
local bash = core.bash {
  name: 'firefox-build-bash-5.2.37',
  buildDeps: [(import '../bootstrap.jsonnet').bootstrap],
  build: super.build { script+: '\nrm -f /out/bin/sh\n' },
};
local bzip2 = b.package('bzip2', |||
  make -j"$BUILD_PARALLELISM" CFLAGS='-O2 -fPIC' libbz2.a
  install -Dm644 libbz2.a /out/lib/libbz2.a
  install -Dm644 bzlib.h /out/include/bzlib.h
|||);
// Supplement the shared Python package without replacing its files.
local python_bz2 = b.package('python', |||
  mkdir -p /out/lib/python3.13/lib-dynload
  gcc -shared -fPIC $(python3-config --cflags) -IInclude -IInclude/internal \
    Modules/_bz2module.c /lib/libbz2.a \
    -o /out/lib/python3.13/lib-dynload/_bz2$(python3-config --extension-suffix)
|||, [tools.python], [bzip2]) { name: 'python-bz2-3.13.7' };
local sqlite = b.package('sqlite', |||
  gcc -O2 -fPIC -c sqlite3.c -o sqlite3.o
  ar rcs libsqlite3.a sqlite3.o
  install -Dm644 libsqlite3.a /out/lib/libsqlite3.a
  install -Dm644 sqlite3.h /out/include/sqlite3.h
  install -Dm644 sqlite3ext.h /out/include/sqlite3ext.h
  mkdir -p /out/lib/pkgconfig
  cat > /out/lib/pkgconfig/sqlite3.pc <<'EOF'
  Name: SQLite
  Description: SQLite for Python's build-time cache
  Version: 3.53.4
  Libs: -L/lib -lsqlite3 -lm -ldl -pthread
  Cflags: -I/include
  EOF
|||);
local python_browser = b.package('python', |||
  # Mach imports curses, ctypes, SSL and SQLite even for an offline build.
  # Configure against these libraries but install only the missing modules.
  ./configure --prefix=/ --without-ensurepip --disable-test-modules
  suffix=$(python3-config --extension-suffix)
  make -j"$BUILD_PARALLELISM" "Modules/_curses$suffix" \
    "Modules/_curses_panel$suffix" "Modules/_ctypes$suffix" "Modules/_ssl$suffix" \
    "Modules/_sqlite3$suffix"
  mkdir -p /out/lib/python3.13/lib-dynload
  cp "Modules/_curses$suffix" "Modules/_curses_panel$suffix" \
    "Modules/_ctypes$suffix" "Modules/_ssl$suffix" "Modules/_sqlite3$suffix" \
    /out/lib/python3.13/lib-dynload/
  PYTHONPATH=/out/lib/python3.13/lib-dynload python3 -c \
    'import curses, ctypes, ssl, sqlite3; assert sqlite3.connect(":memory:").execute("select 42").fetchone() == (42,)'
|||, [tools.python, libs.ncurses, libs.libffi],
  [sqlite, (import '../openssl.jsonnet').openssl]) { name: 'python-browser-modules-3.13.7' };
local llvm_upstream_layout = b.package('llvm', |||
  cmake -S llvm -B build -G Ninja \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/ \
    -DCMAKE_INSTALL_BINDIR=/bin -DCMAKE_INSTALL_LIBDIR=/lib \
    -DCMAKE_INSTALL_INCLUDEDIR=/include -DCMAKE_INSTALL_DATAROOTDIR=/share \
    -DLLVM_ENABLE_PROJECTS='clang;lld' \
    -DLLVM_TARGETS_TO_BUILD='X86;WebAssembly' \
    -DLLVM_BUILD_LLVM_DYLIB=ON -DLLVM_LINK_LLVM_DYLIB=ON \
    -DLLVM_ENABLE_TERMINFO=OFF -DLLVM_ENABLE_LIBXML2=OFF \
    -DLLVM_ENABLE_ZSTD=OFF -DLLVM_ENABLE_ZLIB=ON \
    -DLLVM_INCLUDE_TESTS=OFF -DLLVM_INCLUDE_BENCHMARKS=OFF \
    -DLLVM_INCLUDE_EXAMPLES=OFF -DLLVM_PARALLEL_LINK_JOBS=2
  ninja -C build -j"$BUILD_PARALLELISM"
  DESTDIR=/out cmake --install build
|||, [tools.cxx_runtime, libs.zlib], [tools.cmake, tools.ninja, tools.python]);
// LLVM's auxiliary analyzer installation still creates /usr/libexec despite
// the root prefix. Keep that intermediate private and publish a normalized
// toolchain: a real /usr directory would replace this tree's /usr -> . link.
local llvm = {
  name: 'llvm-native-' + llvmVersion,
  buildEnv: { PATH: '/bin' },
  buildDeps: [(import '../bootstrap.jsonnet').bootstrap, llvm_upstream_layout],
  runDeps: llvm_upstream_layout.runDeps,
  fetch: [],
  build: { kind: 'script', script: std.strReplace(std.strReplace(|||
    mkdir -p /out/bin /out/lib/cmake /out/include /out/share /out/libexec
    for tool in /bin/llvm-* /bin/clang* /bin/lld* /bin/ld.lld /bin/ld64.lld \
      /bin/wasm-ld /bin/llc /bin/lli /bin/opt /bin/bugpoint /bin/c-index-test \
      /bin/diagtool /bin/dsymutil /bin/git-clang-format /bin/hmaptool \
      /bin/amdgpu-arch /bin/nvptx-arch /bin/analyze-build /bin/intercept-build \
      /bin/scan-* /bin/verify-uselistorder /bin/reduce-chunk-list /bin/sancov /bin/sanstats; do
      cp -a "$tool" /out/bin/
    done
    cp -a /include/llvm /include/llvm-c /include/clang /include/clang-c /include/lld /out/include/
    cp -a /lib/libLLVM* /lib/libclang* /lib/liblld* /lib/libLTO* /lib/libRemarks* \
      /lib/clang /lib/libear /lib/libscanbuild /out/lib/
    cp -a /lib/cmake/llvm /lib/cmake/clang /lib/cmake/lld /out/lib/cmake/
    cp -a /share/clang /share/opt-viewer /share/scan-build /share/scan-view /out/share/
    cp -a /usr/libexec/. /out/libexec/
    # Upstream's default target is GNU/Linux even when built on musl. Explicit
    # later --target options (e.g. WASI) still override this native default.
    rm /out/bin/clang /out/bin/clang++
    cat > /out/bin/clang <<'EOF'
    #!/bin/sh
    exec /bin/clang-20 --target=x86_64-unknown-linux-musl "$@"
    EOF
    cat > /out/bin/clang++ <<'EOF'
    #!/bin/sh
    exec /bin/clang-20 --driver-mode=g++ --target=x86_64-unknown-linux-musl "$@"
    EOF
    chmod 0755 /out/bin/clang /out/bin/clang++
    test ! -e /out/usr
    /out/bin/clang --version
    /out/bin/llvm-config --version
  |||, 'clang-20', 'clang-' + llvmMajor),
    '  cp -a "$tool" /out/bin/',
    if llvmMajor == '20' then '  cp -a "$tool" /out/bin/'
    else '  if test -e "$tool"; then cp -a "$tool" /out/bin/; fi') },
};
// A pinned upstream musl compiler is used only to bootstrap the source build.
local rust_bootstrap = b.package('rust-bootstrap', |||
  ./install.sh --prefix=/out/opt/rust-bootstrap --disable-ldconfig \
    --components=rustc,rust-std-x86_64-unknown-linux-musl,cargo
|||, [], [bash]);
local rust = b.package('rust', |||
  # Match the native distro's shared musl, including Cargo build scripts which
  # do not inherit target RUSTFLAGS when Cargo is passed --target.
  sed -i 's/base.crt_static_default = true;/base.crt_static_default = false;/' \
    compiler/rustc_target/src/spec/targets/x86_64_unknown_linux_musl.rs
  export CARGO_NET_OFFLINE=true OPENSSL_DIR=/ OPENSSL_STATIC=1
  export PKG_CONFIG_ALL_STATIC=1
  cat > bootstrap.toml <<EOF
  change-id = "ignore"
  [llvm]
  download-ci-llvm = false
  link-shared = true
  [build]
  build = "x86_64-unknown-linux-musl"
  host = ["x86_64-unknown-linux-musl"]
  target = ["x86_64-unknown-linux-musl"]
  cargo = "/opt/rust-bootstrap/bin/cargo"
  rustc = "/opt/rust-bootstrap/bin/rustc"
  python = "/bin/python3"
  submodules = false
  vendor = true
  docs = false
  extended = true
  tools = ["cargo"]
  jobs = $BUILD_PARALLELISM
  [install]
  prefix = "/"
  sysconfdir = "/etc"
  [rust]
  channel = "stable"
  download-rustc = false
  debuginfo-level = 0
  codegen-units = 16
  codegen-tests = false
  # These are supplied by the external LLVM package.
  lld = false
  llvm-tools = false
  llvm-bitcode-linker = false
  lto = "off"
  [target.x86_64-unknown-linux-musl]
  cc = "/bin/gcc"
  cxx = "/bin/g++"
  linker = "/bin/gcc"
  llvm-config = "/bin/llvm-config"
  llvm-has-rust-patches = false
  crt-static = false
  musl-root = "/"
  EOF
  DESTDIR=/out python3 x.py install --stage 2 compiler/rustc library/std cargo
  /out/bin/rustc --version
  /out/bin/cargo --version
|||, [llvm, tools.cxx_runtime], [rust_bootstrap, tools.python, tools.cmake,
    kt.perl, bash, (import '../openssl.jsonnet').openssl]);
local node = b.package('node', |||
  ./configure --prefix=/ --without-npm --without-corepack
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  /out/bin/node --version
|||, [tools.cxx_runtime], [python_bz2]);
local nasm = b.package('nasm', |||
  ./configure --prefix=/
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);
local zip = b.package('zip', |||
  # Info-ZIP's feature probes predate modern C prototype requirements.
  make -f unix/Makefile -j"$BUILD_PARALLELISM" generic \
    CC='gcc -std=gnu89 -Wno-error=implicit-function-declaration -Wno-error=implicit-int'
  install -Dm755 zip /out/bin/zip
|||);
local patch = b.package('patch', |||
  ./configure --prefix=/opt/build-patch
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
|||);
{ xz: xz, patch: patch, llvm: llvm, rust_bootstrap: rust_bootstrap, rust: rust,
  python_bz2: python_bz2, python_browser: python_browser,
  node: node, nasm: nasm, zip: zip, bash: bash }
