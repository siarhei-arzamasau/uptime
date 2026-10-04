# Database migrations

This guide follows the [task template](../templates/task.md). Use it when a task changes the PostgreSQL schema or stored data, or asks an agent to apply existing migrations.

## [C] Context

Read the [repository instructions](../../AGENTS.md), [backend instructions](../../backend/AGENTS.md), and [backend setup](../../backend/README.md) before starting. Run commands below from `backend/` unless stated otherwise.

| Location | Responsibility |
| --- | --- |
| `backend/migrations/NNNNN_description.sql` | Versioned SQL with goose `Up` and `Down` sections. |
| `backend/migrations/migrations.go` | Embeds every `*.sql` file with `go:embed`; no manual file registration is needed. |
| `backend/cmd/migrate/main.go` | Migration runner: reads `DATABASE_URL`, selects PostgreSQL, and runs embedded migrations with a one-minute timeout. |
| `backend/internal/store/` | GORM models and persistence code that must match the schema. GORM does not own schema changes. |
| `backend/internal/auth/controller_integration_test.go` | PostgreSQL fixture and `TestMigrations`; tests create independent schemas, apply migrations, and remove their own schemas. |
| `backend/.env.example`, `backend/docker-compose.yml` | Local configuration and PostgreSQL 17 service. Local secrets belong in ignored `backend/.env`. |
| `scripts/dev.mjs` | Combined launcher: applies all pending migrations before building/starting the API and frontend. |
| `frontend/playwright.config.ts` | E2E invokes the launcher with `--e2e`, targeting `uptime_e2e_test`. |

The current migration sequence is `00001_auth.sql`, `00002_user_name.sql`, `00003_user_avatar.sql`, and `00004_monitors.sql`. Inspect the directory each time; do not assume the next version is still `00005`.

The runner supports exactly these commands:

| Command | Effect |
| --- | --- |
| `go run ./cmd/migrate status` | Shows migration state for the selected database/schema. On a fresh database, goose may create its version-tracking table. |
| `go run ./cmd/migrate up` | Applies **all** pending migrations in version order; a second run should apply nothing. |
| `go run ./cmd/migrate down` | Rolls back **one**, the latest applied migration; it may delete data. |

Commands such as `create`, `validate`, `version`, `up-to`, and `down-to` are not exposed by this runner. The integration test's direct use of `goose.DownTo` does not make it a supported shell command. Use the project runner rather than an independently installed goose CLI with different defaults.

## [O] Outcome / Goal

The intended schema/data change is applied to the confirmed target, existing data satisfies the new invariants, and affected API operations work. The agent reports the target identity without credentials, before/after migration state, validation results, and any rollback limitations. Exit code zero or an `Applied` entry alone is insufficient evidence of correctness.

## [S] Scope and procedure

### 1. Establish the target and migration plan

1. Start a new task on a dedicated branch from up-to-date `main`; use the existing branch for follow-up work.
2. Inspect pending SQL, related store models, and feature tests. State which tables/data change and whether a backfill, constraint, index, or destructive operation is involved.
3. Identify the database host, port, database name, and schema/search path. A name ending in `_test` is a test safeguard, not proof that the host is safe. Confirm it is the intended test environment.
4. Capture `status` before applying anything. Account for **every** pending migration because `up` runs all of them.
5. For a persistent target, establish a recoverable backup and a plan for application compatibility, locks, and rollback before mutation. Do not infer production authorization from a request to write migration code or this guide. Check the actual task's authorized environment before running it there.

The API and migration runner read the process environment. Compose reads `.env` automatically, but Go does not. For configured local development:

```sh
set -a
. ./.env
set +a
docker compose config --quiet
docker compose up -d --wait
```

Do not overwrite existing environment files or print connection URLs/passwords into logs. `JWT_SECRET` is required to start the API, but the migration command itself only needs `DATABASE_URL`.

### 2. Prepare a dedicated database for validation

