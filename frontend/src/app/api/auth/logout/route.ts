import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
/** Returns a logout acknowledgement and clears cookies only after backend revocation succeeds. */
export async function POST(request: NextRequest) {
  return handleAuth(request, "logout");
}
