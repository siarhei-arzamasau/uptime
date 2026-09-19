# Frontend Guidelines

## Project Structure

This directory contains Next.js 16.3.5 with React 19, TypeScript, and App Router. Routes and layouts live in `src/app/`; auth code lives in `src/features/auth/`; `@/*` resolves to `src/*`. No static assets exist yet; add them under `public/` when needed.

## Development Commands

Requires Node.js 22.12+ and npm for the application and current test tooling. Run commands from `frontend/`:

- `npm ci`: install dependencies from the committed lockfile.
- `npm run dev`: start development at `http://localhost:3000`.
- `npm run lint`: run Next.js and TypeScript ESLint rules.
- `npm run build`: build for production and check TypeScript.
- `npm start`: serve an existing production build.

## Coding Style & Naming

Match existing TypeScript formatting: two-space indentation, double quotes, and semicolons. Keep strict typing enabled. Use PascalCase component names and Next.js route filenames such as `page.tsx` and `layout.tsx`. ESLint is configured; Prettier is not.

## Testing & Review

Use Vitest for Node-side session logic and jsdom with React Testing Library for components. Colocate `*.test.ts`/`*.test.tsx` files. Run `npm test`, `npm run lint`, and `npm run build`; authentication changes also require `npm run test:e2e` against the dedicated PostgreSQL database. Coverage is available via `npm run test:coverage`, without a numeric threshold. Playwright covers desktop and mobile. Never point E2E at the working database. Include screenshots in PRs for visible UI changes.

## Dependencies & Agent Instructions

Keep `package-lock.json` synchronized with dependency changes. Consult the installed Next.js documentation as directed below before writing frontend code. Preserve the framework-managed instruction block.

<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

Keep Go access in the server-only auth adapter. Never serialize tokens to client responses or React props. Both auth tokens belong in HttpOnly cookies; only theme preferences may be read by JavaScript. Preserve Web Locks serialization across tabs and never trigger refresh from SSR/prefetch. Use `node scripts/dev.mjs` from the repository root for a combined local startup.
