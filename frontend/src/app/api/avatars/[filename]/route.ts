import { NextRequest } from "next/server";
import { serveAvatar } from "@/features/auth/server";
export const runtime = "nodejs";
/** Returns the avatar proxy's image response or HTTP error for the resolved filename. */
export async function GET(_request: NextRequest, context: RouteContext<"/api/avatars/[filename]">) {
  const { filename } = await context.params;
  return serveAvatar(filename);
}
