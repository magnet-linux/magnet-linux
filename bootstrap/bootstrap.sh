#!/bin/sh
set -eu

umask 022

target=x86_64-linux-musl
jobs=${BOOTSTRAP_JOBS:-8}
tools=${BOOTSTRAP_TOOLS:-/}
sources=${BOOTSTRAP_SOURCES:-/src}
build_dir=${BOOTSTRAP_BUILD_DIR:-/build}
out_root=${BOOTSTRAP_OUT_ROOT:-/out}
recipe=${BOOTSTRAP_RECIPE:-/recipe}

if [ "$tools" = "/" ]; then
    tool_bin=/bin
else
    tool_bin=$tools/bin
fi

export PATH="${BOOTSTRAP_PATH:-$tool_bin:/bin:/sbin:/usr/bin:/usr/sbin}"

cc=$tool_bin/$target-gcc
cxx=$tool_bin/$target-g++
host_cc=$(command -v cc || command -v gcc)

test -x "$cc"
test -x "$cxx"
test -d "$sources"
test -d "$build_dir"
test -d "$out_root"

export LC_ALL=C
export LANG=C
export TZ=UTC
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1784851200}"
export CONFIG_SITE=/dev/null
export MAKEFLAGS=

echo "==> Building native musl toolchain"
mcm=$build_dir/musl-cross-make
mkdir -p "$mcm"
tar -xzf "$sources/musl-cross-make.tar.gz" --strip-components=1 -C "$mcm"
sed -i \
    -e 's/tar zxvf/tar zxf/' \
    -e 's/tar jxvf/tar jxf/' \
    -e 's/tar Jxvf/tar Jxf/' \
    "$mcm/Makefile"
mkdir -p "$mcm/sources"
ln -s "$sources/binutils.tar.gz" "$mcm/sources/binutils-2.44.tar.gz"
ln -s "$sources/gcc.tar.xz" "$mcm/sources/gcc-15.1.0.tar.xz"
ln -s "$sources/gmp.tar.xz" "$mcm/sources/gmp-6.3.0.tar.xz"
ln -s "$sources/linux.tar.xz" "$mcm/sources/linux-6.15.7.tar.xz"
ln -s "$sources/mpc.tar.gz" "$mcm/sources/mpc-1.3.1.tar.gz"
ln -s "$sources/mpfr.tar.xz" "$mcm/sources/mpfr-4.2.2.tar.xz"
ln -s "$sources/musl.tar.gz" "$mcm/sources/musl-1.2.6.tar.gz"

cat >"$mcm/config.mak" <<EOF
TARGET = $target
NATIVE = 1
OUTPUT = $out_root
SOURCES = $build_dir/musl-cross-make/sources
BINUTILS_VER = 2.44
GCC_VER = 15.1.0
MUSL_VER = 1.2.6
GMP_VER = 6.3.0
MPC_VER = 1.3.1
MPFR_VER = 4.2.2
ISL_VER =
LINUX_VER = 6.15.7
COMMON_CONFIG += CC="$cc -static --static"
COMMON_CONFIG += CXX="$cxx -static --static"
COMMON_CONFIG += CFLAGS="-g0 -O2 -fno-ident"
COMMON_CONFIG += CXXFLAGS="-g0 -O2 -fno-ident"
COMMON_CONFIG += LDFLAGS="-s"
COMMON_CONFIG += --disable-nls
COMMON_CONFIG += --with-debug-prefix-map=$build_dir=.
GCC_CONFIG += --disable-decimal-float
GCC_CONFIG += --disable-fixed-point
GCC_CONFIG += --disable-libitm
GCC_CONFIG += --disable-libquadmath
GCC_CONFIG += --disable-lto
EOF

