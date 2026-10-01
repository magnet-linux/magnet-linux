# Shared browser packages

`libraries.jsonnet` supplies GTK 3, GDK-Pixbuf, Cairo GObject bindings,
AT-SPI, D-Bus, ALSA, libxml2, libjpeg-turbo, libepoxy, libglvnd, PCI Utilities
and curl. It reuses
the lower-level libraries in `../desktop-libs.jsonnet`.
Firefox and Chromium reuse these library definitions and install their runtime
dependencies globally. `bsd-compat-headers.jsonnet` supplies Alpine's
compatibility headers as a build-only dependency.

curl includes libcurl for Crashpad and uses the tree's existing OpenSSL and
CA bundle. The OpenSSL development files remain in its closure for libcurl's
pkg-config metadata; OpenSSL itself is statically linked into libcurl.

`toolchain.libsonnet` contains reusable native LLVM/Clang/LLD and Rust/Cargo
build recipes, plus Python extensions, Node, NASM, GNU patch and Info-ZIP. A caller can
override source pins and the LLVM version without changing another browser's
toolchain. `toolchain.jsonnet` selects LLVM 20.1.8 and Rust 1.89.0 for Firefox;
Chromium selects its newer versions in its own directory. Source-built Rust
uses a pinned upstream musl binary only for bootstrap.

The full XZ decoder used for newer Rust archives installs under `/opt/build-xz`
inside the build environment. It handles the large dictionary that the small
bootstrap decoder cannot read. The build Bash retains its original package
name to preserve existing artifact identities.

`build.libsonnet` supplies package helpers with an extensible source manifest.
Moving these definitions out of Firefox preserves Firefox's evaluated package
IDs and permits reuse of its existing cached artifacts. WASI, cbindgen and
Autoconf 2.13 remain under `../firefox/`.
