# Backend

Go API для регистрации и аутентификации: PostgreSQL 17, GORM, JWT и refresh-сессии.

## Структура

- `internal/auth/` — сервис аутентификации, токены, HTTP-контроллер и его интеграционные тесты.
- `internal/httpx/` — общие middleware CORS/CSRF и функции JSON-ответов, без контроллеров предметной области.
- `internal/store/` — модели и репозитории GORM; `internal/config/` — конфигурация.
- `cmd/api/` — сборка приложения и регистрация маршрутов модулей на общем mux.

Контроллеры новых модулей размещаются внутри соответствующих модулей и предоставляют `RegisterRoutes`.

## Требования и запуск

Нужны Go 1.26+, Docker с Compose, `curl` и Python 3 для примеров ниже. На macOS запустите Docker Desktop (`open -a Docker`) и дождитесь успешного `docker info`.

Все команды выполняются из `backend/`. Если Go установлен во временную папку текущей разработки, добавьте его в PATH:

```sh
export PATH="/private/tmp/uptime-toolchain/go/bin:$PATH"
```

Для постоянной разработки установите Go обычным способом: временная папка может быть очищена системой.

Создайте локальную конфигурацию (не перезаписывайте уже существующий `.env`):

```sh
cp -n .env.example .env
# Сгенерируйте два разных значения для POSTGRES_PASSWORD и JWT_SECRET:
openssl rand -hex 32
openssl rand -hex 32
```

Заполните `.env`: пароль должен совпадать в `POSTGRES_PASSWORD` и `DATABASE_URL`. Для пароля используйте hex-строку, чтобы не требовалось URL-кодирование. Если порт 5432 занят, измените и `POSTGRES_PORT`, и порт в `DATABASE_URL`; в текущем локальном окружении используется **5433**.

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

API слушает `127.0.0.1:8080`. Приложение и команда миграций читают окружение процесса; `.env` автоматически читает только Compose, поэтому перед Go-командами нужен экспорт выше. Изменение пароля в `.env` не меняет пароль уже созданной БД в volume.

## Конфигурация

| Переменная | Назначение |
|---|---|
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | Начальная конфигурация контейнера |
| `POSTGRES_PORT` | Локальный порт PostgreSQL; по умолчанию 5432 |
| `DATABASE_URL` | URL подключения API и миграций |
| `HTTP_ADDR` | Адрес сервера; по умолчанию `127.0.0.1:8080` |
| `JWT_SECRET` | Обязательный случайный секрет минимум 32 байта, не значение из шаблона |
| `JWT_ISSUER`, `JWT_AUDIENCE` | По умолчанию `uptime-api`, `uptime-web` |
| `ALLOWED_ORIGIN` | Origin фронтенда без завершающего `/`; по умолчанию `http://localhost:3000` |
| `COOKIE_SECURE` | По умолчанию `true`; `.env.example` задаёт `false` для локального HTTP |

Для production требуется HTTPS и `COOKIE_SECURE=true`; `sslmode=disable` в примере относится к локальной БД. Секреты, `.env` и содержимое cookie jar не коммитить.

## API и пример сценария

Маршруты: `POST /api/v1/auth/register`, `/login`, `/refresh`, `/logout` и `GET /api/v1/auth/me`.

Регистрация и вход принимают JSON с `email` и `password`. Email нормализуется; пароль содержит 12–128 символов. Успешный ответ содержит `user`, `access_token`, `token_type`, `expires_in`. Refresh возвращает только поля токена. Пользователь содержит `id`, `email`, `created_at`. Refresh-токен доступен только в HttpOnly cookie.

В другом терминале:

```sh
API=http://localhost:8080/api/v1/auth
COOKIE_JAR=$(mktemp)
chmod 600 "$COOKIE_JAR"

# Используйте новый email для каждого повторения регистрации.
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

# Повторный вход создаёт новую независимую сессию.
AUTH=$(curl --fail-with-body -sS -c "$COOKIE_JAR" \
  -H 'Content-Type: application/json' -H 'X-CSRF-Protection: 1' \
  -d '{"email":"demo@example.com","password":"correct horse battery staple"}' \
  "$API/login")
curl --fail-with-body -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
  -X POST -H 'X-CSRF-Protection: 1' "$API/logout"
rm "$COOKIE_JAR"
unset AUTH TOKEN
```

Из браузера используйте `credentials: "include"` для работы с refresh-cookie и Bearer JWT для `/me`. CORS разрешает только `ALLOWED_ORIGIN`; изменяющий запрос с чужим Origin отклоняется. Клиенты без Origin должны отправлять `X-CSRF-Protection: 1`.

JWT действует 15 минут. Refresh-сессия действует 30 дней от входа без продления. Каждый refresh заменяет токен; обновления на клиенте должны быть последовательными, в том числе между вкладками с общей cookie. Повторное использование старого токена отзывает эту сессию, включая при конкурентном refresh. Другие устройства продолжают работать. После потери ответа refresh может понадобиться повторный вход.

Logout идемпотентен и отзывает текущую refresh-сессию. Уже выданный JWT действует до истечения 15 минут. Восстановление пароля и подтверждение email пока отсутствуют.

Ошибки имеют вид `{"error":{"code":"…","message":"…"}}`: 400 — валидация, 401 — аутентификация, 403 — Origin/CSRF, 409 — занятый email, 500 — внутренняя ошибка без деталей БД.

## Проверки и миграции

```sh
gofmt -w .
go vet ./...
go test ./...
go build -o bin/api ./cmd/api

# Создать один раз отдельную БД для интеграционных тестов:
docker compose exec -T postgres createdb -U "$POSTGRES_USER" uptime_test
export TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT:-5432}/uptime_test?sslmode=disable"
go test -count=1 -v ./...
go test -race ./...
```

Без `TEST_DATABASE_URL` PostgreSQL-тесты явно пропускаются. Имя тестовой БД должно оканчиваться на `_test`; каждый тест создаёт отдельную схему, применяет миграции и удаляет только свою схему после завершения. Не указывайте рабочую БД. Проверяются HTTP-сценарии, конкуренция, отзыв, откат транзакций при ошибках записи и миграции up/down/up.

Миграции находятся в `migrations/`, встроены в команду через `go:embed` и применяются явно; `AutoMigrate` отсутствует. `go run ./cmd/migrate down` откатывает последнюю миграцию и может удалять данные — используйте только осознанно, проверку отката выполняйте на тестовой БД.

## Управление PostgreSQL

```sh
docker compose ps
docker compose logs --tail=50 postgres
docker compose down              # Остановить, сохранив данные
docker compose up -d --wait      # Запустить снова
```

**Удаление всей локальной БД:** `docker compose down -v` удаляет volume и все его данные. Это не обычная команда остановки.
