import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
/** Returns the newly registered user with HttpOnly session cookies, or an HTTP error. */
export async function POST(request: NextRequest) {
  return handleAuth(request, "register");
}
