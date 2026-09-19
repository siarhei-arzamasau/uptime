# Uptime App

Два независимых проекта: Go API аутентификации и Next.js интерфейс регистрации, входа и личного кабинета.

```text
backend/
  cmd/api/main.go    # HTTP API аутентификации
  internal/          # Аутентификация, HTTP и работа с БД
  go.mod
frontend/
  src/app/           # Базовый Next.js App Router
  package.json
  package-lock.json
```

## Backend

Требуется Go 1.26+.

Инструкции запуска PostgreSQL через Docker Compose, миграций, API и тестов: [backend/README.md](backend/README.md).

План и состояние задач: [docs/authentication-plan.md](docs/authentication-plan.md).

## Frontend

Next.js 16, TypeScript, ESLint. Требуется Node.js 22.12+ и npm.

```sh
cd frontend
npm ci
npm run dev
```

Фронтенд доступен по адресу http://localhost:3000.

Проверка и сборка: `npm run lint` и `npm run build` из `frontend/`.

## Совместный запуск

Требуются Node.js 22.12+, Go 1.26+, Docker Compose, установленные npm-зависимости и локальные `backend/.env`, `frontend/.env.local` (см. примеры).

```sh
node scripts/dev.mjs
```

Скрипт поднимает PostgreSQL, применяет миграции и запускает API и фронтенд. Ctrl+C останавливает оба приложения, сохраняя БД. Если Go находится во временной установке, сначала добавьте `/private/tmp/uptime-toolchain/go/bin` в PATH.

Подробные настройки, темы и тесты: [frontend/README.md](frontend/README.md). План и чекбоксы: [docs/frontend-authentication-plan.md](docs/frontend-authentication-plan.md).
