#!/usr/bin/env python3
"""Check the selected Uptime diff; exit 0 passed, 1 failed, or 2 incomplete.

Uses Git and existing Go/Node installations; never installs frontend packages.
Go may download pinned modules into its cache. Checks execute project
configuration: inspect changed executable configuration first.
"""

import argparse
import hashlib
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile


SECRET_PATTERNS = (
    ("private key", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----")),
    ("GitHub token", re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{80,})\b")),
    ("Slack token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b")),
    ("credential URL", re.compile(r"\b(?:postgres(?:ql)?|mysql|https?)://[^\s:@/]+:([^\s@/]+)@")),
    ("credential literal", re.compile(
        r"\b(?:JWT_SECRET|POSTGRES_PASSWORD|API_KEY|CLIENT_SECRET|ACCESS_TOKEN|REFRESH_TOKEN)"
        r"[\"']?\s*[:=]\s*[\"']?([^\"'\s,;)}]{16,})", re.IGNORECASE)),
)
# Failed tools may truncate PEM output before END; redact the remainder too.
PRIVATE_KEY_BLOCK = re.compile(
    r"-----BEGIN (?P<label>(?:[A-Z0-9]+ )*PRIVATE KEY)-----.*?"
    r"(?:-----END (?P=label)-----|\Z)", re.DOTALL)
PLACEHOLDER = re.compile(r"(?:example|placeholder|change[-_]?me|replace[-_]|your[-_]|\$\{|<|test[-_]password)", re.IGNORECASE)
# Keep ordinary toolchain configuration, but do not forward deployment credentials.
ENV_KEYS = {"PATH", "HOME", "USER", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL",
            "SYSTEMROOT", "PATHEXT", "GOPATH", "GOCACHE", "GOMODCACHE", "GOPROXY",
            "GOTOOLCHAIN", "CGO_ENABLED", "CC", "CXX", "SDKROOT", "DEVELOPER_DIR"}


class InvalidComparison(Exception):
    """Indicate missing history or a checkout that cannot validate the target."""


def invoke(argv, cwd, timeout, env=None):
    """Return exit status and merged output, killing the process group on timeout."""
    proc = subprocess.Popen(argv, cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, start_new_session=(os.name == "posix"))
    try:
        output, _ = proc.communicate(timeout=timeout)
        return proc.returncode, output.decode("utf-8", errors="replace")
    except subprocess.TimeoutExpired:
        if os.name == "posix":
            os.killpg(proc.pid, signal.SIGKILL)
        else:
            proc.kill()
        proc.communicate()
        raise


def git(repo, *args):
    """Read Git metadata without executing external diff helpers; raise on failure."""
    code, output = invoke(["git", *args], repo, 30)
    if code:
        raise InvalidComparison("Git could not resolve the comparison; fetch/verify the requested refs.")
    return output


def paths(output):
    """Decode a NUL-delimited Git path list, preserving spaces and line breaks."""
    return [item for item in output.split("\0") if item]


def select_projects(changed):
    """Select affected language projects, including shared executable/contracts."""
    projects = set()
    for name in changed:
        if Path(name).suffix.lower() in {".md", ".rst", ".txt"}:
            continue
        if name.startswith("backend/"):
            projects.add("backend")
        if name.startswith("frontend/"):
            projects.add("frontend")
        if name.startswith(("contracts/", "scripts/", ".github/workflows/")) or name in {"Makefile", "compose.yaml", "docker-compose.yml"}:
            projects.update(("backend", "frontend"))
    return projects


def redact(output):
    """Remove recognized credential values from diagnostic output."""
    output = PRIVATE_KEY_BLOCK.sub("[redacted private key]", output)
    for _, pattern in SECRET_PATTERNS:
        output = pattern.sub("[redacted credential candidate]", output)
    return output


