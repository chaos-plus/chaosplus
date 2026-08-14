from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check_gates.py")
SPEC = importlib.util.spec_from_file_location("check_gates", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
GATES = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = GATES
SPEC.loader.exec_module(GATES)


class CheckGatesTest(unittest.TestCase):
    def test_auto_scope_routes_shared_changes_to_every_domain(self) -> None:
        self.assertEqual(
            {"backend": True, "frontend": True, "docs": True},
            GATES.resolve_scopes("auto", [".claude/skills/dev-quality-gate/SKILL.md"]),
        )

    def test_auto_scope_routes_domain_changes(self) -> None:
        self.assertEqual(
            {"backend": True, "frontend": False, "docs": False},
            GATES.resolve_scopes("auto", ["apps/server/internal/app/app.go"]),
        )
        self.assertEqual(
            {"backend": False, "frontend": True, "docs": False},
            GATES.resolve_scopes("auto", ["apps/admin-ai/apps/platform/src/main.tsx"]),
        )
        self.assertEqual(
            {"backend": False, "frontend": False, "docs": True},
            GATES.resolve_scopes("auto", ["docs/architecture.md"]),
        )

    def test_coverage_profile_uses_exact_statement_counts(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            profile = Path(temporary) / "coverage.out"
            profile.write_text(
                "mode: atomic\n"
                "example.test/a.go:1.1,2.1 3 1\n"
                "example.test/b.go:1.1,2.1 2 0\n",
                encoding="utf-8",
            )
            self.assertEqual((3, 5, 60.0), GATES.parse_coverage_profile(profile))

    def test_sibling_check_uses_real_files(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = root / "apps/server/internal/modules/example"
            package.mkdir(parents=True)
            test = package / "service_test.go"
            test.write_text("package example\n", encoding="utf-8")
            self.assertEqual(1, len(GATES.test_sibling_errors(root)))
            (package / "service.go").write_text("package example\n", encoding="utf-8")
            self.assertEqual([], GATES.test_sibling_errors(root))

    def test_go_tool_versions_are_pinned(self) -> None:
        self.assertEqual("v0.7.0", GATES.STATICCHECK.expected)
        self.assertEqual("v1.64.8", GATES.GOLANGCI_LINT.expected)
        self.assertEqual("v1.6.0", GATES.GOVULNCHECK.expected)


if __name__ == "__main__":
    unittest.main()
