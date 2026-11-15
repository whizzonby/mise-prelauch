import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/** Streams the dashboard's CSV from the API, which checks permissions, neutralises formulas and audits the export. Never cached. */
export async function GET(request: Request, { params }: { params: Promise<{ kind: string }> }) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to download.", { status: 401 });
  const { kind } = await params;
  const q = new URL(request.url).searchParams;
  const query = Object.fromEntries(["from", "to", "plan_id", "meal_id", "zone_id", "supplier_id"].filter((k) => q.get(k)).map((k) => [k, q.get(k)!]));
  try {
    const upstream = await api.intelligence.exportCsv(token, kind, query);
    return new Response(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": upstream.headers.get("Content-Disposition") ?? `attachment; filename="mise-${kind}.csv"`,
        "Cache-Control": "no-store, private",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    if (error instanceof ApiError) return new Response(error.status === 403 ? "Your role cannot export this." : error.message, { status: error.status });
    return new Response("The export could not be downloaded. Try again.", { status: 502 });
  }
}
