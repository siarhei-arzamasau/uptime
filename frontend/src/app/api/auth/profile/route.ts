import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
// Reads use POST because session refresh may rotate HttpOnly cookies.
export async function POST(request: NextRequest) { return handleAuth(request, "profile"); }
export async function PATCH(request: NextRequest) { return handleAuth(request, "profile-update"); }
