---
name: uptime-browser-verification
description: Mandatory post-implementation layout and functional verification of the Uptime application using Playwright MCP. Apply before completing frontend tasks or backend tasks that affect browser-visible flows, and when explicitly asked to check the running UI.
---

# Uptime Browser Verification

Use Playwright MCP to verify the implementation against the user's acceptance criteria in the running application. This is a required completion step under [repository guidelines](../../../AGENTS.md), in addition to the affected project's automated checks.

## Establish the target

- Read the task requirements and final diff; identify affected routes, actions, and expected results. Include adjacent flows that the change can break, without auditing unrelated pages.
- Read [frontend guidelines](../../../frontend/AGENTS.md) and the relevant [startup instructions](../../../README.md). Use the implementation's checkout and confirm the running URL, ports, and database target; do not test a stale server from another worktree.
- Discover the available Playwright MCP tools and their schemas. Use MCP browser tools for navigation, interactions, screenshots, resizing, console messages, and network inspection. Do not replace them with computer-use tools, direct Playwright scripts, or CLI browser automation.
- If MCP is not connected, report the missing dependency and consult the [project setup instructions](../../../README.md#browser-verification). Do not claim the browser check passed. If application startup is blocked, describe the blocker and continue checks that do not depend on it.

## Use an isolated application for mutations

Read-only inspection can use the working application. Registration, profile edits, monitor CRUD, session changes, and other persisted actions must use the dedicated E2E environment.

Start `node scripts/dev.mjs --e2e` from the repository root for manual MCP verification. This selects `uptime_e2e_test`, frontend `http://localhost:3001`, and API `http://127.0.0.1:8081`. Confirm the targets before interacting. Do not run this alongside `npm run test:e2e`, whose webServer starts the same environment. Do not stop another process just to take its port.

Use a unique test account and synthetic data; never reuse the user's real account or mutate the working database for validation. Read existing `frontend/e2e/` scenarios for applicable flows, but verify the changed behavior yourself through MCP. Stop only servers you started and remove only disposable records you created when practical. Do not reset shared test data.

## Verify functionality

- Navigate to each affected route and perform the intended actions using UI controls. Use fresh accessibility snapshots and observed element references or semantic locators instead of guessed selectors.
- Check the successful path and relevant failure paths: invalid input, empty states, server errors, cancellation, and disabled/loading controls when applicable. Confirm meaningful outcomes, rather than treating a click or a toast as proof of success.
- For persisted changes, reload or revisit the page and confirm the state survives. For authentication or protected-route changes, verify the applicable logged-in/logged-out states and redirects. Exercise related keyboard interactions and focus behavior.
- Inspect console errors and failed requests after the actions. Distinguish expected validation/authentication responses from unexpected failures and attribute errors to the tested flow. Do not collect tokens, cookies, passwords, or personal data as evidence.

## Inspect layout visually

- Use MCP resizing to inspect at least a desktop viewport (1440 × 900) and a mobile viewport (390 × 844). Add a width near an affected breakpoint when needed.
- Capture and actually inspect screenshots at both sizes. An accessibility snapshot cannot establish visual correctness.
- Check spacing, alignment, typography, content wrapping, horizontal overflow, clipped controls, overlapping layers, scroll behavior, and visibility of primary actions. Inspect changed dialogs, menus, forms, and error/empty states as applicable.
- Verify light and dark themes when the change affects colors or shared styling. Compare against the user's design/reference or the existing application's conventions; avoid unrelated redesigns.
- Save screenshots outside tracked source or in an ignored output directory. Link relevant screenshots in the completion report and include them in the PR for visible UI changes, as required by frontend guidelines.

## Fix and report

Fix discovered regressions within the authorized task and repeat the affected functional and visual checks. Rerun automated checks when the fixes warrant it. If a defect cannot be resolved, report the reproducible failure and leave that criterion failed or blocked.

Before completion, report the tested environment, routes/scenarios, viewport sizes, results, and screenshot evidence. Use passed, failed, blocked, or not applicable for each relevant criterion; include the reason for blocked or not applicable results. State remaining limitations explicitly. Never mark a task fully verified while a required browser check is failed or blocked.

For documentation-only or agent-configuration work without application changes, record browser verification as not applicable with that reason. This exception does not apply to UI changes or backend behavior exposed through the UI.
