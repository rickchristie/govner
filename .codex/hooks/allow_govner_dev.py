#!/usr/bin/env python3
"""Auto-approve narrowly scoped Govner development commands.

The policy is intentionally syntactic. It recognizes the command shapes used
by Govner's build, test, release, and requested Git workflows, validates every
shell statement and path, and emits an allow decision only when the whole
request is understood. Recognized workflows with a formatting-only defect are
denied with a canonical retry command; unknown syntax falls back to Codex's
normal approval prompt.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
import re
import shlex
import sys


REPO_ROOT = Path(__file__).resolve().parents[2]
COOPER_ROOT = REPO_ROOT / "cooper"
TMP_ROOT = Path("/tmp")
MAX_TIMEOUT_SECONDS = 90 * 60
PROJECT_ROOTS = {
    "cooper": COOPER_ROOT,
    "gowt": REPO_ROOT / "gowt",
    "pgflock": REPO_ROOT / "pgflock",
}
PROJECT_MODULES = {
    project: f"github.com/rickchristie/govner/{project}"
    for project in PROJECT_ROOTS
}
PROJECT_DEV_BINARIES = {
    project: root / project
    for project, root in PROJECT_ROOTS.items()
}
SEMVER_RE = re.compile(
    r"^[0-9]+\.[0-9]+\.[0-9]+"
    r"(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?"
    r"(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$"
)

COOPER_TEST_SCRIPTS = {
    (COOPER_ROOT / "test-e2e.sh").resolve(): {(), ("clean",)},
    (COOPER_ROOT / "test-docker-build.sh").resolve(): {
        (),
        ("all",),
        ("clean",),
        ("latest",),
        ("mirror",),
        ("pinned",),
    },
}
RELEASE_SCRIPTS = {
    (REPO_ROOT / f"scripts/release-{project}.sh").resolve(): project
    for project in PROJECT_ROOTS
}
MANUALLY_REVIEWED_SCRIPTS = {
    (COOPER_ROOT / "internal/templates/doctor.sh").resolve(),
    (REPO_ROOT / "scripts/convert-agents.sh").resolve(),
}
COOPER_RELEASE_TARGETS = {
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("linux", "amd64"),
}
COOPER_TEST_DRIVER = (COOPER_ROOT / "cmd/cooper-test-driver").resolve()
TEST_DRIVER_SCENARIOS = {"barrel-env-smoke", "clipboard-smoke"}

GIT_ADD_BOOL_OPTIONS = {
    "--all",
    "--dry-run",
    "--ignore-errors",
    "--no-all",
    "--update",
    "--verbose",
    "-A",
    "-n",
    "-u",
    "-v",
}
GIT_COMMIT_BOOL_OPTIONS = {
    "--all",
    "--allow-empty",
    "--allow-empty-message",
    "--amend",
    "--dry-run",
    "--no-edit",
    "--no-post-rewrite",
    "--no-verify",
    "--quiet",
    "--reset-author",
    "--signoff",
    "--verbose",
    "-a",
    "-q",
    "-s",
    "-v",
}
GIT_FETCH_BOOL_OPTIONS = {
    "--dry-run",
    "--no-recurse-submodules",
    "--no-tags",
    "--prune",
    "--prune-tags",
    "--quiet",
    "--tags",
    "--verbose",
    "-p",
    "-q",
    "-t",
    "-v",
}
GIT_PUSH_BOOL_OPTIONS = {
    "--atomic",
    "--dry-run",
    "--follow-tags",
    "--no-verify",
    "--porcelain",
    "--quiet",
    "--set-upstream",
    "--verbose",
    "-n",
    "-q",
    "-u",
    "-v",
}
GIT_REF_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]*$")

READ_ONLY_COMMANDS = {
    "cat",
    "date",
    "file",
    "find",
    "grep",
    "head",
    "jq",
    "ls",
    "nl",
    "pwd",
    "rg",
    "sed",
    "sort",
    "stat",
    "tail",
    "uniq",
    "wc",
}
READ_ONLY_NEEDS_PATH = {
    "cat",
    "file",
    "grep",
    "head",
    "jq",
    "nl",
    "sed",
    "sort",
    "stat",
    "tail",
    "uniq",
    "wc",
}
FIND_UNSAFE_TOKENS = {
    "-delete",
    "-exec",
    "-execdir",
    "-fprint",
    "-fprint0",
    "-ok",
    "-okdir",
}
FIND_PATTERN_OPTIONS = {"-iname", "-ipath", "-iregex", "-name", "-path", "-regex"}
TAIL_UNSAFE_TOKENS = {"--follow", "-f"}
DATE_UNSAFE_TOKENS = {"--set", "-s"}
SORT_UNSAFE_TOKENS = {"--output", "-o"}
RG_UNSAFE_OPTIONS = {"--pre", "--pre-glob"}

SENSITIVE_PATH_SUBSTRINGS = {
    ".env",
    ".netrc",
    ".npmrc",
    ".pgpass",
    ".pypirc",
    "db_credentials",
}
SENSITIVE_PATH_NAMES = {
    ".aws",
    ".gnupg",
    ".ssh",
    "credentials.json",
    "id_dsa",
    "id_ecdsa",
    "id_ed25519",
    "id_rsa",
    "service-account.json",
    "token.json",
}
SHELL_EXPANSION_CHARS = {"*", "?", "[", "]", "{", "}"}

ALLOWED_BOOL_ENVS = {"CGO_ENABLED", "CI", "FORCE_COLOR", "NO_COLOR"}
ALLOWED_GO_OFF_ENVS = {"GOPROXY", "GOSUMDB"}
GO_VALUE_OPTIONS = {
    "-asmflags",
    "-bench",
    "-benchtime",
    "-count",
    "-covermode",
    "-coverpkg",
    "-cpu",
    "-gcflags",
    "-ldflags",
    "-p",
    "-parallel",
    "-run",
    "-shuffle",
    "-skip",
    "-tags",
    "-timeout",
    "-vet",
}
GO_OUTPUT_OPTIONS = {
    "-blockprofile",
    "-coverprofile",
    "-cpuprofile",
    "-memprofile",
    "-mutexprofile",
    "-o",
    "-outputdir",
    "-trace",
}
GO_DANGEROUS_OPTIONS = {
    "-args",
    "-exec",
    "-mod",
    "-modfile",
    "-overlay",
    "-toolexec",
}

PS_ARG_SETS = {
    ("-ef",),
    ("auxww",),
    ("axww",),
    ("-eo", "pid,ppid,etime,cmd"),
    ("-eo", "pid,ppid,stat,etime,args"),
    ("-o", "pid,ppid,stat,cmd", "axww"),
}
PROCESS_PROBE_TERMS = {
    "app.test",
    "cooper",
    "cooper-gotest",
    "compile",
    "docker",
    "go test",
    "go-build-cache",
    "internal/app",
    "internal/testdriver",
    "link",
    "test-e2e",
}
PROCESS_PROBE_DENIED_TERMS = {
    "credential",
    "key",
    "password",
    "secret",
    "ssh",
    "token",
}

DOCKER_LIST_BOOL_OPTIONS = {"--all", "--no-trunc", "--quiet", "-a", "-q"}
DOCKER_LIST_VALUE_OPTIONS = {"--filter", "--format", "-f"}
DOCKER_RESOURCE_RE = re.compile(
    r"^(?:"
    r"barrel-(?:e2e-|go)?[A-Za-z0-9_.-]*|"
    r"cooper(?:-gotest)?-[A-Za-z0-9_.:-]+|"
    r"proxy-repro-[A-Za-z0-9_.:-]+|"
    r"test-(?:e2e-)?[A-Za-z0-9_.:-]+"
    r")$"
)

REDIRECT_RE = re.compile(
    r"^(?P<fd>\d*)(?P<op>&>>|&>|>>|>\||>|<|<>)(?P<dest>.*)$"
)
EXIT_CAPTURE_RE = re.compile(
    r"""^(?:printf\s+(['"]%s\\n['"]|['"]%s\\n['"])\s+\$\?|echo\s+\$\?)\s*>\s*(/tmp/[A-Za-z0-9._/-]+)$"""
)
STATUS_ASSIGN_RE = re.compile(r"^status=\$\?$")
STATUS_WRITE_RE = re.compile(
    r"""^(?:echo\s+\$status|printf\s+(['"]%s\\n['"]|['"]%s\\n['"])\s+\$status)\s*>\s*(/tmp/[A-Za-z0-9._/-]+)$"""
)
STATUS_EXIT_RE = re.compile(r"^exit\s+\$status$")


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except (json.JSONDecodeError, TypeError):
        return 0

    command = get_command(payload)
    cwd = get_cwd(payload)
    if not command:
        return 0

    if is_allowed(command, cwd=cwd):
        emit_permission_decision("allow")
        return 0

    message = canonical_test_guidance(command, cwd=cwd)
    if not message:
        message = canonical_release_guidance(command, cwd=cwd)
    if message:
        emit_permission_decision("deny", message=message)

    return 0


def emit_permission_decision(behavior: str, *, message: str | None = None) -> None:
    decision = {"behavior": behavior}
    if message:
        decision["message"] = message
    print(
        json.dumps(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PermissionRequest",
                    "decision": decision,
                }
            }
        )
    )


