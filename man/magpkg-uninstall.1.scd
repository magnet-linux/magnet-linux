MAGPKG-UNINSTALL(1) "magpkg"

# NAME

magpkg-uninstall - remove a synchronized runtime closure

# SYNOPSIS

*magpkg uninstall* *--dest* _PATH_ [options]

# DESCRIPTION

*magpkg uninstall* reads _PATH/.magpkg/manifest.json_, verifies the recorded
paths, and plans removal of the runtime closure previously installed by
*magpkg sync*. The destination must exist and contain magpkg metadata.

By default, any modified or missing manifest-owned path makes planning fail
before changes begin. Unmanaged paths are preserved. Managed directories are
removed only when empty, so directories containing unmanaged files remain.

After a successful uninstall the manifest is removed. The persistent
_PATH/.magpkg/lock_ file remains for later synchronization and locking.

# OPTIONS

*--dest* _PATH_
	Uninstall from _PATH_. This option is required.

*--dry-run*
	Verify the manifest and print the removal plan without changing the
	destination.

*--force*
	Remove manifest-owned paths even when they have been modified or are
	missing. Unmanaged paths are still preserved.

*-h*, *--help*
	Show command usage.

# OUTPUT

Each planned removal is written to standard output. Ordinary removals use the
*remove* action; forced removal of a modified path uses *remove-modified*.
With *--dry-run*, each line begins with "would".

# EXAMPLES

Preview an uninstall:

```
magpkg uninstall --dest /tmp/root --dry-run
```

Discard local changes to managed paths:

```
magpkg uninstall --dest /tmp/root --force
```

# SEE ALSO

*magpkg*(1), *magpkg-sync*(1)