Requires Go 1.26+, Docker Compose, Python 3, and PostgreSQL client tools (`psql`; `pg_dump` for backups). The following example uses the local Compose cluster from `.env`; verify its published port and credentials match `DATABASE_URL` first.

Create a **new, disposable** database once:

```sh
docker compose exec -T postgres createdb -U "$POSTGRES_USER" uptime_migrations_test
export MIGRATION_DATABASE_URL="$(python3 - <<'PY'
import os
from urllib.parse import urlsplit, urlunsplit

url = urlsplit(os.environ["DATABASE_URL"])
if url.scheme not in ("postgres", "postgresql") or not url.hostname:
    raise SystemExit("Expected a PostgreSQL connection URL")
print(urlunsplit(url._replace(path="/uptime_migrations_test")))
PY
)"
```

If that database already exists, choose another explicitly disposable name and update **both** commands. Do not drop an existing database or ignore other creation errors. The URL transformation preserves connection options, including a configured search path; inspect them and ensure the chosen schema exists. This example defaults to the normal `public` schema.

Verify connectivity and target identity using the **same URL** that will be passed to the runner:

```sh
psql "$MIGRATION_DATABASE_URL" -X -v ON_ERROR_STOP=1 \
  -c 'SELECT current_database(), current_user, current_schema();' \
  -c 'SHOW search_path;'
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate status
```

The database must be the selected disposable `_test` database, and the schema/search path must match the plan. Stop on a connection, permission, identity, or unexpected-state error. Test commands below override `DATABASE_URL` only for that process; the working database is not their target.

### 3. Add a migration when the task changes the schema

For an apply-only task, skip creating SQL and validate the existing pending migrations.

- Add the next unused sequential version, padded to five digits, with a short snake_case description, for example `00005_add_<field>.sql`. Check for version collisions after syncing the branch.
- Keep one logical schema/data change per migration. Do not edit or renumber migrations already published/applied; add a corrective migration instead.
- Write the forward operation and its reverse explicitly. Use this skeleton after replacing the placeholders with actual SQL:

```sql
-- +goose Up
ALTER TABLE <table> ADD COLUMN <column> <type>;

-- +goose Down
ALTER TABLE <table> DROP COLUMN <column>;
```

- Terminate statements with semicolons. Use `-- +goose StatementBegin` / `-- +goose StatementEnd` around PL/pgSQL blocks with internal semicolons.
- Keep migrations transactional by default. `-- +goose NO TRANSACTION` affects both directions and needs a documented reason and partial-failure recovery procedure; do not add it simply to avoid an error.
- Plan existing-row behavior: backfill or defaults before adding required constraints; verify duplicate/null/orphan cases before tightening invariants. Make lock duration and compatibility with the previous application version explicit where they matter.
- A `Down` that drops a column/table reverses schema, **not lost data**. Document this distinction; do not claim a successful `down` restores a backup.
- Update the affected GORM model, persistence code, feature tests, and relevant README together. Do not introduce `AutoMigrate` or manual production DDL outside migration history.

### 4. Validate both fresh installation and an upgrade

For a new migration, test two paths on separate disposable databases:

1. **Fresh installation:** run the candidate's complete migration sequence on an empty database.
2. **Existing-data upgrade:** apply the PR base's migrations, insert representative baseline rows, then run the candidate's migration sequence. Assert row preservation, backfilled values, constraints, and the affected feature's behavior.

Before adding the new SQL, a baseline runner can be preserved with `go build -o bin/migrate-before ./cmd/migrate`. Use `DATABASE_URL="$MIGRATION_DATABASE_URL" ./bin/migrate-before up` on the upgrade database, seed baseline fixtures, then use the candidate runner. If new SQL already exists, build the baseline from the PR base in a separate checkout; do not remove files from the active checkout to simulate it.

For each target, apply and check state:

```sh
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate up
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate status
# Rerun: there should be no new application of SQL or data changes.
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate up
```

