import hashlib
import importlib.util
import io
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("prepare", Path(__file__).resolve().parents[1] / "prepare.py")
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class PrepareTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repo = self.root / "distro"
        self.repo.mkdir()
        self.environment = patch.dict(os.environ, {}, clear=True)
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def archive(self, prefix, files):
        path = self.root / (prefix + ".tar.gz")
        files = {"RELEASE": b"version=v0.1.0\nrevision=fixture\n", **files}
        with tarfile.open(path, "w:gz") as archive:
            for name, data in files.items():
                member = tarfile.TarInfo(prefix + "/" + name)
                member.size = len(data)
                member.mode = 0o755 if name.startswith("bin/") else 0o644
                archive.addfile(member, io.BytesIO(data))
        return {"version": "v0.1.0", "prefix": prefix, "url": path.as_uri(),
                "sha256": prepare.digest(path)}

    def lock(self):
        seed = self.root / "seed.tar.zst"
        seed.write_bytes(b"test bootstrap seed")
        return {
            "format": 1,
            "packages": self.archive("packages-v0.1.0", {"bootstrap.jsonnet": b"{}", "hello.jsonnet": b"original"}),
            "magpkg": self.archive("magpkg-v0.1.0-linux-amd64", {
                "bin/magpkg": b"#!/bin/sh\necho 'magpkg v0.1.0'\n",
                "share/licenses/fixture/LICENSE": b"fixture notice",
            }),
            "bootstrap": {"url": seed.as_uri(), "sha256": prepare.digest(seed)},
        }

    def test_release_inputs_and_local_edits_survive_repeated_preparation(self):
        lock = self.lock()
        prepare.prepare(self.repo, lock)
        recipe = self.repo / "packages/hello.jsonnet"
        recipe.write_text("local edit")
        prepare.prepare(self.repo, lock)
        self.assertEqual(recipe.read_text(), "local edit")
        work = self.repo / "magnet-linux/_work"
        self.assertEqual(prepare.digest(work / "bootstrap.tar.zst"), lock["bootstrap"]["sha256"])
        self.assertTrue(os.access(work / "magpkg", os.X_OK))
        self.assertTrue((work / "magpkg-licenses/fixture/LICENSE").is_file())

    def test_bad_checksum_does_not_install_a_tree(self):
        lock = self.lock()
        lock["packages"]["sha256"] = hashlib.sha256(b"wrong").hexdigest()
        with self.assertRaisesRegex(ValueError, "SHA-256 mismatch"):
            prepare.prepare(self.repo, lock)
        self.assertFalse((self.repo / "packages").exists())

    def test_new_version_preserves_existing_tree(self):
        lock = self.lock()
        prepare.prepare(self.repo, lock)
        lock["packages"]["version"] = "v0.2.0"
        with self.assertRaisesRegex(ValueError, "differs from the lock"):
            prepare.prepare(self.repo, lock)
        self.assertEqual((self.repo / "packages/hello.jsonnet").read_text(), "original")

    def test_explicit_development_inputs_need_no_remote(self):
        lock = self.lock()
        source = self.root / "development"
        source.mkdir()
        (source / "bootstrap.jsonnet").write_text("{}")
        (source / "out").mkdir()
        (source / "out/cache").write_text("not source")
        executable = self.root / "magpkg"
        executable.write_text("#!/bin/sh\necho 'magpkg dev'\n")
        executable.chmod(0o755)
        for dependency in lock.values():
            if isinstance(dependency, dict):
                dependency["url"] = "https://invalid.example/unused"
        with patch.dict(os.environ, {"PACKAGES_DIR": str(source), "MAGPKG": str(executable),
                                     "BOOTSTRAP_ARCHIVE": str(self.root / "seed.tar.zst")}):
            prepare.prepare(self.repo, lock)
        self.assertTrue((self.repo / "packages/.magnet-development-tree").is_file())
        self.assertFalse((self.repo / "packages/out").exists())


if __name__ == "__main__":
    unittest.main()
