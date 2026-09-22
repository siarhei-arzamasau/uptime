// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { UserMenu } from "./user-menu";
const user = { id: "1", email: "person@example.com", name: "", avatar_url: "", created_at: "now" };
it("opens account details and closes with Escape or an outside click", async () => {
  render(<UserMenu user={user} busy={false} onSignOut={vi.fn()} />);
  const trigger = screen.getByRole("button", { name: "Account menu" });
  expect(trigger).toHaveAttribute("aria-expanded", "false");
  await userEvent.click(trigger); expect(screen.getByText(user.email)).toBeVisible();
  expect(screen.getByText("Profile")).toBeVisible();
  await userEvent.keyboard("{Escape}"); expect(trigger).toHaveFocus(); expect(trigger).toHaveAttribute("aria-expanded", "false");
  await userEvent.click(trigger); fireEvent.pointerDown(document.body); expect(screen.queryByText(user.email)).not.toBeInTheDocument();
});
it("supports keyboard logout beside the closed profile menu", async () => {
  const logout = vi.fn(); render(<UserMenu user={user} busy={false} onSignOut={logout} />);
  screen.getByRole("button", { name: "Account menu" }).focus();
  await userEvent.tab(); expect(screen.getByRole("button", { name: "Sign out" })).toHaveFocus();
  await userEvent.keyboard("{Enter}"); expect(logout).toHaveBeenCalledOnce();
});
it("disables logout while a request is pending", async () => {
  render(<UserMenu user={user} busy onSignOut={vi.fn()} />);
  expect(screen.getByRole("button", { name: "Signing out…" })).toBeDisabled();
});
