import { beforeEach, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { handleStatistics } from "./statistics-server";

const id = "01234567-89ab-cdef-0123-456789abcdef";
const check = { id, version: 1, status: "pending", last_started_at: null, last_finished_at: null, last_success: null, http_status: null, error_kind: "" };
const fetchMock = vi.fn();
beforeEach(() => { vi.stubGlobal("fetch", fetchMock); fetchMock.mockReset(); });
function request(query: string) {
  return new NextRequest(`http://localhost:3000/api/auth/monitors/status?${query}`, { method: "POST", headers: { Origin: "http://localhost:3000", "X-CSRF-Protection": "1", Cookie: "uptime_access=access" } });
}
it("decodes public statuses without leaking upstream private fields", async () => {
  fetchMock.mockResolvedValue(Response.json({ monitors: [{ ...check, user_id: "private", job_id: "private" }] }));
  const response = await handleStatistics(request(`ids=${id}`), "status");
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ monitors: [check] });
  expect(response.headers.get("cache-control")).toBe("no-store");
});
it.each(["", "bad", `${id},${id}`])("rejects invalid IDs: %s", async ids => {
  expect((await handleStatistics(request(`ids=${ids}`), "status")).status).toBe(400);
  expect(fetchMock).not.toHaveBeenCalled();
});
it("rejects malformed or mismatched status responses", async () => {
  for (const value of [{ ...check, status: "invented" }, { ...check, last_started_at: "bad" }, { ...check, id: "other" }, { ...check, version: 0 }]) {
    fetchMock.mockResolvedValueOnce(Response.json({ monitors: [value] }));
    expect((await handleStatistics(request(`ids=${id}`), "status")).status).toBe(502);
  }
});
it("preserves null gaps and weighted totals in history", async () => {
  const bucket = { start: "2026-10-06T00:00:00Z", end: "2026-10-06T00:01:00Z", successes: 0, failures: 0, availability: null };
  const history = { from: bucket.start, to: bucket.end, version: 1, step_seconds: 60, successes: 0, failures: 0, availability: null, buckets: [bucket] };
  fetchMock.mockResolvedValue(Response.json(history));
  const response = await handleStatistics(request("period=1h"), "history", id);
  expect(response.status).toBe(200); expect(await response.json()).toEqual(history);
  expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining(`/monitors/${id}/history?period=1h`), expect.objectContaining({ method: "GET" }));
});
it("maps ownership errors without disclosing a foreign monitor", async () => {
  fetchMock.mockResolvedValue(Response.json({ error: {} }, { status: 404 }));
  expect((await handleStatistics(request(`ids=${id}`), "status")).status).toBe(404);
});
