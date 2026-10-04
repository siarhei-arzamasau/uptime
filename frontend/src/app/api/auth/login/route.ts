import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
/** Returns the signed-in user or an HTTP error; tokens are delivered only as HttpOnly cookies. */
export async function POST(request: NextRequest) {
  return handleAuth(request, "login");
}
