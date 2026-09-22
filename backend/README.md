# Backend

Go API for authentication, profiles, and website monitors: PostgreSQL 17, GORM, JWT, and refresh sessions.

## Structure

- `internal/auth/`: authentication service, tokens, HTTP controller, and its integration tests.
- `internal/monitor/`: authenticated website monitor creation and listing.
- `internal/httpx/`: shared CORS/CSRF middleware and JSON response helpers, without domain controllers.
- `internal/store/`: GORM models and repositories; `internal/config/`: configuration.
- `cmd/api/`: application wiring and module route registration on the shared mux.

Place new controllers inside their owning modules and expose `RegisterRoutes`.

## Requirements and startup

Requires Go 1.26+, Docker with Compose, `curl`, and Python 3 for the examples below. On macOS, start Docker Desktop (`open -a Docker`) and wait for `docker info` to succeed.

Run all commands from `backend/`. If Go is installed in the temporary development directory, add it to PATH:

```sh
export PATH="/private/tmp/uptime-toolchain/go/bin:$PATH"
```

For ongoing development, install Go normally: the system may clear the temporary directory.

Create local configuration (do not overwrite an existing `.env`):

```sh
cp -n .env.example .env
# Generate two different values for POSTGRES_PASSWORD and JWT_SECRET:
openssl rand -hex 32
openssl rand -hex 32
```

Complete `.env`: the password must match in `POSTGRES_PASSWORD` and `DATABASE_URL`. Use a hex string to avoid URL encoding. If port 5432 is occupied, change both `POSTGRES_PORT` and the port in `DATABASE_URL`; the current local environment uses **5433**.

```sh
set -a
. ./.env
set +a

docker compose config --quiet
docker compose up -d --wait
go mod download
go run ./cmd/migrate up
go run ./cmd/migrate status
go run ./cmd/api
```

The API listens on `127.0.0.1:8080`. The application and migration command read the process environment; only Compose reads `.env` automatically, so export it as shown above before running Go commands. Changing the password in `.env` does not change the password of a database already initialized in the volume.

## Configuration

| Variable | Purpose |
|---|---|
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | Initial container configuration |
| `POSTGRES_PORT` | Local PostgreSQL port; default 5432 |
| `DATABASE_URL` | Connection URL for the API and migrations |
| `HTTP_ADDR` | Server address; default `127.0.0.1:8080` |
| `JWT_SECRET` | Required random secret of at least 32 bytes, not a placeholder |
| `JWT_ISSUER`, `JWT_AUDIENCE` | Defaults: `uptime-api`, `uptime-web` |
| `ALLOWED_ORIGIN` | Frontend origin without a trailing `/`; default `http://localhost:3000` |
| `COOKIE_SECURE` | Defaults to `true`; `.env.example` uses `false` for local HTTP |

Production requires HTTPS and `COOKIE_SECURE=true`; `sslmode=disable` in the example is for the local database. Never commit secrets, `.env`, or cookie jar contents.

## Website monitors

- `POST /api/v1/monitors`: create a monitor with JSON `{"url":"https://example.com","interval_seconds":7}`; returns the monitor with status `201`.
- `GET /api/v1/monitors`: list the authenticated user's monitors, newest first, as `{"monitors":[...]}`. An empty list is `[]`.

Both endpoints require a Bearer JWT. Creation also requires an allowed Origin or `X-CSRF-Protection: 1`. Ownership comes only from the verified JWT; extra request fields are rejected. URLs must be absolute HTTP/HTTPS URLs up to 2048 bytes after trimming, without embedded credentials, whitespace, or fragments. Intervals are whole numbers from 1 to 2,147,483,647 seconds; the frontend converts user-entered seconds, minutes, or hours to seconds. Each monitor exposes `id`, `url`, `interval_seconds`, and `created_at`.

Migration `00004_monitors.sql` creates the monitor table and owner/list-order index. Restart `node scripts/dev.mjs` to apply it. Only configuration is saved: there are no outbound checks, workers, or uptime results yet.

## Profile

- `GET /api/v1/profile`: retrieve your own profile using a Bearer JWT.
- `PATCH /api/v1/profile`: update the name with JSON `{"name":"Sergey"}`. Returns the updated profile.

Name is optional, with a maximum of 100 Unicode characters after trimming surrounding whitespace; control characters are forbidden. An empty string clears the name. Other fields, including `email` and `id`, are rejected with `400`. The user is identified only by the verified JWT. Updates require an allowed Origin or `X-CSRF-Protection: 1`.

Migration `00002_user_name.sql` adds the `name` field with an empty string for existing users. Restart `node scripts/dev.mjs` from the root to apply migrations and rebuild the API.

## API and example workflow

Routes: `POST /api/v1/auth/register`, `/login`, `/refresh`, `/logout`, and `GET /api/v1/auth/me`.

