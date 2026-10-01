set -eu
mkdir -p /out/lib/chromium /out/bin /out/share/applications \
  /out/share/licenses/chromium
for file in chrome chrome_crashpad_handler icudtl.dat resources.pak \
  chrome_100_percent.pak chrome_200_percent.pak v8_context_snapshot.bin; do
  cp -a "out/magnet/$file" /out/lib/chromium/
done
cp -a out/magnet/locales /out/lib/chromium/
# These are ANGLE's runtime libraries. Do not install unrelated build tools or
# optional Vulkan layers that may also appear in the build output directory.
for file in out/magnet/libEGL.so out/magnet/libGLESv2.so out/magnet/snapshot_blob.bin; do
  if test -f "$file"; then cp -a "$file" /out/lib/chromium/; fi
done
# The user-namespace sandbox is enabled by the distro kernel. No setuid helper.
cp LICENSE /out/share/licenses/chromium/LICENSE
install -Dm644 chrome/app/theme/chromium/product_logo_128.png \
  /out/share/icons/hicolor/128x128/apps/chromium.png
cat > /out/bin/chromium <<'LAUNCHER'
#!/bin/sh
# Software rendering matches the initial Wayland desktop and virtio QEMU VM.
# Chromium requires an ordinary user account to retain its sandbox.
if [ "$(id -u)" = 0 ]; then
  echo 'Chromium requires an unprivileged user. Log in as that user and run start-sway.' >&2
  exit 1
fi
exec /lib/chromium/chrome --ozone-platform=wayland --disable-gpu "$@"
LAUNCHER
chmod 0755 /out/bin/chromium
cat > /out/share/applications/chromium.desktop <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=Chromium
Comment=Web browser
Exec=chromium %U
Icon=chromium
Terminal=false
Categories=Network;WebBrowser;
MimeType=text/html;x-scheme-handler/http;x-scheme-handler/https;
StartupWMClass=chromium
DESKTOP
