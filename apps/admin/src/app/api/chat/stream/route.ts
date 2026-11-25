import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/**
 * Relays the support inbox's live signals (conversation id, seq and kind;
 * never message content) from the API. The staff session token stays on this
 * server. The API checks support_chat.read and re-checks the session while
 * the stream is open; the upstream stream closes when the browser leaves.
 */
export async function GET(request: Request) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in again.", { status: 401 });
  const upstream = new AbortController();
  request.signal.addEventListener("abort", () => upstream.abort());
  try {
    const res = await api.admin.chatStream(token, upstream.signal);
    return new Response(res.body, {
      headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-store, no-transform", "X-Accel-Buffering": "no", Connection: "keep-alive" },
    });
  } catch (e) {
    const status = e instanceof ApiError ? e.status : 502;
    return new Response(status === 403 ? "Not allowed." : "Live updates are unavailable.", { status });
  }
}
