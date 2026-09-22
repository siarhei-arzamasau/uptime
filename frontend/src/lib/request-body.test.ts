import { expect, it, vi } from "vitest";
import { readJSON } from "./request-body";

function request(body: BodyInit, headers = {}) {
  return new Request("http://localhost/test", { method: "POST", body, duplex: "half", headers: { "Content-Type": "application/json", ...headers } } as RequestInit);
}
it.each([{}, { "Content-Length": "1" }])("stops an oversized stream and cancels it, even with a false size %j", async headers => {
  let reads = 0;
  const cancel = vi.fn();
  const stream = new ReadableStream({ pull(controller) { reads++; controller.enqueue(new Uint8Array(4096).fill(32)); }, cancel }, { highWaterMark: 0 });
  await expect(readJSON(request(stream, headers), 16 * 1024)).rejects.toMatchObject({ status: 413 });
  expect(reads).toBe(5);
  expect(cancel).toHaveBeenCalledOnce();
});
it("counts UTF-8 bytes rather than JavaScript characters", async () => {
  await expect(readJSON(request(JSON.stringify({ value: "я".repeat(20) })), 40)).rejects.toMatchObject({ status: 413 });
});
it("accepts a valid body exactly at the limit and rejects malformed UTF-8", async () => {
  await expect(readJSON(request('{"a":1}'), 7)).resolves.toEqual({ a: 1 });
  await expect(readJSON(request(new Uint8Array([34, 255, 34])), 7)).rejects.toMatchObject({ status: 400 });
});
it("rejects an excessive Content-Length without consuming the body", async () => {
  const pull = vi.fn();
  const stream = new ReadableStream({ pull }, { highWaterMark: 0 });
  await expect(readJSON(request(stream, { "Content-Length": "1000" }), 20)).rejects.toMatchObject({ status: 413 });
  expect(pull).not.toHaveBeenCalled();
});
