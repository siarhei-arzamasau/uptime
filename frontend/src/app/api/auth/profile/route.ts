import { NextRequest } from "next/server";
import { handleAuth } from "@/features/auth/server";

export const runtime = "nodejs";
/** Returns the profile or an HTTP error; POST permits session recovery to rotate cookies. */
export async function POST(request: NextRequest) { return handleAuth(request, "profile"); }
/** Returns the profile after a name-only save, or an HTTP validation/session/service error. */
export async function PATCH(request: NextRequest) { return handleAuth(request, "profile-update"); }