def get_command(payload: dict) -> str:
    tool_input = payload.get("tool_input")
    if not isinstance(tool_input, dict):
        return ""
    command = tool_input.get("command")
    return command if isinstance(command, str) else ""


def get_cwd(payload: dict) -> str | None:
    tool_input = payload.get("tool_input")
    if isinstance(tool_input, dict):
        for key in ("cwd", "workdir"):
            value = tool_input.get(key)
            if isinstance(value, str) and value:
                return value

    value = payload.get("cwd")
    return value if isinstance(value, str) and value else None


def canonical_test_guidance(
    command: str,
    *,
    cwd: str | Path | None = None,
) -> str | None:
    """Return a safe retry message for a formatting-only test rejection.

    PermissionRequest hooks cannot rewrite tool input. A deny message is used
    only when the complete request is a reviewed test command whose test
    identity and arguments are already safe. Commands with unsafe semantics or
    syntax remain undecided and therefore retain Codex's normal approval flow.
    """

    current_cwd = normalize_cwd(cwd)
    if current_cwd is None or has_shell_wrapped_ansi_c_quote(command):
        return None

    script = unwrap_shell(command)
    if script is None or has_command_substitution(script):
        return None

    extracted = extract_guidable_test_statement(script, current_cwd)
    if extracted is None:
        return None
    statement, current_cwd = extracted
    if (
        contains_unquoted(statement, "||")
        or find_background_amp(statement) >= 0
        or has_forbidden_parameter_expansion(statement)
    ):
        return None

    statement = strip_guidable_tee(statement)
    if statement is None:
        return None

    parsed = clean_guidance_tokens(statement, current_cwd)
    if parsed is None:
        return None
    tokens, _, _ = parsed
    tokens = normalize_tokens(tokens, current_cwd)
    if not tokens:
        return None

    repo_test = guidable_repo_test(tokens, current_cwd)
    if repo_test is not None:
        test_script, args = repo_test
        canonical = canonical_repo_test_command(test_script, args)
        return canonical_retry_message(canonical)

    canonical_go = canonical_go_test_command(tokens, current_cwd)
    if canonical_go is not None:
        return canonical_retry_message(canonical_go)
    return None


def canonical_release_guidance(
    command: str,
    *,
    cwd: str | Path | None = None,
) -> str | None:
    """Guide an otherwise exact release preview to the required /tmp log."""

    current_cwd = normalize_cwd(cwd)
    if current_cwd is None or has_shell_wrapped_ansi_c_quote(command):
        return None
    script = unwrap_shell(command)
    if (
        script is None
        or has_command_substitution(script)
        or len(split_shell_statements(script)) != 1
        or contains_unquoted(script, "&&")
        or contains_unquoted(script, "||")
        or contains_unquoted(script, "|")
        or find_background_amp(script) >= 0
        or has_forbidden_parameter_expansion(script)
    ):
        return None

    parsed = clean_guidance_tokens(script.strip(), current_cwd)
    if parsed is None:
        return None
    tokens, _, _ = parsed
    tokens = normalize_tokens(tokens, current_cwd)
    release = resolve_release_script(tokens, current_cwd)
    if release is None:
        return None

    script_path, project = release
    relative = script_path.relative_to(REPO_ROOT).as_posix()
    canonical = (
        f"./{relative} > /tmp/{project}-release-preview.txt 2>&1"
    )
    return (
        "Govner rejected this noncanonical release preview; the script did not "
        "run. Do not ask the user to approve this form. Retry immediately from "
        f"the Govner repository root with: {canonical}"
    )


def clean_guidance_tokens(
    statement: str,
    cwd: Path,
) -> tuple[list[str], bool, bool] | None:
    """Parse a test whose only invalid redirection is an output destination."""

    parsed = clean_tokens(statement, cwd)
    if parsed is not None:
        return parsed

    try:
        tokens = shlex.split(statement)
    except ValueError:
        return None

    cleaned: list[str] = []
    index = 0
    while index < len(tokens):
        token = tokens[index]
        if token in {"<<", "<<<"} or token == "&":
            return None

        match = REDIRECT_RE.match(token)
        if not match:
            cleaned.append(token)
            index += 1
            continue

        fd = match.group("fd")
        op = match.group("op")
        dest = match.group("dest")
        if op in {"<", "<>"} or fd not in {"", "1", "2"}:
            return None
        if not dest:
            if index + 1 >= len(tokens):
                return None
            dest = tokens[index + 1]
            index += 1
        if not dest or (dest.startswith("&") and dest not in {"&1", "&2"}):
            return None
        index += 1

    return cleaned, False, False


def extract_guidable_test_statement(script: str, cwd: Path) -> tuple[str, Path] | None:
    """Extract one test from the few shell wrappers used by Govner agents."""

    statements = [part.strip() for part in split_shell_statements(script) if part.strip()]
    if len(statements) == 3:
        if statements[0] != "set -o pipefail" or not is_safe_status_statement(statements[2]):
            return None
        return statements[1], cwd
    if len(statements) != 1:
        return None

    parts = [part.strip() for part in split_unquoted(statements[0], "&&")]
    if len(parts) == 1:
        return parts[0], cwd
    if len(parts) != 2 or not parts[0] or not parts[1]:
        return None
    next_cwd = parse_allowed_cd(parts[0], cwd)
    return (parts[1], next_cwd) if next_cwd is not None else None


def strip_guidable_tee(statement: str) -> str | None:
    """Remove one safe tee sink so its test command can receive guidance."""

    parts = [part.strip() for part in split_unquoted(statement, "|")]
    if len(parts) == 1:
        return parts[0]
    if len(parts) != 2 or not parts[0] or not parts[1]:
        return None

    try:
        sink = shlex.split(parts[1])
    except ValueError:
        return None
    if len(sink) != 2 or sink[0] not in {"tee", "/usr/bin/tee"}:
        return None
    return parts[0] if is_safe_tmp_output(sink[1]) else None


def guidable_repo_test(tokens: list[str], cwd: Path) -> tuple[Path, tuple[str, ...]] | None:
    if not tokens:
        return None

    index = 0
    if tokens[0] in {"bash", "/bin/bash", "sh", "/bin/sh"}:
        index = 1
        if index < len(tokens) and tokens[index] == "-x":
            index += 1
        if index >= len(tokens):
            return None

    command_token = tokens[index]
    script = resolve_command_path(command_token, cwd)

    # This is the historical hidden-workdir failure. Denying it is safe, but
    # approving it is not: the payload says repo root while the intended
    # Cooper workdir exists only in the shell tool call and is not reported.
    if (
        script not in COOPER_TEST_SCRIPTS
        and cwd == REPO_ROOT
        and command_token == "./test-e2e.sh"
    ):
        script = (COOPER_ROOT / "test-e2e.sh").resolve()

    if script not in COOPER_TEST_SCRIPTS:
        return None
    args = tuple(tokens[index + 1 :])
    return (script, args) if args in COOPER_TEST_SCRIPTS[script] else None


