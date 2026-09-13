import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("vm_test", Path(__file__).with_name("vm_test.py"))
vm_test = importlib.util.module_from_spec(spec)
spec.loader.exec_module(vm_test)


class VMDevelopmentCommands(unittest.TestCase):
    def test_runtime_guard_blocks_image_work(self):
        with tempfile.TemporaryDirectory() as directory:
            guard = Path(directory) / "docker"
            guard.write_text(vm_test.DOCKER_GUARD)
            guard.chmod(0o755)
            env = dict(os.environ, COOPER_VM_DEV_DOCKER="/bin/echo")
            for command in (["build"], ["buildx", "build"], ["save"], ["load"], ["pull"],
                            ["image", "build"], ["image", "save"], ["image", "load"], ["image", "pull"]):
                result = subprocess.run([str(guard), *command], env=env, capture_output=True)
                self.assertEqual(result.returncode, 97, command)
            for command in (["info"], ["image", "inspect", "fixture"], ["exec", "fixture", "docker", "build"]):
                result = subprocess.run([str(guard), *command], env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, command)
                self.assertEqual(result.stdout.strip(), " ".join(command))

    def test_default_has_no_vm_or_docker_packages(self):
        self.assertEqual(vm_test.parse_args([]), ("unit", ""))
        command = vm_test.command("unit")
        self.assertNotIn("./...", command)
        self.assertNotIn("./internal/vme2e", command)
        self.assertNotIn("./internal/docker", command)

    def test_finite_commands(self):
        for mode in ("prepare-agent", "parity"):
            for agent in vm_test.AGENTS:
                self.assertEqual(vm_test.parse_args([mode, agent]), (mode, agent))
        for agent in ("codex", "antigravity"):
            self.assertEqual(vm_test.parse_args(["profiles", agent]), ("profiles", agent))
        for case in vm_test.LIFECYCLES:
            self.assertEqual(vm_test.parse_args(["lifecycle", case]), ("lifecycle", case))
        for args in (["all"], ["nested"], ["prepare", "extra"], ["parity", "custom"], ["profiles", "custom"],
                     ["smoke", "-run", ".*"], ["clean", "/home"], ["lifecycle", "all"]):
            with self.assertRaises(ValueError):
                vm_test.parse_args(args)

    def test_source_and_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "internal").mkdir()
            source = root / "internal" / "new.go"
            source.write_text("first")
            first = vm_test.source_digest(root)
            source.write_text("second")
            self.assertNotEqual(first, vm_test.source_digest(root))
            source.unlink()
            source.symlink_to("missing")
            with self.assertRaises(ValueError):
                vm_test.source_digest(root)
            report = root / "report.json"
            vm_test.write_report(report, {"exit_code": 7, "elapsed_seconds": 0.1})
            self.assertIn('"exit_code": 7', report.read_text())
            self.assertFalse(report.with_suffix(".part").exists())