Use `psql "$MIGRATION_DATABASE_URL" -X -v ON_ERROR_STOP=1` to inspect the changed object (`\d+ <table>`), column types/defaults, indexes, and constraints. Run task-specific assertion queries for row counts, backfills, uniqueness, foreign keys, and ownership invariants. A SQL query must demonstrate the expected result; `ON_ERROR_STOP` catches SQL errors but does not assert returned values for you.

### 5. Exercise rollback and reapplication on disposable data

Confirm the latest applied migration is the one you intend to roll back. On the disposable target only:

```sh
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate down
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate status
# Verify the expected previous schema and documented data effects here.
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate up
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate status
```

Re-run schema/data assertions after `down` and after `up`. Re-seed data when a documented destructive rollback removed it. For multiple new files, `down` must be repeated deliberately one version at a time. Do not use the dev launcher to inspect the rolled-back state: it would immediately apply pending migrations again.

### 6. Apply to the task's persistent target and recover failures

After validation, re-confirm the authorized target, backup, pending versions, and application compatibility. Use its connection URL for `status → up → status`, repeat the data assertions, and smoke-test affected API operations. Rebuild/restart the application if its code changed. From the repository root, `node scripts/dev.mjs` is the combined **local** startup command and will run `up` first.

Each migration is normally a separate transaction, not the whole batch: an earlier migration may remain applied if a later one fails. The runner has a one-minute context deadline; a timeout does not prove nothing committed. Inspect `status` and actual schema/data before retrying. For nontransactional migrations, also check for partially created objects. Correct the cause and use a tested recovery plan; do not manually alter goose's version-tracking table to mark a failed operation successful.

Do not run `down` automatically against persistent data to repair a failure. Prefer a corrective forward migration when reversal would lose data. Restoring a backup is a separate operation with its own target and application-recovery plan. `docker compose down -v` deletes the database volume and is not migration recovery.

## [T] Testing / Acceptance criteria

Run backend checks from `backend/` with a confirmed, dedicated test URL:

```sh
go vet ./...
TEST_DATABASE_URL="$MIGRATION_DATABASE_URL" go test -count=1 -v ./...
TEST_DATABASE_URL="$MIGRATION_DATABASE_URL" go test -race ./...
go build -o bin/api ./cmd/api
```

Without `TEST_DATABASE_URL`, database integration tests skip. Their fixtures require the database name to end in `_test`, then create an isolated `test_<uuid>` schema and apply embedded migrations there. `TestMigrations` checks complete rollback/reapplication and registration afterward; it does **not** replace a new migration's existing-data upgrade and task-specific assertions. Inspect the test output for skips, not just the process exit code. If the frontend changes, also run its [required checks](../../frontend/AGENTS.md); authentication changes require E2E on its dedicated database.

Completion checklist:

- [ ] Target host/port/database/schema and the intended version range are confirmed without exposing secrets.
- [ ] SQL is new, versioned, and matches models and feature behavior; published migration history is unchanged.
- [ ] Fresh-install and existing-data upgrade checks pass where applicable, including explicit schema/data assertions.
- [ ] Re-running `up` changes nothing; rollback and reapplication match the documented effects on disposable data.
- [ ] Required tests/build pass, and skipped/unavailable checks are reported explicitly.
- [ ] For an authorized persistent application, post-migration state, data assertions, and feature smoke tests pass.
- [ ] Commit/PR describes the purpose, affected projects, migration version, data/rollback effects, and validation.

Use this report template in the task/PR; distinguish code delivery from actual application to a persistent database:

```text
Migration: <filename/version, or existing versions applied>
Purpose and affected schema/data: <change>
Target: <environment, host/port, database, schema; no credentials>
Before → after: <applied/pending versions>
Fresh install / existing-data upgrade: <assertions and results>
Idempotent rerun / rollback / reapplication: <results and data-loss limits>
Tests, build, and feature smoke check: <commands and results>
Persistent application: <applied and verified, or not performed>
Limitations or next action: <skipped checks, backup/recovery requirements>
```
