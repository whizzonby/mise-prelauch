import { createChatRelay } from "@mise/chat/server";
import { headers as nextHeaders } from "next/headers";

import { env } from "@/lib/env";

/**
 * The chat relay for the marketing site (see @mise/chat/server). Visitors
 * here chat as guests; customers are pointed to the customer app, where
 * they chat signed in. The guest token never reaches the browser.
 */
const relay = createChatRelay({
  apiUrl: process.env.API_INTERNAL_URL ?? env.apiUrl,
  secureCookies: process.env.MARKETING_COOKIE_SECURE !== "false",
  source: "site",
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
