import { ApiError, type LeadFilter } from "@mise/api-client";
import type { NextRequest } from "next/server";

import { api, sessionToken } from "@/lib/session";

/**
 * Streams the CSV export from the API to the browser. The session token is
 * added here, on the server; the API checks the `leads:export` permission and
 * writes the audit entry before it sends a single row.
 */
export async function GET(request: NextRequest) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to export leads.", { status: 401 });

  const filter: LeadFilter = {};
  for (const key of ["q", "status", "location", "source", "packaging", "referral", "from", "to"] as const) {
    const value = request.nextUrl.searchParams.get(key);
    if (value) filter[key] = value;
  }

  try {
    const upstream = await api.admin.exportLeads(token, filter);
    return new Response(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": upstream.headers.get("Content-Type") ?? "text/csv; charset=utf-8",
        "Content-Disposition": upstream.headers.get("Content-Disposition") ?? 'attachment; filename="mise-leads.csv"',
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    if (error instanceof ApiError) {
      const message = error.status === 403 ? "Your role cannot export leads." : error.message;
      return new Response(message, { status: error.status });
    }
    return new Response("The export could not be produced. Try again.", { status: 502 });
  }
}
