"""Verify the complete release asset set before publishing to GitHub."""
import argparse
import hashlib
from pathlib import Path
import re
import tarfile
import zipfile

from build import TARGETS


def verify(directory, version, commit):
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}", version):
        raise ValueError("invalid release version")
    expected = {"SHA256SUMS"}
    manifest = []
    for target in TARGETS:
        stem = f"proto-watch_{version}_{target}"
        name = stem + (".zip" if target.startswith("windows-") else ".tar.gz")
        expected.update((name, name + ".sha256"))
        archive = directory / name
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        line = f"{digest}  {name}"
        if (directory / (name + ".sha256")).read_text().strip() != line:
            raise ValueError(f"checksum mismatch: {name}")
        if name.endswith(".zip"):
            with zipfile.ZipFile(archive) as package:
                info = package.read(stem + "/BUILD.txt").decode()
        else:
            with tarfile.open(archive) as package:
                info = package.extractfile(stem + "/BUILD.txt").read().decode()
        for field in (f"ProtoWatch {version}", f"Target: {target}", f"Commit: {commit}"):
            if field not in info.splitlines():
                raise ValueError(f"build metadata mismatch: {name}: {field}")
        manifest.append(line)
    if any(p.name not in expected for p in directory.iterdir()):
        raise ValueError("unexpected release assets")
    (directory / "SHA256SUMS").write_text("\n".join(sorted(manifest)) + "\n", encoding="ascii")
    print(f"Verified {len(manifest)} packages for {version}, commit {commit}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("version")
    parser.add_argument("commit")
    args = parser.parse_args()
    verify(args.directory, args.version, args.commit)
