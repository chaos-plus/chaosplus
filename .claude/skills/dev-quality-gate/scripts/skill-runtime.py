#!/usr/bin/env python3
"""仓库 Skill 校验、事实刷新和受控学习的跨平台运行时。"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator, TextIO


NAME_PATTERN = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
FRONTMATTER_PATTERN = re.compile(r"\A---\r?\n(.*?)\r?\n---(?:\r?\n|\Z)", re.DOTALL)
CJK_PATTERN = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff]")
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
    elif not name.startswith("dev-"):
        errors.append(f"{skill_file}: repository skill names must use the dev- prefix")

    if not description:
        errors.append(f"{skill_file}: missing description")
    elif len(description) > 1024:
        errors.append(f"{skill_file}: description exceeds 1024 characters")
    elif "<" in description or ">" in description:
        errors.append(f"{skill_file}: description cannot contain angle brackets")
    elif CJK_PATTERN.search(description) is None:
        errors.append(f"{skill_file}: description must be written in Chinese")

    if CJK_PATTERN.search(skill_file.read_text(encoding="utf-8-sig")) is None:
        errors.append(f"{skill_file}: skill instructions must be written in Chinese")

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
            "allow_implicit_invocation": r"(?m)^  allow_implicit_invocation:\s*true\s*$",
        }
        for field, pattern in required_patterns.items():
            if not re.search(pattern, agent_source):
                errors.append(f"{agent_file}: missing or invalid {field!r}")
        for field in ("display_name", "short_description", "default_prompt"):
            match = re.search(rf'(?m)^  {field}:\s*"([^"\r\n]+)"\s*$', agent_source)
            if match is not None and CJK_PATTERN.search(match.group(1)) is None:
                errors.append(f"{agent_file}: {field!r} must be written in Chinese")
    return errors


def validate_skills(skills_root: Path, skill_dirs: list[Path] | None = None) -> list[str]:
    selected = skill_dirs or sorted(path for path in skills_root.iterdir() if path.is_dir())
    errors = [error for skill_dir in selected for error in validate_skill(skill_dir)]
    repo_root = skills_root.parents[1]
    brand_terms = repository_brand_terms(repo_root)
    for skill_dir in selected:
        scan_files = [skill_dir / "SKILL.md", skill_dir / "agents/openai.yaml"]
        scan_files.extend(sorted((skill_dir / "scripts").glob("*")) if (skill_dir / "scripts").is_dir() else [])
        scan_files.extend(
            path
            for path in sorted((skill_dir / "references").glob("*"))
            if path.name != "repository-facts.md"
        )
        errors.extend(find_hard_coded_brand_terms(scan_files, brand_terms))

    policy_files = [repo_root / "AGENTS.md"]
    policy_root = repo_root / ".rules"
    if policy_root.is_dir():
        policy_files.extend(sorted(path for path in policy_root.rglob("*") if path.is_file()))
    errors.extend(find_hard_coded_brand_terms(policy_files, brand_terms))
    errors.extend(validate_skill_discovery(repo_root, selected))
    return errors


def validate_skill_discovery(repo_root: Path, skill_dirs: list[Path]) -> list[str]:
    errors: list[str] = []
    legacy_root = repo_root / ".codex" / "skills"
    if legacy_root.exists() or legacy_root.is_symlink():
        errors.append(f"{legacy_root}: legacy repository skill root must not exist")

    discovery_root = repo_root / ".agents" / "skills"
    for skill_dir in skill_dirs:
        if skill_dir.parent.resolve() != (repo_root / ".claude" / "skills").resolve():
            continue
        link = discovery_root / skill_dir.name
        if not is_canonical_skill_link(repo_root, link, skill_dir):
            errors.append(f"{link}: repository skill discovery entry must be a symbolic link")
    return errors


def is_canonical_skill_link(repo_root: Path, link: Path, target: Path) -> bool:
    if link.is_symlink():
        try:
            return link.resolve(strict=True) == target.resolve(strict=True)
        except FileNotFoundError:
            return False
    if not link.is_file() or git_config_symlinks(repo_root):
        return False
    expected = Path("../..") / target.relative_to(repo_root)
    if link.read_text(encoding="utf-8").strip().replace("\\", "/") != expected.as_posix():
        return False
    result = subprocess.run(
        ["git", "ls-files", "-s", "--", link.relative_to(repo_root).as_posix()],
        cwd=repo_root,
        text=True,
        capture_output=True,
        check=False,
    )
    return result.returncode == 0 and result.stdout.startswith("120000 ")


def git_config_symlinks(repo_root: Path) -> bool:
    result = subprocess.run(
        ["git", "config", "--bool", "core.symlinks"],
        cwd=repo_root,
        text=True,
        capture_output=True,
        check=False,
    )
    return result.returncode == 0 and result.stdout.strip() == "true"


def find_hard_coded_brand_terms(paths: list[Path], brand_terms: set[str]) -> list[str]:
    errors: list[str] = []
    for path in paths:
        if not path.is_file():
            continue
        try:
            source = path.read_text(encoding="utf-8-sig").casefold()
        except UnicodeDecodeError:
            continue
        for term in brand_terms:
            if term in source:
                errors.append(f"{path}: repository policy hard-codes brand term {term!r}")
    return errors


def repository_brand_terms(repo_root: Path) -> set[str]:
    terms: set[str] = set()
    root_name = repo_root.name.casefold().strip()
    if len(root_name) >= 4:
        terms.add(root_name)
    go_mod = repo_root / "apps/server/go.mod"
    if go_mod.is_file():
        match = re.search(r"(?m)^module\s+(\S+)", go_mod.read_text(encoding="utf-8-sig"))
        if match:
            leaf = match.group(1).rstrip("/").rsplit("/", 1)[-1].casefold()
            if len(leaf) >= 4:
                terms.add(leaf)
    return terms


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

    admin = _read_json(repo_root / "apps" / "admin-ai" / "package.json")
    admin_app = _read_json(repo_root / "apps" / "admin-ai" / "apps" / "platform" / "package.json")
    docs = _read_json(repo_root / "apps" / "docs" / "package.json")
    workspaces = ", ".join(str(value) for value in admin["workspaces"])

    return f"""# Dev 仓库事实

