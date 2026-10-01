#!/usr/bin/env python3
"""Copy only a selected runtime closure, plus exact installation sources."""
import hashlib
import json
import shutil
import sys
from pathlib import Path

def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()

mode, *args = sys.argv[1:]
if mode == "profile":
    stage, store, media, profile = map(Path, args)
    manifest = json.loads((stage / ".magpkg/manifest.json").read_text())
    (media / "packages").mkdir(exist_ok=True)
    (media / "profiles").mkdir(exist_ok=True)
    rows = []
    for package in manifest["packages"]:
        name = (package.get("name", "") + "-" if package.get("name") else "") + package["id"] + ".tar.zst"
        source = store / "packages" / name
        target = media / "packages" / name
        if not target.exists():
            shutil.copyfile(source, target)
        rows.append(f"{digest(target)}  packages/{name}\n")
    (media / "profiles" / (str(profile) + ".sha256")).write_text("".join(sorted(rows)))
elif mode == "source":
    repo, seed, media = map(Path, args)
    source = media / "source"
    source.mkdir()
    ignored = shutil.ignore_patterns(".git", "_work", "out", "__pycache__", "*.pyc")
    shutil.copytree(repo / "packages", source / "packages", ignore=ignored)
    shutil.copytree(repo / "magnet-linux", source / "magnet-linux", ignore=ignored)
    shutil.copyfile(repo / "dependencies.json", source / "dependencies.json")
    import subprocess
    if (repo / ".git").exists():
        revision = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip()
        (source / "BASE-COMMIT").write_text(revision + "\n")
    elif (repo / "RELEASE").exists():
        shutil.copyfile(repo / "RELEASE", source / "RELEASE")
    shutil.copyfile(seed, media / "bootstrap.tar.zst")
    paths = sorted(p for p in source.rglob("*") if p.is_file())
    paths.append(media / "bootstrap.tar.zst")
    (media / "source.sha256").write_text("".join(
        f"{digest(p)}  {p.relative_to(media)}\n" for p in paths))
else:
    raise SystemExit("expected profile or source")
