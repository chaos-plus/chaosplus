from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("skill-runtime.py")
SPEC = importlib.util.spec_from_file_location("skill_runtime", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
RUNTIME = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNTIME)


class SkillRuntimeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.skills = self.root / ".claude" / "skills"

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def create_skill(self, name: str = "dev-backend") -> Path:
        skill = self.skills / name
        (skill / "agents").mkdir(parents=True)
        (skill / "references").mkdir()
        (skill / "SKILL.md").write_text(
            f"---\nname: {name}\ndescription: 用于仓库工程任务。\n---\n\n# Skill\n",
            encoding="utf-8",
        )
        (skill / "agents" / "openai.yaml").write_text(
            "interface:\n"
            '  display_name: "Dev 后端"\n'
            '  short_description: "实现并验证后端变更"\n'
            f'  default_prompt: "使用 ${name} 完成本次变更。"\n'
            "policy:\n"
            "  allow_implicit_invocation: true\n",
            encoding="utf-8",
        )
        (skill / "references" / "lessons.md").write_text("# Lessons\n", encoding="utf-8")
        discovery = self.root / ".agents" / "skills"
        discovery.mkdir(parents=True, exist_ok=True)
        link = discovery / name
        if link.exists() or link.is_symlink():
            link.unlink()
        try:
            link.symlink_to(skill, target_is_directory=True)
        except OSError:
            self.materialize_git_symlink(link, skill)
        return skill

    def materialize_git_symlink(self, link: Path, target: Path) -> None:
        subprocess.run(["git", "init", "--quiet"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "core.symlinks", "false"], cwd=self.root, check=True)
        relative_target = (Path("../..") / target.relative_to(self.root)).as_posix()
        link.write_text(relative_target, encoding="utf-8")
        blob = subprocess.run(
            ["git", "hash-object", "-w", "--stdin"],
            cwd=self.root,
            input=relative_target,
            text=True,
            capture_output=True,
            check=True,
        ).stdout.strip()
        subprocess.run(
            ["git", "update-index", "--add", "--cacheinfo", "120000", blob, link.relative_to(self.root).as_posix()],
            cwd=self.root,
            check=True,
        )

    def create_repository(self) -> None:
        for path in (
            "apps/server",
            "apps/server/internal/modules/iam/sql/sqlite",
            "apps/server/internal/modules/iam/sql/mysql",
            "apps/server/internal/modules/iam/sql/postgres",
            "apps/server/internal/modules/oauth",
            "apps/admin-ai/apps/platform",
            "apps/docs",
        ):
            (self.root / path).mkdir(parents=True, exist_ok=True)
        (self.root / "apps" / "server" / "go.mod").write_text("module example.test/project\n\ngo 1.26.0\n", encoding="utf-8")
        (self.root / "apps" / "server" / "internal" / "modules" / "iam" / "module.go").write_text("package iam\n", encoding="utf-8")
        dependency = self.root / "apps" / "admin-ai" / "node_modules" / "dependency"
        dependency.mkdir(parents=True)
        (dependency / "dependency.go").write_text("package dependency\n", encoding="utf-8")
        (self.root / "apps" / "admin-ai" / "package.json").write_text(
            json.dumps({"name": "admin", "packageManager": "bun@1", "workspaces": ["apps/*"]}),
            encoding="utf-8",
        )
        (self.root / "apps" / "admin-ai" / "apps" / "platform" / "package.json").write_text(
            json.dumps({"scripts": {"test": "bun test src"}}), encoding="utf-8"
        )
        (self.root / "apps" / "docs" / "package.json").write_text(
            json.dumps(
                {
                    "name": "docs",
                    "dependencies": {"astro": "1", "@astrojs/starlight": "2"},
                }
            ),
            encoding="utf-8",
        )
        quality = self.create_skill("dev-quality-gate")
        (quality / "references" / "repository-facts.md").write_text("stale\n", encoding="utf-8")

    def test_validation_uses_real_files_and_cli(self) -> None:
        skill = self.create_skill()
        self.assertEqual([], RUNTIME.validate_skills(self.skills))
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "validate", str(skill)],
            cwd=Path(__file__).resolve().parents[4],
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("已校验 1", result.stdout)

        (skill / "SKILL.md").write_text(
            "---\nname: wrong-name\ndescription: valid description\n---\n",
            encoding="utf-8",
        )
        self.assertTrue(RUNTIME.validate_skills(self.skills))

    def test_validation_requires_implicit_invocation(self) -> None:
        skill = self.create_skill()
        metadata = skill / "agents" / "openai.yaml"
        metadata.write_text(
            metadata.read_text(encoding="utf-8").replace(
                "policy:\n  allow_implicit_invocation: true\n", ""
            ),
            encoding="utf-8",
        )
        errors = RUNTIME.validate_skills(self.skills)
        self.assertTrue(any("allow_implicit_invocation" in error for error in errors))

    def test_validation_requires_chinese_metadata_and_canonical_discovery_link(self) -> None:
        skill = self.create_skill()
        metadata = skill / "agents" / "openai.yaml"
        metadata.write_text(
            metadata.read_text(encoding="utf-8").replace("实现并验证后端变更", "Backend changes"),
            encoding="utf-8",
        )
        errors = RUNTIME.validate_skills(self.skills)
        self.assertTrue(any("short_description" in error and "Chinese" in error for error in errors))

        metadata.write_text(
            metadata.read_text(encoding="utf-8").replace("Backend changes", "实现并验证后端变更"),
            encoding="utf-8",
        )
        (self.root / ".agents" / "skills" / skill.name).unlink()
        errors = RUNTIME.validate_skills(self.skills)
        self.assertTrue(any("symbolic link" in error for error in errors))

    def test_materialized_git_symlink_requires_exact_target_and_index_mode(self) -> None:
        skill = self.create_skill()
        link = self.root / ".agents" / "skills" / skill.name
        link.unlink()
        self.materialize_git_symlink(link, skill)
        self.assertTrue(RUNTIME.is_canonical_skill_link(self.root, link, skill))

        link.write_text("../../.claude/skills/dev-docs", encoding="utf-8")
        self.assertFalse(RUNTIME.is_canonical_skill_link(self.root, link, skill))

    def test_validation_rejects_legacy_skill_root(self) -> None:
        self.create_skill()
        (self.root / ".codex" / "skills").mkdir(parents=True)
        errors = RUNTIME.validate_skills(self.skills)
        self.assertTrue(any("legacy repository skill root" in error for error in errors))

    def test_refresh_is_deterministic_and_idempotent(self) -> None:
        self.create_repository()
        self.assertTrue(RUNTIME.refresh_context(self.root))
        facts = self.skills / "dev-quality-gate" / "references" / "repository-facts.md"
        source = facts.read_text(encoding="utf-8")
        self.assertIn("example.test/project", source)
        self.assertIn("mysql, postgres, sqlite", source)
        self.assertIn("Go package 目录数：1", source)
        self.assertFalse(RUNTIME.refresh_context(self.root))

    def test_test_policy_allows_miniredis_in_isolated_go_test(self) -> None:
        package = self.root / "apps/server/internal/modules/session"
        package.mkdir(parents=True)
        (package / "store_test.go").write_text(
            'package session\n\nimport "github.com/alicebob/miniredis/v2"\n', encoding="utf-8"
        )
        self.assertEqual([], RUNTIME.find_test_policy_violations(self.root))

    def test_test_policy_rejects_miniredis_in_production_go(self) -> None:
        package = self.root / "apps/server/internal/modules/session"
        package.mkdir(parents=True)
        (package / "store.go").write_text(
            'package session\n\nimport "github.com/alicebob/miniredis/v2"\n', encoding="utf-8"
        )
        errors = RUNTIME.find_test_policy_violations(self.root)
        self.assertTrue(any("store.go:3" in error and "only in *_test.go" in error for error in errors))

    def test_test_policy_rejects_substitutes_in_isolated_tests(self) -> None:
        package = self.root / "apps/runner/src"
        package.mkdir(parents=True)
        forbidden = "mo" + "ck"
        (package / "agent.test.ts").write_text(
            f'const runtime = "{forbidden}";\n', encoding="utf-8"
        )
        errors = RUNTIME.find_test_policy_violations(self.root)
        self.assertTrue(any("agent.test.ts:1" in error and "forbidden" in error for error in errors))

    def test_test_policy_rejects_production_substitute(self) -> None:
        source = self.root / "apps/runner/src/backends/index.ts"
        source.parent.mkdir(parents=True)
        forbidden = "fa" + "ke"
        source.write_text(f'const runtime = "{forbidden}";\n', encoding="utf-8")
        errors = RUNTIME.find_test_policy_violations(self.root)
        self.assertTrue(any("index.ts:1" in error and "forbidden" in error for error in errors))

    def test_test_policy_rejects_monkey_patching_globals_and_spies(self) -> None:
        source = self.root / "apps/admin-ai/apps/platform/src/lib/client.test.ts"
        source.parent.mkdir(parents=True)
        source.write_text(
            "globalThis.fetch = replacement;\nspyOn(client, 'request');\n",
            encoding="utf-8",
        )
        errors = RUNTIME.find_test_policy_violations(self.root)
        self.assertTrue(any("client.test.ts:1" in error and "monkey patching" in error for error in errors))
        self.assertTrue(any("client.test.ts:2" in error and "monkey patching" in error for error in errors))

    def test_learning_routes_normalizes_and_deduplicates(self) -> None:
        skill = self.create_skill()
        values = {
            "symptom": "Build failed\nafter typecheck",
            "cause": "Project references were skipped",
            "prevention": "Use the solution build mode",
            "evidence": "Typecheck and production build passed",
        }
        lesson_id, added = RUNTIME.record_learning(self.skills, "backend", **values)
        self.assertTrue(added)
        self.assertRegex(lesson_id, r"^L-[a-f0-9]{12}$")
        duplicate_id, duplicate_added = RUNTIME.record_learning(self.skills, "backend", **values)
        self.assertEqual(lesson_id, duplicate_id)
        self.assertFalse(duplicate_added)
        source = (skill / "references" / "lessons.md").read_text(encoding="utf-8")
        self.assertEqual(1, source.count(f"## {lesson_id}"))
        self.assertIn("Build failed after typecheck", source)

    def test_learning_rejects_likely_secrets(self) -> None:
        self.create_skill()
        with self.assertRaisesRegex(ValueError, "secret"):
            RUNTIME.record_learning(
                self.skills,
                "backend",
                "Request failed",
                "token=do-not-store-this",
                "Redact credentials",
                "A safe test passed",
            )

    def test_validation_requires_dev_prefix_and_rejects_derived_brand(self) -> None:
        self.create_repository()
        invalid = self.create_skill("backend")
        errors = RUNTIME.validate_skills(self.skills, [invalid])
        self.assertTrue(any("dev-" in error for error in errors))

        branded = self.create_skill("dev-backend")
        (branded / "SKILL.md").write_text(
            f"---\nname: dev-backend\ndescription: {self.root.name} project backend\n---\n",
            encoding="utf-8",
        )
        errors = RUNTIME.validate_skills(self.skills, [branded])
        self.assertTrue(any("brand" in error for error in errors))

    def test_validation_rejects_brand_in_lessons_and_rules(self) -> None:
        self.create_repository()
        skill = self.create_skill("dev-backend")
        brand = self.root.name
        (skill / "references" / "lessons.md").write_text(
            f"# Lessons\n\nDo not copy {brand} defaults.\n", encoding="utf-8"
        )
        errors = RUNTIME.validate_skills(self.skills, [skill])
        self.assertTrue(any("lessons.md" in error and "brand" in error for error in errors))

        (skill / "references" / "lessons.md").write_text("# Lessons\n", encoding="utf-8")
        (self.root / ".rules").mkdir()
        (self.root / ".rules" / "engineering.md").write_text(
            f"Use the {brand} convention.\n", encoding="utf-8"
        )
        errors = RUNTIME.validate_skills(self.skills, [skill])
        self.assertTrue(any("engineering.md" in error and "brand" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
