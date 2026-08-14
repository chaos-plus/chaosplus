#!/usr/bin/env python3
"""Run the repository quality gates on Windows, macOS, and Linux."""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence


ROOT = Path(__file__).resolve().parents[4]
SCRIPT_DIR = Path(__file__).resolve().parent
SKIP_PARTS = {".git", ".local", "node_modules", "vendor"}
GO_MODULES = (Path("apps/server"), Path("apps/server-ai"))
SUPPORTED_LOCALES = ("en-US", "zh-CN", "ms-MY")
FORMAT_PATTERN = re.compile(r"%(?:\[[0-9]+\])?[+#0\- ]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[a-zA-Z%]")
DIRECT_ERROR_PATTERN = re.compile(
    r'(?:huma\.(?:NewError|Error[A-Za-z0-9]+)|respx\.(?:Err|WriteError))\([^\r\n]*?"([a-z][a-z0-9_]*)"'
)
OAUTH_ERROR_PATTERN = re.compile(r'oauthError\([^\r\n]*?"([a-z][a-z0-9_]*)"')
FORBIDDEN_EXTERNAL_IAM = re.compile(r"\b(zitadel|spicedb|authzed|ory)\b", re.IGNORECASE)


@dataclass(frozen=True)
class GoTool:
    name: str
    install: str
    version_args: tuple[str, ...]
    expected: str


STATICCHECK = GoTool(
    "staticcheck",
    "honnef.co/go/tools/cmd/staticcheck@v0.7.0",
    ("-version",),
    "v0.7.0",
)
GOLANGCI_LINT = GoTool(
    "golangci-lint",
    "github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8",
    ("version",),
    "v1.64.8",
)
GOVULNCHECK = GoTool(
    "govulncheck",
    "golang.org/x/vuln/cmd/govulncheck@v1.6.0",
    ("-version",),
    "v1.6.0",
)


def repository_files(root: Path, suffix: str | None = None) -> Iterable[Path]:
    for path in root.rglob("*"):
        if not path.is_file() or any(part in SKIP_PARTS for part in path.parts):
            continue
        if suffix is None or path.name.endswith(suffix):
            yield path


def changed_paths(root: Path) -> list[str]:
    result: list[str] = []
    for command in (
        ("git", "diff", "--name-only", "HEAD"),
        ("git", "ls-files", "--others", "--exclude-standard"),
    ):
        completed = subprocess.run(command, cwd=root, text=True, capture_output=True, check=False)
        if completed.returncode == 0:
            result.extend(line for line in completed.stdout.splitlines() if line)
    return sorted(set(result))


def resolve_scopes(scope: str, paths: Sequence[str]) -> dict[str, bool]:
    if scope == "all":
        return {"backend": True, "frontend": True, "docs": True}
    if scope != "auto":
        return {
            "backend": scope == "backend",
            "frontend": scope == "frontend",
            "docs": scope == "docs",
        }
    if not paths:
        return {"backend": True, "frontend": True, "docs": True}

    normalized = [path.replace("\\", "/") for path in paths]
    shared = any(
        path == "AGENTS.md"
        or path.startswith(".rules/")
        or path.startswith(".github/workflows/")
        or path.startswith(".claude/skills/dev-engineering/")
        or path.startswith(".claude/skills/dev-quality-gate/")
        for path in normalized
    )
    return {
        "backend": shared
        or any(
            path.startswith(("apps/server/", "apps/server-ai/", "apps/runner/", ".claude/skills/dev-backend/"))
            for path in normalized
        ),
        "frontend": shared
        or any(
            path.startswith(("apps/admin/", "apps/admin-ai/", ".claude/skills/dev-frontend/", ".claude/skills/dev-ui-ux/"))
            for path in normalized
        ),
        "docs": shared
        or any(
            path in {"README.md", "CONTRIBUTING.md"}
            or path.startswith(("apps/docs/", "docs/", ".claude/skills/dev-docs/"))
            for path in normalized
        ),
    }


def parse_coverage_profile(path: Path) -> tuple[int, int, float]:
    total = 0
    covered = 0
    lines = path.read_text(encoding="utf-8").splitlines()
    for line in lines[1:]:
        fields = line.rsplit(maxsplit=2)
        if len(fields) != 3:
            raise ValueError(f"invalid Go coverage row: {line}")
        statements = int(fields[1])
        count = int(fields[2])
        total += statements
        if count > 0:
            covered += statements
    percentage = (100.0 * covered / total) if total else 0.0
    return covered, total, percentage


