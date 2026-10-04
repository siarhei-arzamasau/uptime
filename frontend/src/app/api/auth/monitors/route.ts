import { NextRequest } from "next/server";
import { handleMonitors } from "@/features/monitors/server";

export const runtime = "nodejs";
/** Returns a cursor page or an HTTP error; POST permits session recovery to rotate cookies. */
export async function POST(request: NextRequest) { return handleMonitors(request, "list"); }
