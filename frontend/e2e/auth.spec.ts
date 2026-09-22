import { test, expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
const password = "correct horse battery staple";
async function expectAccount(page: Page, email: string) {
  const menu = page.getByRole("button", { name: "Account menu" });
  await expect(menu).toBeVisible();
  await expect(page.locator("header").getByRole("button", { name: "Account menu" })).toBeVisible();
  const accountBox = await menu.boundingBox();
  const themeBox = await page.getByRole("button", { name: "Toggle color theme" }).boundingBox();
  expect(Math.abs((accountBox!.y + accountBox!.height / 2) - (themeBox!.y + themeBox!.height / 2))).toBeLessThan(2);
  const signOut = page.locator("header").getByRole("button", { name: "Sign out" });
  await expect(signOut).toBeVisible();
  const signOutBox = await signOut.boundingBox();
  expect(signOutBox!.x).toBeGreaterThan(accountBox!.x + accountBox!.width);
  expect(Math.abs((signOutBox!.y + signOutBox!.height / 2) - (themeBox!.y + themeBox!.height / 2))).toBeLessThan(2);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await menu.click();
  await expect(page.getByText(email)).toBeVisible();
  await menu.click();
}
async function register(page: Page) {
  const email = `e2e-${randomUUID()}@example.com`;
  await page.goto("/register");
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/dashboard$/); await expectAccount(page, email); return email;
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
  await page.reload(); await expectAccount(page, email);
  await context.clearCookies({ name: "uptime_access" }); await page.reload(); await expectAccount(page, email);
  const rotated = (await context.cookies()).find(c => c.name === "uptime_refresh")!;
  expect(rotated.value).not.toBe(refresh.value); expect(rotated.expires).toBeLessThanOrEqual(refresh.expires + 1);
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/);
  expect((await context.cookies()).some(c => c.name.startsWith("uptime_") && c.name !== "uptime_theme")).toBe(false);
  await expect(page.getByRole("button", { name: "Account menu" })).toHaveCount(0);
  await page.goto("/dashboard"); await expect(page).toHaveURL(/\/login$/);
  const revoked = await context.request.post("http://127.0.0.1:8081/api/v1/auth/refresh", { headers: { "X-CSRF-Protection": "1", Cookie: `refresh_token=${rotated.value}` } });
  expect(revoked.status()).toBe(401);
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
  await other.goto("/dashboard"); await expectAccount(other, email);
  await context.clearCookies({ name: "uptime_access" });
  await Promise.all([page.reload(), other.reload()]);
  await expectAccount(page, email); await expectAccount(other, email);
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
  await page.unroute("**/api/auth/session"); await page.getByRole("button", { name: "Try again" }).click(); await expectAccount(page, email);
});

test("opens profile from toolbar, saves name durably and signs out", async ({ page, context }) => {
  const email = await register(page);
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("link", { name: "Profile", exact: true }).click();
  await expect(page).toHaveURL(/\/profile$/);
  await expect(page.getByLabel("Email address")).toHaveValue(email);
  await expect(page.getByLabel("Email address")).toHaveAttribute("readonly", "");
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("");
  await page.getByLabel("Name", { exact: true }).fill("  Сергей Smith  ");
  await context.clearCookies({ name: "uptime_access" });
  const update = page.waitForResponse(r => r.url().endsWith("/api/auth/profile") && r.request().method() === "PATCH");
  await page.getByLabel("Name", { exact: true }).press("Enter");
  const reply = await update; expect(reply.status()).toBe(200);
  const body = await reply.json(); expect(body.user.name).toBe("Сергей Smith"); expect(body).not.toHaveProperty("access_token");
  await expect(page.getByRole("status")).toHaveText("Profile saved.");
  await page.reload(); await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Сергей Smith");
  await page.getByRole("button", { name: "Account menu" }).click(); await expect(page.getByText("Сергей Smith", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.route("**/api/auth/profile", route => route.request().method() === "PATCH" ? route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: { message: "Service unavailable" } }) }) : route.continue());
  await page.getByLabel("Name", { exact: true }).fill("New name"); await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("main").getByRole("alert")).toHaveText("Service unavailable");
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("New name");
  await page.unroute("**/api/auth/profile");
  await page.getByRole("button", { name: "Save changes" }).click(); await expect(page.getByRole("status")).toHaveText("Profile saved.");
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email address").fill(email); await page.getByLabel("Password", { exact: true }).fill(password); await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard$/); await page.goto("/profile"); await expect(page.getByLabel("Name", { exact: true })).toHaveValue("New name");
  await page.getByLabel("Name", { exact: true }).fill(""); await page.getByRole("button", { name: "Save changes" }).click(); await expect(page.getByRole("status")).toHaveText("Profile saved.");
  await page.reload(); await expect(page.getByLabel("Name", { exact: true })).toHaveValue("");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test("redirects anonymous profile visits to login", async ({ page }) => {
  await page.goto("/profile"); await expect(page).toHaveURL(/\/login$/);
});

