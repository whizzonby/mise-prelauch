import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/**
 * Streams a finished export file from the API. Only the person who asked
 * for it can download it, and only while their role still allows it; the
 * API checks both and records the download in the audit log.
 */
export async function GET(_: Request, { params }: { params: Promise<{ id: string }> }) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to download exports.", { status: 401 });
  const { id } = await params;
  try {
    const upstream = await api.admin.downloadExport(token, id);
    return new Response(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": upstream.headers.get("Content-Type") ?? "text/csv; charset=utf-8",
        "Content-Disposition": upstream.headers.get("Content-Disposition") ?? 'attachment; filename="mise-export.csv"',
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    if (error instanceof ApiError) {
      const message = error.status === 403 ? "Your role cannot download this export." : error.status === 410 ? "This export has expired. Request a new one." : error.message;
      return new Response(message, { status: error.status });
    }
    return new Response("The export could not be downloaded. Try again.", { status: 502 });
  }
}
