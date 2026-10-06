// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Monitors } from "./monitors";
import { AuthError } from "../auth/transport";
import { createMonitor, loadMonitors, updateMonitor, deleteMonitor } from "./client";

const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("./client", () => ({ loadMonitors: vi.fn(), createMonitor: vi.fn(), updateMonitor: vi.fn(), deleteMonitor: vi.fn(), loadStatuses: vi.fn().mockResolvedValue({ monitors: [] }), loadHistory: vi.fn() }));
const monitor = { id: "monitor-id", url: "https://example.com", interval_seconds: 7, created_at: "2026-09-22" };
beforeEach(() => { vi.clearAllMocks(); vi.mocked(loadMonitors).mockResolvedValue({ monitors: [] }); vi.mocked(createMonitor).mockResolvedValue({ monitor }); });
async function openForm() {
  const user = userEvent.setup(); render(<Monitors />);
  await screen.findByText("No websites yet"); await user.click(screen.getByRole("button", { name: "Add website" }));
  expect(screen.getByLabelText("Website URL")).toHaveFocus();
  return user;
}

it.each([["7", "1", 7], ["10", "60", 600], ["2", "3600", 7200]])("creates a monitor for %s × %s seconds", async (count, unit, seconds) => {
  vi.mocked(createMonitor).mockResolvedValue({ monitor: { ...monitor, interval_seconds: seconds } });
  const user = await openForm();
  await user.type(screen.getByLabelText("Website URL"), "https://example.com");
  await user.clear(screen.getByRole("spinbutton")); await user.type(screen.getByRole("spinbutton"), count);
  await user.selectOptions(screen.getByLabelText("Interval unit"), unit);
  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(createMonitor).toHaveBeenCalledWith({ url: monitor.url, interval_seconds: seconds });
  expect(await screen.findByText(monitor.url)).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("Website added");
  expect(screen.getByRole("button", { name: "Add website" })).toHaveFocus();
  expect(screen.queryByRole("form")).not.toBeInTheDocument();
});

it("validates URL and whole-number interval before saving", async () => {
  const user = await openForm();
  await user.type(screen.getByLabelText("Website URL"), "javascript:alert(1)");
  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(screen.getByLabelText("Website URL")).toHaveAttribute("aria-invalid", "true");
  expect(createMonitor).not.toHaveBeenCalled();
  await user.clear(screen.getByLabelText("Website URL")); await user.type(screen.getByLabelText("Website URL"), monitor.url);
  for (const value of ["0", "-2", "1.5", "2147483648", ""]) {
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value } });
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(screen.getByRole("spinbutton")).toHaveFocus(); expect(createMonitor).not.toHaveBeenCalled();
  }
});

it("retains inputs after a failure and prevents duplicate submissions", async () => {
  let reject!: (error: Error) => void;
  vi.mocked(createMonitor).mockReturnValueOnce(new Promise((_, no) => { reject = no; }));
  const user = await openForm(); await user.type(screen.getByLabelText("Website URL"), monitor.url);
  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(screen.getByRole("button", { name: "Creating…" })).toBeDisabled();
  fireEvent.submit(screen.getByRole("form")); expect(createMonitor).toHaveBeenCalledTimes(1);
  await act(async () => reject(new Error("Service unavailable")));
  expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable"); expect(screen.getByLabelText("Website URL")).toHaveValue(monitor.url);
  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(await screen.findByText(monitor.url)).toBeVisible();
});

it("cancels without creating a monitor and restores focus", async () => {
  const user = await openForm(); await user.type(screen.getByLabelText("Website URL"), monitor.url);
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(createMonitor).not.toHaveBeenCalled(); expect(screen.getByRole("button", { name: "Add website" })).toHaveFocus();
});

it("recovers a failed list request and displays saved intervals", async () => {
  vi.mocked(loadMonitors).mockRejectedValueOnce(new Error("Service unavailable")).mockResolvedValueOnce({ monitors: [{ ...monitor, interval_seconds: 7200 }] });
  render(<Monitors />); expect(await screen.findByRole("alert")).toHaveTextContent("Service unavailable");
  await userEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByText("Every 2 hours")).toBeVisible(); expect(screen.getByText("Awaiting first check")).toBeVisible();
});

it("redirects an expired session without showing saved monitors", async () => {
  vi.mocked(loadMonitors).mockRejectedValueOnce(new AuthError("Session ended", 401));
  render(<Monitors />); await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
  expect(screen.queryByText(monitor.url)).not.toBeInTheDocument();
});

it("appends the next page and keeps existing rows when a page request fails", async () => {
  vi.mocked(loadMonitors).mockResolvedValueOnce({ monitors: [monitor], next_cursor: "page-2" })
    .mockRejectedValueOnce(new Error("Service unavailable"))
    .mockResolvedValueOnce({ monitors: [{ ...monitor, id: "second", url: "https://second.example" }] });
  render(<Monitors />);
  await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Service unavailable");
  expect(screen.getByText(monitor.url)).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(await screen.findByText("https://second.example")).toBeVisible();
  expect(screen.getAllByRole("listitem")).toHaveLength(2);
  expect(loadMonitors).toHaveBeenLastCalledWith("page-2");
  expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
});

