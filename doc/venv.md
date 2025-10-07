# Virtual Environments

`magpkg venv` builds on the package graph to give you reproducible, cached development environments.  Each invocation evaluates a Jsonnet manifest, exports the runtime closure of the requested packages into `~/.magpkg/venv/<hash>/rootfs`, and then launches [bubblewrap](https://github.com/containers/bubblewrap) with the mounts and environment variables you specify.

## Quick Start

```bash
# Launch the example environment defined in magpkg/examples/core-venv.jsonnet
magpkg venv -e '(import "magpkg/examples/core-venv.jsonnet")'

# Run a single command instead of an interactive shell
magpkg venv -e '(import "magpkg/examples/core-venv.jsonnet")' -- git status
```

The first run materializes the venv under the store; subsequent runs with the same manifest hash reuse the cached rootfs instantly.

## Manifest Fields

| Field | Type | Description |
| ----- | ---- | ----------- |
| `packages` | array | Required list of packages to include. Their runtime closures determine the venv hash. |
| `envKeep` | array | Environment variable names to inherit from the host. |
| `envSet` | object | Environment variables to set or override before launch. If `PATH` or `LD_LIBRARY_PATH` are not provided, `magpkg` supplies `/usr/bin:/bin:/usr/sbin:/sbin` and `/usr/lib64:/usr/lib:/lib` respectively. |
| `mountDefaults` | bool | Optional flag (default `true`) that controls whether built-in mounts are added. |
| `mounts` | array | Additional mounts. Strings like `"/home"` expand to `--bind /home /home`; objects give full control (`type`, `source`, `target`, `optional`). |
| `fsEntries` | array | Directories, files, or symlinks to create inside the cached rootfs. These entries are hashed, so changing them produces a new cache key. |

See `magpkg/examples/core-venv.jsonnet` for a commented reference manifest.

## Default Mounts

When `mountDefaults` is `true`, the venv adds the following before applying user mounts:

- `--dev-bind /dev /dev`
- `--proc /proc`
- `--ro-bind /sys /sys`
- `--ro-bind /etc/resolv.conf /etc/resolv.conf`
- `--ro-bind /etc/hosts /etc/hosts`
- `--bind /tmp /tmp`

After merging with user mounts, if `/tmp` is still missing, `magpkg` attaches a `--tmpfs /tmp` to guarantee a writable scratch space.

Network-dependent tools often benefit from additional read-only binds (`/etc/ssl`, distro-specific certificate bundles, `/run/systemd/resolve/...`). Any path you add via `mounts` can be marked `optional: true` to tolerate hosts where it is absent.

## Caching & Cleanup

- Venv root filesystems live under `~/.magpkg/venv/<hash>/rootfs`. They are content-addressed by the package closure plus `fsEntries` and are mounted read-only during execution.
- Temporary state should go in writable mounts such as `/tmp`, `/home`, or custom directories you bind in.
- `magpkg cleanup --venvs --max-age-days <N>` prunes cached venvs older than the selected age, taking a shared lock to avoid deleting environments that are still running.

## Advanced Tips

- Combine `envKeep` with explicit `envSet` entries to thread secrets or tokens in from the host without baking them into the cache hash.
- Use `fsEntries` to pre-create directories like `/etc/ssl` or stub configuration files. File entries can include inline contents and POSIX modes.
- For hermetic environments, set `mountDefaults: false` and list every required mount explicitly. Remember to include `/dev`, `/proc`, and a writable `/tmp` or tmpfs replacement.

Have ideas for smoother ergonomics, such as `--file` manifests or manifest registries? Check the issue tracker or open a discussion—new options for `magpkg venv` are welcome.
