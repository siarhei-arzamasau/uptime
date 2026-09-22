# Repository Guidelines

## Scope & Project Organization

This repository contains a Go authentication API and a Next.js authentication frontend; uptime monitoring is not implemented yet.

These guidelines apply to both projects. Read the corresponding project guide before making changes:

- [Backend guidelines](backend/AGENTS.md)
- [Frontend guidelines](frontend/AGENTS.md)

Keep backend and frontend dependencies separate. Avoid adding speculative infrastructure or domain abstractions.

## Validation

Run the checks documented in each affected project's guide from that project's directory. Report validation performed and any checks that could not run. Update relevant documentation when changing setup or commands.

## Commit & Pull Request Guidelines

Use concise imperative commit messages with a type and optional scope, for example `docs: update contributor guidelines` or `feat(auth): add registration`. Keep changes focused. PRs should describe purpose, affected projects, and validation performed; link relevant issues.

## Security & Configuration

Never commit secrets, dependency directories, or build outputs. Keep project-specific configuration and dependencies within the owning project.

## Commits

- Use Conventional Commits (feat, fix, refactor, test, docs, chore).
- Keep the subject line within 72 characters and use the imperative mood.
- Avoid emojis and filler such as "significantly improved".
- Include a body only when needed to explain why, rather than what.
- Use one commit per logical step.
