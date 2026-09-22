import { NextRequest } from "next/server";
import { handleMonitors } from "@/features/monitors/server";

export const runtime = "nodejs";
// Reads use POST because session refresh may rotate HttpOnly cookies.
export async function POST(request: NextRequest) { return handleMonitors(request, "list"); }
