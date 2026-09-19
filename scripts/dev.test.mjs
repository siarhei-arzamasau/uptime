import { afterEach, describe, expect, it, vi } from "vitest";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { configuration, boot, Processes, waitReady } from "./dev.mjs";
import { spawn } from "node:child_process";
vi.mock("node:child_process", () => ({ spawn: vi.fn() }));
const backend = `DATABASE_URL=postgres://u:p@localhost:5433/uptime\nJWT_SECRET=${"s".repeat(32)}\nPOSTGRES_PASSWORD=private\nHTTP_ADDR=127.0.0.1:8080\nALLOWED_ORIGIN=http://localhost:3000`;
const frontend = "BACKEND_URL=http://127.0.0.1:8080\nAPP_ORIGIN=http://localhost:3000";
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers(); });
it("isolates environments and respects explicit overrides", () => {
  const c = configuration(backend, frontend, { PATH: "/bin", JWT_SECRET: "x".repeat(32), PRIVATE_UNRELATED: "do-not-copy" });
  expect(c.backendEnv.JWT_SECRET).toBe("x".repeat(32)); expect(c.frontendEnv.JWT_SECRET).toBeUndefined(); expect(c.frontendEnv.POSTGRES_PASSWORD).toBeUndefined(); expect(c.frontendEnv.PRIVATE_UNRELATED).toBeUndefined(); expect(c.backendEnv.BACKEND_URL).toBeUndefined();
});
it("rejects inconsistent origins and ports", () => {
  expect(() => configuration(backend, frontend, { HTTP_ADDR: "127.0.0.1:9000" })).toThrow("ports");
  expect(() => configuration(backend, frontend, { APP_ORIGIN: "http://localhost:3333" })).toThrow("ALLOWED_ORIGIN");
});
it("selects a dedicated E2E database and ports", () => {
  const c = configuration(backend, frontend, {}, true); expect(new URL(c.backendEnv.DATABASE_URL).pathname).toBe("/uptime_e2e_test"); expect(c.frontendPort).toBe(3001); expect(c.apiPort).toBe(8081);
});
function operations() { return { check: vi.fn().mockResolvedValue(), free: vi.fn().mockResolvedValue(), run: vi.fn().mockResolvedValue(), start: vi.fn(), ready: vi.fn().mockResolvedValue(), log: vi.fn() }; }
it("starts database, migration, build, API and frontend in order", async () => {
  const c = configuration(backend, frontend, {}); const ops = operations(); await boot(c, ops);
  expect(ops.run.mock.calls.map(([cmd, args]) => [cmd, ...args].join(" "))).toEqual(["docker compose up -d --wait --wait-timeout 60", "go run ./cmd/migrate up", "go build -o bin/api ./cmd/api"]);
  expect(ops.start.mock.calls[0][0]).toContain("bin/api"); expect(ops.ready.mock.calls).toEqual([["http://127.0.0.1:8080/api/v1/auth/me", 401], ["http://localhost:3000/login", 200]]);
  expect(ops.ready.mock.invocationCallOrder[0]).toBeLessThan(ops.start.mock.invocationCallOrder[1]);
});
it("does not start applications after migration failure", async () => {
  const ops = operations(); ops.run.mockResolvedValueOnce().mockRejectedValueOnce(new Error("migration failed")); await expect(boot(configuration(backend, frontend, {}), ops)).rejects.toThrow("migration"); expect(ops.start).not.toHaveBeenCalled();
});
it("does not touch Docker when a port is occupied", async () => {
  const ops = operations(); ops.free.mockRejectedValue(new Error("occupied")); await expect(boot(configuration(backend, frontend, {}), ops)).rejects.toThrow("occupied"); expect(ops.run).not.toHaveBeenCalled();
});
it("bounds readiness and respects cancellation", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 503 })));
  await expect(waitReady("http://localhost:3000", 200, new AbortController().signal, 1)).rejects.toThrow("timeout");
  const controller = new AbortController(); controller.abort(new Error("cancelled")); await expect(waitReady("http://localhost:3000", 200, controller.signal)).rejects.toThrow("cancelled");
});
describe("process supervision", () => {
  function child(pid) { const c = new EventEmitter(); c.pid = pid; c.stdout = new PassThrough(); c.stderr = new PassThrough(); return c; }
  it("detects an unexpected process exit and terminates both groups", async () => {
    const a = child(111), b = child(222); vi.mocked(spawn).mockReturnValueOnce(a).mockReturnValueOnce(b);
    const abort = new AbortController(), supervisor = new Processes(abort);
    supervisor.launch("api", [], { cwd: "/tmp", env: {}, label: "backend" }, true); supervisor.launch("next", [], { cwd: "/tmp", env: {}, label: "frontend" }, true);
    a.emit("exit", 1); await Promise.resolve(); expect(abort.signal.aborted).toBe(true);
    const kill = vi.spyOn(process, "kill").mockImplementation((_pid, signal) => { if (signal === 0) { const e = new Error(); e.code = "ESRCH"; throw e; } return true; });
    await supervisor.stop(); expect(kill).toHaveBeenCalledWith(-111, "SIGTERM"); expect(kill).toHaveBeenCalledWith(-222, "SIGTERM");
  });
});
