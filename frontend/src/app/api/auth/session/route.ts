import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
/** Returns the current user or an HTTP error, refreshing HttpOnly cookies only if needed. */
export async function POST(request: NextRequest) {
  return handleAuth(request, "session");
}
