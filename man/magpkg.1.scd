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

The command can also compose runtime closures into mutable destination trees
and remove old store state.

# COMMANDS

*init*
	Initialize a package store.

*build*
	Build selected roots and their dependencies from a Jsonnet package graph.

*sync*
	Make the magpkg-managed portion of a destination match selected runtime
	closures.

*uninstall*
	Remove paths recorded by a previous *magpkg sync* operation.

*clean*
	Remove old cached fetches, package artifacts, and workspaces.

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

*HOME*
	Used to derive the default package store path.

# EXIT STATUS

Magpkg exits zero after a successful command and non-zero after an error.

# FILES

_$HOME/.magpkg/store_
	Default package store.

# SEE ALSO

*magpkg-init*(1), *magpkg-build*(1), *magpkg-sync*(1),
*magpkg-uninstall*(1), *magpkg-clean*(1), *magpkg-package*(5),
*magpkg-store*(5)
