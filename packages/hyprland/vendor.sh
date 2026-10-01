export CARGO_NET_OFFLINE=true
export RUSTFLAGS='-C target-feature=-crt-static'
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
