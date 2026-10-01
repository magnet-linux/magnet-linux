local native = (import '../browser-common/toolchain.libsonnet')(
  import './sources.libsonnet', llvmVersion='22.1.8', llvmMajor='22', largeXZ=true);
local b = import './build.libsonnet';
local tools = import '../desktop-tools.jsonnet';
local gn = b.package('gn', |||
  unset CFLAGS
  python3 build/gen.py --no-last-commit-position --no-static-libstdc++ \
    --no-strip --allow-warnings
  ninja -C out -j"$BUILD_PARALLELISM"
  out/gn_unittests
  install -Dm755 out/gn /out/bin/gn
  install -Dm644 LICENSE /out/share/licenses/gn/LICENSE
|||, [tools.cxx_runtime], [tools.python, tools.ninja]);
local builtins = b.package('llvm', |||
  cmake -S compiler-rt/lib/builtins -B build -G Ninja \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/ \
    -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
    -DCMAKE_C_COMPILER_TARGET=x86_64-unknown-linux-musl \
    -DCMAKE_CXX_COMPILER_TARGET=x86_64-unknown-linux-musl \
    -DCMAKE_AR=/bin/llvm-ar -DCMAKE_RANLIB=/bin/llvm-ranlib \
    -DCOMPILER_RT_DEFAULT_TARGET_ONLY=ON \
    -DCOMPILER_RT_INSTALL_PATH=/lib/clang/22
  ninja -C build -j"$BUILD_PARALLELISM"
  DESTDIR=/out cmake --install build
  mkdir -p /out/lib/clang/22/include/sanitizer
  cp compiler-rt/include/sanitizer/*.h /out/lib/clang/22/include/sanitizer/
  install -Dm644 LICENSE.TXT /out/share/licenses/compiler-rt/LICENSE.TXT
|||, [], [native.llvm, tools.cmake, tools.ninja, tools.python]) {
  name: 'compiler-rt-builtins-22.1.8',
};
local bindgen = b.package('bindgen', |||
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
  install -Dm755 target/release/bindgen /out/bin/bindgen
  install -Dm644 LICENSE /out/share/licenses/bindgen/LICENSE
|||, [tools.cxx_runtime, native.llvm], [native.rust, tools.python]) {
  fetch+: import './bindgen-crates.libsonnet',
};
local esbuild = b.package('esbuild', |||
  export GOTOOLCHAIN=local GOSUMDB=off GOPROXY=file:///build/go-proxy GOFLAGS=-modcacherw
  export GOCACHE=/build/go-cache GOPATH=/build/go-path CGO_ENABLED=0
  mkdir -p /build/go-proxy/golang.org/x/sys/@v
  version=v0.0.0-20220715151400-c0bba94af5f8
  cp /fetch/esbuild-sys.mod "/build/go-proxy/golang.org/x/sys/@v/$version.mod"
  cp /fetch/esbuild-sys.zip "/build/go-proxy/golang.org/x/sys/@v/$version.zip"
  printf '{"Version":"%s","Time":"2022-07-15T15:14:00Z"}\n' "$version" > "/build/go-proxy/golang.org/x/sys/@v/$version.info"
  go mod download
  go mod verify
  go build -mod=readonly -trimpath -o /out/bin/esbuild ./cmd/esbuild
  mkdir -p /out/lib/node_modules/esbuild
  tar --no-same-owner -xzf /fetch/esbuild-0.27.1.tgz --strip-components=1 -C /out/lib/node_modules/esbuild
  # The npm API locates its sibling binary here without running install.js.
  cp /out/bin/esbuild /out/lib/node_modules/esbuild/bin/esbuild
  /out/bin/esbuild --version
|||, [native.node], [(import '../go.jsonnet').go]) {
  fetch+: [(import './sources.libsonnet')[name].archive
           for name in ['esbuild-js', 'go-sys-mod', 'go-sys-zip']],
};
native + { gn: gn, builtins: builtins, bindgen: bindgen, esbuild: esbuild }
