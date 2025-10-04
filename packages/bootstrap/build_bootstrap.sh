#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
REPO_DIR="$PWD"
TOOLCHAIN_URL="https://musl.cc/x86_64-linux-musl-native.tgz"
BUSYBOX_VERSION="1.36.1"
BUSYBOX_URL="https://busybox.net/downloads/busybox-${BUSYBOX_VERSION}.tar.bz2"
CACHE_DIR="${CACHE_DIR:-$REPO_DIR/.cache}"
TOOLCHAIN_DIR="$REPO_DIR/toolchain"
BUILD_DIR="$REPO_DIR/build"
BUSYBOX_SRC_DIR="$BUILD_DIR/busybox-src"
ROOTFS_DIR="$REPO_DIR/rootfs"
ROOTFS_TARBALL="$REPO_DIR/bootstrap.tar.zst"

mkdir -p "$CACHE_DIR" "$BUILD_DIR"

ZSTD_BIN="${ZSTD_BIN:-$(command -v zstd || true)}"
if [[ -z "$ZSTD_BIN" ]]; then
  echo "zstd not found on PATH" >&2
  exit 1
fi

TOOLCHAIN_ARCHIVE="$CACHE_DIR/$(basename "$TOOLCHAIN_URL")"
if [[ ! -f "$TOOLCHAIN_ARCHIVE" ]]; then
  echo "[+] Downloading $(basename "$TOOLCHAIN_ARCHIVE")"
  curl -fL --retry 3 --continue-at - --output "$TOOLCHAIN_ARCHIVE.part" "$TOOLCHAIN_URL"
  mv "$TOOLCHAIN_ARCHIVE.part" "$TOOLCHAIN_ARCHIVE"
else
  echo "[+] Using cached $(basename "$TOOLCHAIN_ARCHIVE")"
fi

BUSYBOX_ARCHIVE="$CACHE_DIR/$(basename "$BUSYBOX_URL")"
if [[ ! -f "$BUSYBOX_ARCHIVE" ]]; then
  echo "[+] Downloading $(basename "$BUSYBOX_ARCHIVE")"
  curl -fL --retry 3 --continue-at - --output "$BUSYBOX_ARCHIVE.part" "$BUSYBOX_URL"
  mv "$BUSYBOX_ARCHIVE.part" "$BUSYBOX_ARCHIVE"
else
  echo "[+] Using cached $(basename "$BUSYBOX_ARCHIVE")"
fi

echo "[+] Extracting toolchain"
rm -rf "$TOOLCHAIN_DIR"
mkdir -p "$TOOLCHAIN_DIR"
tar -xzf "$TOOLCHAIN_ARCHIVE" --strip-components=1 -C "$TOOLCHAIN_DIR"

echo "[+] Preparing BusyBox sources"
rm -rf "$BUSYBOX_SRC_DIR"
mkdir -p "$BUSYBOX_SRC_DIR"
tar -xjf "$BUSYBOX_ARCHIVE" --strip-components=1 -C "$BUSYBOX_SRC_DIR"

cd "$BUSYBOX_SRC_DIR"
make distclean >/dev/null 2>&1 || true
echo "[+] Configuring BusyBox"
make defconfig
if grep -q '^# CONFIG_STATIC is not set' .config; then
  sed -i 's/^# CONFIG_STATIC is not set/CONFIG_STATIC=y/' .config
elif ! grep -q '^CONFIG_STATIC=y' .config; then
  printf '\nCONFIG_STATIC=y\n' >> .config
fi

HOST_CC=$(command -v cc || command -v gcc || true)
if [[ -z "${HOST_CC:-}" ]]; then
  echo "Host C compiler (cc or gcc) not found" >&2
  exit 1
fi

JOBS=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
export PATH="$TOOLCHAIN_DIR/bin:$PATH"
export CC="x86_64-linux-musl-gcc"
export AR="ar"
export RANLIB="ranlib"
export STRIP="strip"

echo "[+] Building BusyBox"
make -j"$JOBS" busybox \
  HOSTCC="$HOST_CC" \
  CC="$CC" \
  AR="$AR" \
  RANLIB="$RANLIB"

"$STRIP" busybox
cd "$REPO_DIR"

echo "[+] Assembling rootfs"
rm -rf "$ROOTFS_DIR"
mkdir -p "$ROOTFS_DIR/bin" "$ROOTFS_DIR/tmp" "$ROOTFS_DIR/dev" "$ROOTFS_DIR/proc" "$ROOTFS_DIR/sys"
chmod 1777 "$ROOTFS_DIR/tmp"
install -m 0755 "$BUSYBOX_SRC_DIR/busybox" "$ROOTFS_DIR/bin/busybox"

"$ROOTFS_DIR/bin/busybox" --list | while IFS= read -r applet; do
  [[ -z "$applet" || "$applet" == "busybox" ]] && continue
  ln -sf busybox "$ROOTFS_DIR/bin/$applet"
done

echo "[+] Installing toolchain into rootfs"
rm -rf "$ROOTFS_DIR/toolchain"
cp -a "$TOOLCHAIN_DIR" "$ROOTFS_DIR/toolchain"

# Provide a /toolchain/usr/include symlink so host builds can find headers.
mkdir -p "$ROOTFS_DIR/toolchain/usr"
ln -sfn ../include "$ROOTFS_DIR/toolchain/usr/include"

for tool_path in "$ROOTFS_DIR"/toolchain/bin/*; do
  tool_name=$(basename "$tool_path")
  ln -sf "/toolchain/bin/$tool_name" "$ROOTFS_DIR/bin/$tool_name"
done

for tool in addr2line ar as c++ cpp g++ gcc ld nm objcopy objdump ranlib strip strings; do
  prefixed="$ROOTFS_DIR/toolchain/bin/x86_64-linux-musl-$tool"
  if [[ ! -e "$prefixed" && -e "$ROOTFS_DIR/toolchain/bin/$tool" ]]; then
    ln -sfn "$tool" "$prefixed"
  fi
  if [[ -e "$prefixed" ]]; then
    ln -sfn "/toolchain/bin/$(basename "$prefixed")" "$ROOTFS_DIR/bin/$(basename "$prefixed")"
  fi
done

rm -f "$ROOTFS_DIR/lib"
ln -s /toolchain/lib "$ROOTFS_DIR/lib"

echo "[+] Creating rootfs tarball"
rm -f "$ROOTFS_TARBALL"
tar -C "$ROOTFS_DIR" -cf - . | "$ZSTD_BIN" -T0 -q -o "$ROOTFS_TARBALL"

echo "[+] Done: rootfs at $ROOTFS_DIR"
echo "[+] Done: tarball at $ROOTFS_TARBALL"
