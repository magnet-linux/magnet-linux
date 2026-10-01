# Runs inside the unpacked Chromium source, without network access.
set -eu
mkdir -p third_party/node/linux/node-linux-x64/bin
ln -sf /bin/node third_party/node/linux/node-linux-x64/bin/node
rm -rf third_party/devtools-frontend/src/node_modules/rollup
mkdir -p third_party/devtools-frontend/src/node_modules/rollup
# Rollup's portable WASM build avoids its downloaded glibc native addon.
tar --no-same-owner -xzf /fetch/wasm-node-4.22.4.tgz --strip-components=1 \
  -C third_party/devtools-frontend/src/node_modules/rollup
rm -rf third_party/devtools-frontend/src/node_modules/esbuild
ln -s /lib/node_modules/esbuild third_party/devtools-frontend/src/node_modules/esbuild
ln -sf /bin/esbuild third_party/devtools-frontend/src/third_party/esbuild/esbuild
mkdir -p third_party/gperf/cipd/bin
ln -sf /bin/gperf third_party/gperf/cipd/bin/gperf
for arch in amd64 arm64 arm ''; do
  mkdir -p "third_party/dawn/tools/golang/linux-$arch/bin"
  ln -sf /bin/go "third_party/dawn/tools/golang/linux-$arch/bin/go"
done
# Read the distro's USB IDs at runtime.
sed -i 's|//third_party/usb_ids/usb.ids|/share/hwdata/usb.ids|g' \
  services/device/public/cpp/usb/BUILD.gn
printf '%s\n' x86_64-unknown-linux-musl >> build/rust/known-target-triples.txt
# Chromium's release tarball omits this test-only input but GN reads its graph.
touch chrome/test/data/webui/i18n_process_css_test.html
