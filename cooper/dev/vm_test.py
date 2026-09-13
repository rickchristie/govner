#!/usr/bin/env python3
"""Finite VM development commands. No user-supplied shell or test expression."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
AGENTS = {"claude", "copilot", "codex", "opencode", "grok"}
LIFECYCLES = {"restart", "resources", "relay", "agent"}
PACKAGES = ["vm", "vmhost", "vmguest", "vmproto", "vmrelay", "vmstate", "vmcontext",
            "vmpayload", "workload", "runtimefs", "launch", "auth", "config",
            "templates", "clipboard", "usercontext", "vmdev", "profiles",
            "profileauth", "profilemanager", "statelock"]

DOCKER_GUARD = '''#!/bin/sh
case "$1:$2" in
    build:*|buildx:*|save:*|load:*|pull:*|image:build|image:save|image:load|image:pull)
        printf 'Run prepare first; forbidden runtime image command: %s %s\\n' "$1" "$2" >&2
        exit 97;;
esac
exec "$COOPER_VM_DEV_DOCKER" "$@"
'''


def parse_args(args):
    if not args:
        return "unit", ""
    if len(args) == 1 and args[0] in {"unit", "prepare", "smoke", "mounts", "clean", "clean-cache"}:
        return args[0], ""
    if len(args) == 2 and args[0] in {"prepare-agent", "parity"} and args[1] in AGENTS:
        return tuple(args)
    if args == ["profiles", "codex"]:
        return tuple(args)
    if len(args) == 2 and args[0] == "lifecycle" and args[1] in LIFECYCLES:
        return tuple(args)
    raise ValueError("usage: cooper/test-vm-dev.sh [unit|prepare|smoke|mounts|lifecycle restart|resources|relay|agent|prepare-agent AGENT|parity AGENT|profiles codex|clean|clean-cache]")


def source_file(relative):
    if relative.endswith((".md", ".pyc")):
        return False
    return relative.startswith(("internal/", "cmd/", "meta/", "dev/")) or relative.endswith(".go") or relative in {"go.mod", "go.sum", "test-vm-dev.sh", "test-vm.sh"}


def source_digest(root):
    digest = hashlib.sha256()
    def visit(directory):
        for path in sorted(directory.iterdir()):
            if path.is_dir() and not path.is_symlink():
                if not path.name.startswith(".") and path.name != "__pycache__":
                    visit(path)
                continue
            relative = path.relative_to(root).as_posix()
            if not source_file(relative):
                continue
            if path.is_symlink() or not path.is_file():
                raise ValueError(f"source must be a regular file: {path}")
            digest.update(f"{relative}\0{path.stat().st_mode & 0o777}\0".encode())
            with path.open("rb") as stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    digest.update(chunk)
            digest.update(b"\0")
    visit(root)
    return digest.hexdigest()


def command(mode):
    base = ["go", "test", "-C", str(ROOT)]
    if mode == "unit":
        return base + [f"./internal/{name}" for name in PACKAGES] + ["-count=1", "-timeout=2m"]
    return base + ["./internal/vme2e", "-run", "^TestVMDevelopment$", "-count=1", "-v", "-timeout=45m"]


def write_report(path, report):
    temporary = path.with_suffix(".part")
    temporary.write_text(json.dumps(report, indent=2) + "\n")
    temporary.replace(path)


def main(args):
    try:
        mode, selection = parse_args(args)
    except ValueError as error:
        print(error, file=sys.stderr)
        return 2
    run_id = uuid.uuid4().hex[:12]
    report_path = Path(tempfile.gettempdir()) / f"cooper-vm-dev-{mode}-{run_id}.json"
    runtime_path = report_path.with_name(report_path.stem + "-runtime.json")
    log_path = report_path.with_suffix(".log")
    started = time.monotonic()
    report = {"schema": 1, "mode": mode, "selection": selection, "run_id": run_id,
              "log": str(log_path), "physical_ssd_writes": {"status": "unavailable",
              "reason": "No physical-host device window was measured. Archive and qcow2 bytes are logical work, not SSD or NAND writes."}}
    env = os.environ.copy()
    for name in ("COOPER_RUN_VM_E2E", "COOPER_NESTED_HARNESS"):
        env.pop(name, None)
    env.update(COOPER_VM_DEV_MODE=mode, COOPER_VM_DEV_SELECTION=selection,
               COOPER_VM_DEV_RUN=run_id, COOPER_VM_DEV_REPORT=str(runtime_path),
               COOPER_VM_DEV_SOURCE=source_digest(ROOT))
    report["source_digest"] = env["COOPER_VM_DEV_SOURCE"]
    # A unit run never downloads a missing toolchain or module. Prepare the Go
    # development environment once if these offline checks report a missing input.
    if mode == "unit":
        env.update(GOPROXY="off", HTTP_PROXY="http://127.0.0.1:9",
                   HTTPS_PROXY="http://127.0.0.1:9", http_proxy="http://127.0.0.1:9",
                   https_proxy="http://127.0.0.1:9", NO_PROXY="localhost,127.0.0.1", no_proxy="localhost,127.0.0.1")
    status = 1
    process = None
    def stop(signum, _frame):
        # Forward to the complete process group, including the Go test binary.
        if process is not None:
            os.killpg(process.pid, signum)
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, stop)
    try:
        with tempfile.TemporaryDirectory(prefix="cooper-vm-unit-tools-") as sentinels:
            if mode == "unit":
                for name in ("docker", "qemu-system-x86_64", "qemu-system-aarch64", "qemu-img", "virtiofsd", "cooper"):
                    path = Path(sentinels) / name
                    path.write_text('#!/bin/sh\nprintf "VM unit command invoked forbidden tool: %s\\n" "$0" >&2\nexit 97\n')
                    path.chmod(0o755)
                env["PATH"] = sentinels + os.pathsep + env.get("PATH", "")
                python_check = subprocess.run([sys.executable, "-B", "-m", "unittest", "discover", "-s", str(ROOT / "dev"), "-p", "vm_test_test.py"], env=env)
                if python_check.returncode:
                    status = python_check.returncode
                    return python_check.returncode
            elif mode in {"smoke", "mounts", "lifecycle", "parity"}:
                # A cache miss must be an error. Guard all host Docker calls,
                # including calls outside vm.Manager, against hidden image work.
                real_docker = shutil.which("docker")
                if real_docker is None:
                    raise FileNotFoundError("Docker is required for VM development profiles")
                env["COOPER_VM_DEV_DOCKER"] = real_docker
                guard = Path(sentinels) / "docker"
                guard.write_text(DOCKER_GUARD)
                guard.chmod(0o755)
                env["PATH"] = sentinels + os.pathsep + env.get("PATH", "")
            with log_path.open("wb") as log:
                process = subprocess.Popen(command(mode), cwd=ROOT.parent, env=env, stdout=subprocess.PIPE,
                                           stderr=subprocess.STDOUT, start_new_session=True)
                for line in process.stdout:
                    log.write(line)
                    log.flush()
                    sys.stdout.buffer.write(line)
                    sys.stdout.buffer.flush()
                status = process.wait()
    except OSError as error:
        print(error, file=sys.stderr)
    finally:
        report.update(elapsed_seconds=round(time.monotonic() - started, 3), exit_code=status)
        if runtime_path.exists():
            try:
                report["runtime"] = json.loads(runtime_path.read_text())
            except (OSError, ValueError) as error:
                report["runtime_report_error"] = str(error)
        if mode == "unit":
            report.update(vm_starts=0, image_exports=0, image_loads=0)
        try:
            write_report(report_path, report)
            print(f"VM development report: {report_path}")
        except (OSError, ValueError) as error:
            print(f"Cannot write optional report: {error}", file=sys.stderr)
    return status if status >= 0 else 128 - status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
