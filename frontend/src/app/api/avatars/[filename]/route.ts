import { NextRequest } from "next/server";
import { serveAvatar } from "@/features/auth/server";
export const runtime = "nodejs";
export async function GET(_request: NextRequest, context: RouteContext<"/api/avatars/[filename]">) {
  const { filename } = await context.params;
  return serveAvatar(filename);
}
