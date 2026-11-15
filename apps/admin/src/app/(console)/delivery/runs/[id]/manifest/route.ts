import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/** Streams the run's manifest CSV from the API, which checks the role (couriers: own runs) and audits the download. Never cached. */
export async function GET(_: Request, { params }: { params: Promise<{ id: string }> }) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to download.", { status: 401 });
  const { id } = await params;
  try {
    const upstream = await api.delivery.manifestCsv(token, id);
    return new Response(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": upstream.headers.get("Content-Disposition") ?? 'attachment; filename="manifest.csv"',
        "Cache-Control": "no-store, private",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    if (error instanceof ApiError) return new Response(error.status === 403 ? "Your role cannot download this." : error.message, { status: error.status });
    return new Response("The manifest could not be downloaded. Try again.", { status: 502 });
  }
}
