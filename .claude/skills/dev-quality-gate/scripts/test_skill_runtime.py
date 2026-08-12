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
        link.symlink_to(skill, target_is_directory=True)
        return skill

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
        self.assertFalse(RUNTIME.refresh_context(self.root))

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