async function existingWebsite() {
  vi.mocked(loadMonitors).mockResolvedValue({ monitors: [monitor] });
  const user = userEvent.setup(); render(<Monitors />);
  await screen.findByText(monitor.url);
  return user;
}

it("edits a saved URL and interval, and restores focus after saving", async () => {
  const user = await existingWebsite();
  const edit = screen.getByRole("button", { name: `Edit ${monitor.url}` });
  await user.click(edit);
  expect(screen.getByLabelText("Website URL")).toHaveValue(monitor.url);
  expect(screen.getByRole("spinbutton")).toHaveValue(7);
  expect(screen.getByLabelText("Interval unit")).toHaveValue("1");
  await user.clear(screen.getByLabelText("Website URL"));
  await user.type(screen.getByLabelText("Website URL"), "https://changed.example");
  fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "10" } });
  await user.selectOptions(screen.getByLabelText("Interval unit"), "60");
  vi.mocked(updateMonitor).mockResolvedValue({ monitor: { ...monitor, url: "https://changed.example", interval_seconds: 600 } });
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(updateMonitor).toHaveBeenCalledWith(monitor.id, { url: "https://changed.example", interval_seconds: 600 });
  expect(await screen.findByText("Every 10 minutes")).toBeVisible();
  expect(screen.getByRole("button", { name: "Edit https://changed.example" })).toHaveFocus();
  expect(createMonitor).not.toHaveBeenCalled();
});

it("retains the edit draft after failure and cancels without changing the list", async () => {
  const user = await existingWebsite();
  await user.click(screen.getByRole("button", { name: `Edit ${monitor.url}` }));
  vi.mocked(updateMonitor).mockRejectedValue(new Error("Service unavailable"));
  fireEvent.change(screen.getByLabelText("Website URL"), { target: { value: "https://draft.example" } });
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Service unavailable");
  expect(screen.getByLabelText("Website URL")).toHaveValue("https://draft.example");
  expect(screen.getByText(monitor.url)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.getByRole("button", { name: `Edit ${monitor.url}` })).toHaveFocus();
});

it("requires deletion confirmation, preserves rows on failure and blocks duplicate requests", async () => {
  const user = await existingWebsite();
  const remove = screen.getByRole("button", { name: `Delete ${monitor.url}` });
  await user.click(remove);
  expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(deleteMonitor).not.toHaveBeenCalled(); expect(remove).toHaveFocus();
  await user.click(remove);
  let reject!: (error: Error) => void;
  vi.mocked(deleteMonitor).mockReturnValueOnce(new Promise((_, no) => { reject = no; }));
  await user.click(screen.getByRole("button", { name: "Delete website" }));
  expect(screen.getByRole("button", { name: "Deleting…" })).toBeDisabled();
  expect(screen.getAllByRole("listitem")).toHaveLength(1);
  await act(async () => reject(new Error("Service unavailable")));
  expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable");
  vi.mocked(deleteMonitor).mockResolvedValue({ ok: true });
  await user.click(screen.getByRole("button", { name: "Delete website" }));
  expect(await screen.findByText("No websites yet")).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("Website deleted");
  expect(screen.getByRole("button", { name: "Add website" })).toHaveFocus();
  expect(deleteMonitor).toHaveBeenCalledTimes(2);
});

it.each(["edit", "delete"])("redirects when the session expires during %s", async action => {
  const user = await existingWebsite();
  vi.mocked(updateMonitor).mockRejectedValue(new AuthError("Session ended", 401));
  vi.mocked(deleteMonitor).mockRejectedValue(new AuthError("Session ended", 401));
  await user.click(screen.getByRole("button", { name: `${action === "edit" ? "Edit" : "Delete"} ${monitor.url}` }));
  await user.click(screen.getByRole("button", { name: action === "edit" ? "Save changes" : "Delete website" }));
  expect(router.replace).toHaveBeenCalledWith("/login");
  expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
});

it("keeps pagination available after deleting all loaded rows", async () => {
  vi.mocked(loadMonitors).mockResolvedValueOnce({ monitors: [monitor], next_cursor: "next-page" })
    .mockResolvedValueOnce({ monitors: [{ ...monitor, id: "next-id", url: "https://next.example" }] });
  vi.mocked(deleteMonitor).mockResolvedValue({ ok: true });
  const user = userEvent.setup(); render(<Monitors />);
  await user.click(await screen.findByRole("button", { name: `Delete ${monitor.url}` }));
  await user.click(screen.getByRole("button", { name: "Delete website" }));
  expect(await screen.findByText("Load more to see the remaining websites.")).toBeVisible();
  expect(screen.queryByText("No websites yet")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Load more" }));
  expect(await screen.findByText("https://next.example")).toBeVisible();
  expect(loadMonitors).toHaveBeenLastCalledWith("next-page");
});
