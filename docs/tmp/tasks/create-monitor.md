## [C] Context

We are moving on to creating website monitors.

## [O] Outcome / Goal

On the main screen, the user clicks “Add website” and enters the URL to monitor and the check interval in the form that appears. The interval is configurable: the user enters a positive whole number and selects seconds, minutes, or hours. Examples include every 5 seconds, every 7 seconds, every 1 minute, every 10 minutes, every 1 hour, or every 2 hours. The user then clicks “Create”, and the newly created monitor appears on the main screen.

## [S] Scope

Only create and display monitors for now. Running the actual monitoring checks is out of scope.

## [T] Testing / Acceptance criteria

- The user can create a monitor by providing a website URL, a positive whole-number interval, and a unit (seconds, minutes, or hours).
- Intervals are configurable rather than limited to preset values.
- The created monitor appears on the main screen with its URL and check interval and remains available after reloading the page.
- Invalid URLs and invalid intervals are rejected with clear validation messages.
- Tests cover successful creation and configurable intervals in all three units.
