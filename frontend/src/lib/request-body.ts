import "server-only";
import { APIError } from "./api-error";

// Bound bytes retained even for chunked requests or a false Content-Length.
export async function readBody(req: Request, limit: number, tooLarge = new APIError(413, "request_too_large", "Request is too large.")) {
  if (Number(req.headers.get("content-length")) > limit) {
    throw tooLarge;
  }
  const reader = req.body?.getReader();
  if (!reader) return new Uint8Array(0);
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) {
        await reader.cancel();
        throw tooLarge;
      }
      chunks.push(value);
    }
  } finally { reader.releaseLock(); }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
  return bytes;
}

export async function readJSON(req: Request, limit: number): Promise<unknown> {
  if (req.headers.get("content-type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
    throw new APIError(400, "invalid_request", "Expected a JSON request.");
  }
  const bytes = await readBody(req, limit);
  try { return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)); }
  catch { throw new APIError(400, "invalid_request", "Invalid request."); }
}
