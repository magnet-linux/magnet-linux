MAGPKG-SERVE-CACHE(1) "magpkg"

# NAME

magpkg-serve-cache - serve package artifacts from a store over HTTP

# SYNOPSIS

*magpkg serve-cache* [options]

# DESCRIPTION

*magpkg serve-cache* runs a read-only HTTP binary cache backed by an initialized
package store. It serves completed package artifacts, but does not expose source
fetches, locks, workspaces, store metadata, or directory listings.

The server implements these version 1 endpoints:

```
GET  /v1/info
HEAD /v1/info
GET  /v1/packages/<artifact-filename>
HEAD /v1/packages/<artifact-filename>
```

Artifacts are held open for the duration of a response. Package cleanup may
therefore unlink an artifact concurrently without interrupting a transfer.

The protocol does not authenticate artifact contents. Serve the cache only to
clients which trust this store, and use an authenticated transport or trusted
network where appropriate.

# OPTIONS

*--store* _PATH_
	Serve artifacts from _PATH_.

*--listen* _ADDRESS_
	Listen on _ADDRESS_. The default is _127.0.0.1:8080_. Use an explicit
	non-loopback address to expose the cache to other hosts.

*--no-touch*
	Do not refresh artifact timestamps for GET requests. By default, GET for
	an existing artifact updates both access and modification times through
	the open file descriptor so package cleanup treats the artifact as
	recently used. HEAD never updates timestamps.

*-h*, *--help*
	Show command usage.

# OUTPUT

After listening successfully, the command writes the cache's HTTP address to
standard output. It then serves until interrupted or until the HTTP server
fails.

# EXAMPLES

Serve the default store locally:

```
magpkg serve-cache
```

Expose a store on a trusted private network without affecting cleanup ages:

```
magpkg serve-cache --store /srv/magpkg/store \
	--listen 0.0.0.0:8080 --no-touch
```

# SEE ALSO

*magpkg*(1), *magpkg-build*(1), *magpkg-store*(5)
