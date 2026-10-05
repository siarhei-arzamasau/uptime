import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { parseEnv } from "node:util";

try {
  const { DATABASE_URL } = parseEnv(await readFile(new URL("../backend/.env", import.meta.url), "utf8"));
  if (!DATABASE_URL) throw new Error("Set DATABASE_URL in backend/.env.");
  const database = new URL(DATABASE_URL);
  // URL fragments are not connection fields; reject raw hashes rather than let parsers disagree.
  if (!["postgres:", "postgresql:"].includes(database.protocol) || !["localhost", "127.0.0.1", "[::1]"].includes(database.hostname) || DATABASE_URL.includes("#")) {
    throw new Error("Postgres MCP requires a local PostgreSQL DATABASE_URL.");
  }

  // libpq query fields and service profiles can override a local URI authority.
  for (const [key] of database.searchParams) {
    if (["host", "hostaddr", "service"].includes(key.toLowerCase())) {
      throw new Error("Postgres MCP does not accept connection address overrides.");
    }
  }
  // Ambient libpq settings must not redirect the checkout's connection.
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.toUpperCase().startsWith("PG")));

  // postgres-mcp 0.3.0 imports FastMCP, which was removed in MCP SDK 2.
  const server = spawn("uvx", ["--with", "mcp==1.30.0", "postgres-mcp==0.3.0", "--access-mode=unrestricted"], {
    stdio: "inherit",
    env: { ...env, DATABASE_URI: DATABASE_URL },
  });
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => server.kill(signal));
  }
  server.on("error", () => {
    console.error("[postgres-mcp] Unable to start uvx. Install uv and make it available on PATH.");
    process.exitCode = 1;
  });
  server.on("exit", (code, signal) => {
    process.exitCode = code ?? (signal ? 1 : 0);
  });
} catch {
  console.error("[postgres-mcp] Check backend/.env: DATABASE_URL must point to local PostgreSQL.");
  process.exitCode = 1;
}
