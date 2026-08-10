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
            f"---\nname: {name}\ndescription: Use this skill for repository work.\n---\n\n# Skill\n",
            encoding="utf-8",
        )
        (skill / "agents" / "openai.yaml").write_text(
            "interface:\n"
            '  display_name: "Dev Backend"\n'
            '  short_description: "Build and verify backend changes"\n'
            f'  default_prompt: "Use ${name} to complete this change."\n',
            encoding="utf-8",
        )
        (skill / "references" / "lessons.md").write_text("# Lessons\n", encoding="utf-8")
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
        self.assertIn("Validated 1", result.stdout)

        (skill / "SKILL.md").write_text(
            "---\nname: wrong-name\ndescription: valid description\n---\n",
            encoding="utf-8",
        )
        self.assertTrue(RUNTIME.validate_skills(self.skills))

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


if __name__ == "__main__":
    unittest.main()
