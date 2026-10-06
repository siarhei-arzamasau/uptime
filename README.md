# Uptime App

Two independent projects: a Go API and a Next.js interface for registration, login, profiles, and website monitors. Users can add, edit, and delete website URLs with configurable check intervals; a separate Go worker runs public HTTP GET checks and stores 30 days of minute aggregates for live statuses and availability charts.

```text
backend/
  cmd/api/main.go    # Authentication HTTP API
  internal/          # Authentication, HTTP, and database access
  go.mod
frontend/
  src/app/           # Base Next.js App Router
  package.json
  package-lock.json
```

## Backend

Requires Go 1.26+.

PostgreSQL setup with Docker Compose, migrations, API startup, and testing instructions: [backend/README.md](backend/README.md).

Implementation plan and task status: [docs/authentication-plan.md](docs/authentication-plan.md).

## Frontend

Next.js 16, TypeScript, ESLint. Requires Node.js 22.12+ and npm.

```sh
cd frontend
npm ci
npm run dev
```

The frontend is available at http://localhost:3000.

Validation and build: run `npm run lint` and `npm run build` from `frontend/`.

## Combined startup

Requires Node.js 22.12+, Go 1.26+, Docker Compose, installed npm dependencies, and local `backend/.env` and `frontend/.env.local` files (see the examples).

```sh
node scripts/dev.mjs
```

The script starts PostgreSQL, applies migrations, and launches the API, monitoring worker, and frontend. Ctrl+C stops both applications while preserving the database. If Go is not on PATH, set `GO_BIN=/absolute/path/to/go` in `backend/.env`. On macOS, install Go permanently with `brew install go`.

Configuration details, themes, and tests: [frontend/README.md](frontend/README.md). Plan and checklist: [docs/frontend-authentication-plan.md](docs/frontend-authentication-plan.md).

## Browser verification

Use **[Playwright MCP](https://github.com/microsoft/playwright-mcp)** for browser
work in this project. After implementation, apply
[$uptime-browser-verification](.agents/skills/uptime-browser-verification/SKILL.md)
to verify the affected user flows and desktop/mobile layout before completing the
task. Builds and automated tests complement this mandatory browser check.

If Playwright MCP is not connected, add the following server to the project's
`.codex/config.toml`, preserving any existing MCP entries:

```toml
[mcp_servers.playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest", "--isolated"]
startup_timeout_sec = 120
```

Node.js and npm must be on PATH. Reload the MCP configuration or start a new Codex
session to expose the tools. The isolated browser session avoids reusing personal
browser state. For checks that change data, start `node scripts/dev.mjs --e2e`
from the repository root and use `http://localhost:3001` with the dedicated
`uptime_e2e_test` database; do not run manual verification and the automated E2E
runner on those ports at the same time. A missing MCP connection or unavailable
application must be reported as an incomplete verification.

## Local PostgreSQL MCP

The project-scoped [Codex configuration](.codex/config.toml) starts
[postgres-mcp](https://github.com/crystaldba/postgres-mcp) over stdio with read/write
access, including schema changes. It requires Node.js 22.12+ and
[uv](https://docs.astral.sh/uv/getting-started/installation/) (`uvx` on PATH).
Open Codex from the repository root and trust the project to load its MCP configuration.

The launcher reads `DATABASE_URL` from this checkout's ignored `backend/.env` and
accepts only local PostgreSQL addresses. Set up that file and start PostgreSQL using
[the backend instructions](backend/README.md). Each worktree needs its own local
`backend/.env`; credentials are never stored in the MCP configuration.
Use `localhost`, `127.0.0.1`, or `[::1]` in the URI authority. Connection-address
query fields (`host`, `hostaddr`, and `service`) and raw URI fragments are rejected;
other options such as `sslmode` remain supported. The launcher removes ambient
`PG*` variables before starting MCP so they cannot change the connection target.

Run the launcher regressions with `npm test -- ../scripts/postgres-mcp.test.mjs`
from `frontend/`. They use a fake `uvx` and never connect to PostgreSQL.

The first launch downloads pinned postgres-mcp and MCP SDK versions. Restart Codex
or start a new chat after adding the configuration, then check that `uptime-postgres`
appears in the MCP server list. The server supports schema inspection, SQL execution,
and query plans; advanced performance tools may need optional PostgreSQL extensions
described in the postgres-mcp documentation.

## Code review

Use the project skill `$uptime-code-review` to review a commit, PR, or local diff. It runs scoped static checks before reviewing the [product architecture](.agents/skills/uptime-code-review/references/architecture.md) and [checklist](.agents/skills/uptime-code-review/references/review-checklist.md), then reports actionable findings from P0 (critical) to P3 (low).

The project also includes [$vercel-react-best-practices](.agents/skills/vercel-react-best-practices/SKILL.md) for React and Next.js performance guidance, with its supporting rules kept locally. The installed source is `vercel-labs/agent-skills`, revision `063bee94c3f4df8453406c830b0a7df0f2860278`, under `skills/react-best-practices`.

The following Go skills are available at project scope:

- [$golang-code-style](.agents/skills/golang-code-style/SKILL.md)
- [$golang-testing](.agents/skills/golang-testing/SKILL.md)
- [$golang-error-handling](.agents/skills/golang-error-handling/SKILL.md)
- [$golang-design-patterns](.agents/skills/golang-design-patterns/SKILL.md)
- [$golang-security](.agents/skills/golang-security/SKILL.md)
- [$golang-performance](.agents/skills/golang-performance/SKILL.md)
- [$golang-concurrency](.agents/skills/golang-concurrency/SKILL.md)

These skills come from `samber/cc-skills-golang`, revision `8e899e20ff0cd4dc524af3993e4c62d8ee8c5717`, under the corresponding `skills/<skill-name>` directories. Their references and evaluation fixtures are kept locally, with the upstream MIT license notice in each skill directory. Apply them to the requested Go task; repository instructions and the product architecture remain authoritative.

To run only the checks from the repository root:

```sh
python3 .agents/skills/uptime-code-review/scripts/static_checks.py --base origin/main --head HEAD
# Include staged, unstaged, and untracked changes:
python3 .agents/skills/uptime-code-review/scripts/static_checks.py --base origin/main --worktree
```

See [SKILL.md](.agents/skills/uptime-code-review/SKILL.md) for comparison modes, prerequisites, and validation limits.
