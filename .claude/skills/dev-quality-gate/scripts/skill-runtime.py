#!/usr/bin/env python3
"""Cross-platform runtime for repository skill validation and learning."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import tempfile
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator, TextIO


NAME_PATTERN = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
FRONTMATTER_PATTERN = re.compile(r"\A---\r?\n(.*?)\r?\n---(?:\r?\n|\Z)", re.DOTALL)
ALLOWED_FIELDS = {"name", "description"}
DOMAINS = {"backend", "frontend", "docs", "quality-gate"}
SECRET_PATTERNS = (
    re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----", re.IGNORECASE),
    re.compile(r"\b(password|secret|token|api[_-]?key)\s*[:=]\s*[^\s*]+", re.IGNORECASE),
    re.compile(r"\bBearer\s+[A-Za-z0-9._~+/-]+=*", re.IGNORECASE),
    re.compile(r"\b[a-z][a-z0-9+.-]*://[^\s/:]+:[^\s/@]+@", re.IGNORECASE),
)


def repository_root() -> Path:
    return Path(__file__).resolve().parents[4]


def parse_frontmatter(skill_file: Path) -> tuple[dict[str, str] | None, str | None]:
    source = skill_file.read_text(encoding="utf-8-sig")
    match = FRONTMATTER_PATTERN.match(source)
    if match is None:
        return None, "missing or malformed YAML frontmatter"

    metadata: dict[str, str] = {}
    for line_number, line in enumerate(match.group(1).splitlines(), start=2):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if line[:1].isspace() or ":" not in line:
            return None, f"unsupported frontmatter syntax at line {line_number}"
        key, value = line.split(":", 1)
        key = key.strip()
        value = value.strip()
        if key in metadata:
            return None, f"duplicate frontmatter field: {key}"
        if key not in ALLOWED_FIELDS:
            return None, f"unexpected frontmatter field: {key}"
        metadata[key] = value
    return metadata, None


def validate_skill(skill_dir: Path) -> list[str]:
    errors: list[str] = []
    skill_file = skill_dir / "SKILL.md"
    if not skill_file.is_file():
        return [f"{skill_dir}: SKILL.md not found"]

    metadata, parse_error = parse_frontmatter(skill_file)
    if parse_error is not None:
        return [f"{skill_file}: {parse_error}"]
    assert metadata is not None

    name = metadata.get("name", "")
    description = metadata.get("description", "")
    if not name:
        errors.append(f"{skill_file}: missing name")
    elif not NAME_PATTERN.fullmatch(name):
        errors.append(f"{skill_file}: name must use hyphen-case")
    elif len(name) > 64:
        errors.append(f"{skill_file}: name exceeds 64 characters")
    elif name != skill_dir.name:
        errors.append(f"{skill_file}: name must match directory {skill_dir.name!r}")

    if not description:
        errors.append(f"{skill_file}: missing description")
    elif len(description) > 1024:
        errors.append(f"{skill_file}: description exceeds 1024 characters")
    elif "<" in description or ">" in description:
        errors.append(f"{skill_file}: description cannot contain angle brackets")

    agent_file = skill_dir / "agents" / "openai.yaml"
    if not agent_file.is_file():
        errors.append(f"{agent_file}: file not found")
    else:
        agent_source = agent_file.read_text(encoding="utf-8-sig")
        required_patterns = {
            "interface": r"(?m)^interface:\s*$",
            "display_name": r'(?m)^  display_name:\s*"[^"\r\n]+"\s*$',
            "short_description": r'(?m)^  short_description:\s*"[^"\r\n]+"\s*$',
            "default_prompt": rf'(?m)^  default_prompt:\s*"[^"\r\n]*\${re.escape(name)}[^"\r\n]*"\s*$',
        }
        for field, pattern in required_patterns.items():
            if not re.search(pattern, agent_source):
                errors.append(f"{agent_file}: missing or invalid {field!r}")
    return errors


def validate_skills(skills_root: Path, skill_dirs: list[Path] | None = None) -> list[str]:
    selected = skill_dirs or sorted(path for path in skills_root.iterdir() if path.is_dir())
    return [error for skill_dir in selected for error in validate_skill(skill_dir)]


def _read_json(path: Path) -> dict[str, object]:
    return json.loads(path.read_text(encoding="utf-8-sig"))


def render_repository_facts(repo_root: Path) -> str:
    go_mod = (repo_root / "apps" / "server" / "go.mod").read_text(encoding="utf-8-sig")
    module_match = re.search(r"(?m)^module\s+(\S+)", go_mod)
    version_match = re.search(r"(?m)^go\s+(\S+)", go_mod)
    if module_match is None or version_match is None:
        raise ValueError("go.mod must declare module and go version")

    module_root = repo_root / "apps" / "server" / "internal" / "modules"
    modules = sorted(path.name for path in module_root.iterdir() if path.is_dir())
    dialect_root = module_root / "iam" / "sql"
    dialects = sorted(path.name for path in dialect_root.iterdir() if path.is_dir())
    ignored_parts = {".git", ".local", "vendor"}
    go_packages = {
        path.parent
        for path in repo_root.rglob("*.go")
        if not ignored_parts.intersection(path.relative_to(repo_root).parts)
    }

    admin = _read_json(repo_root / "apps" / "admin" / "package.json")
    admin_app = _read_json(repo_root / "apps" / "admin" / "apps" / "web" / "package.json")
    docs = _read_json(repo_root / "docs" / "package.json")
    workspaces = ", ".join(str(value) for value in admin["workspaces"])

    return f"""# Dev Repository Facts

