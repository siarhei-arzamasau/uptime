import "server-only";
import { APIError } from "./api-error";

/**
 * Returns the request bytes, or an empty array when there is no body.
 * Counts streamed bytes even without a truthful Content-Length; throws tooLarge above
 * limit and propagates stream errors. It uses the body stream, not a separate abort signal.
 */
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

/**
 * Returns parsed, strictly decoded UTF-8 JSON within the byte limit.
 * Throws APIError 400 for a wrong media type or invalid JSON/UTF-8 and 413 for oversize
 * input; body-stream errors propagate. Object-shape validation belongs to the caller.
 */
export async function readJSON(req: Request, limit: number): Promise<unknown> {
  if (req.headers.get("content-type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
    throw new APIError(400, "invalid_request", "Expected a JSON request.");
  }
  const bytes = await readBody(req, limit);
  try { return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)); }
  catch { throw new APIError(400, "invalid_request", "Invalid request."); }
}
