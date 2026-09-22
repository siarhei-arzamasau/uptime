import { expect, it } from "vitest";
import cases from "../../../../contracts/monitor-urls.json";
import { validMonitorURL } from "./validation";

it("matches the same URL contract as the Go API", () => {
  for (const { value, valid } of cases) expect(validMonitorURL(value), value).toBe(valid);
});
