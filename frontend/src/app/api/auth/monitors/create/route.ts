import { NextRequest } from "next/server";
import { handleMonitors } from "@/features/monitors/server";

export const runtime = "nodejs";
/** Returns the saved monitor with status 201, or an HTTP validation/session/service error. */
export async function POST(request: NextRequest) { return handleMonitors(request, "create"); }
