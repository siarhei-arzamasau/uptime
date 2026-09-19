import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
export async function POST(request: NextRequest) {
  return handleAuth(request, "session");
}
