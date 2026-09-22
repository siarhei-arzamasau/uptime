import { NextRequest } from "next/server";
import { handleMonitors } from "@/features/monitors/server";

export const runtime = "nodejs";
export async function POST(request: NextRequest) { return handleMonitors(request, "create"); }
