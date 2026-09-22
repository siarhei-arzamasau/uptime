// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { AuthForm } from "./auth-form";
import { Dashboard } from "./dashboard";
import { AuthError, session, signIn, signOut, subscribeAuth } from "./client";
const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("./client", async importOriginal => ({ ...await importOriginal<typeof import("./client")>(), session: vi.fn(), signIn: vi.fn(), signOut: vi.fn(), subscribeAuth: vi.fn(() => () => {}) }));
beforeEach(() => { vi.clearAllMocks(); });
async function fill(register = false) {
  const user = userEvent.setup(); await user.type(screen.getByLabelText("Email address"), "person@example.com"); await user.type(screen.getByLabelText("Password", { exact: true }), "correct long password");
  if (register) await user.type(screen.getByLabelText("Confirm password"), "correct long password"); return user;
}
it("validates fields and confirmation accessibly", async () => {
  render(<AuthForm mode="register" />); const user = userEvent.setup(); await user.click(screen.getByRole("button", { name: "Create account" }));
  expect(screen.getByText("Enter a valid email address.")).toBeVisible(); expect(screen.getByLabelText("Email address")).toHaveFocus();
  await fill(); await user.type(screen.getByLabelText("Confirm password"), "different"); await user.click(screen.getByRole("button", { name: "Create account" })); expect(screen.getByText("Passwords do not match.")).toBeVisible(); expect(signIn).not.toHaveBeenCalled();
});
it("reveals password and submits Enter without confirmation field", async () => {
  vi.mocked(signIn).mockResolvedValue({ user: { id: "1", email: "person@example.com", name: "", avatar_url: "", created_at: "now" } });
  render(<AuthForm mode="register" />); const user = await fill(true); await user.click(screen.getByRole("button", { name: "Show password" })); expect(screen.getByLabelText("Password", { exact: true })).toHaveAttribute("type", "text");
  await user.click(screen.getByLabelText("Confirm password")); await user.keyboard("{Enter}"); await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/dashboard"));
  expect(signIn).toHaveBeenCalledWith("register", "person@example.com", "correct long password");
});
it("blocks duplicate submissions and displays server errors", async () => {
  let reject!: (e: Error) => void; vi.mocked(signIn).mockReturnValue(new Promise((_, no) => { reject = no; })); render(<AuthForm mode="login" />); const user = await fill();
  await user.click(screen.getByRole("button", { name: "Sign in" })); expect(screen.getByRole("button", { name: "Signing in…" })).toBeDisabled(); fireEvent.submit(screen.getByLabelText("Password", { exact: true }).closest("form")!); expect(signIn).toHaveBeenCalledTimes(1);
  await act(async () => reject(new Error("Incorrect email or password."))); expect(screen.getByRole("alert")).toHaveTextContent("Incorrect email or password.");
});
it("loads a verified user and signs out", async () => {
  vi.mocked(session).mockResolvedValue({ user: { id: "1", email: "person@example.com", name: "", avatar_url: "", created_at: "now" } }); vi.mocked(signOut).mockResolvedValue({} as never);
  render(<><header><div id="account-menu-slot" /></header><Dashboard /></>); expect(await screen.findByRole("button", { name: "Account menu" })).toBeVisible(); await userEvent.click(screen.getByRole("button", { name: "Account menu" })); await userEvent.click(screen.getByRole("button", { name: "Sign out" })); await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
});
it("shows loading, then redirects an unauthenticated user", async () => {
  let reject!: (e: Error) => void; vi.mocked(session).mockReturnValue(new Promise((_, no) => { reject = no; })); render(<><header><div id="account-menu-slot" /></header><Dashboard /></>); expect(screen.getByRole("status")).toBeVisible(); await act(async () => reject(new AuthError("Sign in", 401))); expect(router.replace).toHaveBeenCalledWith("/login");
});
it("keeps a retry action after service outage and pauses automatic retry", async () => {
  vi.mocked(session).mockRejectedValue(new AuthError("Service unavailable", 503)); render(<><header><div id="account-menu-slot" /></header><Dashboard /></>); expect(await screen.findByRole("alert")).toHaveTextContent("Service unavailable"); fireEvent.focus(window); expect(session).toHaveBeenCalledTimes(1); await userEvent.click(screen.getByRole("button", { name: "Try again" })); expect(session).toHaveBeenCalledTimes(2);
});
it("reacts to a session change in another tab", async () => {
  let changed!: () => void; vi.mocked(subscribeAuth).mockImplementation(listener => { changed = listener; return () => {}; }); vi.mocked(session).mockResolvedValue({ user: { id: "1", email: "person@example.com", name: "", avatar_url: "", created_at: "now" } }); render(<><header><div id="account-menu-slot" /></header><Dashboard /></>); await screen.findByRole("button", { name: "Account menu" }); vi.mocked(session).mockRejectedValueOnce(new AuthError("Ended", 401)); await act(async () => changed()); expect(router.replace).toHaveBeenCalledWith("/login");
});

it("keeps the account menu available when sign out fails", async () => {
  vi.mocked(session).mockResolvedValue({ user: { id: "1", email: "person@example.com", name: "", avatar_url: "", created_at: "now" } });
  vi.mocked(signOut).mockRejectedValueOnce(new Error("Service unavailable"));
  render(<><header><div id="account-menu-slot" /></header><Dashboard /></>); await screen.findByRole("button", { name: "Account menu" });
  await userEvent.click(screen.getByRole("button", { name: "Account menu" }));
  await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Service unavailable");
  expect(router.replace).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
});
