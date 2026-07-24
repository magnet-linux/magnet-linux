#!/usr/bin/env bash
set -euo pipefail

umask 022

bootstrap_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
work_dir=${BOOTSTRAP_WORK_DIR:-"$bootstrap_dir/work"}
out_dir=${BOOTSTRAP_OUT_DIR:-"$bootstrap_dir/out"}
distfiles_dir="$work_dir/distfiles"
bridge_build_dir="$work_dir/bridge-build"
bridge_dir="$work_dir/bridge"
jobs=${BOOTSTRAP_JOBS:-8}
source_date_epoch=1784851200
target=x86_64-linux-musl

required_commands=(
    awk
    bison
    bwrap
    bzip2
    cc
    curl
    flex
    g++
    gzip
    make
    patch
    rsync
    sed
    sha256sum
    tar
    xz
)

for command_name in "${required_commands[@]}"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
        echo "missing required host command: $command_name" >&2
        exit 1
    fi
done

if [[ $(uname -s) != Linux || $(uname -m) != x86_64 ]]; then
    echo "the initial bootstrap supports only x86-64 Linux hosts" >&2
    exit 1
fi

mkdir -p "$distfiles_dir" "$out_dir"

empty_work_directory() {
    local path=$1
    case "$path" in
        "$work_dir"/*) ;;
        *)
            echo "refusing to clean a path outside $work_dir: $path" >&2
            exit 1
            ;;
    esac
    rm -rf -- "$path"
    mkdir -p "$path"
}

fetch_sources() {
    local expected filename url extra destination temporary actual

    echo "==> Fetching and verifying sources"
    while read -r expected filename url extra; do
        [[ -z ${expected:-} || $expected == \#* ]] && continue
        if [[ -n ${extra:-} || ! $expected =~ ^[0-9a-f]{64}$ ]]; then
            echo "invalid source entry: $expected $filename $url ${extra:-}" >&2
            exit 1
        fi
        if [[ ! $filename =~ ^[A-Za-z0-9._+-]+$ ]]; then
            echo "unsafe source filename: $filename" >&2
            exit 1
        fi

        destination="$distfiles_dir/$filename"
        if [[ -f $destination ]]; then
            actual=$(sha256sum "$destination")
            actual=${actual%% *}
            if [[ $actual == "$expected" ]]; then
                echo "  cached $filename"
                continue
            fi
            echo "  discarding checksum-mismatched $filename"
            rm -f -- "$destination"
        fi

        temporary="$destination.part"
        curl \
            --fail \
            --location \
            --retry 3 \
            --continue-at - \
            --output "$temporary" \
            "$url"
        actual=$(sha256sum "$temporary")
        actual=${actual%% *}
        if [[ $actual != "$expected" ]]; then
            echo "$filename: expected $expected, got $actual" >&2
            exit 1
        fi
        mv -- "$temporary" "$destination"
    done <"$bootstrap_dir/sources"
}

populate_mcm_sources() {
    local destination=$1
    mkdir -p "$destination"
    ln -s "$distfiles_dir/binutils.tar.gz" "$destination/binutils-2.44.tar.gz"
    ln -s "$distfiles_dir/gcc.tar.xz" "$destination/gcc-15.1.0.tar.xz"
    ln -s "$distfiles_dir/gmp.tar.xz" "$destination/gmp-6.3.0.tar.xz"
    ln -s "$distfiles_dir/linux.tar.xz" "$destination/linux-6.15.7.tar.xz"
    ln -s "$distfiles_dir/mpc.tar.gz" "$destination/mpc-1.3.1.tar.gz"
    ln -s "$distfiles_dir/mpfr.tar.xz" "$destination/mpfr-4.2.2.tar.xz"
    ln -s "$distfiles_dir/musl.tar.gz" "$destination/musl-1.2.6.tar.gz"
}

build_bridge() {
    local mcm="$bridge_build_dir/musl-cross-make"

    if [[ -x "$bridge_dir/bin/$target-gcc" ]]; then
        echo "==> Reusing bridge toolchain at $bridge_dir"
        return
    fi

    echo "==> Building host-running musl cross-toolchain bridge"
    empty_work_directory "$bridge_build_dir"
    empty_work_directory "$bridge_dir"
    mkdir -p "$mcm"
    tar \
        -xzf "$distfiles_dir/musl-cross-make.tar.gz" \
        --strip-components=1 \
        -C "$mcm"
    sed -i \
        -e 's/tar zxvf/tar zxf/' \
        -e 's/tar jxvf/tar jxf/' \
        -e 's/tar Jxvf/tar Jxf/' \
        "$mcm/Makefile"
    populate_mcm_sources "$mcm/sources"

    cat >"$mcm/config.mak" <<EOF
TARGET = $target
OUTPUT = $bridge_dir
SOURCES = $mcm/sources
BINUTILS_VER = 2.44
GCC_VER = 15.1.0
MUSL_VER = 1.2.6
GMP_VER = 6.3.0
MPC_VER = 1.3.1
MPFR_VER = 4.2.2
ISL_VER =
LINUX_VER = 6.15.7
COMMON_CONFIG += CFLAGS="-g0 -O2 -fno-ident"
COMMON_CONFIG += CXXFLAGS="-g0 -O2 -fno-ident"
COMMON_CONFIG += LDFLAGS="-s"
COMMON_CONFIG += --disable-nls
COMMON_CONFIG += --with-debug-prefix-map=$bridge_build_dir=/build
GCC_CONFIG += --disable-decimal-float
GCC_CONFIG += --disable-fixed-point
GCC_CONFIG += --disable-libitm
GCC_CONFIG += --disable-libquadmath
GCC_CONFIG += --disable-lto
EOF

    make -s -C "$mcm" -j"$jobs"
    make -s -C "$mcm" install
    test -x "$bridge_dir/bin/$target-gcc"
    "$bridge_dir/bin/$target-gcc" --version
}

run_first_seed() {
    local stage_build=$1
    local stage_root=$2
    local host_path=$PATH
    local host_shell
    host_shell=$(command -v bash)

    bwrap \
        --die-with-parent \
        --new-session \
        --unshare-ipc \
        --unshare-net \
        --unshare-pid \
        --unshare-uts \
        --hostname bootstrap \
        --ro-bind / / \
        --dev /dev \
        --proc /proc \
        --tmpfs /tmp \
        --dir /tmp/recipe \
        --dir /tmp/sources \
        --dir /tmp/tools \
        --dir /tmp/build \
        --dir /tmp/out \
        --ro-bind "$bootstrap_dir/bootstrap.sh" /tmp/recipe/bootstrap.sh \
        --ro-bind "$bootstrap_dir/sources" /tmp/recipe/sources \
        --ro-bind "$distfiles_dir" /tmp/sources \
        --ro-bind "$bridge_dir" /tmp/tools \
        --bind "$stage_build" /tmp/build \
        --bind "$stage_root" /tmp/out \
        --clearenv \
        --setenv BOOTSTRAP_BUILD_DIR /tmp/build \
        --setenv BOOTSTRAP_JOBS "$jobs" \
        --setenv BOOTSTRAP_OUT_ROOT /tmp/out \
        --setenv BOOTSTRAP_PATH "/tmp/tools/bin:$host_path" \
        --setenv BOOTSTRAP_RECIPE /tmp/recipe \
        --setenv BOOTSTRAP_SOURCES /tmp/sources \
        --setenv BOOTSTRAP_TOOLS /tmp/tools \
        --setenv SOURCE_DATE_EPOCH "$source_date_epoch" \
        "$host_shell" /tmp/recipe/bootstrap.sh
}

run_native_seed() {
    local input_root=$1
    local stage_build=$2
    local stage_root=$3

    bwrap \
        --die-with-parent \
        --new-session \
        --unshare-ipc \
        --unshare-net \
        --unshare-pid \
        --unshare-uts \
        --hostname bootstrap \
        --ro-bind "$input_root" / \
        --dev /dev \
        --proc /proc \
        --tmpfs /tmp \
        --dir /tmp/recipe \
        --dir /tmp/sources \
        --dir /tmp/build \
        --dir /tmp/out \
        --ro-bind "$bootstrap_dir/bootstrap.sh" /tmp/recipe/bootstrap.sh \
        --ro-bind "$bootstrap_dir/sources" /tmp/recipe/sources \
        --ro-bind "$distfiles_dir" /tmp/sources \
        --bind "$stage_build" /tmp/build \
        --bind "$stage_root" /tmp/out \
        --clearenv \
        --setenv BOOTSTRAP_BUILD_DIR /tmp/build \
        --setenv BOOTSTRAP_JOBS "$jobs" \
        --setenv BOOTSTRAP_OUT_ROOT /tmp/out \
        --setenv BOOTSTRAP_RECIPE /tmp/recipe \
        --setenv BOOTSTRAP_SOURCES /tmp/sources \
        --setenv BOOTSTRAP_TOOLS / \
        --setenv SOURCE_DATE_EPOCH "$source_date_epoch" \
        /bin/sh /tmp/recipe/bootstrap.sh
}

build_seed_stages() {
    local seed1_build="$work_dir/seed1-build"
    local seed2_build="$work_dir/seed2-build"
    local seed3_build="$work_dir/seed3-build"
    local seed1="$work_dir/seed1"
    local seed2="$work_dir/seed2"
    local seed3="$work_dir/seed3"

    echo "==> Building seed1 from the bridge"
    empty_work_directory "$seed1_build"
    empty_work_directory "$seed1"
    run_first_seed "$seed1_build" "$seed1"

    echo "==> Building seed2 from seed1"
    empty_work_directory "$seed2_build"
    empty_work_directory "$seed2"
    run_native_seed "$seed1" "$seed2_build" "$seed2"

    echo "==> Building seed3 from seed2"
    empty_work_directory "$seed3_build"
    empty_work_directory "$seed3"
    run_native_seed "$seed2" "$seed3_build" "$seed3"
}

check_seed() {
    local root=$1

    echo "==> Checking $(basename "$root")"
    bwrap \
        --die-with-parent \
        --new-session \
        --unshare-ipc \
        --unshare-net \
        --unshare-pid \
        --unshare-uts \
        --hostname bootstrap \
        --ro-bind "$root" / \
        --dev /dev \
        --proc /proc \
        --tmpfs /tmp \
        --clearenv \
        --setenv LC_ALL C \
        --setenv PATH /bin:/sbin:/usr/bin:/usr/sbin \
        /bin/sh -eu -c '
            busybox true
            awk --version
            gcc --version
            g++ --version
            make --version
            tar --version
            zstd --version

            cat >/tmp/hello.c <<EOF
#include <stdio.h>
int main(void) { puts("hello from C"); return 0; }
EOF
            gcc /tmp/hello.c -o /tmp/hello-c
            /tmp/hello-c
            gcc -static /tmp/hello.c -o /tmp/hello-c-static
            /tmp/hello-c-static

            cat >/tmp/hello.cc <<EOF
#include <iostream>
int main() { std::cout << "hello from C++" << std::endl; }
EOF
            g++ /tmp/hello.cc -o /tmp/hello-cxx
            /tmp/hello-cxx
            g++ -static /tmp/hello.cc -o /tmp/hello-cxx-static
            /tmp/hello-cxx-static

            cat >/tmp/cxx11.cc <<EOF
#if __cplusplus != 201103L
#error C++11 mode was not selected
#endif
int main() {}
EOF
            g++ -std=c++11 -c /tmp/cxx11.cc -o /tmp/cxx11.o

            mkdir /tmp/make-test
            cat >/tmp/make-test/Makefile <<EOF
all:
	printf "make works\n" > result
EOF
            make -C /tmp/make-test
            test "$(cat /tmp/make-test/result)" = "make works"
        '
}

package_seed() {
    local root=$1
    local archive=$2
    local result_dir
    result_dir="$work_dir/package-$(basename "$root")"

    empty_work_directory "$result_dir"
    bwrap \
        --die-with-parent \
        --new-session \
        --unshare-ipc \
        --unshare-net \
        --unshare-pid \
        --unshare-uts \
        --hostname bootstrap \
        --ro-bind "$root" / \
        --dev /dev \
        --proc /proc \
        --tmpfs /tmp \
        --dir /tmp/rootfs \
        --dir /tmp/result \
        --ro-bind "$root" /tmp/rootfs \
        --bind "$result_dir" /tmp/result \
        --clearenv \
        --setenv LC_ALL C \
        --setenv PATH /bin:/sbin:/usr/bin:/usr/sbin \
        --setenv SOURCE_DATE_EPOCH "$source_date_epoch" \
        /bin/sh -eu -c '
            tar \
                --sort=name \
                --format=posix \
                --mtime="@$SOURCE_DATE_EPOCH" \
                --owner=0 \
                --group=0 \
                --numeric-owner \
                --pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime \
                --hard-dereference \
                -C /tmp/rootfs \
                -cf /tmp/result/bootstrap.tar \
                .
            zstd \
                -19 \
                --threads=1 \
                --no-progress \
                --force \
                /tmp/result/bootstrap.tar \
                -o /tmp/result/bootstrap.tar.zst
            rm /tmp/result/bootstrap.tar
        '
    mv "$result_dir/bootstrap.tar.zst" "$archive"
}

verify_fixed_point() {
    local seed2="$work_dir/seed2"
    local seed3="$work_dir/seed3"
    local seed2_archive="$work_dir/seed2.tar.zst"
    local seed3_archive="$work_dir/seed3.tar.zst"

    check_seed "$seed2"
    check_seed "$seed3"

    if grep -R -a -l -F -m1 /nix/store "$seed2" "$seed3" \
        >/dev/null 2>&1
    then
        echo "a fixed-point seed contains a Nix store reference" >&2
        exit 1
    fi

    echo "==> Creating canonical seed archives"
    package_seed "$seed2" "$seed2_archive"
    package_seed "$seed3" "$seed3_archive"

    echo "==> Comparing seed2 and seed3"
    if ! cmp "$seed2_archive" "$seed3_archive"; then
        sha256sum "$seed2_archive" "$seed3_archive"
        echo "seed2 and seed3 did not reach a fixed point" >&2
        exit 1
    fi

    cp "$seed3_archive" "$out_dir/bootstrap.tar.zst"
    sha256sum "$out_dir/bootstrap.tar.zst"

    if grep -Eq '^[0-9a-f]{64}[[:space:]]+[*]?bootstrap\.tar\.zst$' \
        "$bootstrap_dir/bootstrap.sha256"
    then
        (
            cd "$out_dir"
            sha256sum -c "$bootstrap_dir/bootstrap.sha256"
        )
    else
        echo "bootstrap.sha256 has no released checksum yet"
    fi
}

fetch_sources
build_bridge
build_seed_stages
verify_fixed_point
