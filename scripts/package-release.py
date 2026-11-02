#!/usr/bin/env python3
"""Package cross-compiled Oberon binaries for GitHub Releases."""

from __future__ import annotations

import argparse
import re
import shutil
import tarfile
import tempfile
import zipfile
from pathlib import Path


TARGETS = (
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("linux", "arm"),
    ("linux", "386"),
    ("linux", "riscv64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
    ("windows", "386"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("freebsd", "amd64"),
    ("freebsd", "arm64"),
    ("openbsd", "amd64"),
    ("openbsd", "arm64"),
    ("openbsd", "386"),
)

DOCUMENTS = ("LICENSE", "README.md", "README.fa.md", "UPSTREAM_README.md")


def package_target(
    project_root: Path,
    binaries_dir: Path,
    output_dir: Path,
    tag: str,
    os_name: str,
    architecture: str,
) -> Path:
    windows = os_name == "windows"
    executable_name = "oberon.exe" if windows else "oberon"
    source_name = f"oberon-{os_name}-{architecture}{'.exe' if windows else ''}"
    binary = binaries_dir / source_name
    if not binary.is_file():
        raise FileNotFoundError(f"Missing build output: {binary}")

    output_dir.mkdir(parents=True, exist_ok=True)
    suffix = ".zip" if windows else ".tar.gz"
    archive_path = output_dir / f"oberon-{tag}-{os_name}-{architecture}{suffix}"

    with tempfile.TemporaryDirectory(prefix="oberon-release-") as temp_dir:
        staging = Path(temp_dir)
        shutil.copy2(binary, staging / executable_name)
        for document in DOCUMENTS:
            shutil.copy2(project_root / document, staging / document)

        if windows:
            wintun_script = project_root / "scripts" / "setup-wintun.ps1"
            target_script = staging / "scripts" / "setup-wintun.ps1"
            target_script.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(wintun_script, target_script)

        if windows:
            with zipfile.ZipFile(
                archive_path,
                mode="w",
                compression=zipfile.ZIP_DEFLATED,
                compresslevel=9,
            ) as archive:
                for path in sorted(staging.rglob("*")):
                    if path.is_file():
                        archive.write(path, path.relative_to(staging).as_posix())
        else:
            with tarfile.open(archive_path, mode="w:gz") as archive:
                for path in sorted(staging.rglob("*")):
                    archive.add(path, arcname=path.relative_to(staging).as_posix(), recursive=False)

    return archive_path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True, help="Version tag, for example v0.4.4")
    parser.add_argument("--binaries", required=True, type=Path, help="build-matrix output directory")
    parser.add_argument("--output", required=True, type=Path, help="release asset output directory")
    args = parser.parse_args()

    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", args.tag):
        parser.error("--tag must use the vMAJOR.MINOR.PATCH format, such as v0.4.4")

    project_root = Path(__file__).resolve().parent.parent
    archives = [
        package_target(
            project_root,
            args.binaries,
            args.output,
            args.tag,
            os_name,
            architecture,
        )
        for os_name, architecture in TARGETS
    ]

    # Publish standalone Windows executables in addition to the archives so
    # users can download the binary directly when they already have Wintun.
    for os_name, architecture in TARGETS:
        if os_name != "windows":
            continue
        suffix = ".exe"
        binary = args.binaries / f"oberon-{os_name}-{architecture}{suffix}"
        executable = args.output / f"oberon-{args.tag}-{os_name}-{architecture}{suffix}"
        shutil.copy2(binary, executable)
        print(executable)

    for archive in archives:
        print(archive)


if __name__ == "__main__":
    main()
