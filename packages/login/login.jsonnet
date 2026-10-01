local b = (import '../browser-common/build.libsonnet') { sources+: import './sources.libsonnet' };
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local pam = b.meson('linux-pam',
  '-Dauto_features=disabled -Dpam_unix=enabled -Dpam_unix-try-getspnam=true -Ddocs=disabled -Dexamples=false -Dxtests=false -Dsbindir=bin -Dsecuredir=/lib/security -Dvendordir=/share/pam',
  [], [], '', |||
    # Configuration is provisioned once by setup-login.sh, outside magpkg.
    rm -rf /out/etc
    install -Dm644 Copyright /out/share/licenses/linux-pam/Copyright
  |||);
local greetd = b.package('greetd', |||
  export CARGO_NET_OFFLINE=true RUSTFLAGS='-C target-feature=-crt-static'
  mkdir -p /build/vendor .cargo
  python3 - <<'PY'
  import json, pathlib, tarfile, tomllib
  for package in tomllib.loads(pathlib.Path('Cargo.lock').read_text())['package']:
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
  cargo build --release --locked --offline -p greetd -p agreety -j"$BUILD_PARALLELISM"
  install -Dm755 target/release/greetd /out/bin/greetd
  install -Dm755 target/release/agreety /out/bin/agreety
  install -Dm644 LICENSE /out/share/licenses/greetd/LICENSE
|||, [pam], [(import '../chromium/toolchain.jsonnet').rust, t.python]) {
  fetch+: import './greetd-crates.libsonnet',
};
local gtkgreet = b.meson('gtkgreet', '-Dlayershell=disabled -Dman-pages=disabled',
  [common.gtk, l.jsonc], [],
  "patch -p1 <<'MAGNET_PATCH'\n" + (importstr './fixed-command.patch') + "\nMAGNET_PATCH\n", |||
    install -Dm644 LICENSE /out/share/licenses/gtkgreet/LICENSE
  |||);
{ pam: pam, greetd: greetd, gtkgreet: gtkgreet }
