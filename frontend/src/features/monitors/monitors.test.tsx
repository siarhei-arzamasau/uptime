// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Monitors } from "./monitors";
import { AuthError, createMonitor, loadMonitors } from "../auth/client";

const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("../auth/client", async original => ({ ...await original<typeof import("../auth/client")>(), loadMonitors: vi.fn(), createMonitor: vi.fn() }));
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
  expect(await screen.findByText("Every 2 hours")).toBeVisible(); expect(screen.getByText("Not checked yet")).toBeVisible();
});

it("redirects an expired session without showing saved monitors", async () => {
  vi.mocked(loadMonitors).mockRejectedValueOnce(new AuthError("Session ended", 401));
  render(<Monitors />); await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
  expect(screen.queryByText(monitor.url)).not.toBeInTheDocument();
});