def canonical_repo_test_command(script: Path, args: tuple[str, ...]) -> str:
    relative_script = script.relative_to(REPO_ROOT).as_posix()
    output_name = (
        "cooper-e2e.txt"
        if script.name == "test-e2e.sh"
        else "cooper-docker-build.txt"
    )
    invocation = shlex.join(
        ["timeout", "90m", f"./{relative_script}", *args]
    )
    return f"{invocation} > /tmp/{output_name} 2>&1"


def canonical_go_test_command(tokens: list[str], cwd: Path) -> str | None:
    if len(tokens) < 2 or tokens[0] != "go" or tokens[1] not in {"test", "vet"}:
        return None
    if not validate_go_packages(tokens[1:], cwd):
        return None

    canonical = list(tokens)
    if cwd == COOPER_ROOT:
        if has_go_chdir_option(canonical[2:]):
            return None
        canonical[2:2] = ["-C", "./cooper"]
    elif cwd != REPO_ROOT:
        # A relative -C or package path could change meaning if rewritten from
        # an arbitrary subdirectory. Leave that request to normal approval.
        return None

    output_name = "cooper-go-test.txt" if tokens[1] == "test" else "cooper-go-vet.txt"
    return f"{shlex.join(canonical)} > /tmp/{output_name} 2>&1"


def has_go_chdir_option(args: list[str]) -> bool:
    return any(split_option(token)[0] == "-C" for token in args)


def canonical_retry_message(command: str) -> str:
    return (
        "Govner rejected this noncanonical test command; the test did not run. "
        "Do not ask the user to approve this form. Retry immediately from the "
        f"Govner repository root with: {command}"
    )


def is_allowed(
    command: str,
    *,
    cwd: str | Path | None = None,
    output_captured: bool = False,
) -> bool:
    current_cwd = normalize_cwd(cwd)
    if current_cwd is None or has_shell_wrapped_ansi_c_quote(command):
        return False

    script = unwrap_shell(command)
    if script is None or has_command_substitution(script):
        return False

    statements = split_shell_statements(script)
    if not statements:
        return False

    saw_action = False
    for statement in statements:
        statement = statement.strip()
        if not statement:
            continue

        parts = [part.strip() for part in split_unquoted(statement, "&&") if part.strip()]
        if not parts:
            return False

        for part in parts:
            part = strip_true_fallback(part)
            if part is None:
                return False

            next_cwd = parse_allowed_cd(part, current_cwd)
            if next_cwd is not None:
                current_cwd = next_cwd
                saw_action = True
                continue

            if is_safe_status_statement(part):
                saw_action = True
                continue

            if has_forbidden_parameter_expansion(part):
                return False

            if contains_unquoted(part, "|"):
                if not (
                    is_process_probe_pipe_statement(part, current_cwd)
                    or is_docker_read_pipe_statement(part, current_cwd)
                ):
                    return False
                saw_action = True
                continue

            if not is_allowed_simple_command(
                part,
                current_cwd,
                output_captured=output_captured,
            ):
                return False
            saw_action = True

    return saw_action


def normalize_cwd(value: str | Path | None) -> Path | None:
    try:
        path = Path(value) if value is not None else REPO_ROOT
        if not path.is_absolute():
            path = REPO_ROOT / path
        path = path.resolve(strict=False)
    except (OSError, RuntimeError, ValueError):
        return None
    return path if is_relative_to(path, REPO_ROOT) else None


def unwrap_shell(command: str) -> str | None:
    try:
        tokens = shlex.split(command)
    except ValueError:
        return None

    if tokens and tokens[0] in {"bash", "/bin/bash", "sh", "/bin/sh"}:
        if len(tokens) == 3 and tokens[1] in {"-c", "-lc"}:
            return tokens[2]
        if len(tokens) >= 2 and tokens[1] in {"-c", "-lc"}:
            return None
    return command


def has_shell_wrapped_ansi_c_quote(command: str) -> bool:
    try:
        tokens = shlex.split(command)
    except ValueError:
        return True
    return (
        len(tokens) >= 3
        and tokens[0] in {"bash", "/bin/bash", "sh", "/bin/sh"}
        and tokens[1] in {"-c", "-lc"}
        and (has_ansi_c_quote(command) or "$'" in tokens[2])
    )


def has_ansi_c_quote(command: str) -> bool:
    quote = ""
    escaped = False
    for index, char in enumerate(command):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote != "'":
            escaped = True
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            continue
        if not quote and char == "$" and index + 1 < len(command) and command[index + 1] == "'":
            return True
    return False


def has_command_substitution(script: str) -> bool:
    return "$(" in script or "`" in script or "<(" in script or ">(" in script


def has_forbidden_parameter_expansion(script: str) -> bool:
    quote = ""
    index = 0
    while index < len(script):
        char = script[index]
        if char == "\\" and quote != "'":
            index += 2
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            index += 1
            continue
        if char == "$" and quote != "'":
            next_char = script[index + 1] if index + 1 < len(script) else ""
            if next_char and (next_char.isalnum() or next_char in "_{(?!#@*-"):
                return True
        index += 1
    return False


def split_unquoted(script: str, operator: str) -> list[str]:
    parts: list[str] = []
    start = 0
    quote = ""
    index = 0
    while index < len(script):
        char = script[index]
        if char == "\\" and quote != "'":
            index += 2
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            index += 1
            continue
        if not quote and script.startswith(operator, index):
            parts.append(script[start:index])
            index += len(operator)
            start = index
            continue
        index += 1
    parts.append(script[start:])
    return parts


def split_shell_statements(script: str) -> list[str]:
    parts: list[str] = []
    start = 0
    quote = ""
    index = 0
    while index < len(script):
        char = script[index]
        if char == "\\" and quote != "'":
            index += 2
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            index += 1
            continue
        if not quote and char in {";", "\n"}:
            parts.append(script[start:index])
            start = index + 1
        index += 1
    parts.append(script[start:])
    return parts


def contains_unquoted(script: str, operator: str) -> bool:
    return len(split_unquoted(script, operator)) > 1


def strip_true_fallback(statement: str) -> str | None:
    parts = [part.strip() for part in split_unquoted(statement, "||")]
    if len(parts) == 1:
        return parts[0]
    if len(parts) == 2 and parts[0] and parts[1] == "true":
        return parts[0]
    return None


def parse_allowed_cd(statement: str, cwd: Path) -> Path | None:
    try:
        tokens = shlex.split(statement)
    except ValueError:
        return None
    if len(tokens) != 2 or tokens[0] != "cd":
        return None
    return resolve_safe_path(tokens[1], cwd, allow_tmp=False)


def is_safe_status_statement(statement: str) -> bool:
    if STATUS_ASSIGN_RE.match(statement) or STATUS_EXIT_RE.match(statement):
        return True
    match = EXIT_CAPTURE_RE.match(statement) or STATUS_WRITE_RE.match(statement)
    return bool(match and is_safe_tmp_output(match.group(match.lastindex or 1)))