test("previews, saves and replaces a local avatar in profile and toolbar", async ({ page, context }, testInfo) => {
  const email = await register(page);
  await page.goto("/profile");
  await expect(page.getByLabel("Email address")).toHaveValue(email);
  let uploads = 0;
  page.on("request", r => { if (r.url().endsWith("/api/auth/profile/avatar")) uploads++; });
  await page.getByLabel("Choose avatar").setInputFiles("e2e/fixtures/avatar.png");
  await expect(page.getByAltText("Selected avatar preview")).toBeVisible();
  await expect(page.locator("header").getByAltText("Your avatar")).toHaveCount(0);
  expect(uploads).toBe(0);
  await page.getByRole("button", { name: "Cancel selection" }).click();
  await expect(page.getByAltText("Selected avatar preview")).toHaveCount(0);
  await page.getByLabel("Choose avatar").setInputFiles("e2e/fixtures/avatar.png");
  await page.getByLabel("Name", { exact: true }).fill("Avatar owner");
  await context.clearCookies({ name: "uptime_access" });
  const response = page.waitForResponse(r => r.url().endsWith("/api/auth/profile/avatar"));
  await page.getByRole("button", { name: "Save changes" }).click();
  const saved = await response; expect(saved.status()).toBe(200);
  const data = await saved.json(); const url = data.user.avatar_url;
  expect(data.user.name).toBe("Avatar owner"); expect(data).not.toHaveProperty("access_token");
  await expect(page.getByRole("status")).toHaveText("Profile saved.");
  const headerAvatar = page.locator("header").getByAltText("Your avatar");
  await expect(headerAvatar).toHaveAttribute("src", url);
  await expect.poll(() => headerAvatar.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(8);
  await expect(page.getByRole("main").getByAltText("Your avatar")).toHaveAttribute("src", url);
  await page.reload(); await expect(headerAvatar).toHaveAttribute("src", url);
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Avatar owner");
  await page.screenshot({ path: testInfo.outputPath("avatar-profile.png"), fullPage: true });
  const image = await page.request.get(url); expect(image.status()).toBe(200); expect(image.headers()["content-type"]).toBe("image/png"); expect(image.headers()["x-content-type-options"]).toBe("nosniff");
  await page.getByLabel("Choose avatar").setInputFiles({ name: "fake.png", mimeType: "image/png", buffer: Buffer.from("not a PNG") });
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("main").getByRole("alert")).toContainText("valid JPEG or PNG");
  await expect(headerAvatar).toHaveAttribute("src", url);
  await page.getByLabel("Choose avatar").setInputFiles("e2e/fixtures/avatar.png");
  await page.getByRole("button", { name: "Save changes" }).click(); await expect(page.getByRole("status")).toHaveText("Profile saved.");
  const replacement = await headerAvatar.getAttribute("src"); expect(replacement).not.toBe(url);
  expect((await page.request.get(url)).status()).toBe(404);
  await page.getByRole("button", { name: "Sign out" }).click(); await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email address").fill(email); await page.getByLabel("Password", { exact: true }).fill(password); await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard$/); await expect(headerAvatar).toHaveAttribute("src", replacement!);
});
