"use client";

import { cx, MiseButton } from "@mise/ui";
import { useRouter } from "next/navigation";
import { startTransition, useActionState, useEffect, useRef, useState, type KeyboardEvent } from "react";

import { agentTyping, markRead, sendFromComposer } from "@/app/(console)/support/chat/actions";

/**
 * Keeps the inbox live: listens to the API's support stream (through this
 * app's relay) and refreshes the server-rendered page when something
 * changes; shows when the customer on screen is typing; and records how far
 * the agent has read the open conversation.
 */
export function InboxLive({ currentId, lastSeq }: { currentId?: string; lastSeq?: number }) {
  const router = useRouter();
  const [typing, setTyping] = useState(false);
  // "Live" only once the stream is actually open.
  const [link, setLink] = useState<"connecting" | "live" | "down">("connecting");

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    let typingTimer: ReturnType<typeof setTimeout> | null = null;
    const refresh = () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => startTransition(() => router.refresh()), 400);
    };
    const source = new EventSource("/api/chat/stream");
    source.onopen = () => setLink("live");
    source.onerror = () => setLink(source.readyState === EventSource.CLOSED ? "down" : "connecting");
    for (const kind of ["message", "status", "read"]) source.addEventListener(kind, refresh);
    source.addEventListener("typing", (e) => {
      const data = JSON.parse((e as MessageEvent).data) as { conversation_id: string; actor: string };
      if (data.conversation_id !== currentId || data.actor === "agent") return;
      setTyping(true);
      if (typingTimer) clearTimeout(typingTimer);
      typingTimer = setTimeout(() => setTyping(false), 4000);
    });
    source.addEventListener("end", () => {
      source.close();
      setLink("down");
    });
    // Without a live connection, fall back to a refresh every 20 seconds.
    const poll = setInterval(() => {
      if (source.readyState === EventSource.CLOSED) refresh();
    }, 20_000);
    return () => {
      source.close();
      clearInterval(poll);
      if (timer) clearTimeout(timer);
      if (typingTimer) clearTimeout(typingTimer);
    };
  }, [router, currentId]);

  useEffect(() => {
    if (currentId && lastSeq && document.visibilityState === "visible") void markRead(currentId, lastSeq);
  }, [currentId, lastSeq]);

  return (
    <div className="type-caption flex min-h-6 items-center gap-3 text-muted" aria-live="polite">
      <span className="inline-flex items-center gap-1.5">
        <span aria-hidden="true" className={cx("inline-block size-2 rounded-full", link === "live" ? "bg-success" : "bg-warning")} />
        {link === "live" ? "Live" : link === "connecting" ? "Connecting…" : "Not live: refreshing every 20 seconds"}
      </span>
      {typing ? <span>The customer is typing…</span> : null}
    </div>
  );
}

interface SavedReply {
  id: string;
  title: string;
  body: string;
}

/** Reply or internal note, with saved replies. Enter sends; Shift+Enter adds a line. */
export function Composer({ conversationId, savedReplies, canReply, canNote }: { conversationId: string; savedReplies: SavedReply[]; canReply: boolean; canNote: boolean }) {
  const [kind, setKind] = useState<"reply" | "note">(canReply ? "reply" : "note");
  const [body, setBody] = useState("");
  const [clientId, setClientId] = useState(() => crypto.randomUUID());
  // Cleared only once the API has stored the message; a failed send keeps the text.
  const [state, action, busy] = useActionState(async (prev: Awaited<ReturnType<typeof sendFromComposer>>, form: FormData) => {
    const result = await sendFromComposer(prev, form);
    if (result.message && !result.error) {
      setBody("");
      setClientId(crypto.randomUUID());
    }
    return result;
  }, {});
  const form = useRef<HTMLFormElement>(null);
  const lastTyping = useRef(0);

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.current?.requestSubmit();
    }
  }

  function onChange(v: string) {
    setBody(v);
    const now = Date.now();
    if (kind === "reply" && v && now - lastTyping.current > 3000) {
      lastTyping.current = now;
      void agentTyping(conversationId);
    }
  }

  if (!canReply && !canNote) return null;
  return (
    <form ref={form} action={action} className={cx("grid gap-2 border-t p-3", kind === "note" ? "border-accent bg-accent/5" : "border-border")}>
      <input type="hidden" name="conversation_id" value={conversationId} />
      <input type="hidden" name="kind" value={kind} />
      <input type="hidden" name="client_message_id" value={clientId} />
      <div className="flex flex-wrap items-center gap-2">
        <div role="radiogroup" aria-label="Message type" className="inline-flex rounded-md border border-border p-0.5">
          {canReply ? (
            <button type="button" role="radio" aria-checked={kind === "reply"} onClick={() => setKind("reply")} className={cx("type-caption min-h-9 rounded px-3 font-semibold", kind === "reply" && "bg-primary text-primary-foreground")}>
              Reply
            </button>
          ) : null}
          {canNote ? (
            <button type="button" role="radio" aria-checked={kind === "note"} onClick={() => setKind("note")} className={cx("type-caption min-h-9 rounded px-3 font-semibold", kind === "note" && "bg-accent text-accent-foreground")}>
              Internal note
            </button>
          ) : null}
        </div>
        {savedReplies.length && kind === "reply" ? (
          <label className="type-caption ml-auto flex items-center gap-2">
            <span className="text-muted">Saved reply</span>
            <select
              className="min-h-9 rounded-md border border-border bg-background px-2"
              value=""
              onChange={(e) => {
                const r = savedReplies.find((x) => x.id === e.target.value);
                if (r) setBody((b) => (b ? `${b}\n\n${r.body}` : r.body));
              }}
            >
              <option value="">Insert…</option>
              {savedReplies.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.title}
                </option>
              ))}
            </select>
          </label>
        ) : null}
      </div>
      <label htmlFor="composer-body" className="sr-only">
        {kind === "note" ? "Internal note (only staff see this)" : "Reply to the customer"}
      </label>
      <textarea
        id="composer-body"
        name="body"
        rows={3}
        maxLength={4000}
        value={body}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={kind === "note" ? "Internal note: only staff see this" : "Write a reply"}
        className="type-body-sm w-full resize-y rounded-md border border-border bg-background p-3"
      />
      <div className="flex items-center justify-between gap-3">
        <p className="type-caption text-muted" role={state.error ? "alert" : "status"}>
          {state.error ?? (kind === "note" ? "Notes are never shown to the customer." : "Enter sends. Shift+Enter adds a line.")}
        </p>
        <MiseButton type="submit" disabled={busy || !body.trim()} variant={kind === "note" ? "secondary" : "primary"}>
          {busy ? "Sending…" : kind === "note" ? "Add note" : "Send reply"}
        </MiseButton>
      </div>
    </form>
  );
}
