# Repository Guidelines

## Scope & Project Organization

This repository contains a Go authentication API and a Next.js authentication frontend; uptime monitoring is not implemented yet.

These guidelines apply to both projects. Read the corresponding project guide before making changes:

- [Backend guidelines](backend/AGENTS.md)
- [Frontend guidelines](frontend/AGENTS.md)

Keep backend and frontend dependencies separate. Avoid adding speculative infrastructure or domain abstractions.

## Code Comments

- Explain why the code is written this way instead of restating what it does; the code already shows the behavior.
- Do not comment on obvious behavior; use clear, descriptive names.
- Add doc comments to public functions describing their purpose, return values, error conditions, and context handling where applicable.
- Explain the reasoning behind complex arithmetic in a nearby comment instead of paraphrasing the calculation.
- When changing code, review nearby comments and keep them up to date; remove inaccurate comments.

## API Contract

- Add swaggo annotations to the controller handler for every new backend HTTP route, covering its method and path, request parameters and body, response schemas and status codes, errors, and applicable authentication.
- When changing a route or its API behavior, including request/response types, validation, status codes, or authentication, update the corresponding annotations and regenerate the OpenAPI contract by running `go generate ./cmd/api` from `backend/`.
- Run `go test ./docs` from `backend/` to verify contract freshness and route coverage. Include both `backend/docs/swagger.json` and `backend/docs/swagger.yaml` in the same commit as the API change; do not edit generated contracts manually. Follow the additional [backend guidelines](backend/AGENTS.md) for annotation placement and security definitions.

## Validation

Run the checks documented in each affected project's guide from that project's directory. Report validation performed and any checks that could not run. Update relevant documentation when changing setup or commands.

## Browser Work & Required Implementation Verification

- Use **Playwright MCP** for all browser interaction, inspection, and manual verification in this project.
- After every implementation task, apply [$uptime-browser-verification](.agents/skills/uptime-browser-verification/SKILL.md) before declaring the task complete or requesting review. Verify the affected user flows and layout in the running application, including backend changes observable through the UI.
- Check desktop and mobile layouts, exercise functionality through the UI, inspect console/network errors, and capture screenshots for visible changes. Fix issues within the task's scope and repeat affected checks.
- Automated tests, builds, and accessibility snapshots alone do not satisfy this browser verification requirement. Existing Playwright E2E tests remain required where the project guides specify them.
- If Playwright MCP or the application is unavailable, complete independent checks, report the specific blocker, and mark browser verification as incomplete; do not silently substitute another browser tool or claim success.
- Documentation-only and agent-configuration tasks without application behavior changes may mark browser verification as not applicable, explaining why.

## When starting a new task use GitHub Flow & Branch Naming

- Before starting a new task, create a dedicated branch from the up-to-date `main` branch. Do not implement changes or commit directly on `main`.
- Name branches `feat/<feature-name>` for features and improvements (including documentation), or `fix/<fix-name>` for bug fixes. Use short, descriptive English names in lowercase kebab-case, for example `feat/avatar-upload` or `fix/profile-validation`.
- Keep one task per branch. Continue follow-up work for the same task on its existing branch.
- Commit changes according to the conventions below and run the required checks before requesting review.
- Push the branch and open a pull request targeting `main`. Describe the changes and validation results, and link the relevant task or issue.

## Commit & Pull Request Guidelines

Use concise imperative commit messages with a type and optional scope, for example `docs: update contributor guidelines` or `feat(auth): add registration`. Keep changes focused. PRs should describe purpose, affected projects, and validation performed; link relevant issues.

## Commits

- Use Conventional Commits (feat, fix, refactor, test, docs, chore).
- Keep the subject line within 72 characters and use the imperative mood.
- Avoid emojis and filler such as "significantly improved".
- Include a body only when needed to explain why, rather than what.
- Use one commit per logical step.

## Security & Configuration

Never commit secrets, dependency directories, or build outputs. Keep project-specific configuration and dependencies within the owning project.
