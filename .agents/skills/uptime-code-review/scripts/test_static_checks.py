"""Exercise review-runner behavior against disposable Git repositories/tools."""

import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import static_checks


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="uptime-review-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.log = self.root / "commands.jsonl"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.git("init", "-q")
        self.git("config", "user.name", "Review Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.write("README.md", "Fixture\n")
        self.write("backend/go.mod", "module fixture\n")
        self.write("frontend/package.json", '{}\n')
        self.write("frontend/.gitignore", "node_modules/\n.next/\n")
        self.write(".gitignore", ".env\n")
        self.commit()
        self.base = self.git("rev-parse", "HEAD").strip()
        for name in ("eslint/bin/eslint.js", "next/dist/bin/next", "typescript/bin/tsc"):
            self.write("frontend/node_modules/" + name, "// fixture\n")
        self.install_tools()

    def git(self, *argv):
        return subprocess.check_output(["git", *argv], cwd=self.repo, text=True, stderr=subprocess.STDOUT)

    def write(self, name, content):
        file = self.repo / name
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(content)
        return file

    def commit(self):
        self.git("add", ".")
        self.git("commit", "-qm", "fixture change")

    def install_tools(self, fail="", sleep="", mutate=False):
        for name in ("go", "gofmt", "node", "npm"):
            file = self.bin / name
            file.write_text(f"#!{os.sys.executable}\n" +
                "import json, os, pathlib, sys, time\n" +
                f"name = {name!r}\n" +
                f"with open({str(self.log)!r}, 'a') as f:\n" +
                "    f.write(json.dumps({'tool': name, 'args': sys.argv[1:], 'cwd': os.getcwd(), 'credential': os.getenv('JWT_SECRET')}) + '\\n')\n" +
                f"if name == {sleep!r}: time.sleep(5)\n" +
                ("if name == 'npm': pathlib.Path('package.json').write_text('{\"mutated\":true}\\n')\n" if mutate else "") +
                f"if name == {fail!r}:\n" +
                "    print('fixture diagnostic')\n" +
                "    sys.exit(1)\n" +
                "if name == 'gofmt' and any('unformatted' in arg for arg in sys.argv): print(sys.argv[-1])\n")
            file.chmod(0o755)

    def run_checks(self, *args):
        out, err = io.StringIO(), io.StringIO()
        env = {"PATH": str(self.bin) + os.pathsep + os.environ["PATH"], "JWT_SECRET": "placeholder-not-forwarded"}
        with patch.dict(os.environ, env), contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            result = static_checks.main(["--repo", str(self.repo), "--base", self.base, *args])
        return result, out.getvalue() + err.getvalue()

    def commands(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_documentation_only_does_not_run_language_tools(self):
        self.write("backend/README.md", "Updated docs\n")
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 0, report)
        self.assertFalse(self.commands())
        self.assertIn("no affected project", report)

    def test_shared_contract_runs_both_without_database_tests(self):
        self.write("contracts/cases.json", '{}\n')
        self.commit()
        self.write(".env", "JWT_SECRET=placeholder-not-sourced\n")
        code, report = self.run_checks()
        self.assertEqual(code, 0, report)
        calls = self.commands()
        self.assertEqual({call['tool'] for call in calls}, {"go", "npm", "node"})
        self.assertIn(["test", "./docs"], [call['args'] for call in calls])
        self.assertNotIn(["test", "./..."], [call['args'] for call in calls])
        self.assertTrue(all(call['credential'] is None for call in calls))
        next_index = next(i for i, call in enumerate(calls) if "typegen" in call['args'])
        tsc_index = next(i for i, call in enumerate(calls) if "--noEmit" in call['args'])
        self.assertLess(next_index, tsc_index)

    def test_rename_and_deletion_still_select_old_project(self):
        old = self.write("backend/old.go", "package old\n")
        self.commit()
        self.base = self.git("rev-parse", "HEAD").strip()
        old.rename(self.repo / "old.go")
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 0, report)
        self.assertIn("Projects: backend", report)
        self.assertTrue(any(call['tool'] == 'go' for call in self.commands()))

    def test_worktree_includes_staged_unstaged_and_untracked_files(self):
        self.write("backend/staged.go", "package staged\n")
        self.git("add", "backend/staged.go")
        self.write("frontend/local.ts", "export const value = 1;\n")
        self.write("README.md", "Unstaged docs\n")
        code, report = self.run_checks("--worktree")
        self.assertEqual(code, 0, report)
        self.assertIn("Changed paths: 3", report)
        self.assertEqual({call['tool'] for call in self.commands()}, {"go", "gofmt", "npm", "node"})

    def test_wrong_head_and_dirty_committed_checkout_are_blocked(self):
        self.write("README.md", "Next commit\n")
        self.commit()
        code, report = self.run_checks("--head", self.base)
        self.assertEqual(code, 2, report)
        self.assertIn("differs", report)
        self.write("README.md", "Local change\n")
        code, report = self.run_checks()
        self.assertEqual(code, 2, report)
        self.assertIn("local project changes", report)
        self.assertFalse(self.commands())

    def test_staged_change_even_when_worktree_matches_head_is_blocked(self):
        self.write("README.md", "Staged replacement\n")
        self.git("add", "README.md")
        self.write("README.md", "Fixture\n")
        code, report = self.run_checks()
        self.assertEqual(code, 2, report)
        self.assertIn("local project changes", report)

    def test_missing_dependencies_are_incomplete(self):
        self.write("frontend/local.ts", "export const value = 1;\n")
        self.commit()
        (self.repo / "frontend/node_modules/typescript/bin/tsc").unlink()
        code, report = self.run_checks()
        self.assertEqual(code, 2, report)
        self.assertIn("BLOCKED: Next.js", report)

    def test_failure_continues_independent_checks(self):
        self.write("contracts/cases.json", '{}\n')
        self.commit()
        self.install_tools(fail="go")
        code, report = self.run_checks()
        self.assertEqual(code, 1, report)
        self.assertTrue(any(call['tool'] == 'npm' for call in self.commands()))
        self.assertIn("FAIL: Go vet", report)

    def test_timeout_is_incomplete_and_continues_other_checks(self):
        self.write("contracts/cases.json", '{}\n')
        self.commit()
        self.install_tools(sleep="npm")
        code, report = self.run_checks("--timeout", "0.5")
        self.assertEqual(code, 2, report)
        self.assertIn("timed out", report)
        self.assertTrue(any("--noEmit" in call['args'] for call in self.commands()))

    def test_secret_location_is_reported_without_credential_value(self):
        credential = "gh" + "p_" + "x" * 36
        self.write("backend/config.go", f'package backend\nvar key = "{credential}"\n')
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 2, report)
        self.assertIn("backend/config.go':2", report)
        self.assertNotIn(credential, report)
        self.assertNotIn(credential, static_checks.redact(f"error: {credential}"))

    def test_unquoted_environment_secret_is_flagged(self):
        credential = "sensitive" + "-value-0123456789"
        self.write("backend/.env.example", "JWT_SECRET=" + credential + "\n")
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 2, report)
        self.assertIn("credential literal", report)
        self.assertNotIn(credential, report)

    def test_binary_marker_inside_source_is_not_reported_as_binary(self):
        self.write("backend/text.go", 'package backend\nvar marker = "Binary files differ"\n')
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 0, report)
        self.assertNotIn("binary; inspect separately", report)

    def test_placeholder_does_not_trigger_secret_candidate(self):
        self.write("backend/.env.example", 'JWT_SECRET="replace-this-with-a-random-secret"\n')
        self.commit()
        code, report = self.run_checks()
        self.assertEqual(code, 0, report)
        self.assertNotIn("REVIEW:", report)

    def test_gofmt_output_and_untracked_whitespace_fail(self):
        self.write("backend/unformatted.go", "package backend\n")
        self.write("frontend/new.ts", "const value = 1; \n")
        code, report = self.run_checks("--worktree")
        self.assertEqual(code, 1, report)
        self.assertIn("FAIL: Go formatting", report)
        self.assertIn("FAIL: untracked whitespace", report)

    def test_tracked_mutation_is_detected_and_preserved(self):
        self.write("frontend/local.ts", "export const value = 1;\n")
        self.commit()
        self.install_tools(mutate=True)
        code, report = self.run_checks()
        self.assertEqual(code, 1, report)
        self.assertIn("tracked files changed", report)
        self.assertIn("mutated", (self.repo / "frontend/package.json").read_text())

    def test_dry_run_executes_no_project_tools(self):
        self.write("contracts/cases.json", '{}\n')
        self.commit()
        code, report = self.run_checks("--dry-run")
        self.assertEqual(code, 0, report)
        self.assertFalse(self.commands())
        self.assertIn("PLAN ONLY", report)


if __name__ == "__main__":
    unittest.main()
