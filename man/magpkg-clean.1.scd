MAGPKG-CLEAN(1) "magpkg"

# NAME

magpkg-clean - remove old cached magpkg state

# SYNOPSIS

*magpkg clean* (*--all* | *--fetches* | *--packages* | *--environments*) [options]

# DESCRIPTION

*magpkg clean* removes recognized state which has not been used for a selected
amount of time. At least one cleanup category is required. The default minimum
unused age is seven days.

Cleanup uses modification times. Recently used fetches, package artifacts,
and shell environments are touched by magpkg so that they remain live. Each
candidate is locked without waiting; an entry in active use is skipped.
Unknown files are not removed.

Each removal is written to standard output as "cleaned", its kind, and its
path.

# OPTIONS

*--all*
	Select fetches, packages, and shell environments. This selects categories
	only; it does not mean an age of zero and may be combined with
	*--older-than*.

*--fetches*
	Remove old verified source files, partial fetches, and associated idle
	lock files.

*--packages*
	Remove old package archives, abandoned build workspaces, and associated
	idle lock files.

*--environments*
	Remove old materialized shell environments, abandoned environment
	workspaces, and associated idle lock files. Active launchers retain a
	shared lease and are skipped. This category does not require the package
	store to exist.

*--older-than* _AGE_
	Remove selected state unused for at least _AGE_. The default is *7d*.
	Go duration forms such as *12h* and *90m* are accepted, as are integer
	day and week forms such as *30d* and *2w*. The age must be positive.

*--store* _PATH_
	Use _PATH_ as the package store.

*-h*, *--help*
	Show command usage.

# EXAMPLES

Clean every category using the default seven-day age:

```
magpkg clean --all
```

Clean fetches unused for thirty days:

```
magpkg clean --fetches --older-than 30d
```

Clean package artifacts unused for twelve hours:

```
magpkg clean --packages --older-than 12h
```

Clean materialized shell environments unused for one day:

```
magpkg clean --environments --older-than 1d
```

# SEE ALSO

*magpkg*(1), *magpkg-shell*(1), *magpkg-store*(5)
