import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("creates configurable website monitors, persists them and isolates accounts", async ({ page, context }, testInfo) => {
  await page.goto("/register");
  await page.getByLabel("Email address").fill(`monitor-${randomUUID()}@example.com`);
  await page.getByLabel("Password", { exact: true }).fill("correct horse battery staple");
  await page.getByLabel("Confirm password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("heading", { name: "No websites yet" })).toBeVisible();
  await page.getByRole("button", { name: "Add website" }).click();
  await expect(page.getByLabel("Website URL")).toBeFocused();
  await page.getByLabel("Website URL").fill("ftp://example.com");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByLabel("Website URL")).toHaveAttribute("aria-invalid", "true");
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("button", { name: "Add website" })).toBeFocused();

  for (const [index, [count, unit, description]] of [["7", "1", "Every 7 seconds"], ["10", "60", "Every 10 minutes"], ["2", "3600", "Every 2 hours"]].entries()) {
    await page.getByRole("button", { name: "Add website" }).click();
    await page.getByLabel("Website URL").fill(`https://example.com/health/${index}`);
    await page.getByLabel("Check interval", { exact: true }).fill(count);
    await page.getByLabel("Interval unit").selectOption(unit);
    if (index === 0) {
      // Exercise the shared refresh path during creation.
      await context.clearCookies({ name: "uptime_access" });
      await page.route("**/api/auth/monitors/create", route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: { message: "Service unavailable" } }) }));
      await page.getByRole("button", { name: "Create", exact: true }).click();
      await expect(page.getByRole("main").getByRole("alert")).toHaveText("Service unavailable");
      await expect(page.getByLabel("Check interval", { exact: true })).toHaveValue(count);
      await page.unroute("**/api/auth/monitors/create");
    }
    if (index === 1) await page.screenshot({ path: testInfo.outputPath("create-monitor-form.png"), fullPage: true });
    const response = page.waitForResponse(reply => reply.url().endsWith("/api/auth/monitors/create"));
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const reply = await response; expect(reply.status()).toBe(201);
    const data = await reply.json(); expect(data.monitor.interval_seconds).toBe(Number(count) * Number(unit)); expect(data).not.toHaveProperty("access_token");
    await expect(page.getByText(description, { exact: true })).toBeVisible();
  }
  await page.reload();
  await expect(page.getByRole("listitem")).toHaveCount(3);
  await expect(page.getByText("Every 7 seconds", { exact: true })).toBeVisible();
  await expect(page.getByText("Every 10 minutes", { exact: true })).toBeVisible();
  await expect(page.getByText("Every 2 hours", { exact: true })).toBeVisible();
  await expect(page.getByText("Not checked yet", { exact: true })).toHaveCount(3);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("website-monitors.png"), fullPage: true });
  await page.getByRole("button", { name: "Toggle color theme" }).click();
  await page.screenshot({ path: testInfo.outputPath("website-monitors-alternate-theme.png"), fullPage: true });

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/register");
  await page.getByLabel("Email address").fill(`other-${randomUUID()}@example.com`);
  await page.getByLabel("Password", { exact: true }).fill("correct horse battery staple");
  await page.getByLabel("Confirm password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("heading", { name: "No websites yet" })).toBeVisible();
  await expect(page.getByText("https://example.com/health/0", { exact: true })).toHaveCount(0);
});
