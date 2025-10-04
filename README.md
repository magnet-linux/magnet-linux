# Magnet Linux

Magnet Linux is a package manager and package tree for Linux that is resilient, simple and secure. 

If you ever...

- Wanted to share software environments with your friends or coworkers.
- Worried if you can keep your software stack working years or decades from now.
- Are an early adopter and accept it might have rough edges or lose support (Though you can fork it!).

Then it might be right for you!


## Why a new package manager?

- [Nix](https://github.com/NixOS/nixpkgs) and [Guix](https://guix.gnu.org/) have a lot of cool ideas, but are very complex.
- Software supply chain security and reliability is a growing concern.
- Automation of packaging using AI is now possible so it seemed like a good time to try.

## Why It’s Different

- **Reproducible and auditable**: Package definitions are deterministic, reproducible and easily cached.
- **Decentralized and reliable**: Release source code is mirrored on p2p networks (BitTorrent for now) with no reliance on central project infrastructure.
- **Dev shells**: Spin up project specific environments as easily as `python -m venv` or `nix-shell`.
- **OCI & containers**: Export any package as an OCI image ready for Docker/Podman, or layer Magnet Linux tooling into your existing pipelines.
- **Simplicity**: Packages are simply tarballs that can be unpacked and looked at.

## Project Principles

- Take good ideas, simplify where possible.
- Be easy to fork, vendor, modify and self host!
- Use AI automation where it makes sense.

## How it works

- Packages are defined as simple Jsonnet definitions that form a dependency graph.
- Each package's build definition is hashed, giving a unique id for each package (or package variable).
- If that package id doesn't exist then the package sources are fetched, validated and built.
- If a package is already cached, no need to rebuild from source.

## Try It

You will need:

- You will need [bwrap](https://github.com/containers/bubblewrap) preinstalled (used for package build sandboxing). 
- A Rust compiler so you can compile magpkg (releases coming soon!).

```bash
# Evaluate a manifest, fetch sources via P2P/HTTP, and build artifacts
magpkg build '(import "packages/core.jsonnet").coreutils'
```

## Status and Roadmap

- [ ] Initial concept.
  - [x] Simple reproducible packages built from source.
  - [x] Easy P2P source code hosting and mirroring.
  - [ ] Development shells inspired by nix-shell and python venv.
  - [ ] Optional self hostable precompilation caches.
- [ ] Containers
  - [ ] Export OCI and Docker containers from a simple manifest.
- [ ] Magnet Linux Distro!!!??
  - [ ] A full-blown distro that used magpkg as its package manager.

## Documentation

- [Bootstrapping the package tree](doc/bootstrap.md)
- [Package store layout](doc/store-layout.md)
- [P2P hosting guide](doc/p2p-hosting.md)
