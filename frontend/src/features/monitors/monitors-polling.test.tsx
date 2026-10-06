// @vitest-environment jsdom
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Monitors } from "./monitors";
import { loadMonitors, loadStatuses, updateMonitor } from "./client";
import type { Monitor, MonitorCheck } from "./types";
const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("./client", () => ({ loadMonitors: vi.fn(), loadStatuses: vi.fn(), updateMonitor: vi.fn(), createMonitor: vi.fn(), deleteMonitor: vi.fn(), loadHistory: vi.fn() }));
const pending: MonitorCheck = { id: "monitor", version: 1, status: "pending", last_started_at: null, last_finished_at: null, last_success: null, http_status: null, error_kind: "" };
const monitor: Monitor = { id: "monitor", url: "https://example.com", interval_seconds: 5, created_at: "2026-10-06T00:00:00Z", check: pending };
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks();
  vi.mocked(loadMonitors).mockResolvedValue({ monitors: [monitor] });
  vi.mocked(loadStatuses).mockResolvedValue({ monitors: [pending] });
});
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

it("pauses hidden tabs and prevents overlapping status requests", async () => {
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  await act(async () => { render(<Monitors />); });
  await act(async () => { await vi.advanceTimersByTimeAsync(15_000); });
  expect(loadStatuses).not.toHaveBeenCalled();
  let resolve!: (value: { monitors: MonitorCheck[] }) => void;
  vi.mocked(loadStatuses).mockReturnValueOnce(new Promise(done => { resolve = done; }));
  hidden.mockReturnValue(false);
  await act(async () => { fireEvent(document, new Event("visibilitychange")); });
  await act(async () => { await vi.advanceTimersByTimeAsync(15_000); });
  expect(loadStatuses).toHaveBeenCalledTimes(1);
  await act(async () => { resolve({ monitors: [pending] }); });
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  expect(loadStatuses).toHaveBeenCalledTimes(2);
});

it("discards a status request that finishes after an edit", async () => {
  let resolve!: (value: { monitors: MonitorCheck[] }) => void;
  vi.mocked(loadStatuses).mockReturnValueOnce(new Promise(done => { resolve = done; }));
  await act(async () => { render(<Monitors />); });
  await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
  fireEvent.click(screen.getByRole("button", { name: `Edit ${monitor.url}` }));
  fireEvent.change(screen.getByLabelText("Website URL"), { target: { value: "https://changed.example" } });
  vi.mocked(updateMonitor).mockResolvedValue({ monitor: { ...monitor, url: "https://changed.example", check: { ...pending, version: 2 } } });
  await act(async () => { fireEvent.submit(screen.getByRole("form")); });
  await act(async () => { resolve({ monitors: [{ ...pending, status: "down", last_started_at: new Date().toISOString(), last_finished_at: new Date().toISOString(), last_success: false, http_status: 503, error_kind: "http_status" }] }); });
  expect(screen.getByText("https://changed.example")).toBeVisible();
  expect(screen.getByText("Awaiting first check")).toBeVisible();
  expect(screen.queryByText("Unavailable", { exact: true })).not.toBeInTheDocument();
});
