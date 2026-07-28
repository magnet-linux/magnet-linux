MAGPKG-STORE(5) "magpkg"

# NAME

magpkg-store - package store layout, persistent artifacts, and transient state

# DESCRIPTION

The magpkg store contains version metadata, verified source fetches, built
package archives, advisory lock files, and temporary build workspaces. It is
deliberately a filesystem-only store: there is no database, package index,
dependency graph, or persisted package definition.

Package definitions and their dependency graph are materialized from Jsonnet
when magpkg runs. The store records only the fetched and built results needed
to reuse that work.

# STORE LOCATION

The command-line tool resolves its store path in this order:

- the *--store* option
- the *MAGPKG_STORE* environment variable
- _$HOME/.magpkg/store_

The Go package-store API accepts an explicitly initialized store. *magpkg init*
creates the store root and version metadata if necessary. Opening an ordinary
directory or a store with unsupported metadata fails. Subdirectories are
created lazily by fetch, build, and cleanup operations.

# LAYOUT

A populated store has this general layout:

```
<store>/
|-- .magpkg-store.json
|-- fetch/
|   |-- <sha256>
|   |-- <sha256>.lock
|   `-- <sha256>.tmp
|-- packages/
|   |-- <[name-]id>.tar.zst
|   |-- <[name-]id>.lock
|   `-- <[name-]id>.workdir/
|       |-- root/
|       |-- build/
|       |-- output/
|       |-- fetch/
|       |-- artifact.tar.zst
|       `-- binary-cache-*.tmp
`-- tmp/
    |-- <prefix><random>/
    `-- <prefix><random>.lock