def test_sibling_errors(root: Path) -> list[str]:
    errors: list[str] = []
    for test in repository_files(root, "_test.go"):
        production = test.with_name(test.name.removesuffix("_test.go") + ".go")
        if not production.is_file():
            errors.append(
                f"Go test requires exact sibling production file: {test.relative_to(root)} -> {production.name}"
            )
    return errors


def locale_errors(root: Path) -> list[str]:
    errors: list[str] = []
    server = root / "apps/server"
    base_catalog_path = server / "pkg/i18n/locales/en-US.json"
    if not base_catalog_path.is_file():
        return [f"base locale is missing: {base_catalog_path.relative_to(root)}"]
    base_error_keys = set(json.loads(base_catalog_path.read_text(encoding="utf-8")))
    module_error_keys: dict[str, set[str]] = {}

    modules_root = server / "internal/modules"
    for module in sorted(path for path in modules_root.iterdir() if path.is_dir()):
        registration = module / "i18n.go"
        if not registration.is_file():
            errors.append(f"Module i18n registration is missing: {registration.relative_to(root)}")

        catalogs: dict[str, dict[str, str]] = {}
        for locale in SUPPORTED_LOCALES:
            locale_file = module / "i18n/locales" / f"{locale}.json"
            if not locale_file.is_file():
                errors.append(f"Module locale is missing: {locale_file.relative_to(root)}")
                continue
            try:
                raw = json.loads(locale_file.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError) as error:
                errors.append(f"Module locale is invalid JSON: {locale_file.relative_to(root)}: {error}")
                continue
            if not isinstance(raw, dict):
                errors.append(f"Module locale must be an object: {locale_file.relative_to(root)}")
                continue
            catalogs[locale] = {str(key): str(value) for key, value in raw.items()}
            for key, value in catalogs[locale].items():
                if not value.strip():
                    errors.append(f"Module locale has an empty translation: {locale_file.relative_to(root)} -> {key}")

        base = catalogs.get("en-US")
        if base is None:
            continue
        module_error_keys[module.name] = set(base)
        for locale in ("zh-CN", "ms-MY"):
            translated = catalogs.get(locale)
            if translated is None:
                continue
            for key in sorted(set(base) - set(translated)):
                errors.append(f"Module locale is missing key: {module.name}/{locale} -> {key}")
            for key in sorted(set(translated) - set(base)):
                errors.append(f"Module locale has an extra key: {module.name}/{locale} -> {key}")
            for key in sorted(set(base) & set(translated)):
                if sorted(FORMAT_PATTERN.findall(base[key])) != sorted(FORMAT_PATTERN.findall(translated[key])):
                    errors.append(f"Module locale format placeholders differ: {module.name}/{locale} -> {key}")

    production_roots = (server / "internal", server / "pkg")
    for production_root in production_roots:
        for path in repository_files(production_root, ".go"):
            if path.name.endswith("_test.go"):
                continue
            source = path.read_text(encoding="utf-8")
            match = re.search(r"[\\/]internal[\\/]modules[\\/]([^\\/]+)[\\/]", str(path))
            owner = match.group(1) if match else ""
            owned = module_error_keys.get(owner, set())
            for key in DIRECT_ERROR_PATTERN.findall(source):
                if key not in base_error_keys and key not in owned:
                    errors.append(f"Public error key has no owning locale: {path.relative_to(root)} -> {key}")
            oauth_keys = module_error_keys.get("oauth", set())
            for key in OAUTH_ERROR_PATTERN.findall(source):
                normalized = "oauth_" + key
                if normalized not in oauth_keys:
                    errors.append(f"OAuth protocol error key has no owning locale: {path.relative_to(root)} -> {normalized}")
    return errors


def external_iam_errors(root: Path) -> list[str]:
    server = root / "apps/server"
    files: list[Path] = [server / "go.mod", server / "go.sum"]
    deploy = server / "deploy"
    if deploy.is_dir():
        files.extend(
            path
            for path in repository_files(deploy)
            if not path.name.endswith(".env") and "secrets" not in path.parts
        )
    for source_root in (server / "cmd", server / "internal", server / "pkg"):
        if source_root.is_dir():
            files.extend(path for path in repository_files(source_root, ".go") if not path.name.endswith("_test.go"))

    errors: list[str] = []
    for path in files:
        if not path.is_file():
            continue
        for line_number, line in enumerate(path.read_text(encoding="utf-8", errors="replace").splitlines(), 1):
            if FORBIDDEN_EXTERNAL_IAM.search(line):
                errors.append(f"Forbidden external IAM/PDP dependency in {path.relative_to(root)}:{line_number}")
    return errors


