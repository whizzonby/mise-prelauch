"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";

import { chat, ChatError, newClientId, type Conversation, type Message } from "./client";

type Shown = Message & { state?: "sending" | "failed" };

function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

function time(iso: string) {
  return new Intl.DateTimeFormat("en-TT", { hour: "numeric", minute: "2-digit" }).format(new Date(iso));
}

/** Plain text with line breaks kept; never HTML. */
function Body({ text }: { text: string }) {
  return <span className="whitespace-pre-wrap break-words">{text}</span>;
}

/**
 * One conversation, live. History comes over HTTP; new messages, status
 * changes, the team's read position and typing arrive over Server-Sent
 * Events (the browser resumes from the last message id after a drop).
 * If live updates are unavailable it falls back to checking every 15 s.
 */
export function ConversationView({
  id,
  onBack,
  autoFocus = true,
  className,
}: {
  id: string;
  onBack?: () => void;
  autoFocus?: boolean;
  className?: string;
}) {
  const [conv, setConv] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Shown[]>([]);
  const [typing, setTyping] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [live, setLive] = useState(true);
  const [draft, setDraft] = useState("");
  const lastSeq = useRef(0);
  const listRef = useRef<HTMLOListElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const typingSent = useRef(0);
  const readSent = useRef(0);

  const merge = useCallback((incoming: Message[]) => {
    if (!incoming.length) return;
    setMessages((prev) => {
      const next = [...prev];
      for (const m of incoming) {
        lastSeq.current = Math.max(lastSeq.current, m.seq);
        const i = next.findIndex((x) => x.seq === m.seq || (m.client_message_id && x.client_message_id === m.client_message_id));
        if (i >= 0) next[i] = m;
        else next.push(m);
      }
      return next.sort((a, b) => (a.state && !b.state ? 1 : !a.state && b.state ? -1 : a.seq - b.seq));
    });
  }, []);

  // History, then live updates.
  useEffect(() => {
    let cancelled = false;
    let source: EventSource | null = null;
    let poll: ReturnType<typeof setInterval> | null = null;
    let typingTimer: ReturnType<typeof setTimeout> | null = null;
    // A different conversation is a different component (callers pass key={id}).

    const refresh = async () => {
      try {
        const [c, ms] = await Promise.all([chat.conversation(id), chat.messages(id, lastSeq.current)]);
        if (cancelled) return;
        setConv(c);
        merge(ms.items);
      } catch (e) {
        if (!cancelled) setProblem(e instanceof ChatError ? e.message : "This conversation could not be loaded.");
      }
    };

    void refresh().then(() => {
      if (cancelled || typeof EventSource === "undefined") {
        poll = setInterval(refresh, 15_000);
        return;
      }
      source = new EventSource(chat.streamUrl(id, lastSeq.current));
      source.addEventListener("message", (e) => {
        const m = JSON.parse((e as MessageEvent).data) as Message;
        if (m.author === "agent") setTyping(false); // the reply they were typing has arrived
        merge([m]);
      });
      source.addEventListener("conversation", (e) => setConv(JSON.parse((e as MessageEvent).data) as Conversation));
      source.addEventListener("typing", () => {
        setTyping(true);
        if (typingTimer) clearTimeout(typingTimer);
        typingTimer = setTimeout(() => setTyping(false), 4000);
      });
      source.addEventListener("end", () => {
        source?.close();
        setLive(false);
        poll = setInterval(refresh, 15_000);
      });
      source.onopen = () => setLive(true);
      source.onerror = () => {
        // The browser retries on its own (with the last message id). A refused
        // stream (signed out, not found) is closed: fall back to polling.
        if (source?.readyState === EventSource.CLOSED && !poll) {
          setLive(false);
          poll = setInterval(refresh, 15_000);
        }
      };
    });

    return () => {
      cancelled = true;
      source?.close();
      if (poll) clearInterval(poll);
      if (typingTimer) clearTimeout(typingTimer);
    };
  }, [id, merge]);

  // Keep the newest message in view.
  useLayoutEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length, typing]);

  // Tell the team how far we have read (only while the page is visible).
  useEffect(() => {
    const teamSeq = Math.max(0, ...messages.filter((m) => m.author === "agent" && !m.state).map((m) => m.seq));
    if (teamSeq <= readSent.current || document.visibilityState !== "visible") return;
    readSent.current = teamSeq;
    void chat.read(id, teamSeq).catch(() => undefined);
  }, [messages, id]);

  useEffect(() => {
    if (autoFocus) inputRef.current?.focus();
  }, [autoFocus, id]);

  async function deliver(body: string, clientId: string) {
    setMessages((prev) => prev.map((m) => (m.client_message_id === clientId ? { ...m, state: "sending" } : m)));
    try {
      merge([await chat.send(id, body, clientId)]);
    } catch {
      setMessages((prev) => prev.map((m) => (m.client_message_id === clientId ? { ...m, state: "failed" } : m)));
    }
  }

  function submit(e?: FormEvent) {
    e?.preventDefault();
    const body = draft.trim();
    if (!body) return;
    const clientId = newClientId();
    setDraft("");
    // A placeholder seq above any real one keeps unsent messages last, in send order.
    setMessages((prev) => [
      ...prev,
      { conversation_id: id, seq: 1e15 + Date.now() + prev.length, author: "customer", author_name: "", body, client_message_id: clientId, created_at: new Date().toISOString(), state: "sending" },
    ]);
    void deliver(body, clientId);
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  }

  function onType(value: string) {
    setDraft(value);
    const now = Date.now();
    if (value && now - typingSent.current > 3000) {
      typingSent.current = now;
      void chat.typing(id).catch(() => undefined);
    }
  }

  const mine = (m: Message) => m.author === "customer" || m.author === "guest";
  const lastMine = [...messages].reverse().find((m) => mine(m) && !m.state);

  return (
    <section className={cx("flex min-h-0 flex-1 flex-col", className)} aria-label={conv ? `Conversation ${conv.number}` : "Conversation"}>
      <header className="flex items-center gap-2 border-b border-border px-4 py-3">
        {onBack ? (
          <button type="button" onClick={onBack} className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md hover:bg-surface" aria-label="Back to messages">
            <span aria-hidden="true">←</span>
          </button>
        ) : null}
        <div className="min-w-0">
          <p className="type-body-sm font-semibold">{conv ? `Conversation ${conv.number}` : "Conversation"}</p>
          <p className="type-caption text-muted">
            {conv?.status === "resolved" ? "Resolved. Write again to reopen it." : live ? "The Mise team" : "Checking for replies every few seconds"}
          </p>
        </div>
      </header>

      {problem ? (
        <p role="alert" className="type-body-sm m-4 border-l-4 border-error pl-3">
          {problem}
        </p>
      ) : null}

      <ol ref={listRef} aria-live="polite" aria-relevant="additions" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 py-4">
        {messages.map((m) => (
          <li key={m.client_message_id ?? m.seq} className={cx("flex max-w-[85%] flex-col", mine(m) ? "items-end self-end" : "items-start self-start")}>
            {!mine(m) ? <span className="type-caption mb-1 text-muted">{m.author_name || "Mise team"}</span> : null}
            <span
              className={cx(
                "type-body-sm rounded-2xl px-3.5 py-2.5",
                mine(m) ? "rounded-br-sm bg-primary text-primary-foreground" : "rounded-bl-sm border border-border bg-surface",
                m.state === "failed" && "opacity-70",
              )}
            >
              <Body text={m.body} />
            </span>
            <span className="type-caption mt-1 text-muted">
              {m.state === "sending" ? (
                "Sending…"
              ) : m.state === "failed" ? (
                <button type="button" className="font-semibold text-error underline underline-offset-2" onClick={() => void deliver(m.body, m.client_message_id!)}>
                  Not sent. Try again
                </button>
              ) : (
                <>
                  {time(m.created_at)}
                  {lastMine && m.seq === lastMine.seq ? (conv && conv.team_read_seq >= m.seq ? " · Seen" : " · Delivered") : ""}
                </>
              )}
            </span>
          </li>
        ))}
        {typing ? (
          <li className="type-caption self-start text-muted" aria-live="off">
            The Mise team is typing…
          </li>
        ) : null}
      </ol>

      {conv?.status === "resolved" && conv.feedback_rating == null ? <Feedback id={id} onDone={(r) => setConv({ ...conv, feedback_rating: r })} /> : null}

      <form onSubmit={submit} className="flex items-end gap-2 border-t border-border p-3">
        <label htmlFor={`chat-input-${id}`} className="sr-only">
          Your message
        </label>
        <textarea
          id={`chat-input-${id}`}
          ref={inputRef}
          value={draft}
          onChange={(e) => onType(e.target.value)}
          onKeyDown={onKeyDown}
          rows={1}
          maxLength={4000}
          placeholder="Write a message"
          className="type-body-sm max-h-32 min-h-11 flex-1 resize-none rounded-md border border-border bg-background px-3 py-2.5 focus:outline-2 focus:outline-primary"
        />
        <button
          type="submit"
          disabled={!draft.trim()}
          className="min-h-11 rounded-md bg-primary px-4 text-[0.9375rem] font-semibold text-primary-foreground disabled:opacity-50"
        >
          Send
        </button>
      </form>
    </section>
  );
}

function Feedback({ id, onDone }: { id: string; onDone: (rating: number) => void }) {
  const [sent, setSent] = useState(false);
  if (sent) return <p className="type-caption border-t border-border px-4 py-3 text-muted">Thanks for telling us.</p>;
  return (
    <fieldset className="border-t border-border px-4 py-3">
      <legend className="type-caption float-left mb-2 w-full text-muted">How did we do? (optional)</legend>
      <div className="clear-left flex gap-1.5">
        {[1, 2, 3, 4, 5].map((r) => (
          <button
            key={r}
            type="button"
            className="type-body-sm min-h-11 min-w-11 rounded-md border border-border font-semibold hover:border-primary"
            aria-label={`${r} out of 5`}
            onClick={() => {
              setSent(true);
              void chat.feedback(id, r).then(() => onDone(r), () => setSent(false));
            }}
          >
            {r}
          </button>
        ))}
      </div>
    </fieldset>
  );
}
