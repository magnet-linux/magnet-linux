local b = import './build.libsonnet';
local tools = import '../desktop-tools.jsonnet';
local kt = import '../kernel-tools.jsonnet';
local shared = import '../browser-common/toolchain.jsonnet';
local llvm = shared.llvm;
local rust = shared.rust;
local bash = shared.bash;
local autoconf = b.package('autoconf', |||
  ./configure --prefix=/ --program-suffix=2.13
  make
  make DESTDIR=/out install
|||, [kt.m4, kt.perl]);
local cbindgen = b.package('cbindgen', |||
  export CARGO_NET_OFFLINE=true
  export RUSTFLAGS='-C target-feature=-crt-static'
  mkdir -p /build/vendor .cargo
  python3 - <<'PY'
  import json, pathlib, tarfile, tomllib
  lock = tomllib.loads(pathlib.Path('Cargo.lock').read_text())
  for package in lock['package']:
      if not package.get('source', '').startswith('registry+'):
          continue
      name = package['name'] + '-' + package['version']
      with tarfile.open('/fetch/' + name + '.crate') as archive:
          archive.extractall('/build/vendor', filter='data')
      pathlib.Path('/build/vendor', name, '.cargo-checksum.json').write_text(
          json.dumps({'files': {}, 'package': package['checksum']}))
  PY
  cat > .cargo/config.toml <<'EOF'
  [source.crates-io]
  replace-with = "vendored-sources"
  [source.vendored-sources]
  directory = "/build/vendor"
  EOF
  cargo build --release --locked --offline -j"$BUILD_PARALLELISM"
  install -Dm755 target/release/cbindgen /out/bin/cbindgen
  install -Dm644 LICENSE /out/share/licenses/cbindgen/LICENSE
|||, [tools.cxx_runtime], [rust, tools.python]) {
  fetch+: import './cbindgen-crates.libsonnet',
};
local wasi_libc = b.package('wasi-libc', |||
  # Only the static sysroot is needed; shared WASI libraries would bootstrap
  # compiler-rt via an upstream network download.
  make -j"$BUILD_PARALLELISM" CC=clang AR=llvm-ar NM=llvm-nm \
    BUILD_LIBSETJMP=no no-check-symbols
  mkdir -p /out/share/wasi-sysroot
  cp -a sysroot/. /out/share/wasi-sysroot/
|||, [], [llvm, bash]);
local wasi_builtins = b.package('llvm', |||
  cmake -S compiler-rt/lib/builtins -B build -G Ninja \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_SYSTEM_NAME=Generic \
    -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
    -DCMAKE_C_COMPILER_TARGET=wasm32-wasi \
    -DCMAKE_C_FLAGS='--sysroot=/share/wasi-sysroot' \
    -DCMAKE_AR=/bin/llvm-ar -DCMAKE_RANLIB=/bin/llvm-ranlib \
    -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DCOMPILER_RT_DEFAULT_TARGET_ONLY=ON \
    -DCOMPILER_RT_BAREMETAL_BUILD=ON -DCOMPILER_RT_OS_DIR=wasi \
    -DLLVM_CMAKE_DIR=/lib/cmake/llvm
  ninja -C build -j"$BUILD_PARALLELISM"
  install -Dm644 build/lib/wasi/libclang_rt.builtins-wasm32.a \
    /out/lib/clang/20/lib/wasi/libclang_rt.builtins-wasm32.a
|||, [wasi_libc], [llvm, tools.cmake, tools.ninja, tools.python]) {
  name: 'wasi-compiler-rt-20.1.8',
};
local wasi_cxx = b.package('llvm', |||
  # Static WASI C++ runtime, following wasi-sdk's LLVM runtime configuration.
  mkdir -p /build/cmake/Platform
  echo 'set(WASI 1)' > /build/cmake/Platform/WASI.cmake
  cmake -S runtimes -B build -G Ninja \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_SYSTEM_NAME=WASI \
    -DCMAKE_SYSTEM_PROCESSOR=wasm32 -DCMAKE_MODULE_PATH=/build/cmake \
    -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
    -DCMAKE_C_COMPILER_TARGET=wasm32-wasi -DCMAKE_CXX_COMPILER_TARGET=wasm32-wasi \
    -DCMAKE_SYSROOT=/share/wasi-sysroot -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DCMAKE_AR=/bin/llvm-ar -DCMAKE_RANLIB=/bin/llvm-ranlib \
    -DCMAKE_INSTALL_PREFIX=/share/wasi-sysroot \
    -DCMAKE_C_LINKER_DEPFILE_SUPPORTED=OFF -DCMAKE_CXX_LINKER_DEPFILE_SUPPORTED=OFF \
    -DLLVM_ENABLE_RUNTIMES='libcxx;libcxxabi' -DLLVM_ENABLE_PER_TARGET_RUNTIME_DIR=OFF \
    -DLIBCXX_ENABLE_SHARED=OFF -DLIBCXXABI_ENABLE_SHARED=OFF \
    -DLIBCXX_ENABLE_EXPERIMENTAL_LIBRARY=OFF -DLIBCXX_ENABLE_EXCEPTIONS=OFF \
    -DLIBCXX_ENABLE_ABI_LINKER_SCRIPT=OFF -DLIBCXX_CXX_ABI=libcxxabi \
    -DLIBCXX_HAS_MUSL_LIBC=ON -DLIBCXX_ABI_VERSION=2 \
    -DLIBCXXABI_ENABLE_EXCEPTIONS=OFF -DLIBCXXABI_SILENT_TERMINATE=ON \
    -DLIBCXXABI_USE_LLVM_UNWINDER=OFF -DUNIX=ON \
    -DLIBCXX_LIBDIR_SUFFIX=/wasm32-wasi -DLIBCXXABI_LIBDIR_SUFFIX=/wasm32-wasi \
    -DLIBCXX_INCLUDE_TESTS=OFF -DLIBCXX_INCLUDE_BENCHMARKS=OFF \
    -DLIBCXXABI_INCLUDE_TESTS=OFF
  ninja -C build -j"$BUILD_PARALLELISM"
  DESTDIR=/out cmake --install build
  printf '#include <string>\nint main() { return std::string("ok").size() != 2; }\n' > /build/check.cpp
  clang++ --target=wasm32-wasi --sysroot=/share/wasi-sysroot \
    -isystem /out/share/wasi-sysroot/include/c++/v1 \
    -L/out/share/wasi-sysroot/lib/wasm32-wasi /build/check.cpp -o /build/check.wasm
|||, [wasi_builtins], [llvm, tools.cmake, tools.ninja, tools.python]) {
  name: 'wasi-libcxx-20.1.8',
};
shared + { autoconf: autoconf, cbindgen: cbindgen, wasi: wasi_cxx }