def is_allowed_simple_command(
    statement: str,
    cwd: Path,
    *,
    output_captured: bool,
) -> bool:
    if find_background_amp(statement) >= 0:
        return False

    parsed = clean_tokens(statement, cwd)
    if parsed is None:
        return False
    raw_tokens, stdout_tmp, stderr_tmp = parsed
    complete_capture = output_captured or (stdout_tmp and stderr_tmp)

    if (
        is_release_go_build(raw_tokens, cwd, complete_capture)
        or is_release_module_index(raw_tokens, cwd, complete_capture)
    ):
        return True

    tokens = normalize_tokens(raw_tokens, cwd)
    if not tokens:
        return False

    return (
        is_repo_test_script(tokens, cwd, complete_capture)
        or is_interpreter_test_script(tokens, cwd, complete_capture)
        or is_repo_release_script(tokens, cwd, complete_capture)
        or is_interpreter_release_script(tokens, cwd, complete_capture)
        or is_script_pty_wrapper(tokens, cwd)
        or is_project_dev_build(tokens, cwd, complete_capture)
        or is_go_command(tokens, cwd, complete_capture)
        or is_git_command(tokens, cwd)
        or is_docker_read_command(tokens)
        or is_lsof_probe(tokens)
        or is_direct_ps_probe(tokens)
        or is_pgrep_probe(tokens)
        or is_tmp_mkdir(tokens)
        or is_release_mkdir(tokens, cwd)
        or is_read_only_command(tokens, cwd)
    )


def find_background_amp(statement: str) -> int:
    quote = ""
    index = 0
    while index < len(statement):
        char = statement[index]
        if char == "\\" and quote != "'":
            index += 2
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            index += 1
            continue
        if not quote and char == "&":
            previous = statement[index - 1] if index else ""
            following = statement[index + 1] if index + 1 < len(statement) else ""
            if previous != ">" and following not in {"&", ">"}:
                return index
        index += 1
    return -1


def clean_tokens(statement: str, cwd: Path) -> tuple[list[str], bool, bool] | None:
    try:
        tokens = shlex.split(statement)
    except ValueError:
        return None

    cleaned: list[str] = []
    stdout_tmp = False
    stderr_tmp = False
    index = 0
    while index < len(tokens):
        token = tokens[index]
        if token in {"<<", "<<<"} or token == "&":
            return None

        match = REDIRECT_RE.match(token)
        if match:
            fd = match.group("fd")
            op = match.group("op")
            dest = match.group("dest")
            if not dest:
                if index + 1 >= len(tokens):
                    return None
                dest = tokens[index + 1]
                index += 1

            result = apply_redirection(fd, op, dest, cwd, stdout_tmp, stderr_tmp)
            if result is None:
                return None
            stdout_tmp, stderr_tmp = result
        else:
            cleaned.append(token)
        index += 1

    return cleaned, stdout_tmp, stderr_tmp


def apply_redirection(
    fd: str,
    op: str,
    dest: str,
    cwd: Path,
    stdout_tmp: bool,
    stderr_tmp: bool,
) -> tuple[bool, bool] | None:
    if op == "<":
        if dest == "/dev/null" or resolve_safe_path(dest, cwd) is not None:
            return stdout_tmp, stderr_tmp
        return None
    if op == "<>":
        return None

    if dest in {"&1", "&2"}:
        if op not in {">", ">>"}:
            return None
        if fd == "2" and dest == "&1":
            return stdout_tmp, stdout_tmp
        if fd in {"", "1"} and dest == "&2":
            return stderr_tmp, stderr_tmp
        return None

    is_tmp = is_safe_tmp_output(dest)
    if not is_tmp and dest != "/dev/null":
        return None

    if op in {"&>", "&>>"}:
        if fd:
            return None
        return is_tmp, is_tmp
    if fd in {"", "1"}:
        return is_tmp, stderr_tmp
    if fd == "2":
        return stdout_tmp, is_tmp
    return None


def normalize_tokens(tokens: list[str], cwd: Path) -> list[str]:
    current = list(tokens)
    for _ in range(12):
        previous = list(current)
        current = strip_time(current)
        current = strip_timeout(current)
        current, invalid_env = strip_allowed_env(current, cwd)
        if invalid_env:
            return []
        if current == previous:
            return current
    return []


def strip_time(tokens: list[str]) -> list[str]:
    if len(tokens) >= 3 and tokens[0] in {"time", "/usr/bin/time"} and tokens[1] == "-p":
        return tokens[2:]
    return tokens


def strip_timeout(tokens: list[str]) -> list[str]:
    if len(tokens) >= 3 and tokens[0] in {"timeout", "/usr/bin/timeout"}:
        return tokens[2:] if is_safe_duration(tokens[1], MAX_TIMEOUT_SECONDS) else []
    return tokens


def strip_allowed_env(tokens: list[str], cwd: Path) -> tuple[list[str], bool]:
    if not tokens:
        return tokens, False

    start = 1 if tokens[0] == "env" else 0
    index = start
    while index < len(tokens) and is_env_assignment(tokens[index]):
        if not is_allowed_env(tokens[index], cwd):
            return tokens, True
        index += 1

    if index == start:
        return tokens, bool(start)
    return tokens[index:], False


def is_env_assignment(token: str) -> bool:
    name, separator, _ = token.partition("=")
    return bool(separator) and bool(re.match(r"^[A-Z_][A-Z0-9_]*$", name))


def is_allowed_env(token: str, cwd: Path) -> bool:
    name, _, value = token.partition("=")
    if name in ALLOWED_BOOL_ENVS:
        return value in {"0", "1", "FALSE", "TRUE", "false", "true"}
    if name in {"GOCACHE", "GOMODCACHE"}:
        return is_safe_tmp_output(value)
    if name == "GOMAXPROCS":
        return value.isdigit() and 1 <= int(value) <= 64
    if name == "GOWORK":
        return value == "off"
    if name in ALLOWED_GO_OFF_ENVS:
        return value == "off"
    if name == "GOTOOLCHAIN":
        return value == "local"
    return False


def is_repo_test_script(tokens: list[str], cwd: Path, complete_capture: bool) -> bool:
    if not complete_capture or not tokens:
        return False
    script = resolve_command_path(tokens[0], cwd)
    if script not in COOPER_TEST_SCRIPTS:
        return False
    return tuple(tokens[1:]) in COOPER_TEST_SCRIPTS[script]


def is_interpreter_test_script(tokens: list[str], cwd: Path, complete_capture: bool) -> bool:
    if not complete_capture or len(tokens) < 2 or tokens[0] not in {
        "bash",
        "/bin/bash",
        "sh",
        "/bin/sh",
    }:
        return False

    index = 1
    if tokens[index] == "-x":
        index += 1
    if index >= len(tokens):
        return False

    script = resolve_command_path(tokens[index], cwd)
    if script not in COOPER_TEST_SCRIPTS:
        return False
    return tuple(tokens[index + 1 :]) in COOPER_TEST_SCRIPTS[script]


def is_repo_release_script(tokens: list[str], cwd: Path, complete_capture: bool) -> bool:
    return (
        complete_capture
        and bool(tokens)
        and tokens[0] not in {"bash", "/bin/bash", "sh", "/bin/sh"}
        and resolve_release_script(tokens, cwd) is not None
    )


def is_interpreter_release_script(
    tokens: list[str],
    cwd: Path,
    complete_capture: bool,
) -> bool:
    return (
        complete_capture
        and bool(tokens)
        and tokens[0] in {"bash", "/bin/bash", "sh", "/bin/sh"}
        and resolve_release_script(tokens, cwd) is not None
    )


def resolve_release_script(tokens: list[str], cwd: Path) -> tuple[Path, str] | None:
    if not tokens:
        return None

    index = 0
    if tokens[0] in {"bash", "/bin/bash", "sh", "/bin/sh"}:
        index = 1
        if index < len(tokens) and tokens[index] == "-x":
            index += 1
        if index >= len(tokens):
            return None

    script = resolve_command_path(tokens[index], cwd)
    if script not in RELEASE_SCRIPTS or len(tokens) != index + 1:
        return None
    return script, RELEASE_SCRIPTS[script]


def is_script_pty_wrapper(tokens: list[str], cwd: Path) -> bool:
    parsed = parse_script_pty_wrapper(tokens)
    if parsed is None:
        return False
    command, transcript = parsed
    return is_safe_tmp_output(transcript) and is_allowed(
        command,
        cwd=cwd,
        output_captured=True,
    )


