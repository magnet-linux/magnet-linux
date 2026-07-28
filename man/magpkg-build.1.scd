MAGPKG-BUILD(1) "magpkg"

# NAME

magpkg-build - build roots from a Jsonnet package graph

# SYNOPSIS

*magpkg build* [options] _FILE_ [_ROOT_ ...]

*magpkg build* [options] *--file* _FILE_ [_ROOT_ ...]

# DESCRIPTION

*magpkg build* evaluates _FILE_ as a package graph, selects the requested
roots, and realizes their build and runtime dependencies in the package store.
A _ROOT_ may be an exported top-level Jsonnet field name or a package ID
present in the evaluated graph.

When no roots are named, every exported root is selected. Duplicate selections
which resolve to the same package ID are built once.

The final package artifact path for each selected root is written to standard
output. Fetch progress and line-prefixed package build output are written to
standard error. Already available package artifacts are reused. When a binary
cache is configured, missing artifacts are requested from it before build
planning. A cache hit omits that package's build-only dependencies and source
fetches.

# OPTIONS

*-f*, *--file* _FILE_
	Read the package graph from _FILE_. Without this option, the first
	positional argument is the file.

*--store* _PATH_
	Use _PATH_ as the package store.

*--binary-cache* _URL_
	Request missing package artifacts from _URL_. Without this option,
	*MAGPKG_BINARY_CACHE* is used, including with *--no-build*.

*--no-build*
	Permit existing local artifacts and binary-cache substitutions, but never
	fetch package sources or execute local builds. A required cache miss is an
	error.

*--no-binary-cache*
	Do not request missing artifacts from a binary cache. This overrides both
	*--binary-cache* and *MAGPKG_BINARY_CACHE*. Local builds remain enabled
	unless *--no-build* is also specified. Combining the two options permits
	only artifacts already in the local store.

*--jobs* _N_
	Run at most _N_ package builds concurrently. The default is 1. A blocked
	package lock lets an executor continue with another ready DAG branch.

*--fetch-jobs* _N_
	Run at most _N_ source fetches concurrently. The default is 4.

*--parallelism* _N_
	Set *BUILD_PARALLELISM* to _N_ for build scripts. The default is the
	number of logical CPUs visible to magpkg. This does not set the number of
	concurrent package builds.

*--ext-str* _NAME_=_VALUE_
	Bind a Jsonnet external string. The option may be repeated.

*-h*, *--help*
	Show command usage.

# CONCURRENCY

Package builds and source fetches use per-object advisory locks. Separate
magpkg processes may build the same graph concurrently: each observes
artifacts published by the others and works on another ready package when
possible.

*--jobs* controls package-level concurrency within this invocation.
*--fetch-jobs* independently limits network or file fetches, while
*--parallelism* is only a value exposed to each build script.

# EXAMPLES

Build every exported root:

```
magpkg build packages.jsonnet
```

Build two named roots with four package workers:

```
magpkg build --jobs 4 --parallelism 8 packages.jsonnet gcc make
```

Pass an external string to Jsonnet:

```
magpkg build --ext-str mirror=https://mirror.example packages.jsonnet
```

# SEE ALSO

*magpkg*(1), *magpkg-init*(1), *magpkg-export*(1),
*magpkg-serve-cache*(1), *magpkg-package*(5), *magpkg-store*(5),
*jsonnet*(1)
