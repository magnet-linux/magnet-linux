# Packages

## Bootstrap hello

`hello.jsonnet` builds a statically linked hello-world executable using the
reproducible bootstrap root:

```sh
go -C magpkg run ./cmd/magpkg init
go -C magpkg run ./cmd/magpkg build ../packages/hello.jsonnet
```

The command prints the resulting package archive path. The executable is
stored as `bin/hello` inside that archive.

## Core

`core.jsonnet` rebuilds the core toolchain and userland from source, using the
bootstrap only as the seed environment. In particular, musl, binutils, and GCC
are normal packages rather than permanent parts of the seed.

The package tree uses a root-prefix filesystem layout. Executables, libraries,
headers, and architecture-independent data are installed in `/bin`, `/lib`,
`/include`, and `/share`. The `root-layout` package supplies the filesystem
skeleton and compatibility links `/usr -> .` and `/sbin -> bin`. The original
bootstrap archive remains unchanged; `bootstrap-tools` normalizes it into this
layout before it is used by regular builds.

Packages also apply this tree's build-environment policy explicitly, overriding
the default `PATH` with `/bin`. Other trees can use the default environment or
override any value through a package's `buildEnv` object.

Each exported field is a build root. For example, this rebuilds GCC and all of
its prerequisites:

```sh
go -C magpkg run ./cmd/magpkg build \
  --jobs 1 \
  --parallelism "$(nproc)" \
  ../packages/core.jsonnet gcc
```

The other roots are `make`, `musl`, `musl_rt`, `binutils`, `coreutils`,
`gawk`, `sed`, `findutils`, `diffutils`, `pkgconfig`, `bash`, `gzip`, `xz`,
`tar`, `grep`, `libgcc_rt`, and `libstdcpp_rt`. Separate build invocations can
share the same store: package locks prevent duplicate publication while an
executor continues with any other ready branch.

## Shell environment

`shell.jsonnet` demonstrates a package-defined development shell. Its runtime
closure supplies the bootstrap tools and an executable launcher at
`/libexec/magpkg-shell`. Magpkg materializes and leases the closure, while the
launcher controls bubblewrap mounts, isolation, and the default interactive
shell:

```sh
go -C magpkg run ./cmd/magpkg shell ../packages/shell.jsonnet shell
```

A command after `--` is passed unchanged to the launcher:

```sh
go -C magpkg run ./cmd/magpkg shell \
  ../packages/shell.jsonnet shell -- cc --version
```

Package-provided launchers execute on the host with the invoking user's
permissions before creating their sandbox. Only trusted packages and binary
caches should be used for shell environments.
