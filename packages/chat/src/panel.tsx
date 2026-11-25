"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";

import { chat, ChatError, newClientId, type Availability, type Conversation, type HelpArticle, type HelpCategory, type HelpSummary, type Mode } from "./client";
import { ConversationView } from "./conversation";

type View = { tab: "home" } | { tab: "messages" } | { tab: "new" } | { tab: "conversation"; id: string } | { tab: "help"; q?: string } | { tab: "article"; slug: string };

function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

const field = "type-body-sm min-h-11 w-full rounded-md border border-border bg-background px-3 py-2.5 focus:outline-2 focus:outline-primary";

export interface PanelProps {
  onClose: () => void;
  onUnread: (n: number) => void;
  /** Shown to visitors on a site without accounts (the marketing site). */
  signInUrl?: string;
}

/**
 * The chat panel: Home, Messages and Help. Full screen on phones, a card
 * above the launcher from a tablet up. Escape closes it; focus moves in on
 * open and back to the launcher on close (handled by the launcher).
 */
export default function ChatPanel({ onClose, onUnread, signInUrl }: PanelProps) {
  const [view, setView] = useState<View>({ tab: "home" });
  const [mode, setMode] = useState<Mode>("none");
  const [availability, setAvailability] = useState<Availability | null>(null);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const titleRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    titleRef.current?.focus();
    void chat.availability().then(setAvailability, () => undefined);
    void chat.session().then((s) => {
      setMode(s.mode);
      if (s.mode !== "none") void loadConversations();
    }, () => undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function loadConversations() {
    try {
      const { items } = await chat.conversations();
      setConversations(items);
      onUnread(items.reduce((n, c) => n + c.unread, 0));
    } catch {
      /* the list is a convenience; the conversation itself still works */
    }
  }

  const open = conversations.filter((c) => c.status !== "resolved");
  const go = (v: View) => {
    if (v.tab === "messages" || v.tab === "home") void loadConversations();
    setView(v);
  };

  return (
    <div
      role="dialog"
      aria-labelledby="mise-chat-title"
      className="fixed inset-0 z-50 flex flex-col bg-background text-foreground shadow-2xl sm:inset-auto sm:bottom-24 sm:right-6 sm:h-[min(640px,calc(100vh-8rem))] sm:w-[380px] sm:overflow-hidden sm:rounded-2xl sm:border sm:border-border"
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
    >
      <header className="flex items-center justify-between bg-primary px-4 py-3 text-primary-foreground">
        <h2 id="mise-chat-title" ref={titleRef} tabIndex={-1} className="font-display text-[1.25rem] leading-tight outline-none">
          Mise help
        </h2>
        <button type="button" onClick={onClose} className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md hover:bg-white/10" aria-label="Close chat">
          <span aria-hidden="true" className="text-[1.25rem]">×</span>
        </button>
      </header>

      <div className="flex min-h-0 flex-1 flex-col">
        {view.tab === "home" ? (
          <Home
            availability={availability}
            open={open}
            onNew={() => go({ tab: "new" })}
            onOpen={(id) => go({ tab: "conversation", id })}
            onSearch={(q) => go({ tab: "help", q })}
            signInUrl={mode === "none" ? signInUrl : undefined}
          />
        ) : null}
        {view.tab === "messages" ? (
          <Messages items={conversations} onOpen={(id) => go({ tab: "conversation", id })} onNew={() => go({ tab: "new" })} />
        ) : null}
        {view.tab === "new" ? (
          <NewConversation
            guest={mode === "none"}
            availability={availability}
            onCreated={(c) => {
              setMode((m) => (m === "none" ? "guest" : m));
              setConversations((prev) => [c, ...prev]);
              setView({ tab: "conversation", id: c.id });
            }}
            onBack={() => go({ tab: "home" })}
          />
        ) : null}
        {view.tab === "conversation" ? <ConversationView key={view.id} id={view.id} onBack={() => go({ tab: "messages" })} /> : null}
        {view.tab === "help" ? <Help initial={view.q ?? ""} onOpen={(slug) => setView({ tab: "article", slug })} /> : null}
        {view.tab === "article" ? <Article slug={view.slug} onBack={() => setView({ tab: "help" })} onAsk={() => go({ tab: "new" })} /> : null}
      </div>

      <nav aria-label="Chat sections" className="grid grid-cols-3 border-t border-border bg-surface">
        {(
          [
            ["home", "Home"],
            ["messages", "Messages"],
            ["help", "Help"],
          ] as const
        ).map(([tab, label]) => {
          const current = view.tab === tab || (tab === "messages" && (view.tab === "conversation" || view.tab === "new")) || (tab === "help" && view.tab === "article");
          return (
            <button
              key={tab}
              type="button"
              aria-current={current ? "page" : undefined}
              onClick={() => go(tab === "help" ? { tab: "help" } : { tab })}
              className={cx("type-caption min-h-12 font-semibold", current ? "text-primary" : "text-muted")}
            >
              {label}
              {tab === "messages" && conversations.some((c) => c.unread > 0) ? <span className="ml-1 inline-block size-2 rounded-full bg-accent align-middle" aria-label="unread" /> : null}
            </button>
          );
        })}
      </nav>
    </div>
  );
}

function AvailabilityLine({ a }: { a: Availability | null }) {
  if (!a) return null;
  return (
    <p className="type-body-sm text-muted">
      {a.online === true ? <span className="mr-2 inline-block size-2 rounded-full bg-success align-middle" aria-hidden="true" /> : null}
      {a.message ? `${a.message} ` : ""}
      {a.response_expectation}
    </p>
  );
}

function Home({
  availability,
  open,
  onNew,
  onOpen,
  onSearch,
  signInUrl,
}: {
  availability: Availability | null;
  open: Conversation[];
  onNew: () => void;
  onOpen: (id: string) => void;
  onSearch: (q: string) => void;
  signInUrl?: string;
}) {
  const [q, setQ] = useState("");
  return (
    <div className="flex flex-1 flex-col gap-5 overflow-y-auto p-4">
      <div>
        <p className="type-h4">How can we help?</p>
        <div className="mt-2">
          <AvailabilityLine a={availability} />
        </div>
      </div>
      {open[0] ? (
        <button type="button" onClick={() => onOpen(open[0]!.id)} className="rounded-lg border border-border bg-surface p-3 text-left hover:border-primary">
          <span className="type-caption block text-muted">Continue conversation {open[0].number}</span>
          <span className="type-body-sm mt-1 line-clamp-2 block">{open[0].last_message}</span>
        </button>
      ) : null}
      {availability && !availability.enabled ? (
        <p className="type-body-sm border-l-4 border-warning pl-3">Chat is closed right now. The help articles are still here.</p>
      ) : (
        <button type="button" onClick={onNew} className="min-h-11 rounded-md bg-primary px-4 text-[0.9375rem] font-semibold text-primary-foreground">
          Send us a message
        </button>
      )}
      <form
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          onSearch(q.trim());
        }}
      >
        <label htmlFor="mise-chat-search" className="type-body-sm font-semibold">
          Search help articles
        </label>
        <div className="mt-2 flex gap-2">
          <input id="mise-chat-search" value={q} onChange={(e) => setQ(e.target.value)} className={field} maxLength={100} placeholder="Delivery, billing, pausing…" />
          <button type="submit" className="min-h-11 rounded-md border border-border px-3 text-[0.875rem] font-semibold">
            Search
          </button>
        </div>
      </form>
      {signInUrl ? (
        <p className="type-caption text-muted">
          Have an account?{" "}
          <a href={signInUrl} className="font-semibold text-foreground underline underline-offset-2">
            Sign in
          </a>{" "}
          to chat about your orders and subscription.
        </p>
      ) : null}
    </div>
  );
}