def parse_script_pty_wrapper(tokens: list[str]) -> tuple[str, str] | None:
    if not tokens or tokens[0] != "script":
        return None

    command = ""
    transcript = ""
    index = 1
    while index < len(tokens):
        token = tokens[index]
        if token in {"--force", "--log-in", "--log-io", "-B", "-I"}:
            return None
        if token in {"--append", "--flush", "--quiet", "--return", "-a", "-e", "-f", "-q"}:
            index += 1
            continue
        if (
            len(token) > 2
            and token.startswith("-")
            and not token.startswith("--")
            and set(token[1:]).issubset({"a", "e", "f", "q"})
        ):
            index += 1
            continue
        if token in {"--command", "-c"}:
            if command or index + 1 >= len(tokens):
                return None
            command = tokens[index + 1]
            index += 2
            continue
        if token.startswith("--command="):
            if command:
                return None
            command = token.split("=", 1)[1]
            index += 1
            continue
        if token.startswith("-c") and token != "-c":
            if command:
                return None
            command = token[2:]
            index += 1
            continue
        if token.startswith("-"):
            return None
        if transcript:
            return None
        transcript = token
        index += 1

    if not command or not transcript:
        return None
    return command, transcript


def normalize_release_tokens(
    tokens: list[str],
    cwd: Path,
    *,
    allow_remote_proxy: bool,
) -> tuple[list[str], dict[str, str]] | None:
    """Normalize wrappers while retaining release-relevant environment values."""

    current = list(tokens)
    environment: dict[str, str] = {}
    for _ in range(12):
        previous = list(current)
        current = strip_time(current)
        current = strip_timeout(current)
        if not current:
            return None

        start = 1 if current[0] == "env" else 0
        index = start
        while index < len(current) and is_env_assignment(current[index]):
            name, _, value = current[index].partition("=")
            valid = (
                (name == "GOOS" and value in {"darwin", "linux"})
                or (name == "GOARCH" and value in {"amd64", "arm64"})
                or (
                    allow_remote_proxy
                    and name == "GOPROXY"
                    and value == "https://proxy.golang.org"
                )
                or is_allowed_env(current[index], cwd)
            )
            if not valid or (name in environment and environment[name] != value):
                return None
            environment[name] = value
            index += 1

        if index > start:
            current = current[index:]
        elif start:
            return None
        if current == previous:
            return current, environment
    return None


def is_release_go_build(tokens: list[str], cwd: Path, complete_capture: bool) -> bool:
    if not complete_capture:
        return False
    normalized = normalize_release_tokens(
        tokens,
        cwd,
        allow_remote_proxy=False,
    )
    if normalized is None:
        return False
    command, environment = normalized
    if len(command) < 3 or command[:2] != ["go", "build"]:
        return False

    target = (environment.get("GOOS"), environment.get("GOARCH"))
    if target not in COOPER_RELEASE_TARGETS:
        return False

    build_cwd = cwd
    output = ""
    packages: list[str] = []
    index = 2
    while index < len(command):
        token = command[index]
        option, attached = split_option(token)
        if option == "-C":
            value, consumed = option_value(command, index, attached)
            next_cwd = (
                resolve_safe_path(value, build_cwd, allow_tmp=False)
                if value
                else None
            )
            if next_cwd is None:
                return False
            build_cwd = next_cwd
            index += consumed
            continue
        if option == "-o":
            value, consumed = option_value(command, index, attached)
            if value is None or output:
                return False
            output = value
            index += consumed
            continue
        if token == "-trimpath":
            index += 1
            continue
        if token.startswith("-"):
            return False
        packages.append(token)
        index += 1

    version = project_version("cooper")
    if (
        version is None
        or build_cwd != COOPER_ROOT
        or packages not in (["."], ["./"])
        or not output
    ):
        return False
    resolved_output = resolve_safe_path(output, build_cwd, allow_tmp=False)
    expected = (
        REPO_ROOT
        / "dist"
        / "cooper"
        / f"v{version}"
        / f"cooper-{target[0]}-{target[1]}"
    )
    return resolved_output == expected


def is_release_module_index(
    tokens: list[str],
    cwd: Path,
    complete_capture: bool,
) -> bool:
    if not complete_capture:
        return False
    normalized = normalize_release_tokens(
        tokens,
        cwd,
        allow_remote_proxy=True,
    )
    if normalized is None:
        return False
    command, environment = normalized
    if environment.get("GOPROXY") != "https://proxy.golang.org":
        return False
    if any(name not in {"GOPROXY", "GOTOOLCHAIN"} for name in environment):
        return False
    if len(command) != 4 or command[:3] != ["go", "list", "-m"]:
        return False

    module_version = command[3]
    for project, module in PROJECT_MODULES.items():
        version = project_version(project)
        if version is not None and module_version == f"{module}@v{version}":
            return True
    return False


def is_project_dev_build(
    tokens: list[str],
    cwd: Path,
    complete_capture: bool,
) -> bool:
    """Allow only each module's conventional gitignored development binary."""

    if not complete_capture or len(tokens) < 3 or tokens[:2] != ["go", "build"]:
        return False

    build_cwd = cwd
    output = ""
    packages: list[str] = []
    index = 2
    while index < len(tokens):
        token = tokens[index]
        option, attached = split_option(token)
        if option == "-C":
            value, consumed = option_value(tokens, index, attached)
            next_cwd = (
                resolve_safe_path(value, build_cwd, allow_tmp=False)
                if value
                else None
            )
            if next_cwd is None:
                return False
            build_cwd = next_cwd
            index += consumed
            continue
        if option == "-o":
            value, consumed = option_value(tokens, index, attached)
            if value is None or output:
                return False
            output = value
            index += consumed
            continue
        if token == "-trimpath":
            index += 1
            continue
        if token.startswith("-"):
            return False
        packages.append(token)
        index += 1

    if not output or len(packages) != 1:
        return False
    resolved_output = resolve_safe_path(output, build_cwd, allow_tmp=False)
    package = packages[0]
    resolved_package = (
        build_cwd
        if package in {".", "./"}
        else resolve_safe_path(package, build_cwd, allow_tmp=False)
    )
    return any(
        resolved_output == PROJECT_DEV_BINARIES[project]
        and resolved_package == project_root
        for project, project_root in PROJECT_ROOTS.items()
    )


def project_version(project: str) -> str | None:
    root = PROJECT_ROOTS.get(project)
    if root is None:
        return None
    try:
        source = (root / "meta/version.go").read_text()
    except (OSError, UnicodeError):
        return None
    match = re.search(r'\bVersion\s*=\s*"([^"]+)"', source)
    if match is None or SEMVER_RE.fullmatch(match.group(1)) is None:
        return None
    return match.group(1)


def project_release_tag(project: str) -> str | None:
    version = project_version(project)
    return f"{project}/v{version}" if version is not None else None


def is_current_release_tag(value: str) -> bool:
    return any(value == project_release_tag(project) for project in PROJECT_ROOTS)


def is_go_command(tokens: list[str], cwd: Path, complete_capture: bool) -> bool:
    if len(tokens) < 2 or tokens[0] != "go" or not complete_capture:
        return False
    if tokens[1] in {"test", "vet"}:
        return validate_go_packages(tokens[1:], cwd)
    if tokens[1] == "build":
        return validate_go_packages(tokens[1:], cwd, require_tmp_binary=True)
    if tokens[1] == "run":
        return validate_test_driver(tokens[2:], cwd)
    return False


