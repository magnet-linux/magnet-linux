MAGPKG-PACKAGE(5) "magpkg"

# NAME

magpkg-package - package definitions, source fetches, and package identities

# DESCRIPTION

Magpkg package trees are Jsonnet programs. Evaluation produces a graph of
package definitions. Each definition describes its build, dependencies, and
fixed-output source fetches.

A package ID identifies the materialized definition and all package
dependencies named by it. It does not identify the bytes produced by running
the build.

# JSONNET ROOTS

The top-level Jsonnet value must be an object. Each visible field is a named
root and its value must be a package object. For example:

```
local hello = {
  name: 'hello',
  build: {
    kind: 'script',
    script: 'mkdir -p /out/bin; cp /fetch/hello /out/bin/hello',
  },
  runDeps: [],
  buildDeps: [],
  fetch: [{
    filename: 'hello',
    sha256: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
    urls: ['https://example.invalid/hello'],
  }],
};

{
  hello: hello,
}
```

The root field name, Jsonnet filename, source formatting, comments, and import
paths are not directly part of a package ID. They affect an ID only when they
change the materialized package definition.

Shared Jsonnet package objects are materialized once. Distinct objects with
identical materialized definitions also receive the same ID. Dependency cycles
are rejected.

# PACKAGE DEFINITION

A package object accepts exactly these fields:

*name*
	Optional string. The default is the empty string. A non-empty name must
	be at most 128 bytes, start with an ASCII letter or digit, and contain
	only ASCII letters, digits, period, underscore, plus, or hyphen.

	The name is part of the package ID. It is also used as a human-readable
	prefix on the stored package archive.

*build*
	Required object describing how output is produced. It has a required
	*kind* string and accepts a *script* string:

	- *{ kind: 'none' }* produces an empty meta-package.
	- *{ kind: 'script', script: '...' }* runs the non-empty shell program in
	  the build sandbox.
	- *{ kind: 'unpack' }* extracts each fetched archive directly into the
	  package output in fetch order.

	*script* is required and non-empty for the *script* kind and is rejected
	for the other kinds.

*buildEnv*
	Optional object whose field names and values are strings. Its values
	override the default build environment described in *BUILD ENVIRONMENT*.
	Materialized graphs retain only effective overrides; entries equal to a
	default are removed.

*runDeps*
	Required array of package objects. Runtime dependencies are included in
	the package's runtime closure and are also present while building it.

*buildDeps*
	Required array of package objects. Build dependencies and their runtime
	closures are present while building the package, but do not enter the
	package's runtime closure.

*fetch*
	Required array of fetch objects, described in *FETCH DEFINITIONS*.

Unknown fields are rejected. Array order and duplicate entries are preserved.
Consequently, reordering or duplicating dependencies or fetches changes the
package ID even when it does not change the eventual filesystem contents.

# BUILD ENVIRONMENT

Before hashing and building, *buildEnv* is overlaid on these defaults:

```
CONFIG_SHELL=/bin/sh
HOME=/build
LC_ALL=C
PATH=/usr/bin:/bin:/usr/sbin:/sbin
SHELL=/bin/sh
TZ=UTC
```

Variable names must be valid ASCII shell variable names. Values must not
contain NUL. *BUILD_PARALLELISM* is reserved: the executor sets it to the
requested per-package parallelism and package definitions may not set it.
Because this value is chosen at execution time, it is not part of the package
ID.

For a normal build, the dependency closure is overlaid into a disposable root
filesystem. Fetched files appear read-only below _/fetch_. The build directory
is _/build_, the writable package result is _/out_, and _/tmp_ is a tmpfs. The
build runs as:

```
/bin/sh -e /build/.magpkg-build
```

The sandbox receives only the resolved build environment plus
*BUILD_PARALLELISM*. Its network namespace is isolated from the host.

# FETCH DEFINITIONS

Each element of *fetch* is an object accepting exactly these required fields:

*filename*
	A non-empty string naming the source as _/fetch/_*filename*. It must be
	one path component: slash, backslash, and the name "." are not allowed.
	Filenames within one package must be unique.

*sha256*
	A 64-character hexadecimal SHA-256 digest of the fetched bytes.
	Uppercase hexadecimal is currently accepted, but lowercase should be
	used. The spelling is retained in the package definition, so changing
	only its case changes the package ID.

*urls*
	An array of strings, tried in order until one succeeds. An empty array is
	accepted as a definition, but fetching fails unless the digest is already
	in the local fetch cache.

The filename, digest spelling, URLs, and their ordering are all part of the
package ID.

# FETCH PROTOCOLS

The following URL schemes are supported:

*file*
	The URL must name an absolute local path. The host must be empty or
	"localhost". User information, query parameters, and fragments are
	rejected.

*http*, *https*
	The URL must have a host. Magpkg sends a GET request with
	"Accept-Encoding: identity", accepts only a 2xx response, and hashes the
	response bytes without transparent content decoding.