```

_.magpkg-store.json_ identifies the directory as a magpkg store and records
the store format version. Version 1 has this form:

```
{
  "format": "magpkg-store",
  "version": 1
}
```

Initialization publishes this metadata atomically and is idempotent for an
existing compatible store. Magpkg does not overwrite malformed or unsupported
metadata.

Here, *sha256* is a 64-character lowercase hexadecimal source digest and *id*
is a 52-character lowercase Base32 package ID. Not every entry exists at once.
Files ending in _.tmp_, package workspaces, workspace artifacts, and
everything under _tmp/_ are transient.

The store root and its standard subdirectories are created with mode 0755.
Store metadata, cached sources, final package archives, and lock files are
initially created with mode 0600. All creation modes are subject to the process
umask.

# FETCH CACHE

## fetch/<sha256>

This regular file contains source bytes whose SHA-256 digest is *sha256*. The
digest is always written as lowercase hexadecimal, even if a package definition
used uppercase hexadecimal.

The cache path depends only on the digest. Fetch filenames and URLs do not
affect it, so different definitions referring to the same bytes share an
entry.

Magpkg rehashes this file every time it is requested. A valid entry is touched
before it is returned. An invalid entry is removed and fetched again.

## fetch/<sha256>.tmp

This is an incomplete download for *sha256*. Only the process holding the
corresponding fetch lock may create or publish it.

Bytes are hashed while being written. After the file is flushed and its digest
matches, it is renamed atomically to _fetch/<sha256>_ and the fetch directory
is synchronized. A failed fetch normally removes its temporary file. One may
remain after an interrupted process and is eligible for cleanup.

## fetch/<sha256>.lock

This file carries the advisory lock used to serialize verification and
download of one digest. It contains no meaningful data. Its existence does not
mean a fetch is active; activity is represented by a kernel lock on the file.

# PACKAGE ARCHIVES

Named packages are stored as:

```
packages/<name>-<id>.tar.zst
```

An unnamed package is stored as:

```
packages/<id>.tar.zst
```

The name is a readable prefix and is also part of the definition hashed into
the package ID. Availability is determined by a regular file at the exact
expected path. The store does not maintain a separate index.

Each artifact is a zstd-compressed tar archive of the package's _/out_
directory. Archive paths are relative to _/out_ and sorted. User and group IDs
are normalized to zero, names are cleared, timestamps are normalized to the
Unix epoch, and extended attributes and PAX records are removed. File modes,
symlinks, and symlink targets are preserved.

Package archives should be treated as immutable after publication. Magpkg
touches an archive when planning with it or extracting it, but currently does
not hash or otherwise verify its contents. A package ID hashes the package
definition, not this archive.

# PACKAGE LOCKS

## packages/<[name-]id>.lock

All operations that can conflict for one package coordinate through this
file. The lock shares its _[name-]id_ key with the package archive and
workspace; the name is part of the definition hashed into the package ID, so
the key is unique per package.

- a builder holds an exclusive lock while realizing and publishing the package
- extraction and other package use hold a shared lock
- cleanup attempts an exclusive lock without waiting

The file contains no meaningful data, and its presence alone does not indicate
that a package is busy. Lock files commonly remain after builds and reads.

Build executors use non-blocking acquisition. If another process is building a
ready package, an executor can build another ready DAG branch and later observe
the published archive. This permits multiple magpkg processes to execute the
same plan without a central work queue.

# TEMPORARY PATHS

## tmp/<prefix><random>

Transient trees too large for system temporary storage, such as sync and
export staging and store initialization scratch space. The prefix names the
operation (for example _sync-_, _export-_, or _init-_).

A temporary path only ever exists while its sibling _.lock_ file is held: an
owner reserves and exclusively locks the lock file before creating the
directory, and removes the directory before unlinking the lock. Cleanup takes
the same lock before removing anything, so any entry whose lock can be
acquired was abandoned by an interrupted operation and may be removed
regardless of age. See *magpkg-clean*(1) *--tmp*.

# BUILD WORKSPACES

## packages/<[name-]id>.workdir

One workspace is used while locally building or downloading a package
artifact. The operation holds the exclusive sibling _.lock_ while using it.
A local builder removes any workspace left by an earlier attempt before
starting.

The workspace normally contains:

*root/*
	A disposable root filesystem assembled by overlaying the package's build
	and runtime dependencies. Bubblewrap binds this directory at _/_.

*build/*
	The writable build directory, bound at _/build_. For a non-empty normal
	build, _.magpkg-build_ contains the package's build program.

*output/*
	The writable package result, bound at _/out_. Its contents become the
	final package archive.

*fetch/*
	Read-only staged copies of the package's fetched sources. This directory
	is bound at _/fetch_ for normal builds.

*artifact.tar.zst*
	The completed but not yet published package archive.

*binary-cache-\*.tmp*
	A binary-cache response being downloaded and validated.

After a successful build, _artifact.tar.zst_ is renamed into _packages/_,
that directory is synchronized, and the workspace is removed. A failed or
cancelled build leaves its workspace for inspection. The next attempt removes
it before rebuilding. A successful binary-cache substitution also removes its
workspace; failed downloads remove their temporary file.

# PUBLICATION

Source and package publication use a temporary file followed by a rename
within the same store. The destination directory is synchronized after the
rename. Readers check for the final regular file while holding the appropriate
lock and never consume workspace artifacts or partial fetches. The binary-cache
server instead opens the immutable artifact without following symlinks and
holds that descriptor for the complete response.

Publishing package output is idempotent under its package lock. If the final
archive already exists, a second result is not substituted for it.

# BINARY CACHE

Build operations may be configured with a trusted binary-cache URL. For each
missing package, magpkg first requests:

```
<base-url>/v1/packages/<artifact-filename>
```

An HTTP 404 is a cache miss. A successful response is written under the
package lock, flushed, completely decoded and structurally validated as a safe
tar.zst archive, and atomically published into _packages/_. Other HTTP
responses, transport failures, and invalid archives are errors.

Package IDs identify build definitions rather than output bytes. Structural
validation detects malformed or truncated archives but does not authenticate
their contents. Binary caches must therefore be trusted. Authentication,
uploads, signatures, and multi-cache selection are not part of version 1.

*magpkg serve-cache* exposes only completed package artifacts and protocol
information. It holds each artifact open while serving, so cleanup may unlink
the path without interrupting an active response. GET requests for existing
artifacts refresh their access and modification times through the open file
descriptor unless *--no-touch* is used. HEAD requests never refresh them.

# CLEANUP

Age-based cleanup uses filesystem modification times, not a database or access
log. Successful source use touches _fetch/<sha256>_. Package planning and
extraction touch the package archive. Cleanup considers package archives,
partial fetches, and build workspaces independently. Temporary
paths under _tmp/_ are not caches, so their cleanup ignores age: liveness is
proven by a held lock, and every idle entry is removed.

For every recognized fetch digest or package ID, cleanup tries to acquire its
exclusive lock without waiting. A busy entry is skipped because it is being
fetched, built, or read. For an idle entry, expired data is removed and the
lock pathname is unlinked last while its inode is still locked. Lock
acquisition verifies that it locked the inode currently named by the path and
retries after this cleanup race when necessary.

The library cleanup functions interpret an age of zero as removal of all idle
entries. The command-line interface requires a positive *--older-than* value
and defaults to seven days. Its *--all* option selects all cleanup categories;
it does not override the age:

```
magpkg clean --fetches --older-than 30d
magpkg clean --packages --older-than 7d
magpkg clean --all
```

Cleanup recognizes entries only when their names have the expected digest,
package ID, and suffix form. Unknown files and directories are ignored.

# CONCURRENCY AND FILESYSTEM REQUIREMENTS

The store is safe for concurrent magpkg processes when its filesystem provides:

- BSD-style shared and exclusive advisory file locks
- stable inode identity while a file is open
- atomic rename within a directory
- ordinary directory synchronization semantics

Locking is advisory. External programs that edit store files or ignore the
lock protocol can corrupt active operations. In particular, lock files should
not be manually unlinked while magpkg processes are using the store.

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-export*(1),
*magpkg-serve-cache*(1), *magpkg-package*(5)