def validate_go_packages(
    args: list[str],
    cwd: Path,
    *,
    require_tmp_binary: bool = False,
) -> bool:
    if not args:
        return False

    command = args[0]
    current_cwd = cwd
    saw_tmp_binary = False
    compile_only = False
    index = 1
    while index < len(args):
        token = args[index]
        option, attached = split_option(token)

        if option in GO_DANGEROUS_OPTIONS:
            return False
        if token == "-c":
            compile_only = True
            index += 1
            continue
        if option == "-C":
            value, consumed = option_value(args, index, attached)
            target = resolve_safe_path(value, current_cwd, allow_tmp=False) if value else None
            if target is None:
                return False
            current_cwd = target
            index += consumed
            continue
        if option in GO_OUTPUT_OPTIONS:
            value, consumed = option_value(args, index, attached)
            if value is None or not is_safe_tmp_output(value):
                return False
            if option == "-o":
                saw_tmp_binary = True
            index += consumed
            continue
        if option in GO_VALUE_OPTIONS:
            value, consumed = option_value(args, index, attached)
            if value is None or has_sensitive_path_mention(value):
                return False
            index += consumed
            continue
        if token.startswith("-"):
            if has_sensitive_path_mention(token) or has_any_url_scheme(token):
                return False
            index += 1
            continue
        if not is_safe_go_package(token, current_cwd):
            return False
        index += 1

    if command == "build" and not saw_tmp_binary:
        return False
    if compile_only and not saw_tmp_binary:
        return False
    return not require_tmp_binary or saw_tmp_binary


def validate_test_driver(args: list[str], cwd: Path) -> bool:
    if not args:
        return False
    driver = resolve_command_path(args[0], cwd)
    if driver != COOPER_TEST_DRIVER:
        return False

    index = 1
    while index < len(args):
        token = args[index]
        option, attached = split_option(token)
        if option == "--scenario":
            value, consumed = option_value(args, index, attached)
            if value not in TEST_DRIVER_SCENARIOS:
                return False
            index += consumed
            continue
        if option == "--timeout":
            value, consumed = option_value(args, index, attached)
            if value is None or not is_safe_duration(value, 10 * 60):
                return False
            index += consumed
            continue
        if option == "--prefix":
            value, consumed = option_value(args, index, attached)
            if value is None or not re.match(r"^(?:cooper-gotest|test|manual)-[a-z0-9-]*$", value):
                return False
            index += consumed
            continue
        if token == "--keep":
            index += 1
            continue
        if option == "--disable-host-clipboard":
            if attached is not None and attached not in {"false", "true"}:
                return False
            index += 1
            continue
        return False
    return True


def is_safe_go_package(value: str, cwd: Path) -> bool:
    if (
        not value
        or has_any_url_scheme(value)
        or "@" in value
        or any(char in value for char in {"*", "?", "[", "]", "{", "}"})
    ):
        return False
    if value in {".", "./", "./..."}:
        return True
    if value.endswith("/..."):
        value = value[:-4] or "."
    if not value.startswith(("./", "../", "/")):
        return False
    return resolve_safe_path(value, cwd, allow_tmp=False) is not None


def is_git_command(tokens: list[str], cwd: Path) -> bool:
    parsed = split_git_command(tokens, cwd)
    if parsed is None:
        return False
    command, args, git_cwd = parsed
    if command == "add":
        return is_git_add(args, git_cwd)
    if command == "commit":
        return is_git_commit(args, git_cwd)
    if command == "fetch":
        return is_git_fetch(args)
    if command == "push":
        return is_git_push(args)
    if command == "tag":
        return is_git_tag(args)
    return False


def split_git_command(tokens: list[str], cwd: Path) -> tuple[str, list[str], Path] | None:
    if len(tokens) < 2 or tokens[0] != "git":
        return None

    git_cwd = cwd
    index = 1
    while index < len(tokens) and tokens[index] == "-C":
        if index + 1 >= len(tokens):
            return None
        next_cwd = resolve_safe_path(tokens[index + 1], git_cwd, allow_tmp=False)
        if next_cwd is None:
            return None
        git_cwd = next_cwd
        index += 2
    if index >= len(tokens):
        return None
    return tokens[index], tokens[index + 1 :], git_cwd


def is_git_add(args: list[str], cwd: Path) -> bool:
    if not args:
        return False

    saw_scope = False
    paths_only = False
    for token in args:
        if token == "--" and not paths_only:
            paths_only = True
            continue
        if not paths_only and token in GIT_ADD_BOOL_OPTIONS:
            saw_scope = True
            continue
        if not paths_only and token in {"--intent-to-add", "--renormalize", "-N"}:
            saw_scope = True
            continue
        if not paths_only and token.startswith("--chmod="):
            if token not in {"--chmod=+x", "--chmod=-x"}:
                return False
            continue
        if not paths_only and token.startswith("-"):
            return False
        if not is_safe_git_pathspec(token, cwd):
            return False
        saw_scope = True
    return saw_scope


def is_git_commit(args: list[str], cwd: Path) -> bool:
    if not args:
        return False

    saw_message = False
    saw_amend = False
    saw_no_edit = False
    paths_only = False
    index = 0
    while index < len(args):
        token = args[index]
        if token == "--" and not paths_only:
            paths_only = True
            index += 1
            continue
        if paths_only:
            if not is_safe_git_pathspec(token, cwd):
                return False
            index += 1
            continue
        if token in GIT_COMMIT_BOOL_OPTIONS:
            saw_amend = saw_amend or token == "--amend"
            saw_no_edit = saw_no_edit or token == "--no-edit"
            index += 1
            continue
        if token in {"-m", "--message"}:
            if index + 1 >= len(args):
                return False
            saw_message = True
            index += 2
            continue
        if token.startswith("--message=") or (token.startswith("-m") and token != "-m"):
            saw_message = True
            index += 1
            continue
        if token == "-am":
            if index + 1 >= len(args):
                return False
            saw_message = True
            index += 2
            continue
        if token in {"-F", "--file"}:
            if index + 1 >= len(args) or not is_safe_git_message_file(args[index + 1], cwd):
                return False
            saw_message = True
            index += 2
            continue
        if token.startswith("--file="):
            if not is_safe_git_message_file(token.split("=", 1)[1], cwd):
                return False
            saw_message = True
            index += 1
            continue
        if token.startswith(("--fixup=", "--squash=")):
            if not is_safe_git_ref(token.split("=", 1)[1]):
                return False
            saw_message = True
            index += 1
            continue
        if token.startswith("--cleanup="):
            if token.split("=", 1)[1] not in {
                "default",
                "scissors",
                "strip",
                "verbatim",
                "whitespace",
            }:
                return False
            index += 1
            continue
        if token.startswith("-"):
            return False
        if not is_safe_git_pathspec(token, cwd):
            return False
        index += 1

    return saw_message or (saw_amend and saw_no_edit)


def is_git_fetch(args: list[str]) -> bool:
    positionals: list[str] = []
    for token in args:
        if token == "--":
            continue
        if token in GIT_FETCH_BOOL_OPTIONS or token == "--recurse-submodules=no":
            continue
        if token.startswith("-"):
            return False
        positionals.append(token)

    if not positionals:
        return True
    if positionals[0] != "origin":
        return False
    if len(positionals) >= 2 and positionals[1] == "tag":
        return len(positionals) == 3 and is_current_release_tag(positionals[2])
    return all(is_safe_git_ref(ref) for ref in positionals[1:])


def is_git_push(args: list[str]) -> bool:
    positionals: list[str] = []
    for token in args:
        if token == "--":
            continue
        if token in GIT_PUSH_BOOL_OPTIONS:
            continue
        if token.startswith("-"):
            return False
        positionals.append(token)

    # A bare push uses the already configured upstream. Repository policy does
    # not auto-approve git config changes, so this cannot silently add a new
    # remote through another approved Git operation.
    if not positionals:
        return True
    if positionals[0] != "origin":
        return False
    return all(is_safe_git_ref(ref) for ref in positionals[1:])


