"""Exercise release packaging with synthetic files, never local credentials."""
from pathlib import Path
import runpy
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile


class ReleaseArchiveTests(unittest.TestCase):
    def setUp(self):
        workspace = tempfile.TemporaryDirectory()
        self.addCleanup(workspace.cleanup)
        self.root = Path(workspace.name)
        self.dest = self.root / "downloads"
        self.dest.mkdir()
        self.release = runpy.run_path(str(Path(__file__).with_name("release.py")))
        globals_patch = patch.dict(self.release["archive_target"].__globals__, ROOT=self.root, DEST=self.dest)
        globals_patch.start()
        self.addCleanup(globals_patch.stop)
        self.documents = {
            "README.md": b"Synthetic release readme.\n",
            "LICENSE": b"Synthetic release license.\n",
            "SECURITY.md": b"Synthetic release security notes.\n",
            ".env.example": b"RESEND_API_KEY=\n",
        }
        self.local_files = {
            ".env": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
            ".env.local": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
            "secrets.env": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
            ".ENV": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
            ".Env.production": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
            "secrets.ENV": b"RESEND_API_KEY=SYNTHETIC_ONLY\n",
        }

    def test_platform_archives_only_include_release_files(self):
        for system, arch in (("windows", "amd64"), ("linux", "amd64"), ("darwin", "arm64")):
            with self.subTest(system=system):
                folder = self.root / system
                binary = "resetwatch.exe" if system == "windows" else "resetwatch"
                contents = dict(self.documents, **{binary: f"Synthetic {system}/{arch} binary.\n".encode()})
                for name, data in contents.items():
                    self.write_fixture(folder / name, data)
                for name, data in dict(self.local_files, **{"state.db": b"Synthetic local database."}).items():
                    self.write_fixture(folder / name, data)
                name = self.release["archive_target"](system, arch, folder)
                self.assertEqual(name, f"resetwatch_0.1.0_{system}_{arch}")
                suffix = ".zip" if system == "windows" else ".tar.gz"
                self.assert_platform_archive(self.dest / (name + suffix), contents)

    def test_source_archive_excludes_environment_files_at_every_depth(self):
        contents = dict(self.documents, **{
            "cmd/fixture.go": b"package main\n",
            "tools/fixture.py": b"# Synthetic source fixture.\n",
            "site/dist/fonts/fixture.woff2": b"Synthetic font fixture.",
        })
        for name, data in contents.items():
            self.write_fixture(self.root / name, data)
        for directory in ("", "cmd", "tools", "site/dist/fonts"):
            for index, (name, data) in enumerate(self.local_files.items()):
                # Separate folders keep case variants distinct on Windows.
                parent = self.root / directory / str(index) if directory else self.root
                file = parent / name
                self.write_fixture(file, data)
                self.assertFalse(self.release["include_source"](file))
        self.release["package_source"]()
        with zipfile.ZipFile(self.dest / "resetwatch-source.zip") as archive:
            self.assertEqual(set(archive.namelist()), {"resetwatch/" + name for name in contents})
            self.assertEqual(len(archive.namelist()), len(contents))
            for name, data in contents.items():
                self.assertEqual(archive.read("resetwatch/" + name), data)

    def assert_platform_archive(self, path, contents):
        if path.suffix == ".zip":
            with zipfile.ZipFile(path) as archive:
                self.assertEqual(archive.namelist(), sorted(contents, key=Path))
                for name, data in contents.items():
                    self.assertEqual(archive.read(name), data)
                    self.assertEqual(archive.getinfo(name).compress_type, zipfile.ZIP_DEFLATED)
        else:
            with tarfile.open(path, "r:gz") as archive:
                self.assertEqual(archive.getnames(), sorted(contents, key=Path))
                for member in archive.getmembers():
                    self.assertTrue(member.isfile())
                    self.assertEqual(member.mode, 0o755 if member.name == "resetwatch" else 0o644)
                    with archive.extractfile(member) as file:
                        self.assertEqual(file.read(), contents[member.name])

    def write_fixture(self, path, data):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)


if __name__ == "__main__":
    unittest.main()
