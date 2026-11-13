import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/** Streams the requirements CSV from the API, which checks the role and audits the download. */
export async function GET(request: Request) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to download.", { status: 401 });
  const q = new URL(request.url).searchParams;
  try {
    const upstream = await api.kitchen.requirementsCsv(token, {
      plan_id: q.get("plan_id") ?? undefined,
      from: q.get("from") ?? undefined,
      to: q.get("to") ?? undefined,
    });
    return new Response(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": 'attachment; filename="ingredient-requirements.csv"',
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    if (error instanceof ApiError) return new Response(error.status === 403 ? "Your role cannot download this." : error.message, { status: error.status });
    return new Response("The file could not be downloaded. Try again.", { status: 502 });
  }
}
