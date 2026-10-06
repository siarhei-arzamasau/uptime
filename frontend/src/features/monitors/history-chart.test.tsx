// @vitest-environment jsdom
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { HistoryChart } from "./history-chart";
import { loadHistory } from "./client";
import type { MonitorHistory } from "./types";
vi.mock("next/navigation", () => ({ useRouter: () => router }));
const router = { replace: vi.fn() };
vi.mock("./client", () => ({ loadHistory: vi.fn() }));
const history: MonitorHistory = {
  from: "2026-10-06T00:00:00Z", to: "2026-10-06T00:02:00Z", step_seconds: 60, version: 1,
  successes: 9, failures: 1, availability: 90,
  buckets: [
    { start: "2026-10-06T00:00:00Z", end: "2026-10-06T00:01:00Z", successes: 0, failures: 0, availability: null },
    { start: "2026-10-06T00:01:00Z", end: "2026-10-06T00:02:00Z", successes: 9, failures: 1, availability: 90 },
  ],
};
beforeEach(() => { vi.mocked(loadHistory).mockReset().mockResolvedValue(history); });
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });
it("renders weighted availability and lets a keyboard control inspect gaps", async () => {
  render(<HistoryChart id="monitor" />);
  expect((await screen.findAllByText("90.00%")).length).toBeGreaterThan(0);
  fireEvent.change(screen.getByRole("slider"), { target: { value: "0" } });
  expect(screen.getByRole("slider")).toHaveAttribute("aria-valuetext", expect.stringContaining("No data"));
  expect(screen.getByText(/0 successful \/ 0 failed/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "7 days" }));
  expect(loadHistory).toHaveBeenLastCalledWith("monitor", "7d");
});
it("does not overlap refresh requests and refreshes when the tab becomes visible", async () => {
  vi.useFakeTimers();
  let resolve!: (value: MonitorHistory) => void;
  vi.mocked(loadHistory).mockReturnValueOnce(new Promise(done => { resolve = done; }));
  render(<HistoryChart id="monitor" />);
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000); });
  expect(loadHistory).toHaveBeenCalledTimes(1);
  await act(async () => { resolve(history); });
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000); });
  expect(loadHistory).toHaveBeenCalledTimes(1);
  hidden.mockReturnValue(false);
  await act(async () => { fireEvent(document, new Event("visibilitychange")); });
  expect(loadHistory).toHaveBeenCalledTimes(2);
});
it("discards an old response after changing periods", async () => {
  let resolve!: (value: MonitorHistory) => void;
  vi.mocked(loadHistory).mockReturnValueOnce(new Promise(done => { resolve = done; }));
  render(<HistoryChart id="monitor" />);
  fireEvent.click(screen.getByRole("button", { name: "30 days" }));
  await act(async () => { resolve(history); });
  expect(screen.queryByText("90.00%")).not.toBeInTheDocument();
});
