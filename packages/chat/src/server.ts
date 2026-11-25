/**
 * The chat relay (BFF). Mounted by each Next.js app at /api/chat/[...path],
 * it is the only thing the browser talks to:
 *   - the customer's session token and the guest token stay on this server
 *     (httpOnly cookies on the app host); the browser never sees them;
 *   - state-changing requests are accepted only from the app's own origin;
 *   - the guest token returned when a guest starts chatting is stored in a
 *     cookie and removed from the response;
 *   - live streams are piped through and closed upstream when the browser
 *     leaves.
 * The API decides what each caller may see; this relay adds no rules of its own.
 */

export const GUEST_COOKIE = "mise_chat_guest";
const GUEST_MAX_AGE = 30 * 24 * 60 * 60;

export interface ChatRelayOptions {
  /** The Mise API origin, e.g. http://api:8080 (server-to-server). */
  apiUrl: string;
  /** Headers added to every API call (the BFF key and the client's address). */
  headers?: (request: Request) => Record<string, string> | Promise<Record<string, string>>;
  /** The signed-in customer's API token, if this app has customer sessions. */
  customerToken?: (request: Request) => string | undefined | Promise<string | undefined>;
  /** Mark cookies Secure (true except on local http). */
  secureCookies: boolean;
  /** Where conversations started here come from. */
  source: "site" | "app";
}

type Mode = { kind: "customer"; token: string } | { kind: "guest"; token: string } | { kind: "none" };

function readCookie(request: Request, name: string): string | undefined {
  const raw = request.headers.get("cookie");
  if (!raw) return undefined;
  for (const part of raw.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return decodeURIComponent(v.join("="));
  }
  return undefined;
}

function guestCookie(value: string, opts: ChatRelayOptions, maxAge = GUEST_MAX_AGE): string {
  return [
    `${GUEST_COOKIE}=${encodeURIComponent(value)}`,
    "Path=/api/chat",
    "HttpOnly",
    "SameSite=Lax",
    `Max-Age=${maxAge}`,
    ...(opts.secureCookies ? ["Secure"] : []),
  ].join("; ");
}

