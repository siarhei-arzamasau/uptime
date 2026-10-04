## [C] Context

Users should be able to upload an avatar from their profile and see it displayed.

## [O] Outcome / Goal

The user opens the profile editor, clicks **Upload avatar**, chooses an image in a supported format such as JPEG or PNG, and confirms the selection. A preview appears. Clicking the shared **Save changes** button saves the new avatar and updates it in both the profile and the top toolbar.

## [S] Scope

Store avatars locally in a dedicated directory excluded from Git, and serve the images to the frontend from that directory.

## [T] Testing / Acceptance criteria

The avatar upload test passes and the new image upload API works.

## Completed

- [x] JPEG/PNG selection and local preview with cancellation.
- [x] A shared Save changes button saves the name and avatar.
- [x] Authenticated upload API with format, file size, and image dimension validation.
- [x] Local storage in backend/var/avatars, excluded from Git, and an image-serving API.
- [x] Profile migration and toolbar avatar update after saving.
- [x] Replacement of the old file and cleanup of the new file if the database save fails.
- [x] Unit tests, Go/PostgreSQL integration tests, and Playwright desktop/mobile coverage.
- [x] API and local storage documentation.

Validation: 70 Vitest tests and 18 Playwright E2E tests passed, along with Go tests against PostgreSQL, go vet, ESLint, and production builds. Restart `node scripts/dev.mjs` to apply the migration locally.
