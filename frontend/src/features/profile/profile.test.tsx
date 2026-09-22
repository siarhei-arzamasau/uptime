// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Profile, ProfileForm } from "./profile";
import { AuthError, loadProfile, subscribeAuth, updateProfile, uploadAvatar } from "../auth/client";
const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("../auth/client", async original => ({ ...await original<typeof import("../auth/client")>(), loadProfile: vi.fn(), subscribeAuth: vi.fn(() => () => {}), updateProfile: vi.fn(), uploadAvatar: vi.fn() }));
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
  const view = render(<ProfileForm user={user} onSaved={onSaved} />);
  await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  expect(screen.getByAltText("Selected avatar preview")).toHaveAttribute("src", "blob:preview"); expect(uploadAvatar).not.toHaveBeenCalled();
  await userEvent.clear(screen.getByLabelText("Name")); await userEvent.type(screen.getByLabelText("Name"), "Bob");
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(uploadAvatar).toHaveBeenCalledWith("Bob", avatarFile); expect(updateProfile).not.toHaveBeenCalled();
  expect(onSaved).toHaveBeenCalledWith(updated);
  view.rerender(<ProfileForm user={updated} onSaved={onSaved} />);
  expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", updated.avatar_url);
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

it("refreshes untouched fields before saving instead of restoring stale values", async () => {
  const onSaved = vi.fn();
  const view = render(<ProfileForm user={user} onSaved={onSaved} />);
  const refreshed = { ...user, name: "From another tab", avatar_url: "/api/avatars/new.png" };
  view.rerender(<ProfileForm user={refreshed} onSaved={onSaved} />);
  expect(screen.getByLabelText("Name")).toHaveValue(refreshed.name);
  expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", refreshed.avatar_url);
  vi.mocked(updateProfile).mockResolvedValueOnce({ user: refreshed });
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(updateProfile).toHaveBeenCalledWith(refreshed.name);
});

it("keeps edited fields and preview while refreshing the saved profile", async () => {
  mockBlobURL();
  const onSaved = vi.fn();
  const view = render(<ProfileForm user={user} onSaved={onSaved} />);
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My draft" } });
  await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  const refreshed = { ...user, name: "Another tab", avatar_url: "/api/avatars/latest.png" };
  view.rerender(<ProfileForm user={refreshed} onSaved={onSaved} />);
  expect(screen.getByLabelText("Name")).toHaveValue("My draft");
  expect(screen.getByAltText("Selected avatar preview")).toHaveAttribute("src", "blob:preview");
  await userEvent.click(screen.getByRole("button", { name: "Cancel selection" }));
  expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", refreshed.avatar_url);
  vi.mocked(updateProfile).mockResolvedValueOnce({ user: { ...refreshed, name: "My draft" } });
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(updateProfile).toHaveBeenCalledWith("My draft");
});

it.each([0, 503])("preserves the draft and selected file across a revalidation failure (%i)", async status => {
  mockBlobURL();
  vi.mocked(loadProfile).mockResolvedValue({ user });
  render(<Profile />);
  await screen.findByLabelText("Name");
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved draft" } });
  await userEvent.upload(screen.getByLabelText("Choose avatar"), avatarFile);
  vi.mocked(loadProfile).mockRejectedValueOnce(new AuthError("Service unavailable", status));
  await act(async () => { fireEvent.focus(window); });
  expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable");
  expect(screen.getByLabelText("Name")).toHaveValue("Unsaved draft");
  expect(screen.getByAltText("Selected avatar preview")).toBeVisible();
  expect(URL.revokeObjectURL).not.toHaveBeenCalled();
  const calls = vi.mocked(loadProfile).mock.calls.length;
  fireEvent.focus(window);
  expect(loadProfile).toHaveBeenCalledTimes(calls);
  await userEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Name")).toHaveValue("Unsaved draft");
  vi.mocked(uploadAvatar).mockResolvedValueOnce({ user: { ...user, name: "Unsaved draft", avatar_url: "/api/avatars/saved.png" } });
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(uploadAvatar).toHaveBeenCalledWith("Unsaved draft", avatarFile);
  expect(screen.getByAltText("Your avatar")).toHaveAttribute("src", "/api/avatars/saved.png");
  vi.mocked(loadProfile).mockResolvedValueOnce({ user: { ...user, name: "Updated after saving" } });
  await act(async () => { fireEvent.focus(window); });
  expect(screen.getByLabelText("Name")).toHaveValue("Updated after saving");
});

it("discards the draft when revalidation confirms an expired session", async () => {
  vi.mocked(loadProfile).mockResolvedValue({ user });
  render(<Profile />);
  await screen.findByLabelText("Name");
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Private draft" } });
  vi.mocked(loadProfile).mockRejectedValueOnce(new AuthError("Session ended", 401));
  await act(async () => { fireEvent.focus(window); });
  expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  expect(router.replace).toHaveBeenCalledWith("/login");
});

it("discards the previous account's draft after an authentication change", async () => {
  let changed!: () => void;
  vi.mocked(subscribeAuth).mockImplementationOnce(listener => { changed = listener; return () => {}; });
  vi.mocked(loadProfile).mockResolvedValueOnce({ user });
  render(<Profile />);
  await screen.findByLabelText("Name");
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Private draft" } });
  vi.mocked(loadProfile).mockResolvedValueOnce({ user: { ...user, id: "2", email: "bob@example.com", name: "Bob" } });
  await act(async () => { changed(); });
  expect(screen.getByLabelText("Email address")).toHaveValue("bob@example.com");
  expect(screen.getByLabelText("Name")).toHaveValue("Bob");
});