def is_git_tag(args: list[str]) -> bool:
    if not args:
        return True
    if args[0] in {"-l", "--list"}:
        allowed_patterns = {
            f"{project}/v*"
            for project in PROJECT_ROOTS
        }
        for token in args[1:]:
            if token == "--sort=-version:refname":
                continue
            if token not in allowed_patterns and not is_safe_git_ref(token):
                return False
        return True

    annotated = False
    saw_message = False
    message_file = ""
    tag = ""
    index = 0
    while index < len(args):
        token = args[index]
        if token in {"-a", "--annotate"}:
            annotated = True
            index += 1
            continue
        if token in {"-m", "--message"}:
            if index + 1 >= len(args):
                return False
            saw_message = True
            index += 2
            continue
        if token.startswith("--message=") or (token.startswith("-m") and token != "-m"):
            saw_message = True
            index += 1
            continue
        if token in {"-F", "--file"}:
            if index + 1 >= len(args) or message_file:
                return False
            message_file = args[index + 1]
            index += 2
            continue
        if token.startswith("--file="):
            if message_file:
                return False
            message_file = token.split("=", 1)[1]
            index += 1
            continue
        if token.startswith("-") or tag:
            return False
        tag = token
        index += 1
    if not annotated or not is_current_release_tag(tag):
        return False
    if message_file:
        if saw_message:
            return False
        project = tag.split("/", 1)[0]
        return is_release_tag_message_file(project, message_file)
    return saw_message


def is_release_tag_message_file(project: str, value: str) -> bool:
    """Validate the private annotation generated by a release preview."""

    path = Path(value)
    if (
        not path.is_absolute()
        or path.parent != TMP_ROOT
        or re.fullmatch(
            rf"{re.escape(project)}-release-tag-message\.[A-Za-z0-9]{{6}}",
            path.name,
        )
        is None
    ):
        return False

    try:
        if path.is_symlink() or not path.is_file():
            return False
        metadata = path.stat()
        if (
            metadata.st_uid != os.getuid()
            or metadata.st_mode & 0o077
            or metadata.st_size > 64 * 1024
        ):
            return False
        content = path.read_text()
    except (OSError, UnicodeError):
        return False

    version = project_version(project)
    return (
        version is not None
        and content.startswith(
            f"{project} {version}\n\nChanges since "
        )
        and content.endswith("\n")
        and "\x00" not in content
    )


def is_safe_git_pathspec(value: str, cwd: Path) -> bool:
    if (
        not value
        or value.startswith(":")
        or has_sensitive_path_mention(value)
        or has_any_url_scheme(value)
        or has_shell_expansion(value)
    ):
        return False
    return resolve_safe_path(value, cwd, allow_tmp=False) is not None


def is_safe_git_message_file(value: str, cwd: Path) -> bool:
    if value == "-" or has_sensitive_path_mention(value) or has_shell_expansion(value):
        return False
    return resolve_safe_path(value, cwd) is not None


def is_safe_git_ref(value: str) -> bool:
    if value == "HEAD":
        return True
    if (
        GIT_REF_RE.fullmatch(value) is None
        or value.startswith((".", "/", "-"))
        or value.endswith((".", "/", ".lock"))
        or ".." in value
        or "@{" in value
        or any(part in {"", ".", ".."} for part in value.split("/"))
    ):
        return False
    return True


def is_docker_read_command(tokens: list[str]) -> bool:
    if len(tokens) < 2 or tokens[0] != "docker":
        return False

    if tokens[1:3] in (["image", "ls"], ["network", "ls"], ["container", "ls"]):
        return validate_docker_list_args(tokens[3:])
    if tokens[1] in {"images", "ps"}:
        return validate_docker_list_args(tokens[2:])
    if tokens[1:3] == ["image", "inspect"]:
        return validate_docker_inspect_args(tokens[3:])
    if tokens[1:3] == ["network", "inspect"]:
        return validate_docker_inspect_args(tokens[3:])
    return False


def validate_docker_list_args(args: list[str]) -> bool:
    index = 0
    while index < len(args):
        token = args[index]
        option, attached = split_option(token)
        if token in DOCKER_LIST_BOOL_OPTIONS:
            index += 1
            continue
        if option in DOCKER_LIST_VALUE_OPTIONS:
            value, consumed = option_value(args, index, attached)
            if value is None or has_sensitive_path_mention(value):
                return False
            index += consumed
            continue
        return False
    return True


def validate_docker_inspect_args(args: list[str]) -> bool:
    if not args:
        return False
    index = 0
    resources = 0
    while index < len(args):
        token = args[index]
        option, attached = split_option(token)
        if option == "--format":
            value, consumed = option_value(args, index, attached)
            if value is None or has_sensitive_path_mention(value):
                return False
            index += consumed
            continue
        if token.startswith("-") or not DOCKER_RESOURCE_RE.match(token):
            return False
        resources += 1
        index += 1
    return resources > 0


def is_docker_read_pipe_statement(statement: str, cwd: Path) -> bool:
    parts = split_single_pipes(statement)
    if len(parts) != 2:
        return False
    left = clean_tokens(parts[0].strip(), cwd)
    right = clean_tokens(parts[1].strip(), cwd)
    if left is None or right is None:
        return False
    return is_docker_read_command(left[0]) and is_safe_stdin_filter(right[0], cwd)


def is_process_probe_pipe_statement(statement: str, cwd: Path) -> bool:
    parts = split_single_pipes(statement)
    if len(parts) != 2:
        return False
    left = clean_tokens(parts[0].strip(), cwd)
    right = clean_tokens(parts[1].strip(), cwd)
    if left is None or right is None:
        return False
    return is_safe_ps_command(left[0]) and is_safe_process_filter(right[0])


def split_single_pipes(statement: str) -> list[str]:
    parts: list[str] = []
    start = 0
    quote = ""
    index = 0
    while index < len(statement):
        char = statement[index]
        if char == "\\" and quote != "'":
            index += 2
            continue
        if char in {"'", '"'}:
            if not quote:
                quote = char
            elif quote == char:
                quote = ""
            index += 1
            continue
        if not quote and char == "|":
            before = statement[index - 1] if index else ""
            after = statement[index + 1] if index + 1 < len(statement) else ""
            if before != "|" and after != "|":
                parts.append(statement[start:index])
                start = index + 1
        index += 1
    parts.append(statement[start:])
    return parts


def is_safe_stdin_filter(tokens: list[str], cwd: Path) -> bool:
    if not tokens or tokens[0] not in {"grep", "head", "rg", "tail"}:
        return False
    if tokens[0] in {"head", "tail"}:
        return all(
            token in {"-n"} or token.isdigit() or (token.startswith("-") and token[1:].isdigit())
            for token in tokens[1:]
        )
    return is_read_only_command(tokens, cwd)


def is_safe_ps_command(tokens: list[str]) -> bool:
    return len(tokens) >= 2 and tokens[0] == "ps" and tuple(tokens[1:]) in PS_ARG_SETS


def is_safe_process_filter(tokens: list[str]) -> bool:
    if len(tokens) < 2 or tokens[0] not in {"grep", "rg"}:
        return False
    pattern_index = first_pattern_index(tokens)
    if pattern_index is None:
        return False
    pattern = tokens[pattern_index].lower()
    if len(pattern) > 512 or any(term in pattern for term in PROCESS_PROBE_DENIED_TERMS):
        return False
    alternatives = split_regex_alternatives(pattern)
    return bool(alternatives) and all(
        any(term in alternative for term in PROCESS_PROBE_TERMS)
        for alternative in alternatives
    )


def split_regex_alternatives(pattern: str) -> list[str]:
    parts: list[str] = []
    start = 0
    escaped = False
    for index, char in enumerate(pattern):
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == "|":
            parts.append(pattern[start:index].strip())
            start = index + 1
    parts.append(pattern[start:].strip())
    return [part for part in parts if part]


def is_lsof_probe(tokens: list[str]) -> bool:
    return tuple(tokens) in {
        ("lsof", "-iTCP:4343", "-sTCP:LISTEN"),
        ("lsof", "/tmp/cooper-gotest.lock"),
    }


