import { NextRequest } from "next/server";
import { handleMonitors } from "@/features/monitors/server";

export const runtime = "nodejs";
/** Returns the saved monitor, or a validation/session/service error. */
export async function POST(request: NextRequest, context: { params: Promise<{ id: string }> }) {
  const { id } = await context.params;
  return handleMonitors(request, "update", id);
}
