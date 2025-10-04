# Bootstrapping the Package Tree

Magnet Linux needs a ready-made root filesystem before `magpkg` can build anything. We create it once with a helper script, then the Jsonnet packages rebuild the same bits from source so future upgrades are self-hosted.

## Seed tarball (`build_bootstrap.sh`)
- Downloads the musl-native toolchain archive into `.cache/` and unpacks it into `toolchain/`.
- Builds BusyBox 1.36.1 statically with that toolchain and installs it in `rootfs/bin/` with symlinks for every applet.
- Copies the toolchain into `rootfs/toolchain/`, adds the `/toolchain/usr/include` symlink, and exposes each compiler in `rootfs/bin/` (both plain and `x86_64-linux-musl-*` names).
- Creates the minimal rootfs layout (`/bin`, `/dev`, `/proc`, `/sys`, sticky `/tmp`) and writes `bootstrap.tar.zst` via `zstd`.
- The tarball’s SHA-256 must match the `bootstrap-rootfs` fetch entry in `packages/bootstrap.jsonnet` so `magpkg` trusts it.

## Jsonnet bootstrap graph
`packages/bootstrap.jsonnet` turns that tarball into a self-contained dependency chain:
- `bootstrap-rootfs`: `build:"untar"`; simply unpacks `bootstrap.tar.zst` and exposes BusyBox plus the toolchain.
- `make`: builds GNU Make 4.4.1 (static). It depends on `bootstrap`, so the toolchain from the tarball is available inside the Bubblewrap sandbox.
- `busybox`: rebuilds BusyBox using the freshly built `make`.
- `bootstrap_out` (`name:"bootstrap"`): uses musl-cross-make to rebuild binutils, GCC, musl, Linux headers, and then restages BusyBox and the toolchain layout into `/out`. This output is the fully self-hosted rootfs.

`magpkg` inflates every dependency under `pkgs/<hash>.build/rootfs/` and runs the package’s shell script inside Bubblewrap, so later stages always see the binaries produced earlier in the chain.

## Updating the bootstrap tarball
1. Run `magpkg build 'std.objectValues(import "packages/bootstrap.jsonnet")'` to rebuild all four packages; `bootstrap_out` emits the new rootfs archive in `~/.magpkg/pkgs/`.
2. Publish the tarball (HTTP and torrent/magnet), compute its SHA-256, and update the `bootstrap-rootfs` fetch stanza.
3. Commit the new hash and any refreshed torrent metadata. Downstream builders now bootstrap entirely from source-defined artifacts.
