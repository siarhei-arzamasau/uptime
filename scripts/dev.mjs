import { spawn } from "node:child_process";
import { access, readFile } from "node:fs/promises";
import { createServer } from "node:net";
import { createInterface } from "node:readline";
import { parseEnv } from "node:util";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

export const root = fileURLToPath(new URL("../", import.meta.url));
const backendKeys = ["GO_BIN", "DATABASE_URL", "HTTP_ADDR", "JWT_SECRET", "JWT_ISSUER", "JWT_AUDIENCE", "ALLOWED_ORIGIN", "COOKIE_SECURE", "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_PORT"];
const frontendKeys = ["BACKEND_URL", "APP_ORIGIN", "NEXT_DIST_DIR"];
const commonKeys = ["PATH", "HOME", "USER", "TMPDIR", "SHELL", "LANG", "LC_ALL", "GOPATH", "GOCACHE", "GOMODCACHE", "GOPROXY", "GOTOOLCHAIN", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG"];
function pick(env, keys) { return Object.fromEntries(keys.filter(k => env[k] !== undefined).map(k => [k, env[k]])); }
export function configuration(backendText, frontendText, ambient, e2e = false) {
  const common = pick(ambient, commonKeys);
  const b = { ...parseEnv(backendText), ...pick(ambient, backendKeys) };
  const f = { ...parseEnv(frontendText), ...pick(ambient, frontendKeys) };
  b.HTTP_ADDR ||= "127.0.0.1:8080";
  f.APP_ORIGIN ||= "http://localhost:3000";
  f.BACKEND_URL ||= "http://127.0.0.1:8080";
  if (e2e) {
    const db = new URL(b.DATABASE_URL); db.pathname = "/uptime_e2e_test"; b.DATABASE_URL = db.toString();
    b.HTTP_ADDR = "127.0.0.1:8081"; b.ALLOWED_ORIGIN = "http://localhost:3001"; b.COOKIE_SECURE = "false";
    f.APP_ORIGIN = "http://localhost:3001"; f.BACKEND_URL = "http://127.0.0.1:8081"; f.NEXT_DIST_DIR = ".next-e2e";
  }
  const origin = new URL(f.APP_ORIGIN), api = new URL(f.BACKEND_URL);
  if (origin.protocol !== "http:" || api.protocol !== "http:" || !["localhost", "127.0.0.1"].includes(origin.hostname) || !["localhost", "127.0.0.1"].includes(api.hostname) || origin.pathname !== "/" || api.pathname !== "/" || origin.search || api.search || origin.username || api.username) throw new Error("Local startup requires localhost HTTP origins without paths or credentials.");
  const apiPort = Number(b.HTTP_ADDR.split(":").at(-1));
  if (!["127.0.0.1", "localhost"].includes(b.HTTP_ADDR.split(":")[0]) || apiPort !== Number(api.port || 80) || Number(origin.port || 80) === apiPort) throw new Error("HTTP_ADDR, BACKEND_URL and APP_ORIGIN ports must agree and be distinct.");
  if (b.ALLOWED_ORIGIN && b.ALLOWED_ORIGIN !== origin.origin) throw new Error("ALLOWED_ORIGIN must match APP_ORIGIN.");
  if (!b.JWT_SECRET || b.JWT_SECRET.length < 32 || b.JWT_SECRET.startsWith("replace-") || !b.DATABASE_URL) throw new Error("Complete backend/.env with DATABASE_URL and a random JWT_SECRET first.");
  return { root, e2e, goCommand: b.GO_BIN || "go", api: api.origin, origin: origin.origin, apiPort, frontendPort: Number(origin.port || 80), backendEnv: { ...common, ...pick(b, backendKeys) }, frontendEnv: { ...common, ...pick(f, frontendKeys), NEXT_TELEMETRY_DISABLED: "1" }, common };
}
export async function boot(c, ops) {
  await ops.check(c);
  await ops.free(c.apiPort); await ops.free(c.frontendPort);
  const b = { cwd: path.join(c.root, "backend"), env: c.backendEnv, label: "database" };
  await ops.run("docker", ["compose", "up", "-d", "--wait", "--wait-timeout", "60"], b);
  if (c.e2e) {
    // Fixed dedicated DB name; never reset or drop the working database.
    await ops.run("docker", ["compose", "exec", "-T", "postgres", "sh", "-c", 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "SELECT 1 FROM pg_database WHERE datname=\'uptime_e2e_test\'" | grep -q 1 || createdb -U "$POSTGRES_USER" uptime_e2e_test'], b);
  }
  await ops.run(c.goCommand, ["run", "./cmd/migrate", "up"], { ...b, label: "backend" });
  await ops.run(c.goCommand, ["build", "-o", c.e2e ? "bin/api-e2e" : "bin/api", "./cmd/api"], { ...b, label: "backend" });
  ops.start(path.join(b.cwd, c.e2e ? "bin/api-e2e" : "bin/api"), [], { ...b, label: "backend" });
  await ops.ready(`${c.api}/api/v1/auth/me`, 401);
  ops.start("npm", ["run", "dev", "--", "--hostname", "127.0.0.1", "--port", String(c.frontendPort)], { cwd: path.join(c.root, "frontend"), env: c.frontendEnv, label: "frontend" });
  await ops.ready(`${c.origin}/login`, 200);
  ops.log(`Ready: frontend ${c.origin} · backend ${c.api}`);
}
export async function assertFree(port) {
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", () => reject(new Error(`Port ${port} is occupied. Stop its owner or change the configuration.`)));
    server.listen(port, "127.0.0.1", () => server.close(resolve));
  });
}
export async function waitReady(url, expected, signal, timeout = 60_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    signal.throwIfAborted();
    try { const res = await fetch(url, { signal: AbortSignal.any([signal, AbortSignal.timeout(1500)]), redirect: "manual" }); if (res.status === expected) return; } catch { signal.throwIfAborted(); }
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  throw new Error(`Readiness timeout: ${new URL(url).origin}`);
}
export class Processes {
  children = new Set();
  stopping = false;
  constructor(abort) { this.abort = abort; }
  launch(command, args, options, persistent = false) {
    const child = spawn(command, args, { cwd: options.cwd, env: options.env, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    this.children.add(child);
    for (const stream of [child.stdout, child.stderr]) {
      const lines = createInterface({ input: stream });
      lines.on("line", line => {
        // Remove configured secrets if a dependency happens to print them.
        for (const key of ["JWT_SECRET", "POSTGRES_PASSWORD", "DATABASE_URL"]) { const value = options.env[key]; if (value) line = line.split(value).join("[redacted]"); }
        console.log(`[${options.label}] ${line}`);
      });
    }
    const done = new Promise((resolve, reject) => {
      child.once("error", () => {
        this.children.delete(child);
        const hint = path.basename(command) === "go" ? "Install Go 1.26+ and add it to PATH, or set GO_BIN=/absolute/path/to/go in backend/.env." : "Check installed tools and PATH.";
        reject(new Error(`Cannot start ${command}. ${hint}`));
      });
      child.once("exit", code => { if (!persistent) this.children.delete(child); if (code === 0) resolve(); else reject(new Error(`${options.label} exited with code ${code ?? "signal"}.`)); });
    });
    if (persistent) done.then(() => { if (!this.stopping) this.abort.abort(new Error(`${options.label} stopped unexpectedly.`)); }, e => { if (!this.stopping) this.abort.abort(e); });
    return done;
  }
  async run(command, args, options) {
    const timer = setTimeout(() => this.abort.abort(new Error(`${options.label} startup step timed out.`)), 120_000);
    try { await Promise.race([this.launch(command, args, options), new Promise((_, reject) => { if (this.abort.signal.aborted) reject(this.abort.signal.reason); else this.abort.signal.addEventListener("abort", () => reject(this.abort.signal.reason), { once: true }); })]); } finally { clearTimeout(timer); }
  }
  async stop() {
    this.stopping = true;
    const signal = name => { for (const child of this.children) { if (child.pid) { try { process.kill(-child.pid, name); } catch (e) { if (e.code !== "ESRCH") throw e; } } } };
    signal("SIGTERM");
    const deadline = Date.now() + 4000;
    while (Date.now() < deadline) {
      const alive = [...this.children].some(child => { try { process.kill(-child.pid, 0); return true; } catch { return false; } });
      if (!alive) return;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    signal("SIGKILL");
  }
}
export async function main(e2e = process.argv.includes("--e2e")) {
  const [major, minor] = process.versions.node.split(".").map(Number);
  if (major < 22 || (major === 22 && minor < 12)) throw new Error("The shared launcher requires Node.js 22.12+.");
  const abort = new AbortController(); const processes = new Processes(abort);
  let interrupted = false;
  const stop = () => { interrupted = true; abort.abort(new Error("Shutdown requested")); };
  process.once("SIGINT", stop); process.once("SIGTERM", stop);
  try {
    let c;
    try { c = configuration(await readFile(path.join(root, "backend/.env"), "utf8"), await readFile(path.join(root, "frontend/.env.local"), "utf8"), process.env, e2e); }
    catch (error) { if (error.code === "ENOENT") throw new Error("Create backend/.env and frontend/.env.local from their examples first."); throw error; }
    const ops = {
      log: console.log,
      check: async () => {
        await access(path.join(root, "frontend/node_modules/next/package.json")).catch(() => { throw new Error("Run npm ci in frontend first."); });
        for (const [cmd, args] of [["npm", ["--version"]], [c.goCommand, ["version"]], ["docker", ["compose", "version"]], ["docker", ["info", "--format", "{{.ServerVersion}}"]]]) await processes.run(cmd, args, { cwd: root, env: c.common, label: "check" });
      },
      free: assertFree,
      run: (...args) => processes.run(...args),
      start: (...args) => { processes.launch(...args, true); },
      ready: (url, code) => waitReady(url, code, abort.signal),
    };
    await boot(c, ops);
    if (!abort.signal.aborted) await new Promise(resolve => abort.signal.addEventListener("abort", resolve, { once: true }));
    if (!interrupted) throw abort.signal.reason;
  } catch (error) { if (!interrupted) throw error; }
  finally { await processes.stop(); process.removeListener("SIGINT", stop); process.removeListener("SIGTERM", stop); }
}
if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch(error => { console.error(`[startup] ${error.message}`); process.exitCode = 1; });
}