def structure_errors(root: Path) -> list[str]:
    errors = test_sibling_errors(root)
    legacy_database_key = f"{root.name.upper()}_DATABASE_TYPE"
    app_root = root / "apps/server/internal/app"
    for path in repository_files(app_root):
        if path.suffix.lower() in {".yaml", ".yml"}:
            errors.append(f"YAML is forbidden under apps/server/internal/app: {path.relative_to(root)}")
    for path in repository_files(root / "apps/server"):
        if path.name in {"README.md"} or not path.is_file():
            continue
        if legacy_database_key in path.read_text(encoding="utf-8", errors="replace"):
            errors.append(f"Forbidden legacy database environment key: {path.relative_to(root)}")
    errors.extend(external_iam_errors(root))
    errors.extend(locale_errors(root))
    return errors


class Gate:
    def __init__(self, root: Path, full: bool) -> None:
        self.root = root
        self.full = full
        self.failures: list[str] = []

    def step(
        self,
        label: str,
        command: Sequence[str],
        cwd: Path | None = None,
        env: dict[str, str] | None = None,
    ) -> bool:
        print(f"\n== {label} ==", flush=True)
        try:
            completed = subprocess.run(
                command,
                cwd=cwd or self.root,
                env=env,
                check=False,
            )
        except OSError as error:
            self.failures.append(f"{label}: {error}")
            return False
        if completed.returncode != 0:
            self.failures.append(f"{label}: {' '.join(command)} exited with {completed.returncode}")
            return False
        return True

    def common(self) -> None:
        self.step("Refresh repository facts", (sys.executable, str(SCRIPT_DIR / "skill-runtime.py"), "refresh"))
        self.step("Validate test dependency policy", (sys.executable, str(SCRIPT_DIR / "skill-runtime.py"), "test-policy"))
        print("\n== Repository structure ==", flush=True)
        self.failures.extend(structure_errors(self.root))
        self.step(
            "Architecture contract",
            (sys.executable, str(self.root / ".claude/skills/dev-engineering/scripts/check_architecture_contract.py")),
        )
        self.step(
            "Schema contract",
            (sys.executable, str(self.root / ".claude/skills/dev-engineering/scripts/check_schema_contract.py")),
        )
        self.step("Validate repository skills", (sys.executable, str(SCRIPT_DIR / "skill-runtime.py"), "validate"))
        self.step(
            "Test repository skill runtime",
            (sys.executable, "-m", "unittest", "discover", "-s", str(SCRIPT_DIR), "-p", "test_*.py"),
        )

    def capture(self, command: Sequence[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
        return subprocess.run(command, cwd=cwd or self.root, text=True, capture_output=True, check=False)

    def go_bin(self, name: str) -> Path:
        gobin = self.capture(("go", "env", "GOBIN")).stdout.strip()
        if not gobin:
            gopath = self.capture(("go", "env", "GOPATH")).stdout.strip().split(os.pathsep)[0]
            gobin = str(Path(gopath) / "bin")
        suffix = ".exe" if os.name == "nt" else ""
        return Path(gobin) / f"{name}{suffix}"

    def ensure_go_tool(self, tool: GoTool) -> str | None:
        candidate = shutil.which(tool.name)
        if candidate:
            version = self.capture((candidate, *tool.version_args))
            if version.returncode == 0 and tool.expected in version.stdout + version.stderr:
                return candidate
        if not self.step(f"Install {tool.name} {tool.expected}", ("go", "install", tool.install)):
            return None
        installed = self.go_bin(tool.name)
        version = self.capture((str(installed), *tool.version_args))
        if version.returncode != 0 or tool.expected not in version.stdout + version.stderr:
            self.failures.append(f"{tool.name} did not report required version {tool.expected}")
            return None
        return str(installed)

    def check_go_format(self) -> None:
        go_files = [str(path) for path in repository_files(self.root, ".go")]
        unformatted: list[str] = []
        for index in range(0, len(go_files), 200):
            result = self.capture(("gofmt", "-l", *go_files[index : index + 200]))
            if result.returncode != 0:
                self.failures.append(f"gofmt exited with {result.returncode}: {result.stderr.strip()}")
                return
            unformatted.extend(result.stdout.splitlines())
        self.failures.extend(f"Go file is not formatted: {path}" for path in unformatted)

    def backend(self) -> None:
        environment = os.environ.copy()
        environment["GOSUMDB"] = "sum.golang.org"
        staticcheck = self.ensure_go_tool(STATICCHECK)
        golangci = self.ensure_go_tool(GOLANGCI_LINT)
        govuln = self.ensure_go_tool(GOVULNCHECK) if self.full else None
        self.check_go_format()

        for module in GO_MODULES:
            module_root = self.root / module
            label = module.as_posix()
            self.step(f"Go module verify ({label})", ("go", "mod", "verify"), module_root, environment)
            self.step(f"Go vet ({label})", ("go", "vet", "./..."), module_root, environment)
            if staticcheck:
                self.step(f"Go staticcheck ({label})", (staticcheck, "./..."), module_root, environment)
            if golangci:
                self.step(f"Go static analysis ({label})", (golangci, "run", "./..."), module_root, environment)
            if self.full:
                with tempfile.TemporaryDirectory(prefix="quality-gate-coverage-") as temporary:
                    profile = Path(temporary) / "coverage.out"
                    passed = self.step(
                        f"Go race tests ({label})",
                        ("go", "test", "-race", "-count=1", "-covermode=atomic", f"-coverprofile={profile}", "./..."),
                        module_root,
                        environment,
                    )
                    if passed:
                        covered, total, percentage = parse_coverage_profile(profile)
                        print(f"repository coverage ({label}): {covered}/{total} ({percentage:.6f}%)")
                        if percentage < 90.0:
                            self.failures.append(
                                f"Go coverage ({label}) is {percentage:.6f}%, below required 90%"
                            )
                if govuln:
                    self.step(f"Go vulnerability scan ({label})", (govuln, "./..."), module_root, environment)
            else:
                self.step(f"Go tests ({label})", ("go", "test", "-count=1", "./..."), module_root, environment)

        runner = self.root / "apps/runner"
        self.step("Runner install", ("bun", "install", "--frozen-lockfile"), runner)
        self.step("Runner typecheck", ("bun", "run", "typecheck"), runner)
        self.step("Runner tests", ("bun", "test"), runner)

    def frontend(self) -> None:
        frontend = self.root / "apps/admin-ai"
        for name in ("Dockerfile", "nginx.conf"):
            if not (frontend / name).is_file():
                self.failures.append(f"Frontend deployment file is missing: apps/admin-ai/{name}")
        self.step("Frontend install", ("bun", "install", "--frozen-lockfile"), frontend)
        for script, label in (
            ("lint", "Frontend lint"),
            ("typecheck", "Frontend typecheck"),
            ("test", "Frontend tests"),
            ("build", "Frontend production build"),
        ):
            self.step(label, ("bun", "run", script), frontend)

    def docs(self) -> None:
        docs = self.root / "apps/docs"
        self.step("Documentation install", ("bun", "install", "--frozen-lockfile"), docs)
        self.step("Documentation source sync", ("bun", "run", "sync"), docs)
        self.step("Documentation checks", ("bun", "run", "check"), docs)
        protected = os.environ.copy()
        protected.setdefault("DOC_PASSWORD", "quality-gate-local-verification")
        self.step("Documentation production build", ("bun", "run", "build"), docs, protected)
        self.step("Documentation protected output", ("bun", "run", "check:protected"), docs, protected)
        if self.full:
            portable = protected.copy()
            portable["DOC_BUILD_TARGET"] = "portable"
            self.step("Documentation portable build", ("bun", "run", "build:portable"), docs, portable)
            self.step("Documentation portable protected output", ("bun", "run", "check:protected"), docs, portable)

    def finish(self) -> int:
        if self.failures:
            print(f"\nQuality gate failed ({len(self.failures)}):", file=sys.stderr)
            for failure in self.failures:
                print(f"- {failure}", file=sys.stderr)
            return 1
        print("\nQuality gate passed.")
        return 0


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scope", choices=("auto", "backend", "frontend", "docs", "all"), default="auto")
    parser.add_argument("--full", action="store_true", help="run release-level race, coverage, vulnerability, and portable docs gates")
    parser.add_argument("--changed-path", action="append", default=[], help="override auto-scope path detection; repeat as needed")
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    paths = args.changed_path or changed_paths(ROOT)
    scopes = resolve_scopes(args.scope, paths)
    selected = ", ".join(name for name, enabled in scopes.items() if enabled) or "common-only"
    print(f"Quality gate scope: {selected}; full={args.full}")
    gate = Gate(ROOT, args.full)
    gate.common()
    if scopes["backend"]:
        gate.backend()
    if scopes["frontend"]:
        gate.frontend()
    if scopes["docs"]:
        gate.docs()
    return gate.finish()


if __name__ == "__main__":
    raise SystemExit(main())
