import { createChatRelay } from "@mise/chat/server";
import { headers as nextHeaders } from "next/headers";

import { SESSION_COOKIE } from "@/lib/session";

/**
 * The chat relay for the customer app (see @mise/chat/server). A signed-in
 * customer chats as themselves; a visitor chats as a guest. Tokens never
 * reach the browser.
 */
const relay = createChatRelay({
  apiUrl: process.env.API_INTERNAL_URL ?? "http://localhost:8090",
  secureCookies: process.env.WEB_COOKIE_SECURE !== "false",
  source: "app",
  customerToken: (request) => {
    const match = new RegExp(`(?:^|;\\s*)${SESSION_COOKIE}=([^;]+)`).exec(request.headers.get("cookie") ?? "");
    return match ? decodeURIComponent(match[1]!) : undefined;
  },
  headers: async (): Promise<Record<string, string>> => {
    const key = process.env.BFF_SHARED_KEY;
    if (!key) return {};
    const h = await nextHeaders();
    return {
      "X-Mise-Bff-Key": key,
      "X-Mise-Client-Ip": h.get("x-forwarded-for")?.split(",").at(-1)?.trim() ?? "",
      "X-Mise-Client-Ua": h.get("user-agent") ?? "",
    };
  },
});

type Ctx = { params: Promise<{ path: string[] }> };
const handle = async (request: Request, { params }: Ctx) => relay.handle(request, (await params).path);

export const GET = handle;
export const POST = handle;