function Messages({ items, onOpen, onNew }: { items: Conversation[]; onOpen: (id: string) => void; onNew: () => void }) {
  return (
    <div className="flex flex-1 flex-col overflow-y-auto">
      {items.length ? (
        <ul className="divide-y divide-border">
          {items.map((c) => (
            <li key={c.id}>
              <button type="button" onClick={() => onOpen(c.id)} className="flex min-h-16 w-full items-start gap-3 px-4 py-3 text-left hover:bg-surface">
                <span className="min-w-0 flex-1">
                  <span className="type-body-sm block font-semibold">
                    Conversation {c.number}
                    {c.status === "resolved" ? <span className="type-caption ml-2 font-normal text-muted">Resolved</span> : null}
                  </span>
                  <span className="type-caption mt-0.5 line-clamp-1 block text-muted">{c.last_message}</span>
                </span>
                {c.unread ? (
                  <span className="type-caption rounded-full bg-accent px-2 py-0.5 font-semibold text-accent-foreground" aria-label={`${c.unread} unread`}>
                    {c.unread}
                  </span>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-body-sm p-4 text-muted">No conversations yet.</p>
      )}
      <div className="mt-auto p-4">
        <button type="button" onClick={onNew} className="min-h-11 w-full rounded-md bg-primary px-4 text-[0.9375rem] font-semibold text-primary-foreground">
          New conversation
        </button>
      </div>
    </div>
  );
}

function NewConversation({
  guest,
  availability,
  onCreated,
  onBack,
}: {
  guest: boolean;
  availability: Availability | null;
  onCreated: (c: Conversation) => void;
  onBack: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const clientId = useRef(newClientId());

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError(null);
    setFields({});
    try {
      const res = await chat.start({
        body: String(f.get("body") ?? ""),
        client_message_id: clientId.current,
        ...(guest ? { name: String(f.get("name") ?? ""), email: String(f.get("email") ?? "") } : {}),
      });
      onCreated(res.conversation);
    } catch (err) {
      if (err instanceof ChatError) {
        setError(err.message);
        setFields(Object.fromEntries(err.fields.map((x) => [x.field, x.message])));
      } else setError("Your message could not be sent. Try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-1 flex-col gap-4 overflow-y-auto p-4" noValidate>
      <button type="button" onClick={onBack} className="type-caption self-start font-semibold underline underline-offset-2">
        Back
      </button>
      <AvailabilityLine a={availability} />
      {guest ? (
        <>
          <Field label="Your name" name="name" error={fields.name} autoComplete="name" maxLength={100} />
          <Field label="Email (so we can reply if you leave)" name="email" type="email" error={fields.email} autoComplete="email" maxLength={254} />
        </>
      ) : null}
      <div>
        <label htmlFor="mise-chat-body" className="type-body-sm font-semibold">
          Message
        </label>
        <textarea id="mise-chat-body" name="body" rows={5} maxLength={4000} required className={cx(field, "mt-2")} aria-invalid={fields.body ? true : undefined} aria-describedby={fields.body ? "mise-chat-body-error" : undefined} />
        {fields.body ? (
          <p id="mise-chat-body-error" className="type-caption mt-1 text-error">
            {fields.body}
          </p>
        ) : null}
      </div>
      {error && !Object.keys(fields).length ? (
        <p role="alert" className="type-body-sm border-l-4 border-error pl-3">
          {error}
        </p>
      ) : null}
      <button type="submit" disabled={busy} className="min-h-11 rounded-md bg-primary px-4 text-[0.9375rem] font-semibold text-primary-foreground disabled:opacity-60">
        {busy ? "Sending…" : "Send"}
      </button>
      {guest ? <p className="type-caption text-muted">We use your email only to reply to this conversation.</p> : null}
    </form>
  );
}

function Field({ label, name, error, ...rest }: { label: string; name: string; error?: string } & React.InputHTMLAttributes<HTMLInputElement>) {
  const id = `mise-chat-${name}`;
  return (
    <div>
      <label htmlFor={id} className="type-body-sm font-semibold">
        {label}
      </label>
      <input id={id} name={name} required className={cx(field, "mt-2")} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-error` : undefined} {...rest} />
      {error ? (
        <p id={`${id}-error`} className="type-caption mt-1 text-error">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function Help({ initial, onOpen }: { initial: string; onOpen: (slug: string) => void }) {
  const [q, setQ] = useState(initial);
  const [category, setCategory] = useState("");
  const [cats, setCats] = useState<HelpCategory[]>([]);
  const [results, setResults] = useState<HelpSummary[] | null>(null);

  useEffect(() => {
    void chat.helpCategories().then((r) => setCats(r.items), () => undefined);
  }, []);
  useEffect(() => {
    const t = setTimeout(() => {
      void chat.helpSearch(q.trim(), category).then((r) => setResults(r.items), () => setResults([]));
    }, 250);
    return () => clearTimeout(t);
  }, [q, category]);

  return (
    <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
      <div role="search">
        <label htmlFor="mise-help-q" className="sr-only">
          Search help articles
        </label>
        <input id="mise-help-q" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search help" className={field} maxLength={100} />
      </div>
      {cats.length ? (
        <div className="flex flex-wrap gap-2" aria-label="Categories">
          {cats.map((c) => (
            <button
              key={c.slug}
              type="button"
              aria-pressed={category === c.slug}
              onClick={() => setCategory(category === c.slug ? "" : c.slug)}
              className={cx("type-caption min-h-9 rounded-full border px-3 font-semibold", category === c.slug ? "border-primary bg-primary text-primary-foreground" : "border-border")}
            >
              {c.name}
            </button>
          ))}
        </div>
      ) : null}
      {results === null ? null : results.length ? (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {results.map((a) => (
            <li key={a.slug}>
              <button type="button" onClick={() => onOpen(a.slug)} className="w-full px-3 py-3 text-left hover:bg-surface">
                <span className="type-body-sm block font-semibold">{a.title}</span>
                {a.summary ? <span className="type-caption mt-0.5 block text-muted">{a.summary}</span> : null}
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-body-sm text-muted">{q || category ? "No articles match. Send us a message instead." : "Help articles will appear here once they are published."}</p>
      )}
    </div>
  );
}

function Article({ slug, onBack, onAsk }: { slug: string; onBack: () => void; onAsk: () => void }) {
  const [a, setA] = useState<HelpArticle | null>(null);
  const [missing, setMissing] = useState(false);
  useEffect(() => {
    void chat.helpArticle(slug).then(setA, () => setMissing(true));
  }, [slug]);
  return (
    <article className="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
      <button type="button" onClick={onBack} className="type-caption self-start font-semibold underline underline-offset-2">
        All articles
      </button>
      {missing ? <p className="type-body-sm">This article is no longer available.</p> : null}
      {a ? (
        <>
          <h3 className="type-h4">{a.title}</h3>
          {a.body.split(/\n{2,}/).map((p, i) => (
            <p key={i} className="type-body-sm whitespace-pre-line">
              {p}
            </p>
          ))}
          <button type="button" onClick={onAsk} className="type-body-sm mt-2 self-start font-semibold underline underline-offset-2">
            Still need help? Send us a message
          </button>
        </>
      ) : null}
    </article>
  );
}