def is_direct_ps_probe(tokens: list[str]) -> bool:
    return (
        len(tokens) == 3
        and tokens[:2] == ["ps", "-fp"]
        and bool(re.match(r"^\d+(?:,\d+)*$", tokens[2]))
    )


def is_pgrep_probe(tokens: list[str]) -> bool:
    return (
        len(tokens) == 3
        and tokens[:2] == ["pgrep", "-af"]
        and is_safe_process_filter(["rg", tokens[2]])
    )


def is_tmp_mkdir(tokens: list[str]) -> bool:
    return (
        len(tokens) >= 3
        and tokens[:2] == ["mkdir", "-p"]
        and all(is_safe_tmp_output(path) for path in tokens[2:])
    )


def is_release_mkdir(tokens: list[str], cwd: Path) -> bool:
    if len(tokens) != 3 or tokens[:2] != ["mkdir", "-p"]:
        return False
    version = project_version("cooper")
    if version is None:
        return False
    target = resolve_safe_path(tokens[2], cwd, allow_tmp=False)
    expected = REPO_ROOT / "dist" / "cooper" / f"v{version}"
    return target == expected


def is_read_only_command(tokens: list[str], cwd: Path) -> bool:
    if not tokens or tokens[0] not in READ_ONLY_COMMANDS:
        return False
    if tokens[0] == "find" and any(token in FIND_UNSAFE_TOKENS for token in tokens):
        return False
    if tokens[0] == "sed" and any(token == "-i" or token.startswith("-i") for token in tokens):
        return False
    if tokens[0] == "tail" and any(token in TAIL_UNSAFE_TOKENS for token in tokens):
        return False
    if tokens[0] == "date" and any(token in DATE_UNSAFE_TOKENS or token.startswith("--set=") for token in tokens):
        return False
    if tokens[0] == "sort" and any(token in SORT_UNSAFE_TOKENS or token.startswith("--output=") for token in tokens):
        return False
    if tokens[0] == "rg" and any(
        token in RG_UNSAFE_OPTIONS or any(token.startswith(option + "=") for option in RG_UNSAFE_OPTIONS)
        for token in tokens
    ):
        return False

    ignored = ignored_pattern_indices(tokens)
    if tokens[0] == "find":
        ignored.update(ignored_value_indices(tokens, FIND_PATTERN_OPTIONS))
    return are_paths_safe(
        tokens,
        cwd,
        ignored_indices=ignored,
        require_path=tokens[0] in READ_ONLY_NEEDS_PATH,
    )


def are_paths_safe(
    tokens: list[str],
    cwd: Path,
    *,
    ignored_indices: set[int] | None = None,
    require_path: bool = False,
) -> bool:
    ignored_indices = ignored_indices or set()
    found_path = False
    for index, token in enumerate(tokens):
        if index in ignored_indices:
            continue
        if has_sensitive_path_mention(token) or has_any_url_scheme(token):
            return False
        if has_shell_expansion(token):
            return False
        for path in path_candidates(token, cwd):
            found_path = True
            if resolve_safe_path(path, cwd) is None:
                return False
    return found_path or not require_path


def path_candidates(token: str, cwd: Path) -> list[str]:
    if not token or token in {"-", "--"}:
        return []
    if "=" in token and token.startswith("-"):
        value = token.split("=", 1)[1]
        return [value] if looks_like_path(value, cwd) else []
    return [token] if looks_like_path(token, cwd) else []


def looks_like_path(value: str, cwd: Path) -> bool:
    if not value:
        return False
    if value in {".", ".."} or value.startswith(("/", "./", "../", "~")) or "/" in value:
        return True
    try:
        if (cwd / value).exists():
            return True
    except OSError:
        return False
    return Path(value).suffix in {
        ".css",
        ".go",
        ".html",
        ".js",
        ".json",
        ".jsonl",
        ".log",
        ".md",
        ".mjs",
        ".sh",
        ".sql",
        ".toml",
        ".ts",
        ".txt",
        ".yaml",
        ".yml",
    }


def has_shell_expansion(token: str) -> bool:
    return any(char in token for char in SHELL_EXPANSION_CHARS)


def resolve_command_path(value: str, cwd: Path) -> Path | None:
    if not value or ("/" not in value and not value.startswith(".")):
        return None
    return resolve_safe_path(value, cwd, allow_tmp=False)


def resolve_safe_path(
    value: str,
    cwd: Path,
    *,
    allow_tmp: bool = True,
) -> Path | None:
    if has_sensitive_path_mention(value) or has_shell_expansion(value):
        return None
    try:
        path = Path(value).expanduser()
        if not path.is_absolute():
            path = cwd / path
        path = path.resolve(strict=False)
    except (OSError, RuntimeError, ValueError):
        return None
    if is_relative_to(path, REPO_ROOT):
        return path
    if allow_tmp and is_relative_to(path, TMP_ROOT):
        return path
    return None


def is_safe_tmp_output(value: str) -> bool:
    if not value or value == "/tmp" or has_sensitive_path_mention(value) or has_shell_expansion(value):
        return False
    try:
        path = Path(value).expanduser()
        if not path.is_absolute():
            return False
        path = path.resolve(strict=False)
    except (OSError, RuntimeError, ValueError):
        return False
    return is_relative_to(path, TMP_ROOT)


def has_sensitive_path_mention(value: str) -> bool:
    lower = value.lower()
    if any(part in lower for part in SENSITIVE_PATH_SUBSTRINGS):
        return True
    try:
        parts = [part.lower() for part in Path(value).expanduser().parts]
    except (OSError, RuntimeError, ValueError):
        return True
    return any(part in SENSITIVE_PATH_NAMES for part in parts)


def has_any_url_scheme(value: str) -> bool:
    return bool(re.match(r"^[A-Za-z][A-Za-z0-9+.-]*://", value))


def is_relative_to(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent.resolve(strict=False))
        return True
    except ValueError:
        return False


def is_safe_duration(value: str, maximum_seconds: int) -> bool:
    match = re.match(r"^(\d+)(ms|s|m|h)$", value)
    if match is None:
        return False
    number = int(match.group(1))
    multiplier = {"ms": 0.001, "s": 1, "m": 60, "h": 3600}[match.group(2)]
    seconds = number * multiplier
    return 0 < seconds <= maximum_seconds


def split_option(token: str) -> tuple[str, str | None]:
    if token.startswith("-") and "=" in token:
        option, value = token.split("=", 1)
        return option, value
    if token.startswith("-") and not token.startswith("--") and len(token) > 2:
        option = token[:2]
        if option in GO_OUTPUT_OPTIONS | GO_VALUE_OPTIONS | {"-C"}:
            return option, token[2:]
    return token, None


def option_value(
    tokens: list[str],
    index: int,
    attached: str | None,
) -> tuple[str | None, int]:
    if attached is not None:
        return attached, 1
    if index + 1 >= len(tokens):
        return None, 1
    return tokens[index + 1], 2


def ignored_value_indices(tokens: list[str], options: set[str]) -> set[int]:
    ignored: set[int] = set()
    for index, token in enumerate(tokens[:-1]):
        if token in options:
            ignored.add(index + 1)
    return ignored


def ignored_pattern_indices(tokens: list[str]) -> set[int]:
    pattern_index = first_pattern_index(tokens)
    return {pattern_index} if pattern_index is not None else set()


def first_pattern_index(tokens: list[str]) -> int | None:
    if not tokens:
        return None
    command = tokens[0]
    if command in {"grep", "rg"}:
        index = 1
        while index < len(tokens):
            token = tokens[index]
            if token in {"-g", "--glob", "-t", "--type"}:
                index += 2
                continue
            if not token.startswith("-"):
                return index
            index += 1
    if command == "sed":
        for index, token in enumerate(tokens[1:], start=1):
            if token == "-n" or token.startswith("-"):
                continue
            return index
    if command == "jq":
        for index, token in enumerate(tokens[1:], start=1):
            if not token.startswith("-"):
                return index
    return None


if __name__ == "__main__":
    raise SystemExit(main())
