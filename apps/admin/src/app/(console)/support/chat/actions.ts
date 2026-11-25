"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "../../meals/actions";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

function failure(e: unknown): CatalogState {
  if (e instanceof ApiError) {
    if (e.code === "validation_failed") return { error: "Some fields need attention.", fields: e.fields };
    if (e.status === 403) return { error: "Your role does not allow this." };
    return { error: e.message };
  }
  return { error: "The Mise API could not be reached. Try again." };
}

async function run(fn: (token: string) => Promise<unknown>, message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/support/chat");
  return { message };
}

/** Sends a reply, or with kind=note an internal note (never shown to the customer). */
export async function sendFromComposer(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "conversation_id");
  const body = text(form, "body");
  if (!body) return { error: "Write something first.", fields: [{ field: "body", message: "Write something first." }] };
  if (text(form, "kind") === "note") return run((t) => api.admin.chatNote(t, id, body), "Note added.");
  return run((t) => api.admin.chatReply(t, id, body, text(form, "client_message_id") || undefined), "Reply sent.");
}

export async function updateConversation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const tags = text(form, "tags")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
  return run(
    (t) =>
      api.admin.chatUpdate(t, text(form, "conversation_id"), {
        priority: text(form, "priority") as "low" | "normal" | "high" | "urgent",
        tags,
        order_id: text(form, "order_id"),
        subscription_id: text(form, "subscription_id"),
      }),
    "Saved.",
  );
}

export async function assignConversation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const transfer = text(form, "mode") === "transfer";
  return run(
    (t) => api.admin.chatAssign(t, text(form, "conversation_id"), { assignee_id: text(form, "assignee_id"), team: text(form, "team"), note: text(form, "note") }, transfer),
    transfer ? "Transferred." : "Assignment saved.",
  );
}

export async function setConversationStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  const action = text(form, "action") === "reopen" ? "reopen" : "resolve";
  return run((t) => api.admin.chatSetStatus(t, text(form, "conversation_id"), action, text(form, "note")), action === "resolve" ? "Resolved." : "Reopened.");
}

export async function escalateConversation(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.admin.chatEscalate(t, text(form, "conversation_id"), {
        category: text(form, "category") as "general",
        subject: text(form, "subject") || undefined,
        note: text(form, "note") || undefined,
      }),
    "Support case opened.",
  );
}

/** Called by the inbox while a conversation is on screen (read receipts). */
export async function markRead(id: string, seq: number): Promise<void> {
  const { token } = await requireAdmin();
  try {
    await api.admin.chatRead(token, id, seq);
  } catch {
    // A read receipt is a courtesy; never interrupt the agent for it.
  }
}

/** Throttled by the composer; never stored. */
export async function agentTyping(id: string): Promise<void> {
  const { token } = await requireAdmin();
  try {
    await api.admin.chatTyping(token, id);
  } catch {
    // Typing is best effort.
  }
}
