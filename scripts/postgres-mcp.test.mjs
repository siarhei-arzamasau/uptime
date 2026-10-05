import { expect, it } from "vitest";
import { execFile } from "node:child_process";
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";

const execute = promisify(execFile);
const local = "postgresql://fixture:placeholder@localhost:5433/uptime";

async function launch(databaseURL, ambient = {}) {
  const root = await mkdtemp(path.join(tmpdir(), "uptime-postgres-mcp-test-"));
  try {
    await Promise.all(["scripts", "backend", "bin"].map(name => mkdir(path.join(root, name))));
    await copyFile(new URL("./postgres-mcp.mjs", import.meta.url), path.join(root, "scripts/postgres-mcp.mjs"));
    await writeFile(path.join(root, "backend/.env"), `DATABASE_URL=${JSON.stringify(databaseURL)}\n`);
    const report = path.join(root, "spawn.json");
    await writeFile(path.join(root, "bin/uvx"), `#!${process.execPath}
const { writeFileSync } = require("node:fs");
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.startsWith("PG") || key === "DATABASE_URI" || key === "UV_CACHE_DIR"));
writeFileSync(process.env.MCP_FIXTURE_REPORT, JSON.stringify({ args: process.argv.slice(2), env }));
`, { mode: 0o755 });
    let result;
    try {
      result = { code: 0, ...await execute(process.execPath, [path.join(root, "scripts/postgres-mcp.mjs")], {
        timeout: 10_000,
        env: { PATH: path.join(root, "bin") + path.delimiter + process.env.PATH, HOME: root, MCP_FIXTURE_REPORT: report, ...ambient },
      }) };
    } catch (error) {
      result = { code: error.code, stdout: error.stdout, stderr: error.stderr };
    }
    let spawned;
    try { spawned = JSON.parse(await readFile(report, "utf8")); } catch (error) { if (error.code !== "ENOENT") throw error; }
    return { ...result, spawned };
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

it.each(["localhost", "127.0.0.1", "[::1]"])("starts the pinned MCP for local authority %s", async host => {
  const uri = local.replace("localhost", host) + "?sslmode=disable&connect_timeout=5";
  const result = await launch(uri);
  expect(result.code).toBe(0);
  expect(result.spawned.args).toEqual(["--with", "mcp==1.30.0", "postgres-mcp==0.3.0", "--access-mode=unrestricted"]);
  expect(result.spawned.env.DATABASE_URI).toBe(uri);
});

it.each([
  "?hostaddr=192.0.2.1",
  "?hostaddr=2001%3Adb8%3A%3A1",
  "?host=remote.invalid",
  "?host=localhost%2Cremote.invalid",
  "?%68ostaddr=192.0.2.1",
  "?hostaddr=127.0.0.1&hostaddr=192.0.2.1",
  "?hostaddr=192.0.2.1&hostaddr=127.0.0.1",
  "?hostaddr=",
  "?host=localhost",
  "?service=remote-profile",
  "?service=",
  "?sslmode=disable#&hostaddr=192.0.2.1",
])("rejects connection overrides before spawning: %s", async query => {
  const result = await launch(local + query);
  expect(result.code).toBe(1);
  expect(result.spawned).toBeUndefined();
  expect(result.stderr).toContain("DATABASE_URL must point to local PostgreSQL");
  expect(result.stderr).not.toContain(local);
});

it.each(["postgresql://remote.invalid/uptime", "http://localhost/uptime", ""])(
  "rejects invalid local connection %s", async uri => {
    const result = await launch(uri);
    expect(result.code).toBe(1);
    expect(result.spawned).toBeUndefined();
  },
);

it("removes libpq environment overrides and keeps launcher configuration", async () => {
  const ambient = {
    PGHOST: "remote.invalid", PGHOSTADDR: "192.0.2.1", PGPORT: "6543", PGDATABASE: "remote",
    PGSERVICE: "remote-profile", PGSERVICEFILE: "/irrelevant/services.conf", PGSYSCONFDIR: "/irrelevant",
    PGUSER: "other", PGPASSWORD: "placeholder", DATABASE_URI: "postgresql://remote.invalid/remote",
    UV_CACHE_DIR: "/fixture/uv-cache",
  };
  const result = await launch(local, ambient);
  expect(result.code).toBe(0);
  expect(result.spawned.env).toEqual({ DATABASE_URI: local, UV_CACHE_DIR: ambient.UV_CACHE_DIR });
});