make -s -C "$mcm" -j"$jobs" extract_all
cp -L "$mcm/linux-6.15.7/Makefile" "$mcm/linux-6.15.7/Makefile.copy"
mv "$mcm/linux-6.15.7/Makefile.copy" "$mcm/linux-6.15.7/Makefile"
patch --batch -d "$mcm/linux-6.15.7" -p1 <<'EOF'
--- a/Makefile
+++ b/Makefile
@@ -1352,9 +1352,11 @@
 
 quiet_cmd_headers_install = INSTALL $(INSTALL_HDR_PATH)/include
       cmd_headers_install = \
 	mkdir -p $(INSTALL_HDR_PATH); \
-	rsync -mrl --include='*/' --include='*\.h' --exclude='*' \
-	usr/include $(INSTALL_HDR_PATH)
+	rm -rf $(INSTALL_HDR_PATH)/include; \
+	cp -R usr/include $(INSTALL_HDR_PATH); \
+	find $(INSTALL_HDR_PATH)/include -type f ! -name '*.h' -delete; \
+	find $(INSTALL_HDR_PATH)/include -depth -type d -empty -delete
 
 PHONY += headers_install
 headers_install: headers
EOF

if ! make -s -C "$mcm" -j"$jobs"; then
    echo "==> Parallel toolchain build did not complete; finishing serially"
    if ! make -s -C "$mcm" -j1; then
        echo "==> Rechecking the completed toolchain targets"
        make -s -C "$mcm" -j1
    fi
fi
make -s -C "$mcm" install
mkdir -p "$out_root/usr"
ln -s ../include "$out_root/usr/include"

echo "==> Building BusyBox"
busybox=$build_dir/busybox
mkdir -p "$busybox"
tar -xjf "$sources/busybox.tar.bz2" --strip-components=1 -C "$busybox"
make -s -C "$busybox" defconfig
sed -i 's/^# CONFIG_STATIC is not set$/CONFIG_STATIC=y/' "$busybox/.config"
sed -i 's/^CONFIG_TC=y$/# CONFIG_TC is not set/' "$busybox/.config"
make -s -C "$busybox" \
    -j"$jobs" \
    HOSTCC="$host_cc" \
    CROSS_COMPILE="$tool_bin/$target-"
make -s -C "$busybox" \
    CONFIG_PREFIX="$out_root" \
    CROSS_COMPILE="$tool_bin/$target-" \
    install

echo "==> Building GNU awk"
gawk=$build_dir/gawk
mkdir -p "$gawk"
tar -xJf "$sources/gawk.tar.xz" --strip-components=1 -C "$gawk"
(
    cd "$gawk"
    CC="$cc -static" \
    CFLAGS="-std=gnu17 -g0 -O2 -fno-ident" \
    LDFLAGS="-static -s" \
        ./configure \
            --prefix=/ \
            --disable-extensions \
            --disable-mpfr \
            --disable-nls \
            --disable-pma \
            --without-readline
    make -s -j"$jobs"
    make -s DESTDIR="$out_root" install
)
ln -sf gawk "$out_root/bin/awk"

echo "==> Building GNU Make"
make_src=$build_dir/make
mkdir -p "$make_src"
tar -xzf "$sources/make.tar.gz" --strip-components=1 -C "$make_src"
(
    cd "$make_src"
    CC="$cc -static" \
    CFLAGS="-std=gnu17 -g0 -O2 -fno-ident" \
    LDFLAGS="-static -s" \
        ./configure --prefix=/ --disable-nls
    make -s -j"$jobs"
    make -s DESTDIR="$out_root" install
)

echo "==> Building GNU patch"
patch_src=$build_dir/patch
mkdir -p "$patch_src"
tar -xJf "$sources/patch.tar.xz" --strip-components=1 -C "$patch_src"
(
    cd "$patch_src"
    CC="$cc -static" \
    CFLAGS="-std=gnu17 -g0 -O2 -fno-ident" \
    LDFLAGS="-static -s" \
        ./configure --prefix=/ --disable-nls
    make -s -j"$jobs"
    make -s DESTDIR="$out_root" install
)