class Runner:
    """Run independent checks and retain failed/incomplete outcomes for exit status."""

    def __init__(self, args, repo):
        self.args, self.repo = args, repo
        self.failed = self.incomplete = False
        self.env = {key: value for key, value in os.environ.items() if key in ENV_KEYS}
        # Module-local checks must not discover an unrelated parent Go workspace.
        self.env.update(CI="1", NEXT_TELEMETRY_DISABLED="1", GOFLAGS="-mod=readonly", GOWORK="off")

    def report(self, state, name, detail=""):
        """Print a check outcome; failures take precedence over incomplete checks."""
        print(f"{state}: {name}" + (f" — {detail}" if detail else ""), flush=True)
        self.failed |= state == "FAIL"
        self.incomplete |= state in {"BLOCKED", "REVIEW"}

    def check(self, name, argv, cwd, *, empty_output=False, hide_output=False):
        """Execute a check without a shell; report missing tools, timeout, or errors."""
        if self.args.dry_run:
            self.report("PLAN", name, f"{cwd.relative_to(self.repo) or '.'}: {' '.join(argv)}")
            return True
        try:
            code, output = invoke(argv, cwd, self.args.timeout, self.env)
        except (OSError, subprocess.TimeoutExpired) as error:
            self.report("BLOCKED", name, "timed out" if isinstance(error, subprocess.TimeoutExpired) else "tool unavailable")
            return False
        passed = code == 0 and (not empty_output or not output.strip())
        self.report("PASS" if passed else "FAIL", name, "" if passed else f"exit {code}")
        if not passed and output.strip() and not hide_output:
            print(redact(output)[:6000], flush=True)
        return passed

    def secrets(self, diff_args, changed, untracked, *, snapshot=""):
        """Scan added textual lines and report only candidate locations, never values."""
        scan_name = (snapshot + " " if snapshot else "") + "added-line credential scan"
        if self.args.dry_run:
            self.report("PLAN", scan_name)
            return
        matches = []
        coverage_complete = True
        for name in changed:
            if name in untracked:
                file = self.repo / name
                if file.is_symlink() or not file.is_file():
                    self.report("BLOCKED", "credential scan", f"{name!r}: non-regular file; inspect separately")
                    coverage_complete = False
                    continue
                if file.stat().st_size > 1024 * 1024:
                    self.report("BLOCKED", "credential scan", f"{name!r}: over 1 MiB; inspect separately")
                    coverage_complete = False
                    continue
                data = file.read_bytes()
                if b"\0" in data:
                    self.report("BLOCKED", "credential scan", f"{name!r}: binary; inspect separately")
                    coverage_complete = False
                    continue
                lines = enumerate(data.decode("utf-8", errors="replace").splitlines(), 1)
            else:
                # Changed filenames can contain Git pathspec magic; inspect their literal paths.
                patch = git(self.repo, "--literal-pathspecs", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "-U0", *diff_args, "--", name)
                added = []
                line_number = 0
                for line in patch.splitlines():
                    hunk = re.match(r"@@ .* \+(\d+)(?:,\d+)? @@", line)
                    if hunk:
                        line_number = int(hunk[1])
                    elif line.startswith("+") and line_number:
                        added.append((line_number, line[1:]))
                        line_number += 1
                    elif line.startswith(" "):
                        line_number += 1
                if any(line.startswith("Binary files ") and line.endswith(" differ") for line in patch.splitlines()):
                    self.report("BLOCKED", "credential scan", f"{name!r}: binary; inspect separately")
                    coverage_complete = False
                lines = added
            for number, line in lines:
                for label, pattern in SECRET_PATTERNS:
                    for match in pattern.finditer(line):
                        value = match.group(1) if match.lastindex else match.group(0)
                        if not PLACEHOLDER.search(value):
                            matches.append((name, number, label))
        if matches:
            for name, number, label in sorted(set(matches)):
                self.report("REVIEW", "credential candidate", f"{name!r}:{number} ({label}; value withheld)" + (f" [{snapshot}]" if snapshot else ""))
        if not coverage_complete:
            self.report("BLOCKED", scan_name, "some changed files were not inspected")
        elif not matches:
            self.report("PASS", scan_name, "heuristic only; manual security review remains required")

    def backend(self, changed, output_dir):
        """Check formatting, vet, API compilation, and generated-contract freshness."""
        cwd = self.repo / "backend"
        if not (cwd / "go.mod").is_file():
            self.report("BLOCKED", "Go checks", "backend/go.mod missing")
            return
        files = [str(self.repo / name) for name in changed if name.startswith("backend/") and name.endswith(".go") and (self.repo / name).is_file()]
        if files:
            self.check("Go formatting", ["gofmt", "-l", *files], cwd, empty_output=True)
        else:
            self.report("SKIP", "Go formatting", "no changed Go source files")
        self.check("Go vet", ["go", "vet", "./..."], cwd)
        self.check("Go API build", ["go", "build", "-o", str(output_dir / "api"), "./cmd/api"], cwd)
        self.check("OpenAPI freshness/route coverage", ["go", "test", "./docs"], cwd)

    def frontend(self):
        """Lint and generate route types before strict no-emit TypeScript validation."""
        cwd = self.repo / "frontend"
        packages = ("eslint/bin/eslint.js", "next/dist/bin/next", "typescript/bin/tsc")
        if not self.args.dry_run and (not (cwd / "package.json").is_file() or any(not (cwd / "node_modules" / name).is_file() for name in packages)):
            self.report("BLOCKED", "Next.js checks", "install the existing frontend lockfile dependencies first (npm ci)")
            return
        self.check("Next.js ESLint", ["npm", "run", "lint"], cwd)
        if self.check("Next.js route types", ["node", "node_modules/next/dist/bin/next", "typegen"], cwd):
            self.check("TypeScript", ["node", "node_modules/typescript/bin/tsc", "--noEmit", "--incremental", "false"], cwd)
        else:
            self.report("BLOCKED", "TypeScript", "route type generation did not complete")

    def exit_code(self):
        """Return 1 for failures, otherwise 2 for incomplete checks, otherwise 0."""
        return 1 if self.failed else 2 if self.incomplete else 0


