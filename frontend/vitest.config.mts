import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)), "server-only": fileURLToPath(new URL("./src/test/server-only.ts", import.meta.url)) } },
  test: {
    environment: "node", include: ["src/**/*.test.{ts,tsx}", "../scripts/**/*.test.mjs"],
    setupFiles: ["./src/test/setup.ts"],
    coverage: { provider: "v8", include: ["src/features/auth/**/*.{ts,tsx}", "src/components/*.tsx", "../scripts/dev.mjs"], exclude: ["**/*.test.*", "**/types.ts"] },
  },
});