URLs without a scheme and unsupported schemes are rejected.

# FETCH CACHE

Fetched bytes are cached at:

```
<store>/fetch/<lowercase-sha256>
```

The cache key is the decoded digest, normalized to lowercase hexadecimal.
Neither *filename* nor *urls* affects the cache path, so fetch definitions with
the same digest share cached bytes.

Fetches of one digest are serialized with an advisory lock. A download is
written to a temporary file, hashed while it is copied, and atomically renamed
into place only after its digest matches. An existing cache entry is rehashed
before use. Successful use touches both the cached file and its lock file so
age-based cleanup retains recently used sources.

# PACKAGE ID

Package IDs use a versioned, domain-separated SHA-256 hash. They are computed
as follows:

- Recursively materialize every *runDeps* and *buildDeps* package object and
	replace it with its package ID.
- Overlay *buildEnv* on the default build environment.
- Encode the resulting definition as compact canonical JSON.
- Hash the literal byte prefix "magpkg-package-v2", one NUL byte, and the
	canonical JSON bytes.
- Encode the 32-byte digest as unpadded lowercase Base32.

In notation:

```
digest = SHA256(bytes("magpkg-package-v2") || byte(0) || canonical_json)
id = base32_lowercase_no_padding(digest)
```

The Base32 alphabet is:

```
abcdefghijklmnopqrstuvwxyz234567
```

A package ID is therefore exactly 52 characters. The domain prefix prevents a
package definition from sharing a hash domain with unrelated SHA-256 uses and
allows an incompatible future encoding to select a new version.

## Canonical JSON

Canonical JSON is the output of Go's *encoding/json* encoder for the
materialized definition. It contains no insignificant whitespace and emits
fields in this exact order:

```
name, build, buildEnv, runDeps, buildDeps, fetch
```

Every field is present. An absent *name* becomes "", empty arrays become [],
and *buildEnv* contains the complete default-plus-overrides map. Environment
keys are sorted lexicographically by the encoder. The *build* object emits
*kind* followed by *script*; *script* is present as "" for non-script kinds.
Each fetch object emits fields in this order:

```
filename, sha256, urls
```

Jsonnet object field order and *buildEnv* field order therefore do not affect
the ID. Array order does. JSON strings use the escaping performed by Go's
*encoding/json* encoder, including its default HTML-safe escaping.

## Example

This package:

```
{
  hello: {
    name: 'hello',
    build: { kind: 'script', script: 'true' },
    runDeps: [],
    buildDeps: [],
    fetch: [],
  },
}
```

has this exact canonical JSON (shown on one line because whitespace matters):

```
{"name":"hello","build":{"kind":"script","script":"true"},"buildEnv":{"CONFIG_SHELL":"/bin/sh","HOME":"/build","LC_ALL":"C","PATH":"/usr/bin:/bin:/usr/sbin:/sbin","SHELL":"/bin/sh","TZ":"UTC"},"runDeps":[],"buildDeps":[],"fetch":[]}
```

Its package ID is:

```
fy7f2e4kuvha4t5x66ibmn3itg3vqj3zfj7vbx6gk62q4xq5ghha
```

# HASH BOUNDARY

The following inputs are included, directly or transitively, in the ID:

- the package name, build kind, and build script
- the fully resolved build environment
- ordered runtime and build dependency IDs
- ordered fetch definitions, including filenames, digest spellings, and URLs

The following are not directly included:

- Jsonnet source text, root names, filenames, and formatting
- package output bytes or archive metadata
- executor-selected build parallelism
- the host bubblewrap binary and magpkg implementation
- fetch cache paths and download times

The ID is therefore an input-addressed package identity, not a content hash of
the resulting archive. Reproducible builds should produce equivalent output
for one ID, but the package ID alone does not prove that two produced archives
are byte-for-byte identical.

# PACKAGE ARTIFACTS

Successful output is stored as a zstd-compressed tar archive. Named and unnamed
packages use these paths respectively:

```
<store>/packages/<name>-<id>.tar.zst
<store>/packages/<id>.tar.zst
```

The name prefix is for inspection; *name* is already included in the ID.
Builds and package cleanup coordinate by package ID using advisory locks.

# SHELL LAUNCHERS

A runtime closure intended for *magpkg shell* must provide an executable
regular file at _/libexec/magpkg-shell_. Magpkg executes this launcher on the
host with the absolute materialized environment root as its first argument and
the requested command arguments after it. With no command arguments, the
launcher selects the interactive shell.

The launcher, rather than the package-store or shell-environment libraries,
defines sandboxing, mounts, namespaces, environment variables, and command
execution. It must remain in the foreground for the complete shell session.

Launchers run with the invoking user's host permissions before establishing
any sandbox. A binary cache may supply the package containing a launcher, so
only trusted packages and caches should be used.

# SEE ALSO

*magpkg*(1), *magpkg-shell*(1), *magpkg-store*(5), *jsonnet*(1), *scdoc*(5)
