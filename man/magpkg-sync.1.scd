MAGPKG-SYNC(1) "magpkg"

# NAME

magpkg-sync - synchronize package runtime closures into a destination

# SYNOPSIS

*magpkg sync* *--dest* _PATH_ [options] _FILE_ [_ROOT_ ...]

*magpkg sync* *--dest* _PATH_ [options] *--file* _FILE_ [_ROOT_ ...]

# DESCRIPTION

Unless *--no-build* is specified, *magpkg sync* first builds the selected
packages. It then composes their runtime closures from artifacts already in the
store and makes the magpkg-managed portion of _PATH_ match the composed result.
A _ROOT_ may be an exported Jsonnet field name or a package ID. When no roots
are named, every exported root is selected.

Destination state is recorded in _PATH/.magpkg/manifest.json_. The manifest
records package IDs and the type, mode, ownership, and content identity of
each path owned by magpkg. A persistent advisory lock at
_PATH/.magpkg/lock_ prevents concurrent sync or uninstall operations; a busy
lock causes the command to fail rather than wait.

Before changing the destination, magpkg verifies the old manifest and creates
a complete in-memory plan. By default, modified managed paths and unmanaged
file or symlink collisions are errors. Pre-existing directories needed by the
closure are reused without being claimed or having their modes changed.

The planned changes are written to standard output as an action and a relative
path. Fetch progress and package build output are written to standard error.

# OPTIONS

*--dest* _PATH_
	Synchronize into _PATH_. This option is required. The directory is
	created when absent.

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

*--dry-run*
	Verify and print the destination plan without applying it or creating
	the destination or its metadata. Required packages are still substituted,
	fetched, or built before destination planning.

*--no-build*
	Do not invoke the build or binary-cache pipeline. Every artifact in the
	selected runtime closure must already exist in the store.

*--force*
	Replace modified manifest-owned files and symlinks, remove modified owned
	paths which are no longer desired, repair modified owned directories, and
	replace unmanaged file or symlink collisions. Unsafe directory type
	transitions and special-file collisions remain errors.

*--delete-unmanaged*
	Delete paths outside the desired closure. This requires *--force*.
	Magpkg does not delete the _.magpkg_ metadata tree, follow symlinks, or
	descend into another mounted filesystem.

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

Ordinary actions include *create*, *replace*, *remove*, and *chmod*. Forced
plans may additionally report *replace-modified*, *remove-modified*,
*repair-modified*, *replace-unmanaged*, and *delete-unmanaged*. With
*--dry-run*, each line begins with "would".

# EXAMPLES

Synchronize one runtime closure into a staging root:

```
magpkg sync --dest /tmp/root packages.jsonnet system
```

Inspect a forced cleanup plan:

```
magpkg sync --dest /tmp/root --force --delete-unmanaged --dry-run \
	packages.jsonnet system
```

# NOTES

Planning prevents known conflicts before execution, but applying a multi-file
plan is not a filesystem transaction. If execution is interrupted, inspect the
destination and rerun the command.

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-export*(1),
*magpkg-uninstall*(1), *magpkg-serve-cache*(1),
*magpkg-package*(5), *magpkg-store*(5)