This file is generated from repository manifests and source trees. Do not edit it manually.

## Backend

- Go module: `{module_match.group(1)}`
- Go version: `{version_match.group(1)}`
- Go package directories: {len(go_packages)}
- Feature modules: {', '.join(modules)}
- IAM SQL dialects: {', '.join(dialects)}
- HTTP framework: Huma v2 on chi
- Persistence: Bun plus Goose
- Primary configuration: `apps/server/internal/app/config.go`
- Composition root: `apps/server/internal/app`

## Frontend

- Workspace: `{admin['name']}`
- Package manager: `{admin['packageManager']}`
- Workspaces: {workspaces}
- Application: `apps/admin/apps/web` (React, Vite, TypeScript)
- Shared UI: `apps/admin/packages/ui`
- App test command: `{admin_app['scripts']['test']}`

## Documentation

- Package: `{docs['name']}`
- Site: `docs`
- Generator: Astro {docs['dependencies']['astro']}
- Theme: Starlight {docs['dependencies']['@astrojs/starlight']}
- Authoritative engineering sources: `README.md` and `docs/*.md`

## Required Invariants

- Database configuration uses `type` plus `dsn` or `dsn_file` for SQLite, MySQL, and PostgreSQL.
- No YAML belongs under `internal/app`.
- Every Go `name_test.go` has sibling `name.go`.
- Tests use real dependencies and real listeners; mocks, fakes, stubs, and miniredis are forbidden.
- Full Go acceptance coverage is at least 90%.
- Future business hierarchy is tenant -> entity -> business resources.
"""


def write_if_changed(path: Path, content: str) -> bool:
    normalized = content.replace("\r\n", "\n").rstrip() + "\n"
    existing = path.read_text(encoding="utf-8-sig").replace("\r\n", "\n") if path.exists() else ""
    if existing == normalized:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    handle, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(handle, "w", encoding="utf-8", newline="\n") as temporary:
            temporary.write(normalized)
            temporary.flush()
            os.fsync(temporary.fileno())
        os.replace(temporary_name, path)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)
    return True


def refresh_context(repo_root: Path) -> bool:
    facts_path = repo_root / ".claude" / "skills" / "dev-quality-gate" / "references" / "repository-facts.md"
    return write_if_changed(facts_path, render_repository_facts(repo_root))


def normalize_learning_value(value: str, name: str) -> str:
    normalized = re.sub(r"[\r\n]+", " ", value).strip()
    if len(normalized) < 3:
        raise ValueError(f"{name} must contain a concrete value")
    if len(normalized) > 1000:
        raise ValueError(f"{name} exceeds 1000 characters")
    return normalized


@contextmanager
def locked_text_file(path: Path) -> Iterator[TextIO]:
    handle = path.open("a+", encoding="utf-8", newline="\n")
    try:
        handle.seek(0)
        if os.name == "nt":
            import msvcrt

            msvcrt.locking(handle.fileno(), msvcrt.LK_LOCK, 1)
        else:
            import fcntl

            fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        yield handle
    finally:
        handle.flush()
        os.fsync(handle.fileno())
        handle.seek(0)
        if os.name == "nt":
            import msvcrt

            msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl

            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        handle.close()


def record_learning(
    skills_root: Path,
    domain: str,
    symptom: str,
    cause: str,
    prevention: str,
    evidence: str,
) -> tuple[str, bool]:
    if domain not in DOMAINS:
        raise ValueError(f"unsupported learning domain: {domain}")
    values = (
        normalize_learning_value(symptom, "symptom"),
        normalize_learning_value(cause, "cause"),
        normalize_learning_value(prevention, "prevention"),
        normalize_learning_value(evidence, "evidence"),
    )
    combined = "\n".join(values)
    if any(pattern.search(combined) for pattern in SECRET_PATTERNS):
        raise ValueError("learning text appears to contain a secret; redact it before recording")

    skill_name = "dev-quality-gate" if domain == "quality-gate" else f"dev-{domain}"
    lessons_path = skills_root / skill_name / "references" / "lessons.md"
    if not lessons_path.is_file():
        raise FileNotFoundError(f"lessons file not found: {lessons_path}")
    lesson_id = "L-" + hashlib.sha256(combined.encode()).hexdigest()[:12]
    heading = f"## {lesson_id}"
    with locked_text_file(lessons_path) as lessons:
        source = lessons.read()
        if re.search(rf"(?m)^{re.escape(heading)}$", source):
            return lesson_id, False
        lessons.write(
            f"\n\n{heading}\n\n"
            f"- Symptom: {values[0]}\n"
            f"- Root cause: {values[1]}\n"
            f"- Prevention: {values[2]}\n"
            f"- Evidence: {values[3]}\n"
        )
    return lesson_id, True


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    validate = subparsers.add_parser("validate")
    validate.add_argument("skills", nargs="*")
    refresh = subparsers.add_parser("refresh")
    refresh.add_argument("--repo-root", type=Path, default=repository_root())
    record = subparsers.add_parser("record")
    record.add_argument("--domain", choices=sorted(DOMAINS), required=True)
    for field in ("symptom", "cause", "prevention", "evidence"):
        record.add_argument(f"--{field}", required=True)
    return parser


def main(arguments: list[str]) -> int:
    args = build_parser().parse_args(arguments)
    repo_root = repository_root()
    if args.command == "validate":
        selected = [Path(value).resolve() for value in args.skills] or None
        errors = validate_skills(repo_root / ".claude" / "skills", selected)
        if errors:
            print("Skill validation failed:", file=sys.stderr)
            for error in errors:
                print(f"- {error}", file=sys.stderr)
            return 1
        count = len(selected) if selected is not None else len(list((repo_root / ".claude" / "skills").iterdir()))
        print(f"Validated {count} repository skill(s).")
        return 0
    if args.command == "refresh":
        changed = refresh_context(args.repo_root.resolve())
        print("Repository facts updated." if changed else "Repository facts are current.")
        return 0
    lesson_id, added = record_learning(
        repo_root / ".claude" / "skills",
        args.domain,
        args.symptom,
        args.cause,
        args.prevention,
        args.evidence,
    )
    print(f"Recorded {lesson_id}." if added else f"Lesson {lesson_id} already exists.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
