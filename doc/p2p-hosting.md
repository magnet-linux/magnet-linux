# P2P Hosting Basics

Magnet Linux stores every fetched source in `~/.magpkg/fetch/` and records matching torrent metadata under `~/.magpkg/torrent/<info-hash>/resource.torrent`. Seeding those files keeps the ecosystem fast even when origin mirrors disappear. Torrents only come into play when a package definition lists one of their magnet URLs or `.torrent` files in its `fetch.urls`; adding a new magnet link to the manifest immediately lets other builders reuse your seeded payload.

## Built-in Seeder
- Fetch or build something once, e.g. `magpkg build -e 'import "packages/core.jsonnet"'`.
- Start the bundled seeder: `magpkg seed`.
  - Listens on TCP 6881 (override with `--listen-port` or use `--no-listen` for outbound-only mode).
  - Uses `~/.magpkg/torrent/seed.lock` as its lock file, so you can leave it running in the background or run it on a server with `MAGPKG_STORE=/path/to/store`.

## Seeding with Other Clients
- Copy a torrent: `cp ~/.magpkg/torrent/<info-hash>/resource.torrent my-package.torrent`.
- Point your BitTorrent client at the matching payload directory (`~/.magpkg/torrent/<info-hash>/`). Most clients ask for the data location after you add the torrent; choose that folder and the client will detect it and begin seeding immediately.
- Repeat for any other payloads you want to mirror—each subdirectory in `~/.magpkg/torrent/` is a self-contained torrent you can import into any standard client.
