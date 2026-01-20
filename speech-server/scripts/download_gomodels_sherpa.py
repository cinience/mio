#!/usr/bin/env python3
"""Download the gomodels/sherpa bundle from ModelScope directly into models/."""

from __future__ import annotations

import argparse
import importlib
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


DEFAULT_MODEL_ID = "gomodels/sherpa"
DEFAULT_REVISION = None


def ensure_modelscope() -> None:
    """Ensure the ModelScope package is available, installing it if needed."""

    try:
        import modelscope  # noqa: F401
        return
    except ImportError:
        print("[INFO] Installing ModelScope client (pip install --user modelscope>=1.15.0)...")
        subprocess.check_call(
            [
                sys.executable,
                "-m",
                "pip",
                "install",
                "--user",
                "--upgrade",
                "modelscope>=1.15.0",
            ]
        )
        importlib.invalidate_caches()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Download the gomodels/sherpa ModelScope snapshot into the speech-server"
            " models directory."
        )
    )
    parser.add_argument(
        "dest",
        nargs="?",
        help="Destination directory for the downloaded models (default: <repo>/speech-server/models)",
    )
    parser.add_argument(
        "--model-id",
        default=os.environ.get("MODEL_ID", DEFAULT_MODEL_ID),
        help="ModelScope model identifier to download",
    )
    parser.add_argument(
        "--revision",
        default=os.environ.get("MODEL_REVISION", DEFAULT_REVISION),
        help="Optional model revision/tag to download",
    )
    parser.add_argument(
        "--skip-existing",
        action="store_true",
        help="Do not overwrite existing files/directories with the same name",
    )
    return parser.parse_args()


def resolve_destination(arg_dest: str | None) -> Path:
    if arg_dest:
        return Path(arg_dest).expanduser().resolve()

    script_dir = Path(__file__).resolve().parent
    server_root = script_dir.parent
    return (server_root / "models").resolve()


def download_models(model_id: str, revision: str | None, dest: Path, skip_existing: bool) -> None:
    ensure_modelscope()
    snapshot_download = importlib.import_module(
        "modelscope.hub.snapshot_download"
    ).snapshot_download

    dest.mkdir(parents=True, exist_ok=True)

    print(f"[INFO] Preparing to download ModelScope model '{model_id}' (revision: {revision or 'default'})")
    print(f"[INFO] Target directory: {dest}")

    with tempfile.TemporaryDirectory(prefix="modelscope-download-") as tmpdir:
        kwargs = {"model_id": model_id, "local_dir": tmpdir}
        if revision:
            kwargs["revision"] = revision

        print("[INFO] Downloading snapshot...")
        snapshot_path = Path(snapshot_download(**kwargs)).resolve()

        for item in snapshot_path.iterdir():
            target = dest / item.name
            if target.exists():
                if skip_existing:
                    print(f"[SKIP] Existing path preserved: {target}")
                    continue

                if target.is_dir():
                    shutil.rmtree(target)
                else:
                    target.unlink()

            if item.is_dir():
                shutil.move(str(item), target)
            else:
                shutil.move(str(item), target)

    print("[OK] Download complete.")
    print("Model files are available under", dest)


def main() -> int:
    args = parse_args()
    dest = resolve_destination(args.dest)

    try:
        download_models(args.model_id, args.revision, dest, args.skip_existing)
    except subprocess.CalledProcessError as exc:
        print(f"ERROR: Failed to install dependencies: {exc}", file=sys.stderr)
        return exc.returncode if exc.returncode else 1
    except Exception as exc:  # noqa: BLE001
        print(f"ERROR: Download failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
