#!/usr/bin/env python3
"""Unit tests for Govner's repo-local Codex PermissionRequest hook."""

from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import sys
import unittest

import allow_govner_dev


REPO_ROOT = Path(__file__).resolve().parents[2]
CASES_PATH = Path(__file__).with_name("allow_govner_dev_cases.json")
CONFIG_PATH = Path(__file__).resolve().parents[1] / "config.toml"


def load_cases() -> list[dict]:
    cases = json.loads(CASES_PATH.read_text())
    if not cases:
        raise AssertionError(f"{CASES_PATH} is empty")

    seen: set[str] = set()
    for case in cases:
        case["command"] = case["command"].replace("{REPO_ROOT}", str(REPO_ROOT))
        case["cwd"] = case.get("cwd", "{REPO_ROOT}").replace("{REPO_ROOT}", str(REPO_ROOT))
        if not case.get("id") or case["id"] in seen:
            raise AssertionError(f"missing or duplicate case id: {case.get('id')!r}")
        if not case.get("why"):
            raise AssertionError(f"case {case['id']} has no rationale")
        seen.add(case["id"])
    return cases


def run_hook(command: str, cwd: str) -> str:
    payload = json.dumps(
        {
            "cwd": cwd,
            "hook_event_name": "PermissionRequest",
            "tool_input": {"command": command},
        }
    )
    stdin = io.StringIO(payload)
    stdout = io.StringIO()
    old_stdin = sys.stdin
    with contextlib.redirect_stdout(stdout):
        try:
            sys.stdin = stdin
            allow_govner_dev.main()
        finally:
            sys.stdin = old_stdin
    return stdout.getvalue().strip()