本文件由仓库 manifest 和 source tree 自动生成，严禁手工编辑。

## 后端

- Go module：`{module_match.group(1)}`
- Go 版本：`{version_match.group(1)}`
- Go package 目录数：{len(go_packages)}
- 功能模块：{', '.join(modules)}
- IAM SQL 方言：{', '.join(dialects)}
- HTTP 框架：基于 chi 的 Huma v2
- 持久化：Bun + Goose
- 主配置：`apps/server/internal/app/config.go`
- 组合根：`apps/server/internal/app`

## 前端

- Workspace：`{admin['name']}`
- 包管理器：`{admin['packageManager']}`
- Workspaces：{workspaces}
- 应用：`apps/admin-ai/apps/platform`（React、Vite、TypeScript）
- 共享 UI：`apps/admin/packages/ui`
- 应用测试命令：`{admin_app['scripts']['test']}`

## 文档

- Package：`{docs['name']}`
- 站点：`apps/docs`
- 生成器：Astro {docs['dependencies']['astro']}
- 主题：Starlight {docs['dependencies']['@astrojs/starlight']}
- 权威工程来源：`README.md` 和 `apps/docs/*.md`

## 强制不变量

- SQLite、MySQL、PostgreSQL 数据库配置使用 `type` 加 `dsn` 或 `dsn_file`。
- `internal/app` 下严禁 YAML。
- 每个 Go `name_test.go` 必须有同目录 `name.go`。
- 测试使用真实依赖和真实 listener；严禁 mock、fake、stub 和 miniredis。
- Go 完整验收覆盖率至少 90%。
- 业务层级为 tenant -> entity -> business resources。
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
            print("Skill 校验失败：", file=sys.stderr)
            for error in errors:
                print(f"- {error}", file=sys.stderr)
            return 1
        count = len(selected) if selected is not None else len(list((repo_root / ".claude" / "skills").iterdir()))
        print(f"已校验 {count} 个仓库 Skill。")
        return 0
    if args.command == "refresh":
        changed = refresh_context(args.repo_root.resolve())
        print("仓库事实已更新。" if changed else "仓库事实已是最新。")
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
