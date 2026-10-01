# Patch provenance

The Alpine patches come from `community/chromium` at aports commit
`a44854115ef1a1cda0c41b3e6f0974f51755d736`, Chromium 152.0.7977.82-r1:

https://github.com/alpinelinux/aports/tree/a44854115ef1a1cda0c41b3e6f0974f51755d736/community/chromium

The `cr*.patch` files are the applicable musl/compiler patches from the
`152.0` release of https://codeberg.org/selfisekai/copium, also selected by that
Alpine recipe. Patch headers retain their authors and upstream references.
Patches for Alpine's optional unbundled multimedia libraries are omitted.

`compiler.patch` uses `x86_64-unknown-linux-musl` for Rust in place of Alpine's
custom `x86_64-alpine-linux-musl` target. `rust-cbor.patch` retains the existing
C++ CBOR implementation when Chromium's experimental Crubit tools are absent.
The musl sandbox adjustments preserve sandboxing while permitting musl's
different syscall usage.

`nspr-lfs64.patch` is from `main/nspr` at the same aports commit.
All applied patches are incorporated into the package's content identity.

The local `magnet-*.patch` files set the root-prefix DRI driver directory,
skip cosmetic rustfmt processing of generated bindings, and omit the optional
profiling runtime from the list of copied Rust libraries. They also guard a
Vulkan-only method when Vulkan is disabled. This configuration does not use
coverage instrumentation or PGO.

`magnet-musl-pwrite-fallback.patch` returns `ENOSYS` for `pwritev2` in the
baseline sandbox policy on musl. Musl 1.2.6's `pwrite`/`pwritev` then fall back
to the existing calls and their existing policy. This fixes software Wayland
buffer writes without allowing another syscall or disabling the sandbox.
