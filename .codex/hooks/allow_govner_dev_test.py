#!/usr/bin/env python3
"""Unit tests for Govner's repo-local Codex PermissionRequest hook."""

from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import shutil
import shlex
import subprocess
import sys
import tempfile
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
        for project in allow_govner_dev.PROJECT_ROOTS:
            version = allow_govner_dev.project_version(project)
            if version is None:
                raise AssertionError(f"missing valid {project} version")
            placeholder = "{" + project.upper() + "_VERSION}"
            case["command"] = case["command"].replace(placeholder, version)
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
        deferred = next(case for case in cases if case["id"] == "deny-git-push-force")

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
            ("git push --force origin main", REPO_ROOT),
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
        scripts = (
            set(allow_govner_dev.COOPER_TEST_SCRIPTS)
            | set(allow_govner_dev.RELEASE_SCRIPTS)
            | allow_govner_dev.MANUALLY_REVIEWED_SCRIPTS
        )
        for script in scripts:
            with self.subTest(script=script):
                self.assertTrue(script.is_file(), f"missing reviewed script {script}")

    def test_manual_scripts_are_not_auto_approved(self) -> None:
        for script in allow_govner_dev.MANUALLY_REVIEWED_SCRIPTS:
            relative = script.relative_to(REPO_ROOT)
            for prefix in ("", "bash "):
                with self.subTest(script=relative, prefix=prefix):
                    self.assertFalse(
                        allow_govner_dev.is_allowed(
                            f"{prefix}./{relative} > /tmp/govner-manual-script.txt 2>&1",
                            cwd=REPO_ROOT,
                        )
                    )

    def test_every_repository_shell_script_has_an_explicit_policy(self) -> None:
        result = subprocess.run(
            [
                "git",
                "ls-files",
                "--cached",
                "--others",
                "--exclude-standard",
                "--",
                "*.sh",
            ],
            cwd=REPO_ROOT,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        repository_scripts = {
            (REPO_ROOT / value).resolve()
            for value in result.stdout.splitlines()
            if value
        }
        reviewed = (
            set(allow_govner_dev.COOPER_TEST_SCRIPTS)
            | set(allow_govner_dev.RELEASE_SCRIPTS)
            | allow_govner_dev.MANUALLY_REVIEWED_SCRIPTS
        )
        self.assertEqual(repository_scripts, reviewed)

    def test_docker_build_clean_mode_is_allowed_from_repo_root(self) -> None:
        command = (
            "./cooper/test-docker-build.sh clean "
            "> /tmp/cooper-test-docker-build-clean-1.txt 2>&1"
        )
        output = json.loads(run_hook(command, str(REPO_ROOT)))
        self.assertEqual(
            output["hookSpecificOutput"]["decision"]["behavior"],
            "allow",
        )

    def test_release_remote_ref_query_is_allowed_from_repo_root(self) -> None:
        version = allow_govner_dev.project_version("cooper")
        self.assertIsNotNone(version)
        command = (
            "git ls-remote origin refs/heads/main "
            f"refs/tags/cooper/v{version} "
            f"'refs/tags/cooper/v{version}^{{}}' "
            "> /tmp/cooper-release-remote-refs.txt 2>&1"
        )
        output = json.loads(run_hook(command, str(REPO_ROOT)))
        self.assertEqual(
            output["hookSpecificOutput"]["decision"]["behavior"],
            "allow",
        )

    def test_every_reviewed_test_script_mode_is_allowed(self) -> None:
        for script, argument_sets in allow_govner_dev.COOPER_TEST_SCRIPTS.items():
            relative = script.relative_to(REPO_ROOT)
            for arguments in argument_sets:
                suffix = "-".join(arguments) if arguments else "default"
                invocation = " ".join([f"./{relative}", *arguments])
                command = (
                    f"{invocation} "
                    f"> /tmp/govner-hook-{script.stem}-{suffix}.txt 2>&1"
                )
                with self.subTest(script=relative, arguments=arguments):
                    self.assertTrue(
                        allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
                    )

    def test_every_vscode_workflow_task_has_an_explicit_policy(self) -> None:
        tasks = json.loads((REPO_ROOT / ".vscode/tasks.json").read_text())["tasks"]
        by_label = {task["label"]: task for task in tasks}
        script_tasks = {
            "cooper: Docker Test (All)",
            "cooper: Docker Test (Mirror)",
            "cooper: Docker Test (Latest)",
            "cooper: Docker Test (Pinned)",
            "cooper: Docker Test (Clean)",
            "cooper: E2E Test",
            "cooper: E2E Test (Clean)",
            "cooper: VM Unit",
            "cooper: VM Prepare",
            "cooper: VM Smoke (Prepared Inputs)",
            "cooper: Antigravity VM Prepare",
            "cooper: Antigravity VM Parity",
            "cooper: Antigravity VM Profiles",
        }
        manual_tasks = {"cooper: VM Release Gate (Release Only)"}
        build_tasks = {"cooper: Build", "pgflock: Build"}
        interactive_tasks = {
            "cooper: TUI Test",
            "gowt: Test Current Package",
            "gowt: Test All cooper",
            "gowt: Test All gowt",
            "gowt: Test All pgflock",
        }
        self.assertEqual(
            set(by_label),
            script_tasks | build_tasks | interactive_tasks | manual_tasks,
        )

        for label in script_tasks:
            expanded = by_label[label]["command"].replace(
                "${workspaceFolder}",
                str(REPO_ROOT),
            )
            command = (
                expanded if " > " in expanded else f"{expanded} > /tmp/govner-hook-vscode-script.txt 2>&1"
            )
            with self.subTest(label=label):
                self.assertTrue(
                    allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
                )

        for label in manual_tasks:
            expanded = by_label[label]["command"].replace("${workspaceFolder}", str(REPO_ROOT))
            self.assertFalse(allow_govner_dev.is_allowed(expanded, cwd=REPO_ROOT))

        for label in build_tasks:
            expanded = by_label[label]["command"].replace(
                "${workspaceFolder}",
                str(REPO_ROOT),
            )
            build = expanded.split(" && ", 1)[0]
            command = (
                f"{build} > /tmp/govner-hook-vscode-build.txt 2>&1"
            )
            with self.subTest(label=label):
                self.assertTrue(
                    allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
                )

    def test_unlogged_release_preview_receives_canonical_guidance(self) -> None:
        output = json.loads(
            run_hook("./scripts/release-cooper.sh", str(REPO_ROOT))
        )
        decision = output["hookSpecificOutput"]["decision"]
        self.assertEqual(decision["behavior"], "deny")
        self.assertIn("the script did not run", decision["message"])
        self.assertIn(
            "./scripts/release-cooper.sh "
            "> /tmp/cooper-release-preview.txt 2>&1",
            decision["message"],
        )

    def test_release_tag_message_file_is_private_and_project_matched(self) -> None:
        created = subprocess.run(
            ["mktemp", "/tmp/cooper-release-tag-message.XXXXXX"],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        path = Path(created.stdout.strip())
        version = allow_govner_dev.project_version("cooper")
        self.assertIsNotNone(version)
        command = f"git tag -a cooper/v{version} -F {path}"
        try:
            path.write_text(
                f"cooper {version}\n\n"
                "Changes since initial:\n\n"
                "- safe fixture\n"
            )
            self.assertTrue(
                allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
            )

            path.chmod(0o644)
            self.assertFalse(
                allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
            )

            path.chmod(0o600)
            path.write_text("unrelated local contents\n")
            self.assertFalse(
                allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
            )

            path.unlink()
            path.symlink_to("/etc/passwd")
            self.assertFalse(
                allow_govner_dev.is_allowed(command, cwd=REPO_ROOT)
            )
        finally:
            path.unlink(missing_ok=True)

    def test_release_generators_print_hook_compatible_commands(self) -> None:
        # The real repository already has the current tags. Generate previews
        # in an isolated Git fixture so this test exercises every emitted
        # command, including changelog data that requires shell quoting.
        with tempfile.TemporaryDirectory(
            prefix="govner-release-hook-",
            dir="/tmp",
        ) as directory:
            fixture = Path(directory)

            def run(*args: str, check: bool = True) -> subprocess.CompletedProcess:
                return subprocess.run(
                    args,
                    cwd=fixture,
                    check=check,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                )

            for project in allow_govner_dev.PROJECT_ROOTS:
                script = REPO_ROOT / f"scripts/release-{project}.sh"
                destination = fixture / "scripts" / script.name
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(script, destination)

                meta = fixture / project / "meta"
                meta.mkdir(parents=True, exist_ok=True)
                shutil.copy2(
                    REPO_ROOT / project / "meta/version.go",
                    meta / "version.go",
                )

            git_commands = [
                ["git", "init", "-q", "-b", "main"],
                ["git", "config", "user.name", "Govner Hook Test"],
                [
                    "git",
                    "config",
                    "user.email",
                    "hook-test@example.invalid",
                ],
                ["git", "add", "."],
                [
                    "git",
                    "commit",
                    "-m",
                    'fixture subject with "quotes" and $(literal)',
                ],
            ]
            for command in git_commands:
                run(*command)

            # No preview may publish unfinished branch work or detached HEAD.
            for checkout in (("-b", "feature"), ("--detach",)):
                run("git", "checkout", *checkout)
                for project in allow_govner_dev.PROJECT_ROOTS:
                    with self.subTest(project=project, checkout=checkout):
                        result = run(
                            "bash", f"./scripts/release-{project}.sh", check=False,
                        )
                        self.assertNotEqual(result.returncode, 0)
                        self.assertIn("Run the release preview from main", result.stdout)
                        self.assertNotIn("git tag ", result.stdout)
                        self.assertNotIn("git push ", result.stdout)
                run("git", "checkout", "main")

            remote = fixture / "remote.git"
            run("git", "init", "--bare", "-q", str(remote))
            run("git", "remote", "add", "origin", str(remote))
            run("git", "push", "origin", "main")

            expected_counts = {"cooper": 7, "gowt": 3, "pgflock": 3}
            command_prefixes = (
                "mkdir -p ",
                "GOOS=",
                "git tag ",
                "git push ",
                "GOPROXY=",
            )
            for project, expected_count in expected_counts.items():
                result = run("bash", f"./scripts/release-{project}.sh")
                commands = [
                    line
                    for line in result.stdout.splitlines()
                    if line.startswith(command_prefixes)
                ]
                self.assertEqual(len(commands), expected_count)
                normalized_commands: list[str] = []
                for generated in commands:
                    # The policy intentionally binds release paths to the real
                    # checkout. Replace only the fixture prefix, preserving all
                    # generated quoting and arguments for validation.
                    command = generated.replace(str(fixture), str(REPO_ROOT))
                    normalized_commands.append(command)
                    with self.subTest(project=project, command=command):
                        self.assertTrue(
                            allow_govner_dev.is_allowed(
                                command,
                                cwd=REPO_ROOT,
                            )
                        )
                with self.subTest(project=project, command="combined release"):
                    self.assertTrue(
                        allow_govner_dev.is_allowed(
                            " && ".join(normalized_commands),
                            cwd=REPO_ROOT,
                        )
                    )

                tag_command = next(
                    command
                    for command in commands
                    if command.startswith("git tag ")
                )
                self.assertNotIn("$'", tag_command)
                tag_tokens = shlex.split(tag_command)
                run(*tag_tokens)
                tag = allow_govner_dev.project_release_tag(project)
                self.assertIsNotNone(tag)
                message = run(
                    "git", "for-each-ref", "--format=%(contents)", f"refs/tags/{tag}",
                ).stdout
                self.assertIn('fixture subject with "quotes" and $(literal)', message)
                Path(tag_tokens[-1]).unlink(missing_ok=True)

                push_command = next(
                    command for command in commands if command.startswith("git push ")
                )
                push_tokens = shlex.split(push_command)

                # A concurrent main update must reject the tag as well. Without
                # --atomic, Git can publish the tag even when main is rejected.
                remote_commit = run(
                    "git", "commit-tree", "HEAD^{tree}", "-p", "HEAD",
                    "-m", f"Concurrent change before {project} release",
                ).stdout.strip()
                run("git", "push", "origin", f"{remote_commit}:refs/heads/main")
                with self.subTest(project=project, push="rejected main"):
                    rejected = run(*push_tokens, check=False)
                    self.assertNotEqual(rejected.returncode, 0)
                    self.assertEqual(
                        run("git", "-C", str(remote), "rev-parse", "main").stdout.strip(),
                        remote_commit,
                    )
                    self.assertEqual(
                        run(
                            "git", "-C", str(remote), "show-ref", "--verify", "--quiet",
                            f"refs/tags/{tag}", check=False,
                        ).returncode,
                        1,
                    )

                run("git", "merge", "--ff-only", remote_commit)
                run("git", "commit", "--allow-empty", "-m", f"Complete {project} release")
                with self.subTest(project=project, push="main and tag"):
                    run(*push_tokens)
                    self.assertEqual(
                        run("git", "-C", str(remote), "rev-parse", "main").stdout,
                        run("git", "rev-parse", "main").stdout,
                    )
                    run(
                        "git", "-C", str(remote), "merge-base", "--is-ancestor",
                        f"refs/tags/{tag}^{{commit}}", "main",
                    )


if __name__ == "__main__":
    unittest.main()
