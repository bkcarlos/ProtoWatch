"""Build and package a CGO-free ProtoWatch binary. Build the frontend first."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=TARGETS)
    parser.add_argument("--version", default="0.2.2-mvp")
    parser.add_argument("--commit", default="development")
    args = parser.parse_args()
    for value in (args.version, args.commit):
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}", value):
            parser.error("version and commit must be short letters/digits/dot/plus/underscore/hyphen identifiers")
    goos, goarch = args.target.split("-")
    name = f"proto-watch_{args.version}_{args.target}"
    output = ROOT / "dist"
    folder = output / name
    folder.mkdir(parents=True, exist_ok=True)
    executable = folder / ("proto-watch.exe" if goos == "windows" else "proto-watch")
    module = "github.com/bkcarlos/ProtoWatch/internal/buildinfo"
    flags = f"-s -w -X {module}.Version={args.version} -X {module}.Commit={args.commit}"
    env = {**os.environ, "CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch}
    subprocess.run(["go", "build", "-trimpath", "-ldflags", flags, "-o", str(executable), "./cmd/proto-watch"], cwd=ROOT, env=env, check=True)
    for path in ("README.md", "LICENSE"):
        shutil.copyfile(ROOT / path, folder / path)
    shutil.copytree(ROOT / "docs", folder / "docs", dirs_exist_ok=True)
    for path in ("licenses.txt", "frontend-licenses.txt", "vue-license.txt"):
        shutil.copyfile(ROOT / "internal/server/web" / path, folder / path)
    (folder / "BUILD.txt").write_text(
        f"ProtoWatch {args.version}\nTarget: {args.target}\nCommit: {args.commit}\n"
        "Source: https://github.com/bkcarlos/ProtoWatch\n"
        "Live capture: Linux only; offline PCAP and web UI: all build targets.\n",
        encoding="utf-8",
    )
    if goos == "windows":
        archive = output / (name + ".zip")
        with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as package:
            for path in sorted(folder.rglob("*")):
                if path.is_file():
                    package.write(path, path.relative_to(output))
    else:
        archive = output / (name + ".tar.gz")
        with tarfile.open(archive, "w:gz") as package:
            package.add(folder, arcname=name)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + ".sha256").write_text(f"{digest}  {archive.name}\n", encoding="ascii")
    print(f"Binary: {executable}\nArchive: {archive}", flush=True)


if __name__ == "__main__":
    main()
