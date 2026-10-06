import { NextRequest } from "next/server";
import { handleStatistics } from "@/features/monitors/statistics-server";
export const runtime = "nodejs";
/** Returns owned history through the session-recovering BFF. */
export async function POST(request: NextRequest, context: { params: Promise<{ id: string }> }) {
  return handleStatistics(request, "history", (await context.params).id);
}