echo "==> Building GNU tar"
tar_src=$build_dir/tar
mkdir -p "$tar_src"
tar -xJf "$sources/tar.tar.xz" --strip-components=1 -C "$tar_src"
(
    cd "$tar_src"
    CC="$cc -static" \
    CFLAGS="-std=gnu17 -g0 -O2 -fno-ident" \
    LDFLAGS="-static -s" \
    FORCE_UNSAFE_CONFIGURE=1 \
        ./configure \
            --prefix=/ \
            --disable-nls \
            --without-posix-acls \
            --without-selinux \
            --without-xattrs
    make -s -j"$jobs" MAKEINFO=true
    make -s DESTDIR="$out_root" MAKEINFO=true install
)

echo "==> Building zstd"
zstd=$build_dir/zstd
mkdir -p "$zstd"
tar -xzf "$sources/zstd.tar.gz" --strip-components=1 -C "$zstd"
make -s -C "$zstd/programs" \
    -j"$jobs" \
    CC="$cc" \
    CFLAGS="-std=gnu17 -g0 -O2 -fno-ident" \
    LDFLAGS="-static -s" \
    zstd
install -Dm755 "$zstd/programs/zstd" "$out_root/bin/zstd"
ln -sf zstd "$out_root/bin/unzstd"
ln -sf zstd "$out_root/bin/zstdcat"

echo "==> Installing conventional tool names"
for tool in addr2line ar as c++ c++filt cpp elfedit g++ gcc gcc-ar \
    gcc-nm gcc-ranlib gcov gcov-dump gcov-tool gprof ld ld.bfd nm \
    objcopy objdump ranlib readelf size strings strip
do
    if [ -e "$out_root/bin/$target-$tool" ]; then
        ln -sf "$target-$tool" "$out_root/bin/$tool"
    elif [ -e "$out_root/bin/$tool" ]; then
        ln -sf "$tool" "$out_root/bin/$target-$tool"
    fi
done
ln -sf gcc "$out_root/bin/cc"

echo "==> Installing bootstrap metadata and licenses"
mkdir -p \
    "$out_root/dev" \
    "$out_root/proc" \
    "$out_root/tmp" \
    "$out_root/share/bootstrap-seed" \
    "$out_root/share/licenses"
cp "$recipe/bootstrap.sh" "$out_root/share/bootstrap-seed/"
cp "$recipe/sources" "$out_root/share/bootstrap-seed/"
cat >"$out_root/share/bootstrap-seed/components" <<'EOF'
target x86_64-linux-musl
musl-cross-make 227df8b99103f9c59f6570babf892978e293082f
binutils 2.44
busybox 1.37.0
gcc 15.1.0
gawk 5.3.2
gmp 6.3.0
linux-headers 6.15.7
make 4.4.1
mpc 1.3.1
mpfr 4.2.2
musl 1.2.6
patch 2.8
tar 1.35
zstd 1.5.7
EOF

mkdir -p "$out_root/share/licenses/musl-cross-make"
cp "$mcm/COPYRIGHT" "$mcm/LICENSE" "$out_root/share/licenses/musl-cross-make/"
mkdir -p "$out_root/share/licenses/musl"
cp "$mcm/musl-1.2.6/COPYRIGHT" "$out_root/share/licenses/musl/"
mkdir -p "$out_root/share/licenses/busybox"
cp "$busybox/LICENSE" "$out_root/share/licenses/busybox/"
mkdir -p "$out_root/share/licenses/gawk"
cp "$gawk/COPYING" "$out_root/share/licenses/gawk/"
mkdir -p "$out_root/share/licenses/make"
cp "$make_src/COPYING" "$out_root/share/licenses/make/"
mkdir -p "$out_root/share/licenses/patch"
cp "$patch_src/COPYING" "$out_root/share/licenses/patch/"
mkdir -p "$out_root/share/licenses/tar"
cp "$tar_src/COPYING" "$out_root/share/licenses/tar/"
mkdir -p "$out_root/share/licenses/zstd"
cp "$zstd/LICENSE" "$out_root/share/licenses/zstd/"

echo "==> Seed generation complete"
