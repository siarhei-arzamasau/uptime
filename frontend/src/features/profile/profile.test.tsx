// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ProfileForm } from "./profile";
import { AuthError, updateProfile, uploadAvatar } from "../auth/client";
const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("../auth/client", async original => ({ ...await original<typeof import("../auth/client")>(), updateProfile: vi.fn(), uploadAvatar: vi.fn() }));
const user = { id: "1", email: "alice@example.com", name: "Alice", avatar_url: "", created_at: "2026-01-01" };
beforeEach(() => vi.clearAllMocks());
it("shows email read-only and saves the trimmed name with Enter", async () => {
  const onSaved = vi.fn(); vi.mocked(updateProfile).mockResolvedValue({ user: { ...user, name: "\u0421\u0435\u0440\u0433\u0435\u0439" } });
  render(<ProfileForm user={user} onSaved={onSaved} />);
  expect(screen.getByLabelText("Email address")).toHaveAttribute("readonly");
  const name = screen.getByLabelText("Name"); await userEvent.clear(name); await userEvent.type(name, " \u0421\u0435\u0440\u0433\u0435\u0439 {Enter}");
  expect(updateProfile).toHaveBeenCalledWith("\u0421\u0435\u0440\u0433\u0435\u0439"); expect(await screen.findByRole("status")).toHaveTextContent("Profile saved."); expect(onSaved).toHaveBeenCalledWith({ ...user, name: "\u0421\u0435\u0440\u0433\u0435\u0439" });
});
it("validates length accessibly and keeps the entered value", async () => {
  render(<ProfileForm user={user} onSaved={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "界".repeat(101) } });
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(screen.getByLabelText("Name")).toHaveFocus(); expect(screen.getByLabelText("Name")).toHaveAttribute("aria-invalid", "true"); expect(updateProfile).not.toHaveBeenCalled();
});
it("prevents duplicate submits, retains input on failure and retries", async () => {
  let reject!: (error: Error) => void;
  vi.mocked(updateProfile).mockReturnValueOnce(new Promise((_, no) => { reject = no; }));
  render(<ProfileForm user={user} onSaved={vi.fn()} />);
  await userEvent.clear(screen.getByLabelText("Name")); await userEvent.type(screen.getByLabelText("Name"), "Bob");
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  fireEvent.submit(screen.getByLabelText("Name").closest("form")!);
  expect(updateProfile).toHaveBeenCalledTimes(1); expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
  await act(async () => reject(new AuthError("Service unavailable", 503)));
  expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable"); expect(screen.getByLabelText("Name")).toHaveValue("Bob");
  vi.mocked(updateProfile).mockResolvedValueOnce({ user: { ...user, name: "Bob" } });
  await userEvent.click(screen.getByRole("button", { name: "Save changes" })); expect(await screen.findByRole("status")).toBeVisible();
});
it("returns to login when the session expires during editing", async () => {
  vi.mocked(updateProfile).mockRejectedValue(new AuthError("Session ended", 401));
  render(<ProfileForm user={user} onSaved={vi.fn()} />); await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
});

const avatarFile = new File(["image"], "avatar.png", { type: "image/png" });
function mockBlobURL() { vi.stubGlobal("URL", class extends URL { static createObjectURL = vi.fn(() => "blob:preview"); static revokeObjectURL = vi.fn(); }); }
afterEach(() => vi.unstubAllGlobals());
it("previews locally and saves avatar and name together only on Save changes", async () => {
  mockBlobURL(); const onSaved = vi.fn();
  const updated = { ...user, name: "Bob", avatar_url: "/api/avatars/saved.png" };
  vi.mocked(uploadAvatar).mockResolvedValue({ user: updated });
  render(<ProfileForm user={user} onSaved={onSaved} />);
  await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  expect(screen.getByAltText("Selected avatar preview")).toHaveAttribute("src", "blob:preview"); expect(uploadAvatar).not.toHaveBeenCalled();
  await userEvent.clear(screen.getByLabelText("Name")); await userEvent.type(screen.getByLabelText("Name"), "Bob");
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(uploadAvatar).toHaveBeenCalledWith("Bob", avatarFile); expect(updateProfile).not.toHaveBeenCalled();
  expect(onSaved).toHaveBeenCalledWith(updated); expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", updated.avatar_url);
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
});
it("cancels the selection without changing the saved avatar", async () => {
  mockBlobURL(); render(<ProfileForm user={{ ...user, avatar_url: "/api/avatars/old.png" }} onSaved={vi.fn()} />);
  await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  await userEvent.click(screen.getByRole("button", { name: "Cancel selection" }));
  expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", "/api/avatars/old.png"); expect(uploadAvatar).not.toHaveBeenCalled();
});
it("rejects invalid types and oversized files without a preview", async () => {
  mockBlobURL(); render(<ProfileForm user={user} onSaved={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Choose avatar"), { target: { files: [new File(["svg"], "a.svg", { type: "image/svg+xml" })] } });
  expect(screen.getByRole("alert")).toHaveTextContent("JPEG or PNG");
  fireEvent.change(screen.getByLabelText("Choose avatar"), { target: { files: [new File([new Uint8Array(6 * 1024 * 1024)], "a.png", { type: "image/png" })] } });
  expect(screen.getByRole("alert")).toHaveTextContent("under 5 MB"); expect(URL.createObjectURL).not.toHaveBeenCalled();
});
it("retains selection on upload failure for retry", async () => {
  mockBlobURL(); vi.mocked(uploadAvatar).mockRejectedValueOnce(new AuthError("Service unavailable", 503));
  render(<ProfileForm user={user} onSaved={vi.fn()} />); await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable"); expect(screen.getByAltText("Selected avatar preview")).toBeVisible();
  expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
});