Registration and login accept JSON with `email` and `password`. Email is normalized; passwords contain 12–128 characters. Successful responses include `user`, `access_token`, `token_type`, and `expires_in`. Refresh returns only token fields. User data includes `id`, `email`, `name`, and `created_at`. The refresh token is available only through an HttpOnly cookie.

In another terminal:

```sh
API=http://localhost:8080/api/v1/auth
COOKIE_JAR=$(mktemp)
chmod 600 "$COOKIE_JAR"

# Use a new email for each registration attempt.
AUTH=$(curl --fail-with-body -sS -c "$COOKIE_JAR" \
  -H 'Content-Type: application/json' -H 'X-CSRF-Protection: 1' \
  -d '{"email":"demo@example.com","password":"correct horse battery staple"}' \
  "$API/register")
TOKEN=$(printf '%s' "$AUTH" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl --fail-with-body -sS -H "Authorization: Bearer $TOKEN" "$API/me"

AUTH=$(curl --fail-with-body -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
  -X POST -H 'X-CSRF-Protection: 1' "$API/refresh")
TOKEN=$(printf '%s' "$AUTH" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl --fail-with-body -sS -H "Authorization: Bearer $TOKEN" "$API/me"
curl --fail-with-body -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
  -X POST -H 'X-CSRF-Protection: 1' "$API/logout"

# Logging in again creates a new independent session.
AUTH=$(curl --fail-with-body -sS -c "$COOKIE_JAR" \
  -H 'Content-Type: application/json' -H 'X-CSRF-Protection: 1' \
  -d '{"email":"demo@example.com","password":"correct horse battery staple"}' \
  "$API/login")
curl --fail-with-body -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
  -X POST -H 'X-CSRF-Protection: 1' "$API/logout"
rm "$COOKIE_JAR"
unset AUTH TOKEN
```

From a browser, use `credentials: "include"` for the refresh cookie and a Bearer JWT for `/me`. CORS allows only `ALLOWED_ORIGIN`; mutating requests from a foreign Origin are rejected. Clients without Origin must send `X-CSRF-Protection: 1`.

JWTs last 15 minutes. Refresh sessions last 30 days from login without extension. Every refresh replaces the token; clients must refresh sequentially, including across tabs sharing a cookie. Reusing an old token revokes that session, including during concurrent refresh. Other devices remain unaffected. Losing a refresh response may require logging in again.

Logout is idempotent and revokes the current refresh session. Previously issued JWTs remain valid until their 15-minute expiry. Password recovery and email verification are not implemented yet.

Errors use `{"error":{"code":"…","message":"…"}}`: 400 for validation, 401 for authentication, 403 for Origin/CSRF, 409 for an existing email, and 500 for internal failures without database details.

## Validation and migrations

```sh
gofmt -w .
go vet ./...
go test ./...
go build -o bin/api ./cmd/api

# Create a separate integration test database once:
docker compose exec -T postgres createdb -U "$POSTGRES_USER" uptime_test
export TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT:-5432}/uptime_test?sslmode=disable"
go test -count=1 -v ./...
go test -race ./...
```

Without `TEST_DATABASE_URL`, PostgreSQL tests are explicitly skipped. The database name must end in `_test`; each test creates its own schema, applies migrations, and removes only that schema afterward. Never use the working database. Coverage includes HTTP flows, concurrency, revocation, transaction rollback on write failures, and up/down/up migrations.

Migrations live in `migrations/`, are embedded with `go:embed`, and run explicitly; `AutoMigrate` is not used. `go run ./cmd/migrate down` rolls back the latest migration and may delete data. Use it deliberately and test rollback against a test database.

## Managing PostgreSQL

```sh
docker compose ps
docker compose logs --tail=50 postgres
docker compose down              # Stop while preserving data
docker compose up -d --wait      # Start again
```

**Deleting the entire local database:** `docker compose down -v` removes the volume and all its data. This is not the normal shutdown command.

## Avatars

`POST /api/v1/profile/avatar` accepts a Bearer JWT and `multipart/form-data`: one `name` field and one `avatar` file. Name and avatar are saved together. JPEG/PNG up to 5 MB and 2048 × 2048 pixels are supported. The backend validates decoding and re-encodes the image without original metadata; SVG and other formats are rejected. Invalid formats return `400`, oversized files return `413`, and missing authorization returns `401`.

`avatar_url` is included in profile, `/auth/me`, registration, and login responses; an empty string means no image. `GET /api/v1/avatars/{uuid}.png` (or `.jpg`) serves the image through a public, unguessable URL without directory listing. Do not use avatars for private documents.

Files are stored in `AVATAR_DIR` (by default `backend/var/avatars` when running the API from `backend/`). The `backend/var/` directory is excluded from Git. Replacement deletes the previous file after a successful database update; a failed update removes the new file. Back up or migrate both PostgreSQL and this directory; the API process needs write access. If you override the path, exclude it from Git yourself.

Migration `00003_user_avatar.sql` adds the file reference without changing existing profiles. The shared launcher applies it on startup. E2E uses the separate `backend/var/e2e-avatars` directory; Go integration tests use temporary directories.
