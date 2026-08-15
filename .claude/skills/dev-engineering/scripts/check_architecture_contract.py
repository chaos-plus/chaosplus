#!/usr/bin/env python3
"""检查禁止的所有权、重复基础设施和模块结构。"""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[4]
SERVER_AI = ROOT / "apps/server-ai/internal"
AGENTS = ROOT / "AGENTS.md"
SKILL = ROOT / ".claude/skills/dev-engineering/SKILL.md"
SKILL_AGENT = ROOT / ".claude/skills/dev-engineering/agents/openai.yaml"
BACKEND_RULE = ROOT / ".rules/3.BACKEND.md"
PRODUCT_RULE = ROOT / ".rules/3.PROTOTYPE.md"
PRD_TEMPLATE = ROOT / ".rules/4.PRD_TEMPLATE.md"
FORBIDDEN_DIRS = (
    ROOT / "apps/server-3rd",
    SERVER_AI / "store",
    SERVER_AI / "server",
    SERVER_AI / "core/id",
    SERVER_AI / "core/idgen",
    SERVER_AI / "core/extension/authn",
    SERVER_AI / "core/extension/authz",
    SERVER_AI / "core/extension/websec",
    SERVER_AI / "infra/idgen",
)
FORBIDDEN_IMPORT = re.compile(r'apps/server-ai/internal/(?:store|server|core/(?:id|idgen)|core/extension/(?:authn|authz|websec)|infra/idgen)')
APP_ALLOWED = re.compile(r'^(?:app|bootstrap|composition|config|health|lifecycle|middleware|ready|routes)(?:_[a-z0-9]+)?(?:_test)?\.go$')
RULE_CONTRACTS = {
    AGENTS: (
        "所有任务类型",
        "严禁等待用户点名",
        "STOP GATE",
        "Before ANY code or file edit",
        "Read the complete relevant owner implementation",
        "Inspected Owner/Consumers",
        "Search first, design second, code third",
        "For IAM or security work",
        "only after this trace proves a concrete gap",
    ),
    SKILL: (
        "全仓统一入口",
        ".rules/3.BACKEND.md",
        "产品价值门禁",
        "停止门禁",
        "dev-quality-gate",
    ),
    SKILL_AGENT: (
        "使用 $dev-engineering",
        "allow_implicit_invocation: true",
    ),
    BACKEND_RULE: (
        "canonical backend contract",
        "Pre-code gate",
        "Inspected Owner/Consumers",
        "Each bounded context",
        "Production filenames describe domain roles",
        "sql/sqlite",
        "guid.ID",
        "UTC Unix milliseconds",
        "Use Huma",
        "apps/server-3rd",
        "Commit and push only",
    ),
    PRODUCT_RULE: (
        "产品价值与真实需求门禁",
        "产品角色不是 IAM 角色",
        "默认研究至少 3 个",
        "严禁编造",
        "等待明确确认",
    ),
    PRD_TEMPLATE: (
        "功能价值映射",
        "竞品与现实替代方案研究",
        "确认来源/日期",
    ),
}

SKILL_LINKS = ROOT / ".agents/skills"

WORKSPACE = SERVER_AI / "modules/workspace"
WORKSPACE_LEAF_FILES = {
    "domain.go",
    "service.go",
    "repository.go",
    "rest.go",
    "module.go",
    "migrate.go",
    "i18n.go",
}
LOCALES = {"en-US.json", "zh-CN.json", "ms-MY.json"}
SQL_DIALECTS = {"sqlite", "mysql", "postgres"}
TECHNICAL_FILENAME = re.compile(r"^(?:bun|huma|goose|sqlc|http)_")


