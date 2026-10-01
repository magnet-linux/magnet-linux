#!/usr/bin/env python3
"""Prepare pinned release inputs for a standalone distro checkout or tarball."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.parse
import urllib.request


def digest(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify(path, expected):
    if not re.fullmatch(r"[0-9a-f]{64}", expected) or digest(path) != expected:
        raise ValueError(f"SHA-256 mismatch: {path}")


def fetch(asset, cache):
    expected = asset["sha256"]
    if not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise ValueError("Invalid SHA-256 in dependency lock")
    url = asset["url"]
    if urllib.parse.urlparse(url).scheme not in ("https", "file"):
        raise ValueError("Release dependencies must use HTTPS or an explicit local file")
    destination = cache / expected
    if destination.exists():
        verify(destination, expected)
        return destination
    print(f"Downloading {Path(urllib.parse.urlparse(url).path).name}", flush=True)
    with tempfile.NamedTemporaryFile(dir=cache, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            with urllib.request.urlopen(url, timeout=60) as response:
                if urllib.parse.urlparse(response.geturl()).scheme not in ("https", "file"):
                    raise ValueError("Release download redirected away from HTTPS")
                shutil.copyfileobj(response, stream)
            stream.close()
            verify(temporary, expected)
            temporary.replace(destination)
        finally:
            temporary.unlink(missing_ok=True)
    return destination


def unpack(archive, asset, parent):
    prefix = asset["prefix"]
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", prefix):
        raise ValueError("Invalid archive prefix")
    with tarfile.open(archive, "r:gz") as source:
        for member in source.getmembers():
            if member.name.split("/")[0] != prefix:
                raise ValueError(f"Unexpected archive member: {member.name}")
        source.extractall(parent, filter="data")
    tree = parent / prefix
    metadata = dict(line.split("=", 1) for line in (tree / "RELEASE").read_text().splitlines())
    if metadata.get("version") != asset["version"]:
        raise ValueError("Archive version does not match dependency lock")
    return tree


def prepare(repo, lock):
    distro = repo / "magnet-linux"
    work = distro / "_work"
    cache = work / "downloads"
    cache.mkdir(parents=True, exist_ok=True)
    with (work / "prepare.lock").open("a") as lease:
        fcntl.flock(lease, fcntl.LOCK_EX)
        if lock.get("format") != 1:
            raise ValueError("Unsupported dependency lock format")

        packages = repo / "packages"
        if packages.is_symlink():
            raise ValueError("The prepared package tree must be a real directory")
        if not packages.exists():
            with tempfile.TemporaryDirectory(prefix=".packages-", dir=work) as temporary:
                temporary = Path(temporary)
                override = os.environ.get("PACKAGES_DIR")
                if override:
                    source = Path(override).resolve(strict=True)
                    if not (source / "bootstrap.jsonnet").is_file():
                        raise ValueError("PACKAGES_DIR is not a package tree")
                    tree = temporary / "packages"
                    shutil.copytree(source, tree, ignore=shutil.ignore_patterns(
                        ".git", "out", "_work", "__pycache__", "*.pyc"))
                    (tree / ".magnet-development-tree").write_text("Explicit PACKAGES_DIR copy\n")
                else:
                    asset = lock["packages"]
                    tree = unpack(fetch(asset, cache), asset, temporary)
                if not (tree / "bootstrap.jsonnet").is_file():
                    raise ValueError("Package archive lacks bootstrap.jsonnet")
                tree.replace(packages)
        elif not (packages / "bootstrap.jsonnet").is_file():
            raise ValueError("Existing packages directory is not a package tree")
        elif not (packages / ".magnet-development-tree").exists():
            release = packages / "RELEASE"
            expected = f"version={lock['packages']['version']}"
            if not release.is_file() or expected not in release.read_text().splitlines():
                raise ValueError("Existing package tree differs from the lock; move it aside explicitly first")
        # Keep administrator recipe edits, even when prepare is run repeatedly.

        seed_override = os.environ.get("BOOTSTRAP_ARCHIVE")
        if seed_override:
            seed = Path(seed_override).resolve(strict=True)
            verify(seed, lock["bootstrap"]["sha256"])
        else:
            seed = fetch(lock["bootstrap"], cache)
        destination = work / "bootstrap.tar.zst"
        if not destination.exists() or digest(destination) != lock["bootstrap"]["sha256"]:
            temporary = work / ".bootstrap.tar.zst.new"
            shutil.copyfile(seed, temporary)
            temporary.chmod(0o644)
            temporary.replace(destination)

        override = os.environ.get("MAGPKG")
        if override:
            binary = shutil.which(override)
            if not binary:
                raise ValueError("MAGPKG must name an executable")
        else:
            asset = lock["magpkg"]
            archive = fetch(asset, cache)
            binary = work / "magpkg"
            with tempfile.TemporaryDirectory(prefix=".magpkg-", dir=work) as temporary:
                tree = unpack(archive, asset, Path(temporary))
                released = tree / "bin/magpkg"
                if not released.is_file() or released.is_symlink():
                    raise ValueError("Binary archive lacks a regular bin/magpkg")
                if not binary.is_file() or digest(binary) != digest(released):
                    replacement = work / ".magpkg.new"
                    shutil.copyfile(released, replacement)
                    replacement.chmod(0o755)
                    replacement.replace(binary)
                # Retain the notices distributed with the static executable.
                shutil.copytree(tree / "share/licenses", work / "magpkg-licenses", dirs_exist_ok=True)
        subprocess.run([str(binary), "--version"], check=True)
        print(f"Package tree: {packages}\nBootstrap seed: {destination}\nMagpkg: {binary}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lock", type=Path, help="explicit dependency lock (default: dependencies.json)")
    args = parser.parse_args()
    repo = Path(__file__).resolve().parent.parent
    lock = json.loads((args.lock or repo / "dependencies.json").read_text())
    prepare(repo, lock)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"prepare: {error}") from error
