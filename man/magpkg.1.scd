MAGPKG(1) "magpkg"

# NAME

magpkg - build and manage input-addressed package closures

# SYNOPSIS

*magpkg* <command> [options]

*magpkg* *help*

# DESCRIPTION

Magpkg evaluates package graphs written in Jsonnet, fetches fixed-output
sources, and builds package closures in an isolated environment. Package
artifacts are stored by package ID and may be reused safely by concurrent
magpkg processes.

The command can also export runtime closures as archives, compose them into
mutable destination trees, enter package-defined shell environments, and
remove old cached state.

# COMMANDS

*init*
	Initialize a package store.

*build*
	Build selected roots and their dependencies from a Jsonnet package graph.

*shell*
	Enter a cached, package-defined shell environment.

*export*
	Export selected runtime closures as one tar archive.

*sync*
	Make the magpkg-managed portion of a destination match selected runtime
	closures.

*uninstall*
	Remove paths recorded by a previous *magpkg sync* operation.

*clean*
	Remove old fetches, package artifacts, shell environments, and workspaces.

*serve-cache*
	Serve completed package artifacts from a store over HTTP.

Run *magpkg* <command> *--help* for a command's short usage text.

# STORE SELECTION

Commands which use the package store resolve its path in this order:

- the command's *--store* option
- the *MAGPKG_STORE* environment variable
- _$HOME/.magpkg/store_

The store must first be created with *magpkg init*.

# ENVIRONMENT

*MAGPKG_STORE*
	Default package store path when *--store* is not given.

*MAGPKG_BINARY_CACHE*
	Default trusted binary-cache URL for *build*, *export*, *shell*, and
	*sync*. The corresponding *--binary-cache* option takes precedence.

*HOME*
	Used to derive the default package store and shell-environment paths.

# EXIT STATUS

Magpkg exits zero after a successful command and non-zero after an error.

# FILES

_$HOME/.magpkg/store_
	Default package store.

_$HOME/.magpkg/environments_
	Materialized shell-environment cache.

# SEE ALSO

*magpkg-init*(1), *magpkg-build*(1), *magpkg-shell*(1),
*magpkg-export*(1), *magpkg-sync*(1), *magpkg-uninstall*(1), *magpkg-clean*(1),
*magpkg-serve-cache*(1), *magpkg-package*(5), *magpkg-store*(5)
