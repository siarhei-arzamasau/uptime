import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";
export const runtime = "nodejs";
/** Returns the profile after saving name/avatar, or an HTTP upload/session/service error. */
export async function POST(request: NextRequest) { return handleAuth(request, "avatar"); }
