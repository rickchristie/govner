#!/usr/bin/env python3

import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest


REPO_ROOT = Path(__file__).resolve().parents[1]
CAPTURE_SCRIPT = REPO_ROOT / "scripts" / "capture-tui.sh"
PNG_DATA = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwC"
    "AAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="
)


class CaptureTUITest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory(
            prefix="capture-tui-test-",
            dir="/tmp",
        )
        self.test_root = Path(self.temporary_directory.name)
        self.fake_bin = self.test_root / "bin"
        self.fake_bin.mkdir()
        self.docker_log = self.test_root / "docker.jsonl"
        self.tape_copy = self.test_root / "capture.tape"
        self.application = self.test_root / "storybook-app"
        self.application.write_text("#!/bin/sh\nexit 0\n")
        self.application.chmod(0o755)
        self._write_fake_docker()

        self.environment = os.environ.copy()
        self.environment["PATH"] = f"{self.fake_bin}:{self.environment['PATH']}"
        self.environment["FAKE_DOCKER_LOG"] = str(self.docker_log)
        self.environment["FAKE_TAPE_COPY"] = str(self.tape_copy)
        self.environment["FAKE_XVFB_IMAGE_VERSION"] = self.xvfb_source_version()

    @staticmethod
    def xvfb_source_version() -> str:
        source_paths = (
            REPO_ROOT / "scripts" / "tui-capture" / "Dockerfile.xvfb",
            REPO_ROOT / "scripts" / "tui-capture" / "xvfb-entrypoint.sh",
        )
        return "-".join(
            hashlib.sha256(path.read_bytes()).hexdigest()
            for path in source_paths
        )

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write_fake_docker(self) -> None:
        docker = self.fake_bin / "docker"
        docker.write_text(
            textwrap.dedent(
                f"""\
                #!/usr/bin/env python3
                import base64
                import json
                import os
                from pathlib import Path
                import shutil
                import sys

                arguments = sys.argv[1:]
                with open(os.environ["FAKE_DOCKER_LOG"], "a") as log:
                    log.write(json.dumps(arguments) + "\\n")

                if arguments[:2] == ["image", "inspect"]:
                    if "--format" in arguments:
                        print(os.environ["FAKE_XVFB_IMAGE_VERSION"])
                    raise SystemExit(0)

                if arguments and arguments[0] in {{"pull", "build"}}:
                    raise SystemExit(0)

                if not arguments or arguments[0] != "run":
                    raise SystemExit(2)

                output_directory = None
                tape_path = None
                for index, argument in enumerate(arguments):
                    if argument != "--mount" or index + 1 >= len(arguments):
                        continue
                    values = dict(
                        field.split("=", 1)
                        for field in arguments[index + 1].split(",")
                        if "=" in field
                    )
                    if values.get("dst") == "/output":
                        output_directory = Path(values["src"])
                    if values.get("dst") == "/vhs/capture.tape":
                        tape_path = Path(values["src"])

                if output_directory is None:
                    raise SystemExit(3)
                output_directory.joinpath("capture.png").write_bytes(
                    base64.b64decode({base64.b64encode(PNG_DATA).decode()!r})
                )
                if tape_path is not None:
                    shutil.copyfile(tape_path, os.environ["FAKE_TAPE_COPY"])
                """
            )
        )
        docker.chmod(0o755)

    def run_capture(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(CAPTURE_SCRIPT), *arguments],
            cwd=REPO_ROOT,
            env=self.environment,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

    def docker_calls(self) -> list[list[str]]:
        return [
            json.loads(line)
            for line in self.docker_log.read_text().splitlines()
            if line
        ]

    def test_vhs_capture_writes_png_and_deterministic_tape(self) -> None:
        output = self.test_root / "vhs.png"
        result = self.run_capture(
            "--output",
            str(output),
            "--wait-regex",
            "GOWT",
            "--key",
            "enter",
            "--type",
            "needle",
            "--",
            str(self.application),
            "--storybook",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_bytes(), PNG_DATA)
        tape = self.tape_copy.read_text()
        self.assertIn('Type "exec /input/tui --storybook"', tape)
        self.assertIn("Wait+Screen@15s /GOWT/", tape)
        self.assertIn("Enter\nSleep 250ms", tape)
        self.assertIn('Type "needle"', tape)
        self.assertIn('Sleep 500ms\nScreenshot "/output/capture.png"', tape)
        self.assertIn('Screenshot "/output/capture.png"', tape)

        run_call = self.docker_calls()[-1]
        self.assertIn("none", run_call)
        self.assertTrue(any("charmbracelet/vhs@sha256:" in value for value in run_call))

    def test_xvfb_capture_passes_terminal_actions(self) -> None:
        output = self.test_root / "xvfb.png"
        result = self.run_capture(
            "--backend",
            "xvfb",
            "--output",
            str(output),
            "--key",
            "down",
            "--type",
            "query",
            "--",
            str(self.application),
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_bytes(), PNG_DATA)
        run_call = self.docker_calls()[-1]
        self.assertIn("govner-tui-capture-xvfb:1", run_call)
        self.assertIn("--key", run_call)
        self.assertIn("down", run_call)
        self.assertIn("--type", run_call)
        self.assertIn("query", run_call)

    def test_prepare_all_uses_both_pinned_backends(self) -> None:
        result = self.run_capture("prepare", "all")

        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.docker_calls()
        self.assertEqual(calls[0][0], "pull")
        self.assertIn("charmbracelet/vhs@sha256:", calls[0][1])
        self.assertEqual(calls[1][0], "build")
        self.assertIn("govner-tui-capture-xvfb:1", calls[1])
        self.assertTrue(
            any(
                value.startswith("org.govner.tui-capture.version=")
                for value in calls[1]
            )
        )

    def test_outdated_xvfb_image_is_rejected(self) -> None:
        self.environment["FAKE_XVFB_IMAGE_VERSION"] = "outdated"
        result = self.run_capture(
            "--backend",
            "xvfb",
            "--output",
            str(self.test_root / "outdated.png"),
            "--",
            str(self.application),
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Xvfb image is outdated", result.stderr)

    def test_output_must_be_under_tmp(self) -> None:
        result = self.run_capture(
            "--output",
            str(REPO_ROOT / "capture.png"),
            "--",
            str(self.application),
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be under /tmp", result.stderr)
        self.assertFalse(self.docker_log.exists())

    def test_existing_output_requires_force(self) -> None:
        output = self.test_root / "existing.png"
        output.write_bytes(PNG_DATA)

        result = self.run_capture(
            "--output",
            str(output),
            "--",
            str(self.application),
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Use --force", result.stderr)
        self.assertFalse(self.docker_log.exists())

    def test_dynamic_executable_is_rejected_before_docker(self) -> None:
        result = self.run_capture(
            "--output",
            str(self.test_root / "dynamic.png"),
            "--",
            "/bin/true",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("statically linked", result.stderr)
        self.assertFalse(self.docker_log.exists())


if __name__ == "__main__":
    unittest.main()