def fingerprint(repo):
    """Capture tracked modifications without printing source or resetting files."""
    diff = git(repo, "diff", "--no-ext-diff", "--no-textconv", "--binary", "HEAD")
    index = git(repo, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", "HEAD")
    return hashlib.sha256((diff + "\0" + index).encode()).hexdigest()


def main(argv=None):
    """Resolve the diff, run selected checks, and return a report-based exit status."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd(), help="path inside the repository (default: cwd)")
    parser.add_argument("--base", default="origin/main", help="base commit/ref used to compute the merge base")
    parser.add_argument("--head", default="HEAD", help="reviewed head commit/ref; must be checked out")
    parser.add_argument("--worktree", action="store_true", help="include staged, unstaged and non-ignored untracked changes")
    parser.add_argument("--projects", choices=("auto", "backend", "frontend", "both"), default="auto", help="override changed-path selection")
    parser.add_argument("--timeout", type=float, default=180, help="positive timeout in seconds per check")
    parser.add_argument("--dry-run", action="store_true", help="print the plan without executing project tools or secret scans")
    args = parser.parse_args(argv)
    if not 0 < args.timeout < float("inf"):
        parser.error("--timeout must be positive")
    try:
        repo = Path(git(args.repo, "rev-parse", "--show-toplevel").strip()).resolve()
        base = git(repo, "rev-parse", "--verify", "--end-of-options", args.base + "^{commit}").strip()
        head = git(repo, "rev-parse", "--verify", "--end-of-options", args.head + "^{commit}").strip()
        actual = git(repo, "rev-parse", "HEAD").strip()
        merge_base = git(repo, "merge-base", base, head).strip()
        if actual != head:
            raise InvalidComparison("Checkout HEAD differs from the reviewed head; use an isolated checkout at that commit.")
        dirty = paths(git(repo, "diff", "--name-only", "-z", "--no-ext-diff", "--no-textconv"))
        dirty += paths(git(repo, "diff", "--cached", "--name-only", "-z", "--no-ext-diff", "--no-textconv"))
        untracked = paths(git(repo, "ls-files", "--others", "--exclude-standard", "-z"))
        if not args.worktree and (dirty or any(name.startswith(("backend/", "frontend/", "contracts/", "scripts/")) for name in untracked)):
            raise InvalidComparison("Checkout contains local project changes; use --worktree or an isolated clean checkout.")
        diff_args = [merge_base] if args.worktree else [merge_base, head]
        changed = paths(git(repo, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", *diff_args))
        index_args = ["--cached", merge_base]
        index_changed = []
        if args.worktree:
            # The working snapshot can undo a staged edit without removing it from the index.
            index_changed = paths(git(repo, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", *index_args))
            changed = sorted(set(changed + index_changed + untracked))
        projects = select_projects(changed) if args.projects == "auto" else {"backend", "frontend"} if args.projects == "both" else {args.projects}
        print(f"Base: {base}\nHead: {head}\nMerge base: {merge_base}\nMode: {'worktree' if args.worktree else 'committed'}\nChanged paths: {len(changed)}\nProjects: {', '.join(sorted(projects)) or 'none (documentation/tooling only)'}", flush=True)
        runner = Runner(args, repo)
        before = fingerprint(repo)
        runner.secrets(diff_args, changed, set(untracked) if args.worktree else set())
        # Git --check prints source lines: withhold them so credentials cannot leak.
        runner.check("diff whitespace", ["git", "diff", "--check", "--no-ext-diff", "--no-textconv", *diff_args], repo, hide_output=True)
        if args.worktree:
            runner.secrets(index_args, index_changed, set(), snapshot="index")
            runner.check("index diff whitespace", ["git", "diff", "--check", "--no-ext-diff", "--no-textconv", *index_args], repo, hide_output=True)
            whitespace = []
            for name in untracked:
                file = repo / name
                if file.is_file() and not file.is_symlink() and file.stat().st_size <= 1024 * 1024:
                    data = file.read_bytes()
                    if b"\0" not in data:
                        whitespace.extend((name, i) for i, line in enumerate(data.splitlines(), 1) if line.endswith((b" ", b"\t")))
            if whitespace and not args.dry_run:
                runner.report("FAIL", "untracked whitespace", ", ".join(f"{name!r}:{i}" for name, i in whitespace))
        with tempfile.TemporaryDirectory(prefix="uptime-review-") as temp:
            if "backend" in projects:
                runner.backend(changed, Path(temp))
            if "frontend" in projects:
                runner.frontend()
        if not projects:
            runner.report("SKIP", "Go/Next.js checks", "no affected project selected; use --projects both to force them")
        if fingerprint(repo) != before:
            runner.report("FAIL", "tracked files changed during checks", "inspect git diff; changes were not reverted")
        result = runner.exit_code()
        print("Result: " + ("FAILED" if result == 1 else "INCOMPLETE" if result == 2 else "PLAN ONLY (not validation)" if args.dry_run else "PASSED"), flush=True)
        return result
    except (InvalidComparison, OSError, subprocess.TimeoutExpired) as error:
        print(f"BLOCKED: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
