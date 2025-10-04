# Package Store Layout

`magpkg` stores build results and caches under a single root, defaulting to `~/.magpkg` (override with the `MAGPKG_STORE` environment variable). The directory layout is designed for deterministic rebuilds and safe concurrency between multiple processes.

- `pkgs/`
  - `${name-or-hash}.tar.zst`: final content-addressed package archives.
  - `${name-or-hash}.lock`: lock files used while a package is being built or touched.
  - `${name-or-hash}.build/`: ephemeral build chroot populated for the current build.
- `fetch/`
  - `${sha256}`: cached source artifact named by its checksum.
  - `${sha256}.lock`: per-source lock guards fetch/download work.
  - `${sha256}.tmp`: temporary download target before checksum verification.
  - `.torrent-session-*/`: active librqbit session state (each contains a `downloads/` directory with `${sha256}.torrent-work-*` scratch space while a torrent fetch is running).
- `torrent/`
  - `<info-hash>/resource.torrent`: generated or cached `.torrent` metadata.
  - `<info-hash>/<relative-path>`: seed copy of the fetched payload.
- `seed/`
  - `seeder.lock`: mutex for the long-running torrent seeder.
  - `dht.json`: persisted Distributed Hash Table state for faster restarts.

During a build, dependencies are unpacked beneath `pkgs/${base}.build/rootfs`, output files land in `rootfs/out`, and the finished tree is repacked into `pkgs/${base}.tar.zst`. Fetch, build, cleanup, and seeding commands coordinate exclusively via these files, so you can inspect or back up the store safely.
