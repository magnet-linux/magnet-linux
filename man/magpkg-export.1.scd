MAGPKG-EXPORT(1) "magpkg"

# NAME

magpkg-export - export package runtime closures as a tar archive

# SYNOPSIS

*magpkg export* *--output* _ARCHIVE_ [options] _FILE_ [_ROOT_ ...]

*magpkg export* *--output* _ARCHIVE_ [options] *--file* _FILE_ [_ROOT_ ...]

# DESCRIPTION

Unless *--no-build* is specified, *magpkg export* first builds the selected
packages. It then composes their runtime closures from artifacts in the store
and writes the resulting filesystem as one deterministic tar archive.

A _ROOT_ may be an exported top-level Jsonnet field name or a package ID.
When no roots are named, every exported root is selected. Runtime dependencies
are composed before their dependants; later packages therefore overlay earlier
packages deterministically.

The archive contains only the composed package files. It does not contain the
management metadata created by *magpkg sync*.

# OPTIONS

*-o*, *--output* _ARCHIVE_
	Write the archive to _ARCHIVE_. The output file must not already exist.
	Use *-* to write archive bytes to standard output. This option is required.

*--format* _FORMAT_
	Select *tar*, *tar.gz*, *tar.xz*, or *tar.zst*. Without this option, the
	format is inferred from the output filename. The suffixes _.tgz_ and
	_.txz_ are recognized as aliases for gzip and xz archives. An explicit
	format is required when writing to standard output or to a filename with
	an unrecognized suffix.

*-f*, *--file* _FILE_
	Read the package graph from _FILE_. Without this option, the first
	positional argument is the file.

*--store* _PATH_
	Use _PATH_ as the package store.

*--binary-cache* _URL_
	Request missing package artifacts from _URL_ during the build phase.
	Without this option, *MAGPKG_BINARY_CACHE* is used. This option has no
	effect with *--no-build*.

*--cache-only*
	During the build phase, permit existing local artifacts and binary-cache
	substitutions but prohibit source fetching and local package builds. It
	cannot be combined with *--no-build*.

*--no-build*
	Do not invoke the build or binary-cache pipeline. Every artifact in the
	selected runtime closure must already exist in the store.

*--jobs* _N_
	Run at most _N_ package builds concurrently. The default is 1.

*--fetch-jobs* _N_
	Run at most _N_ source fetches concurrently. The default is 4.

*--parallelism* _N_
	Set *BUILD_PARALLELISM* for build scripts. The default is the number of
	logical CPUs visible to magpkg.

*--ext-str* _NAME_=_VALUE_
	Bind a Jsonnet external string. The option may be repeated.

*-h*, *--help*
	Show command usage.

# OUTPUT

When _ARCHIVE_ is a file, magpkg creates the archive under a temporary name
beside it and publishes it atomically. Existing paths are never overwritten.
The command writes no success message to standard output.

With *--output -*, standard output contains only archive bytes. Fetch progress
and package build output are always written to standard error. A failed or
interrupted standard-output export may have already emitted a partial archive.

# EXAMPLES

Export one runtime closure using format inference:

```
magpkg export -o environment.tar.zst packages.jsonnet development
```

Write an uncompressed archive to standard output and extract it directly:

```
mkdir environment
magpkg export --format tar -o - packages.jsonnet development |
	tar -x -C environment
```

Export without performing builds:

```
magpkg export --no-build -o environment.tar.xz packages.jsonnet development
```

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-sync*(1),
*magpkg-package*(5), *magpkg-store*(5), *tar*(1)
