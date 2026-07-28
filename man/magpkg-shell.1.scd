MAGPKG-SHELL(1) "magpkg"

# NAME

magpkg-shell - enter a package-defined shell environment

# SYNOPSIS

*magpkg shell* [options] _FILE_ [_ROOT_ ...] [*--* _COMMAND_ [_ARG_ ...]]

*magpkg shell* [options] *--file* _FILE_ [_ROOT_ ...] [*--* _COMMAND_ [_ARG_ ...]]

# DESCRIPTION

*magpkg shell* evaluates _FILE_, selects the requested roots, and enters their
composed runtime closure. A _ROOT_ may be an exported top-level Jsonnet field
name or a package ID. When no roots are named, every exported root is selected.

Materialized environments are cached below _$HOME/.magpkg/environments_.
An existing environment is leased directly, even if its original package
artifacts have since been cleaned. On a cache miss, packages are realized
using local artifacts, an optional binary cache, and local builds before the
runtime closure is composed and published atomically.

The composed environment must provide an executable regular file at
_/libexec/magpkg-shell_. Magpkg runs this package-provided launcher on the
host. The absolute materialized environment root is its first argument,
followed by _COMMAND_ and its arguments. When _COMMAND_ is omitted, the
launcher chooses the interactive shell and its sandbox policy.

The launcher inherits magpkg's host environment, working directory, standard
input, standard output, and standard error. Magpkg retains a shared environment
lease until the launcher exits and returns the launcher's exit status.

# LAUNCHER CONTRACT

The launcher is invoked as:

```
<environment>/root/libexec/magpkg-shell ROOT [COMMAND [ARG ...]]
```

It must remain in the foreground for the complete session. A launcher which
starts background processes and exits releases the environment lease while
those processes may still refer to it.

The launcher owns all container and shell behavior. It may invoke bubblewrap,
choose mounts and namespaces, adjust the environment, or use a different
execution mechanism. Magpkg itself applies no sandbox to the launcher.

Package launchers execute with the invoking user's host permissions before
creating any sandbox. Shell packages and binary caches supplying them must be
fully trusted.

# OPTIONS

*-f*, *--file* _FILE_
	Read the package graph from _FILE_. Without this option, the first
	positional argument before *--* is the file.

*--store* _PATH_
	Use _PATH_ as the package store.

*--binary-cache* _URL_
	Request missing package artifacts from _URL_ while realizing an
	uncached environment. Without this option, *MAGPKG_BINARY_CACHE* is used,
	including with *--no-build*.

*--no-build*
	On an environment cache miss, permit existing local artifacts and
	binary-cache substitutions but never fetch package sources or execute
	local builds. A required cache miss is an error.

*--no-binary-cache*
	Do not request missing artifacts from a binary cache. This overrides both
	*--binary-cache* and *MAGPKG_BINARY_CACHE*. Local builds remain enabled
	unless *--no-build* is also specified.

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

# ENVIRONMENT CACHE

The environment directory has this layout:

```
$HOME/.magpkg/environments/
|-- .magpkg-environments.json
|-- roots/
|   `-- <environment-id>/
|       |-- metadata.json
|       `-- root/
|-- locks/
|   `-- <environment-id>.lock
`-- work/
    `-- <environment-id>/
        `-- root/
```

The environment ID hashes a format domain and the dependency-first, ordered
runtime closure. Package overlay order is therefore part of the identity.
The completed root is a derived cache rather than persistent user state.

Materialization holds an exclusive environment lock. Running launchers hold a
shared lock. *magpkg clean --environments* attempts the exclusive lock without
waiting and therefore skips active environments.

# EXAMPLES

Enter the package-defined interactive shell:

```
magpkg shell packages.jsonnet development
```

Run a command in the same environment:

```
magpkg shell packages.jsonnet development -- make -j8
```

Use only an already-materialized environment or local package artifacts:

```
magpkg shell --no-build --no-binary-cache packages.jsonnet development
```

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-clean*(1),
*magpkg-package*(5), *magpkg-store*(5)