def is_canonical_skill_link(link: Path, target: Path) -> bool:
    if link.is_symlink():
        return link.resolve() == target.resolve()
    if not link.is_file() or git_config_symlinks():
        return False
    expected = Path("../..") / target.relative_to(ROOT)
    if link.read_text(encoding="utf-8").strip().replace("\\", "/") != expected.as_posix():
        return False
    result = subprocess.run(
        ["git", "ls-files", "-s", "--", link.relative_to(ROOT).as_posix()],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    return result.returncode == 0 and result.stdout.startswith("120000 ")


def git_config_symlinks() -> bool:
    result = subprocess.run(
        ["git", "config", "--bool", "core.symlinks"],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    return result.returncode == 0 and result.stdout.strip() == "true"


def main() -> int:
    errors: list[str] = []
    for path, required_fragments in RULE_CONTRACTS.items():
        if not path.is_file():
            errors.append(f"missing mandatory engineering rule file: {path.relative_to(ROOT)}")
            continue
        text = path.read_text(encoding="utf-8")
        for fragment in required_fragments:
            if fragment not in text:
                errors.append(
                    f"mandatory engineering rule missing from {path.relative_to(ROOT)}: {fragment}"
                )

    skill_dirs = sorted(path for path in (ROOT / ".claude/skills").iterdir() if path.is_dir())
    for skill_dir in skill_dirs:
        if not skill_dir.name.startswith("dev-"):
            errors.append(f"repository skill directory must use dev- prefix: {skill_dir.relative_to(ROOT)}")
            continue
        link = SKILL_LINKS / skill_dir.name
        if not is_canonical_skill_link(link, skill_dir):
            errors.append(f"Codex discovery symlink is missing: {link.relative_to(ROOT)}")

    for path in FORBIDDEN_DIRS:
        if path.exists():
            errors.append(f"forbidden duplicate/legacy directory: {path.relative_to(ROOT)}")

    for path in sorted((ROOT / "apps/server-ai").rglob("*.go")):
        relative = path.relative_to(ROOT)
        text = path.read_text(encoding="utf-8")
        if FORBIDDEN_IMPORT.search(text):
            errors.append(f"forbidden legacy/duplicate import: {relative}")

    app = SERVER_AI / "app"
    if app.exists():
        for path in sorted(app.glob("*.go")):
            if not APP_ALLOWED.fullmatch(path.name):
                errors.append(f"business code/test leaked into composition root: {path.relative_to(ROOT)}")

    if WORKSPACE.exists():
        root_go = {path.name for path in WORKSPACE.glob("*.go") if not path.name.endswith("_test.go")}
        forbidden_root = {"domain.go", "service.go", "repository.go", "rest.go", "migrate.go"} & root_go
        if forbidden_root:
            errors.append(
                "workspace product root must coordinate leaf modules, not own a flat business aggregate: "
                + ", ".join(sorted(forbidden_root))
            )
        leaves = [path for path in sorted(WORKSPACE.iterdir()) if path.is_dir() and not path.name.startswith(".")]
        if not leaves:
            errors.append("workspace product area requires independently wired leaf bounded-context modules")
        for leaf in leaves:
            relative = leaf.relative_to(ROOT)
            actual_go = {path.name for path in leaf.glob("*.go") if not path.name.endswith("_test.go")}
            missing_go = WORKSPACE_LEAF_FILES - actual_go
            if missing_go:
                errors.append(f"workspace leaf module {relative} missing: {', '.join(sorted(missing_go))}")
            dialects = {path.name for path in (leaf / "sql").iterdir()} if (leaf / "sql").is_dir() else set()
            missing_dialects = SQL_DIALECTS - dialects
            if missing_dialects:
                errors.append(f"workspace leaf module {relative} missing SQL dialects: {', '.join(sorted(missing_dialects))}")
            locales_dir = leaf / "i18n/locales"
            locales = {path.name for path in locales_dir.glob("*.json")} if locales_dir.is_dir() else set()
            missing_locales = LOCALES - locales
            if missing_locales:
                errors.append(f"workspace leaf module {relative} missing locales: {', '.join(sorted(missing_locales))}")

    for path in sorted((ROOT / "apps/server-ai/internal/modules").rglob("*.go")):
        if TECHNICAL_FILENAME.match(path.name):
            errors.append(f"production/test filename exposes framework implementation: {path.relative_to(ROOT)}")
        text = path.read_text(encoding="utf-8")
        package_match = re.search(r"(?m)^package\s+([a-zA-Z0-9_]+)\s*$", text)
        if package_match:
            package_name = package_match.group(1).removesuffix("_test")
            module_path = server_ai_module_path()
            import_pattern = re.compile(rf'"{re.escape(module_path)}/internal/modules/(?:[^"/]+/)*{re.escape(package_name)}"')
            if import_pattern.search(text):
                errors.append(f"module package imports itself instead of owning its implementation: {path.relative_to(ROOT)}")

    if errors:
        print("architecture contract violations:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print("architecture contract OK")
    return 0


def server_ai_module_path() -> str:
    go_mod = ROOT / "apps/server-ai/go.mod"
    for line in go_mod.read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise RuntimeError("apps/server-ai/go.mod 缺少 module 声明")


if __name__ == "__main__":
    raise SystemExit(main())
