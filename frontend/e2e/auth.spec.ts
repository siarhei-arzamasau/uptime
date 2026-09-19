import { test, expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
const password = "correct horse battery staple";
async function register(page: Page) {
  const email = `e2e-${randomUUID()}@example.com`;
  await page.goto("/register");
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/dashboard$/); await expect(page.getByText(email)).toBeVisible(); return email;
}
test("register, restore, logout, login and private cookies", async ({ page, context }) => {
  const responses: string[] = [];
  page.on("response", async r => { if (r.url().includes("/api/auth/")) { try { responses.push(await r.text()); } catch {} } });
  const email = await register(page);
  const cookies = await context.cookies();
  const access = cookies.find(c => c.name === "uptime_access")!, refresh = cookies.find(c => c.name === "uptime_refresh")!;
  expect(access.httpOnly).toBe(true); expect(refresh.httpOnly).toBe(true); expect(access.path).toBe("/"); expect(refresh.path).toBe("/api/auth"); expect(refresh.sameSite).toBe("Lax");
  const storage = await page.evaluate(() => [document.cookie, JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join(" "));
  expect(storage).not.toContain(access.value); expect(storage).not.toContain(refresh.value);
  await page.reload(); await expect(page.getByText(email)).toBeVisible();
  await context.clearCookies({ name: "uptime_access" }); await page.reload(); await expect(page.getByText(email)).toBeVisible();
  const rotated = (await context.cookies()).find(c => c.name === "uptime_refresh")!;
  expect(rotated.value).not.toBe(refresh.value); expect(rotated.expires).toBeLessThanOrEqual(refresh.expires + 1);
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/);
  expect((await context.cookies()).some(c => c.name.startsWith("uptime_") && c.name !== "uptime_theme")).toBe(false);
  await page.getByLabel("Email address").fill(email); await page.getByLabel("Password", { exact: true }).fill(password); await page.getByLabel("Password", { exact: true }).press("Enter"); await expect(page).toHaveURL(/\/dashboard$/);
  for (const body of responses) { expect(body).not.toContain(access.value); expect(body).not.toContain(refresh.value); expect(body).not.toContain("access_token"); }
});
test("validation, duplicate account and incorrect login", async ({ page }) => {
  const email = await register(page);
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email address").fill(email); await page.getByLabel("Password", { exact: true }).fill("incorrect password"); await page.getByRole("button", { name: "Sign in" }).click(); await expect(page.getByRole("main").getByRole("alert")).toContainText("Incorrect email");
  await page.getByRole("link", { name: "Create an account" }).click(); await page.getByRole("button", { name: "Create account" }).click(); await expect(page.getByLabel("Email address")).toBeFocused();
  await page.getByLabel("Email address").fill(email); await page.getByLabel("Password", { exact: true }).fill(password); await page.getByLabel("Confirm password").fill("mismatch"); await page.getByRole("button", { name: "Create account" }).click(); await expect(page.getByText("Passwords do not match.")).toBeVisible();
  await page.getByLabel("Confirm password").fill(password); await page.getByRole("button", { name: "Create account" }).click(); await expect(page.getByRole("main").getByRole("alert")).toContainText("already exists");
});
test("two tabs share refresh safely and observe logout", async ({ page, context }) => {
  const email = await register(page); const other = await context.newPage();
  await other.goto("/dashboard"); await expect(other.getByText(email)).toBeVisible();
  await context.clearCookies({ name: "uptime_access" });
  await Promise.all([page.reload(), other.reload()]);
  await expect(page.getByText(email)).toBeVisible(); await expect(other.getByText(email)).toBeVisible();
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/); await expect(other).toHaveURL(/\/login$/);
});
test("protects workspace and invalid refresh, rejects foreign origin", async ({ page, context }) => {
  await page.goto("/dashboard"); await expect(page).toHaveURL(/\/login$/);
  await context.addCookies([{ name: "uptime_refresh", value: "invalid", domain: "localhost", path: "/api/auth", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/dashboard"); await expect(page).toHaveURL(/\/login$/); expect((await context.cookies()).find(c => c.name === "uptime_refresh")).toBeUndefined();
  const forbidden = await context.request.post("/api/auth/logout", { headers: { Origin: "https://evil.example", "X-CSRF-Protection": "1" } }); expect(forbidden.status()).toBe(403);
});
test("light and dark theme persist, keyboard controls and responsive layout", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" }); await page.goto("/login");
  await page.getByRole("button", { name: "Toggle color theme" }).focus(); await page.keyboard.press("Enter"); await expect(page.locator("html")).toHaveAttribute("data-theme", "dark"); await page.reload(); await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "Toggle color theme" }).click(); await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.getByLabel("Password", { exact: true }).fill(password); await page.getByRole("button", { name: "Show password" }).click(); await expect(page.getByLabel("Password", { exact: true })).toHaveAttribute("type", "text");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
test("shows a recoverable outage without dropping the session", async ({ page, context }) => {
  const email = await register(page); const before = (await context.cookies()).find(c => c.name === "uptime_refresh")!.value;
  // Browser-visible failure response; server-side outage behavior is covered by Vitest.
  await page.route("**/api/auth/session", route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: { message: "The service is temporarily unavailable. Please try again." } }) }));
  await page.reload(); await expect(page.getByRole("main").getByRole("alert")).toContainText("unavailable"); expect((await context.cookies()).find(c => c.name === "uptime_refresh")!.value).toBe(before);
  await page.unroute("**/api/auth/session"); await page.getByRole("button", { name: "Try again" }).click(); await expect(page.getByText(email)).toBeVisible();
});
