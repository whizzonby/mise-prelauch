import { SESSION_COOKIE } from "@/lib/session";

/**
 * Relays customer-app behaviour events (contract v1,
 * docs/analytics/BEHAVIOR_EVENTS.md) to the API. The session token stays on
 * the server: a signed-in customer's events are attributed here, never by
 * the browser. Privacy signals are forwarded so the API also refuses to store.
 */
const API = process.env.API_INTERNAL_URL ?? "http://localhost:8090";
const MAX_BODY = 32 * 1024;

export async function POST(request: Request): Promise<Response> {
  if (request.headers.get("sec-gpc") === "1" || request.headers.get("dnt") === "1") {
    return Response.json({ accepted: 0 }, { status: 202 });
  }
  const body = await request.text();
  if (body.length > MAX_BODY) return Response.json({ accepted: 0 }, { status: 413 });

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const match = new RegExp(`(?:^|;\s*)${SESSION_COOKIE}=([^;]+)`).exec(request.headers.get("cookie") ?? "");
  if (match) headers.Authorization = `Bearer ${decodeURIComponent(match[1]!)}`;
  const key = process.env.BFF_SHARED_KEY;
  if (key) {
    headers["X-Mise-Bff-Key"] = key;
    headers["X-Mise-Client-Ip"] = request.headers.get("x-forwarded-for")?.split(",").at(-1)?.trim() ?? "";
    headers["X-Mise-Client-Ua"] = request.headers.get("user-agent") ?? "";
  }
  try {
    const res = await fetch(`${API}/api/v1/behavior/events`, { method: "POST", headers, body, cache: "no-store" });
    // Behaviour data must never break the page: the browser only learns accepted or not.
    return Response.json({ accepted: res.ok }, { status: res.ok ? 202 : 400 });
  } catch {
    return Response.json({ accepted: false }, { status: 502 });
  }
}