class AllowGovnerDevTest(unittest.TestCase):
    def test_corpus_cases(self) -> None:
        for case in load_cases():
            with self.subTest(case=case["id"]):
                self.assertIs(
                    allow_govner_dev.is_allowed(case["command"], cwd=case["cwd"]),
                    case["expected"],
                    case["why"],
                )

    def test_config_enables_the_repo_hook(self) -> None:
        config = CONFIG_PATH.read_text()
        self.assertIn("[features]\nhooks = true", config)
        self.assertEqual(config.count("[[hooks.PermissionRequest]]"), 1)
        self.assertEqual(config.count("[[hooks.PermissionRequest.hooks]]"), 1)
        self.assertIn('matcher = "^Bash$"', config)
        self.assertIn("allow_govner_dev.py", config)

    def test_main_allows_and_defers_with_codex_payload_shape(self) -> None:
        cases = load_cases()
        allowed = next(case for case in cases if case["expected"])
        deferred = next(case for case in cases if case["id"] == "deny-git-push")

        allowed_output = run_hook(allowed["command"], allowed["cwd"])
        parsed = json.loads(allowed_output)
        self.assertEqual(
            parsed["hookSpecificOutput"]["decision"]["behavior"],
            "allow",
        )
        self.assertEqual(
            parsed["hookSpecificOutput"]["hookEventName"],
            "PermissionRequest",
        )
        self.assertEqual(run_hook(deferred["command"], deferred["cwd"]), "")

    def test_payload_cwd_guides_an_ambiguous_cooper_path(self) -> None:
        ambiguous = "timeout 90m ./test-e2e.sh > /tmp/cooper-e2e.txt 2>&1"
        qualified = (
            "timeout 90m ./cooper/test-e2e.sh "
            "> /tmp/cooper-e2e.txt 2>&1"
        )

        # Codex currently reports the session cwd but omits a shell tool's
        # per-call workdir. Never guess that invisible directory; deny this
        # spelling and tell the agent how to make Cooper visible instead.
        rejected = json.loads(run_hook(ambiguous, str(REPO_ROOT)))
        decision = rejected["hookSpecificOutput"]["decision"]
        self.assertEqual(decision["behavior"], "deny")
        self.assertIn(
            "timeout 90m ./cooper/test-e2e.sh "
            "> /tmp/cooper-e2e.txt 2>&1",
            decision["message"],
        )
        output = json.loads(run_hook(qualified, str(REPO_ROOT)))
        self.assertEqual(
            output["hookSpecificOutput"]["decision"]["behavior"],
            "allow",
        )

    def test_noncanonical_test_formats_receive_safe_retry_guidance(self) -> None:
        cases = [
            (
                "timeout 90m ./test-e2e.sh",
                REPO_ROOT / "cooper",
                "timeout 90m ./cooper/test-e2e.sh "
                "> /tmp/cooper-e2e.txt 2>&1",
            ),
            (
                "timeout 90m ./test-e2e.sh > /tmp/partial.txt",
                REPO_ROOT / "cooper",
                "timeout 90m ./cooper/test-e2e.sh "
                "> /tmp/cooper-e2e.txt 2>&1",
            ),
            (
                "./test-e2e.sh > ./e2e.log 2>&1",
                REPO_ROOT / "cooper",
                "timeout 90m ./cooper/test-e2e.sh "
                "> /tmp/cooper-e2e.txt 2>&1",
            ),
            (
                "go test ./internal/app -run TestDoesNotExist -count=1",
                REPO_ROOT / "cooper",
                "go test -C ./cooper ./internal/app -run "
                "TestDoesNotExist -count=1 > /tmp/cooper-go-test.txt 2>&1",
            ),
            (
                "go test -C ./cooper ./internal/app -count=1 2>&1 "
                "| tee /tmp/cooper-live.txt",
                REPO_ROOT,
                "go test -C ./cooper ./internal/app -count=1 "
                "> /tmp/cooper-go-test.txt 2>&1",
            ),
            (
                "cd ./cooper && go test ./internal/bridge ./internal/auth",
                REPO_ROOT,
                "go test -C ./cooper ./internal/bridge ./internal/auth "
                "> /tmp/cooper-go-test.txt 2>&1",
            ),
            (
                "bash -lc 'set -o pipefail; ./test-e2e.sh "
                "| tee /tmp/cooper-live.txt; "
                "printf \"%s\\n\" $? > /tmp/cooper-live.exit'",
                REPO_ROOT / "cooper",
                "timeout 90m ./cooper/test-e2e.sh "
                "> /tmp/cooper-e2e.txt 2>&1",
            ),
        ]

        for command, cwd, canonical in cases:
            with self.subTest(command=command):
                parsed = json.loads(run_hook(command, str(cwd)))
                decision = parsed["hookSpecificOutput"]["decision"]
                self.assertEqual(decision["behavior"], "deny")
                self.assertIn("the test did not run", decision["message"])
                self.assertIn("Do not ask the user", decision["message"])
                self.assertIn(canonical, decision["message"])

    def test_semantic_or_high_risk_rejections_still_defer(self) -> None:
        cases = [
            (
                "timeout 91m ./test-e2e.sh "
                "> /tmp/cooper-e2e.txt 2>&1",
                REPO_ROOT / "cooper",
            ),
            (
                "./test-e2e.sh production > /tmp/cooper-e2e.txt 2>&1",
                REPO_ROOT / "cooper",
            ),
            (
                "go test -exec=/tmp/custom-runner ./... "
                "> /tmp/cooper-go-test.txt 2>&1",
                REPO_ROOT / "cooper",
            ),
            (
                "go test github.com/example/project",
                REPO_ROOT / "cooper",
            ),
            (
                "go test ./... < /etc/passwd",
                REPO_ROOT / "cooper",
            ),
            ("git push origin main", REPO_ROOT),
        ]

        for command, cwd in cases:
            with self.subTest(command=command):
                self.assertEqual(run_hook(command, str(cwd)), "")

    def test_path_safety_is_scoped_to_repo_and_tmp(self) -> None:
        root = allow_govner_dev.REPO_ROOT
        safe = [
            "AGENTS.md",
            "./cooper/internal/app",
            str(root / ".codex/hooks/allow_govner_dev.py"),
            "/tmp/cooper-go-test.txt",
        ]
        unsafe = [
            "../mark/AGENTS.md",
            "/etc/passwd",
            "~/.ssh/id_rsa",
            ".env",
            "/tmp/.env",
        ]
        for value in safe:
            with self.subTest(value=value):
                self.assertIsNotNone(
                    allow_govner_dev.resolve_safe_path(value, root),
                )
        for value in unsafe:
            with self.subTest(value=value):
                self.assertIsNone(
                    allow_govner_dev.resolve_safe_path(value, root),
                )

    def test_allowlisted_scripts_exist(self) -> None:
        for script in allow_govner_dev.COOPER_TEST_SCRIPTS:
            with self.subTest(script=script):
                self.assertTrue(script.is_file(), f"missing allowlisted script {script}")


if __name__ == "__main__":
    unittest.main()
