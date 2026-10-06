import { NextRequest } from "next/server";
import { handleStatistics } from "@/features/monitors/statistics-server";
export const runtime = "nodejs";
/** Returns owned statuses through the session-recovering BFF. */
export async function POST(request: NextRequest) { return handleStatistics(request, "status"); }
