/** Browser-side calls to this app's own chat relay (/api/chat). Never to the API directly. */

export interface Availability {
  enabled: boolean;
  online: boolean | null;
  message: string;
  response_expectation: string;
}

export interface Conversation {
  id: string;
  number: number;
  status: "open" | "pending" | "resolved";
  subject: string;
  last_seq: number;
  last_message: string;
  last_message_at: string | null;
  unread: number;
  team_read_seq: number;
  feedback_rating: number | null;
  created_at: string;
  updated_at: string;
}

export interface Message {
  conversation_id: string;
  seq: number;
  author: "customer" | "guest" | "agent" | "system";
  author_name: string;
  body: string;
  client_message_id: string | null;
  created_at: string;
}

export interface HelpCategory {
  slug: string;
  name: string;
  articles: number;
}

export interface HelpSummary {
  slug: string;
  category: string;
  title: string;
  summary: string;
}

export interface HelpArticle extends HelpSummary {
  body: string;
}

export type Mode = "customer" | "guest" | "none";

export class ChatError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: { field: string; message: string }[] = [],
  ) {
    super(message);
  }
}

const BASE = "/api/chat";

async function call<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      method: init.method ?? "GET",
      headers: init.body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      credentials: "same-origin",
      cache: "no-store",
    });
  } catch {
    throw new ChatError(0, "offline", "You seem to be offline. We'll try again.");
  }
  const payload = (await res.json().catch(() => ({}))) as { data?: T; error?: { code?: string; message?: string; fields?: { field: string; message: string }[] } };
  if (!res.ok) {
    throw new ChatError(res.status, payload.error?.code ?? "error", payload.error?.message ?? "Something went wrong. Try again.", payload.error?.fields ?? []);
  }
  return payload.data as T;
}

export const chat = {
  session: () => call<{ mode: Mode }>("/session"),
  availability: () => call<Availability>("/availability"),
  conversations: () => call<{ items: Conversation[] }>("/conversations"),
  conversation: (id: string) => call<Conversation>(`/conversations/${id}`),
  messages: (id: string, after = 0) => call<{ items: Message[] }>(`/conversations/${id}/messages?after=${after}`),
  start: (input: { body: string; client_message_id: string; name?: string; email?: string }) =>
    call<{ conversation: Conversation; message: Message }>("/conversations", { method: "POST", body: input }),
  send: (id: string, body: string, clientMessageId: string) =>
    call<Message>(`/conversations/${id}/messages`, { method: "POST", body: { body, client_message_id: clientMessageId } }),
  read: (id: string, seq: number) => call<{ ok: boolean }>(`/conversations/${id}/read`, { method: "POST", body: { seq } }),
  typing: (id: string) => call<{ ok: boolean }>(`/conversations/${id}/typing`, { method: "POST" }),
  feedback: (id: string, rating: number, comment = "") =>
    call<{ ok: boolean }>(`/conversations/${id}/feedback`, { method: "POST", body: { rating, comment } }),
  helpCategories: () => call<{ items: HelpCategory[] }>("/help/categories"),
  helpSearch: (q: string, category = "") =>
    call<{ items: HelpSummary[] }>(`/help/articles?${new URLSearchParams({ ...(q ? { q } : {}), ...(category ? { category } : {}) })}`),
  helpArticle: (slug: string) => call<HelpArticle>(`/help/articles/${slug}`),
  streamUrl: (id: string, after: number) => `${BASE}/conversations/${id}/stream?after=${after}`,
};

/** A message id chosen by the sender, so a resend is the same message. */
export function newClientId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `m${Date.now().toString(36)}${Math.random().toString(36).slice(2, 12)}`;
}
