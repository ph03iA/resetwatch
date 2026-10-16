"""Build unsigned platform archives using Go and Python's standard library."""
import concurrent.futures
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
VERSION = "0.1.0"
DEST = ROOT / "site" / "dist" / "downloads"
STAGING = ROOT / "work" / "release"
TARGETS = (("windows", "amd64"), ("linux", "amd64"), ("darwin", "arm64"))
DOCUMENTS = ("README.md", "LICENSE", "SECURITY.md", ".env.example")


def build(target):
    system, arch = target
    folder = STAGING / f"{system}_{arch}"
    folder.mkdir(parents=True, exist_ok=True)
    binary = folder / ("resetwatch.exe" if system == "windows" else "resetwatch")
    environment = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
    subprocess.run(["go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/resetwatch"], cwd=ROOT, env=environment, check=True)
    for name in DOCUMENTS:
        shutil.copyfile(ROOT / name, folder / name)
    return archive_target(system, arch, folder)


def archive_target(system, arch, folder):
    name = f"resetwatch_{VERSION}_{system}_{arch}"
    files = platform_files(folder, system)
    if system == "windows":
        with zipfile.ZipFile(DEST / f"{name}.zip", "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            for file in files:
                archive.write(file, file.name)
    else:
        with tarfile.open(DEST / f"{name}.tar.gz", "w:gz") as archive:
            for file in files:
                archive.add(file, arcname=file.name, filter=archive_permissions)
    return name


def platform_files(folder, system):
    binary = "resetwatch.exe" if system == "windows" else "resetwatch"
    return sorted(folder / name for name in (*DOCUMENTS, binary))


def archive_permissions(member):
    member.mode = 0o755 if member.name == "resetwatch" else 0o644
    return member


def source_files():
    for name in ("AGENTS.md", "go.mod", "go.sum", "README.md", "SECURITY.md", "LICENSE", "CHANGELOG.md", ".gitignore", ".env.example"):
        yield ROOT / name
    for directory in ("cmd", "internal", "tools", "docs", ".github"):
        yield from (ROOT / directory).rglob("*")
    for name in ("index.html", "styles.css", "app.js", "favicon.svg"):
        yield ROOT / "site" / "dist" / name
    yield from (ROOT / "site" / "dist" / "fonts").rglob("*")


def package_source():
    with zipfile.ZipFile(DEST / "resetwatch-source.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for file in source_files():
            if include_source(file):
                archive.write(file, "resetwatch/" + file.relative_to(ROOT).as_posix())


def include_source(file):
    name = file.name.casefold()
    if name == ".env.example":
        return file.is_file()
    if name.startswith(".env") or file.suffix.casefold() == ".env":
        return False
    return file.is_file()


def checksums():
    lines = []
    for file in sorted(DEST.iterdir()):
        if file.suffix in (".zip", ".gz"):
            digest = hashlib.sha256(file.read_bytes()).hexdigest()
            lines.append(f"{digest}  {file.name}\n")
    (DEST / "SHA256SUMS.txt").write_text("".join(lines), encoding="utf-8")


def main():
    DEST.mkdir(parents=True, exist_ok=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        for name in pool.map(build, TARGETS):
            print(f"Built {name}", flush=True)
    package_source()
    checksums()
    print(f"Archives and checksums: {DEST}")


if __name__ == "__main__":
    main()
