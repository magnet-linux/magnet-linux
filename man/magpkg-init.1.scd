MAGPKG-INIT(1) "magpkg"

# NAME

magpkg-init - initialize a magpkg package store

# SYNOPSIS

*magpkg init* [*--store* _PATH_] [_PATH_]

# DESCRIPTION

*magpkg init* creates the package-store root, any missing parent directories,
and versioned store metadata. Fetch, package, lock, and workspace
subdirectories are created later as they are needed. Existing compatible
stores are accepted; malformed or unsupported metadata is not overwritten.

On success, the absolute store path is written to standard output. Running the
command again for an existing directory is harmless.

# OPTIONS

*--store* _PATH_
	Initialize _PATH_. This option cannot be combined with a positional
	_PATH_.

*-h*, *--help*
	Show command usage.

# STORE SELECTION

When no path is supplied, the command uses *MAGPKG_STORE*, then
_$HOME/.magpkg/store_. A relative path is converted to an absolute path.

# EXAMPLES

Initialize the default store:

```
magpkg init
```

Initialize a store at an explicit path:

```
magpkg init /var/lib/magpkg
```

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-store*(5)