const json = (status: number, body: unknown, extra: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", "Cache-Control": "no-store", ...extra } });

const fail = (status: number, code: string, message: string) => json(status, { error: { code, message, fields: [] } });

/** Same-origin check for anything that changes state (CSRF). */
function sameOrigin(request: Request): boolean {
  const site = request.headers.get("sec-fetch-site");
  if (site) return site === "same-origin";
  const origin = request.headers.get("origin");
  if (!origin) return false;
  const host = request.headers.get("x-forwarded-host") ?? request.headers.get("host");
  try {
    return new URL(origin).host === host;
  } catch {
    return false;
  }
}

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function createChatRelay(opts: ChatRelayOptions) {
  const base = opts.apiUrl.replace(/\/$/, "") + "/api/v1";

  async function call(request: Request, path: string, init: { method?: string; body?: string; auth?: Mode; query?: string; signal?: AbortSignal } = {}) {
    const headers: Record<string, string> = { Accept: "application/json", ...(opts.headers ? await opts.headers(request) : {}) };
    if (init.body !== undefined) headers["Content-Type"] = "application/json";
    if (init.auth?.kind === "customer") headers.Authorization = `Bearer ${init.auth.token}`;
    if (init.auth?.kind === "guest") headers["X-Guest-Token"] = init.auth.token;
    return fetch(base + path + (init.query ?? ""), { method: init.method ?? "GET", headers, body: init.body, cache: "no-store", signal: init.signal });
  }

  async function linkCall(request: Request, customer: string, guest: string) {
    const headers: Record<string, string> = { Accept: "application/json", ...(opts.headers ? await opts.headers(request) : {}) };
    headers.Authorization = `Bearer ${customer}`;
    headers["X-Guest-Token"] = guest;
    return fetch(base + "/me/chat/link-guest", { method: "POST", headers, cache: "no-store" });
  }

  /**
   * Who is asking. A signed-in customer always wins. If they also hold a
   * guest cookie, the guest history is linked to the account (the API checks
   * the verified email and the token) and the cookie is cleared once that
   * has settled.
   */
  async function who(request: Request): Promise<{ mode: Mode; setCookie?: string }> {
    const customer = opts.customerToken ? await opts.customerToken(request) : undefined;
    const guest = readCookie(request, GUEST_COOKIE);
    if (customer) {
      let setCookie: string | undefined;
      if (guest) {
        const res = await linkCall(request, customer, guest);
        if (res.ok || res.status === 401) setCookie = guestCookie("", opts, 0);
      }
      return { mode: { kind: "customer", token: customer }, setCookie };
    }
    if (guest) return { mode: { kind: "guest", token: guest } };
    return { mode: { kind: "none" } };
  }

  /** Passes the API's answer through; a 401 for a guest clears the dead cookie. */
  async function relay(res: Response, m: Mode, setCookie?: string): Promise<Response> {
    const headers: Record<string, string> = { "Content-Type": res.headers.get("Content-Type") ?? "application/json", "Cache-Control": "no-store" };
    const cookies: string[] = [];
    if (setCookie) cookies.push(setCookie);
    if (res.status === 401 && m.kind === "guest") cookies.push(guestCookie("", opts, 0));
    const out = new Response(await res.text(), { status: res.status, headers });
    for (const c of cookies) out.headers.append("Set-Cookie", c);
    return out;
  }

  function ownerBase(m: Mode): string | null {
    if (m.kind === "customer") return "/me/chat/conversations";
    if (m.kind === "guest") return "/chat/guest/conversations";
    return null;
  }

  async function handle(request: Request, path: string[]): Promise<Response> {
    const method = request.method.toUpperCase();
    if (method !== "GET" && !sameOrigin(request)) return fail(403, "cross_origin", "Requests must come from this site.");
    const [first, second, third] = path;

    // Public: availability and the help centre.
    if (method === "GET" && first === "availability" && path.length === 1) {
      return relay(await call(request, "/chat/availability"), { kind: "none" });
    }
    if (method === "GET" && first === "help") {
      if (second === "categories" && path.length === 2) return relay(await call(request, "/help/categories"), { kind: "none" });
      if (second === "articles" && path.length === 2) {
        const u = new URL(request.url);
        const q = new URLSearchParams();
        for (const k of ["q", "category"]) {
          const v = u.searchParams.get(k);
          if (v) q.set(k, v.slice(0, 100));
        }
        return relay(await call(request, "/help/articles", { query: q.size ? "?" + q : "" }), { kind: "none" });
      }
      if (second === "articles" && third && SLUG.test(third) && path.length === 3) {
        return relay(await call(request, "/help/articles/" + third), { kind: "none" });
      }
      return fail(404, "not_found", "Not found.");
    }

    const { mode: m, setCookie } = await who(request);

    // Who the widget is talking to (no tokens, ever).
    if (method === "GET" && first === "session" && path.length === 1) {
      const res = json(200, { data: { mode: m.kind } });
      if (setCookie) res.headers.append("Set-Cookie", setCookie);
      return res;
    }

    if (first !== "conversations") return fail(404, "not_found", "Not found.");
    const ob = ownerBase(m);

    // Start a conversation: signed-in customer, or a new guest.
    if (path.length === 1 && method === "POST") {
      const body = (await request.json().catch(() => null)) as Record<string, unknown> | null;
      if (!body) return fail(400, "bad_request", "Send a JSON body.");
      const payload = { body: body.body, client_message_id: body.client_message_id, source: opts.source };
      if (m.kind === "customer") {
        return relay(await call(request, "/me/chat/conversations", { method: "POST", auth: m, body: JSON.stringify(payload) }), m, setCookie);
      }
      const res = await call(request, "/chat/guest/conversations", {
        method: "POST",
        body: JSON.stringify({ ...payload, name: body.name, email: body.email }),
      });
      if (!res.ok) return relay(res, m);
      const data = (await res.json()) as { data: { guest: { token?: string; name: string; email: string }; conversation: unknown; message: unknown } };
      const token = data.data.guest.token;
      delete data.data.guest.token; // the token lives only in the httpOnly cookie
      const out = json(201, data);
      if (token) out.headers.append("Set-Cookie", guestCookie(token, opts));
      return out;
    }

    if (!ob) {
      // A visitor with no conversation yet.
      if (path.length === 1 && method === "GET") return json(200, { data: { items: [] } });
      return fail(401, "no_conversation", "Start a conversation first.");
    }
    if (path.length === 1 && method === "GET") return relay(await call(request, ob, { auth: m }), m, setCookie);

    const id = second;
    if (!id || !ID.test(id)) return fail(404, "not_found", "Not found.");
    const tail = path.slice(2).join("/");

    if (method === "GET" && tail === "") return relay(await call(request, `${ob}/${id}`, { auth: m }), m, setCookie);
    if (method === "GET" && tail === "messages") {
      const after = new URL(request.url).searchParams.get("after");
      const q = after && /^\d{1,12}$/.test(after) ? `?after=${after}` : "";
      return relay(await call(request, `${ob}/${id}/messages`, { auth: m, query: q }), m, setCookie);
    }
    if (method === "GET" && tail === "stream") {
      const after = new URL(request.url).searchParams.get("after");
      const last = request.headers.get("last-event-id");
      const from = last && /^\d{1,12}$/.test(last) ? last : after && /^\d{1,12}$/.test(after) ? after : "0";
      const upstream = new AbortController();
      request.signal.addEventListener("abort", () => upstream.abort());
      let res: Response;
      try {
        res = await call(request, `${ob}/${id}/stream`, { auth: m, query: `?after=${from}`, signal: upstream.signal });
      } catch {
        return fail(502, "upstream", "Live updates are unavailable.");
      }
      if (!res.ok || !res.body) return relay(res, m, setCookie);
      return new Response(res.body, {
        headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-store, no-transform", "X-Accel-Buffering": "no", Connection: "keep-alive" },
      });
    }
    if (method === "POST" && ["messages", "read", "typing", "feedback"].includes(tail)) {
      const body = tail === "typing" ? undefined : await request.text();
      if (body !== undefined && body.length > 16_000) return fail(413, "too_large", "That message is too long.");
      return relay(await call(request, `${ob}/${id}/${tail}`, { method: "POST", auth: m, body }), m, setCookie);
    }
    return fail(404, "not_found", "Not found.");
  }

  return { handle };
}
