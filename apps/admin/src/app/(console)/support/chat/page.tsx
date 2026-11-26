import type { ChatDetail, ChatInboxItem } from "@mise/api-client";
import { buttonClasses, cx, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Composer, InboxLive } from "@/components/chat-inbox";
import { Labelled, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { assignConversation, escalateConversation, setConversationStatus, updateConversation } from "./actions";

export const metadata: Metadata = { title: "Support chat" };

const VIEWS = [
  ["open", "Waiting for us"],
  ["mine", "Mine"],
  ["unassigned", "Unassigned"],
  ["pending", "Waiting for customer"],
  ["resolved", "Resolved"],
  ["all", "All"],
] as const;

const CASE_CATEGORIES = ["subscription", "billing", "meal_selection", "delivery", "account_access", "product_feedback", "general"];
const PRIORITY_TONE = { low: "neutral", normal: "neutral", high: "accent", urgent: "error" } as const;

function href(view: string, q: string, c?: string) {
  const p = new URLSearchParams();
  if (view !== "open") p.set("view", view);
  if (q) p.set("q", q);
  if (c) p.set("c", c);
  const s = p.toString();
  return s ? `/support/chat?${s}` : "/support/chat";
}

const time = new Intl.DateTimeFormat("en-TT", { hour: "numeric", minute: "2-digit", timeZone: "America/Port_of_Spain" });
const day = new Intl.DateTimeFormat("en-TT", { day: "numeric", month: "short", timeZone: "America/Port_of_Spain" });
function when(iso: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  return Date.now() - d.getTime() < 20 * 3600_000 ? time.format(d) : day.format(d);
}

/**
 * The support inbox: conversations (left), the open conversation (middle)
 * and what we know about the person (right). Server-rendered; InboxLive
 * refreshes it when the live stream says something changed.
 */
export default async function SupportChatPage({ searchParams }: { searchParams: Promise<{ view?: string; q?: string; c?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "support_chat.read")) notFound();
  const sp = await searchParams;
  const view = VIEWS.some(([k]) => k === sp.view) ? sp.view! : "open";
  const q = (sp.q ?? "").slice(0, 100);
  const [inbox, detail, saved, agents] = await Promise.all([
    api.admin.chatInbox(token, { view, q }),
    sp.c ? api.admin.chatConversation(token, sp.c).catch(() => null) : Promise.resolve(null),
    api.admin.savedReplies(token),
    can(admin, "support_chat.assign") || can(admin, "support_chat.transfer") ? api.admin.chatAgents(token) : Promise.resolve({ items: [] }),
  ]);

  return (
    <>
      <div className="mb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-[2rem] leading-tight">Support chat</h1>
          <InboxLive key={detail?.id ?? "inbox"} currentId={detail?.id} lastSeq={detail?.last_seq} />
        </div>
        {can(admin, "support_chat.manage") ? (
          <Link href="/support/chat/settings" className={buttonClasses("secondary")}>
            Chat settings and saved replies
          </Link>
        ) : null}
      </div>

      {/* On wide screens the three columns fill the window and scroll on their own, so the composer stays in view. */}
      <div className="grid min-h-[70vh] gap-4 xl:h-[calc(100svh-9.5rem)] xl:min-h-0 xl:grid-cols-[20rem_minmax(0,1fr)_20rem]">
        {/* ---------------------------------------------------------------- list */}
        <section aria-label="Conversations" className="flex min-h-0 flex-col rounded-lg border border-border bg-surface">
          <nav aria-label="Inbox views" className="flex flex-wrap gap-1 border-b border-border p-2">
            {VIEWS.map(([k, label]) => (
              <Link key={k} href={href(k, q)} aria-current={view === k ? "page" : undefined}
                className={cx("type-caption rounded-md px-2.5 py-1.5 font-semibold", view === k ? "bg-primary text-primary-foreground" : "hover:bg-background")}>
                {label}
              </Link>
            ))}
          </nav>
          <form role="search" action="/support/chat" className="flex gap-2 border-b border-border p-2">
            {view !== "open" ? <input type="hidden" name="view" value={view} /> : null}
            <label htmlFor="inbox-q" className="sr-only">Search conversations</label>
            <input id="inbox-q" name="q" defaultValue={q} placeholder="Name, email, number or words" maxLength={100}
              className="type-body-sm min-h-10 flex-1 rounded-md border border-border bg-background px-3" />
          </form>
          {inbox.items.length ? (
            <ul className="min-h-0 flex-1 divide-y divide-border overflow-y-auto">
              {inbox.items.map((c) => (
                <ListItem key={c.id} c={c} active={c.id === detail?.id} link={href(view, q, c.id)} />
              ))}
            </ul>
          ) : (
            <p className="type-body-sm p-4 text-muted">{q ? "No conversations match." : "Nothing here right now."}</p>
          )}
        </section>

        {/* ---------------------------------------------------------------- conversation */}
        <section aria-label="Conversation" className="flex min-h-[60vh] flex-col overflow-hidden rounded-lg border border-border bg-background xl:min-h-0">
          {detail ? (
            <Thread d={detail} />
          ) : (
            <p className="type-body-sm m-auto p-8 text-muted">{sp.c ? "That conversation could not be found." : "Choose a conversation."}</p>
          )}
          {detail ? (
            <Composer
              key={detail.id}
              conversationId={detail.id}
              savedReplies={saved.items}
              canReply={can(admin, "support_chat.reply")}
              canNote={can(admin, "support_chat.internal_notes")}
            />
          ) : null}
        </section>

        {/* ---------------------------------------------------------------- context */}
        {detail ? (
          <aside aria-label="Customer and actions" className="grid content-start gap-4 xl:min-h-0 xl:overflow-y-auto xl:pr-1">
            <Context d={detail} admin={admin} agents={agents.items} />
          </aside>
        ) : null}
      </div>
    </>
  );
}

function ListItem({ c, active, link }: { c: ChatInboxItem; active: boolean; link: string }) {
  return (
    <li>
      <Link href={link} aria-current={active ? "true" : undefined} className={cx("block px-3 py-3", active ? "bg-background" : "hover:bg-background")}>
        <span className="flex items-baseline justify-between gap-2">
          <span className="type-body-sm truncate font-semibold">{c.owner_name || "Guest"}</span>
          <span className="type-caption shrink-0 text-muted">{when(c.last_message_at)}</span>
        </span>
        <span className="type-caption mt-0.5 line-clamp-2 block text-muted">
          {c.last_author === "agent" ? "You: " : ""}
          {c.last_message}
        </span>
        <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
          <span className="type-caption text-muted">#{c.number}</span>
          {c.owner_kind === "guest" ? <Tag tone="neutral">Guest</Tag> : null}
          {c.priority !== "normal" ? <Tag tone={PRIORITY_TONE[c.priority]}>{humanize(c.priority)}</Tag> : null}
          {!c.assignee_id && c.status !== "resolved" ? <Tag tone="accent">Unassigned</Tag> : null}
          {c.unread ? <span className="type-caption ml-auto rounded-full bg-primary px-2 font-semibold text-primary-foreground">{c.unread}</span> : null}
        </span>
      </Link>
    </li>
  );
}

function Thread({ d }: { d: ChatDetail }) {
  // Messages and internal notes in time order; notes are visibly different.
  type Row = { at: string; kind: "message"; m: ChatDetail["messages"][number] } | { at: string; kind: "note"; n: ChatDetail["notes"][number] };
  const rows: Row[] = [
    ...d.messages.map((m) => ({ at: m.created_at, kind: "message" as const, m })),
    ...d.notes.map((n) => ({ at: n.created_at, kind: "note" as const, n })),
  ].sort((a, b) => a.at.localeCompare(b.at));
  return (
    <>
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <h2 className="type-h4">
          #{d.number} · {d.owner_name || "Guest"}
        </h2>
        <Tag tone={d.status === "open" ? "accent" : d.status === "resolved" ? "success" : "neutral"}>
          {d.status === "open" ? "Waiting for us" : d.status === "pending" ? "Waiting for customer" : "Resolved"}
        </Tag>
        {d.case_id ? (
          <Link href={`/support/${d.case_id}`} className="type-caption font-semibold underline underline-offset-2">
            Support case
          </Link>
        ) : null}
        <span className="type-caption ml-auto text-muted">Started {formatDateTime(d.created_at)} · {humanize(d.source)}</span>
      </header>
      <ol className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4" aria-label="Messages">
        {rows.map((r) =>
          r.kind === "message" ? (
            <li key={`m${r.m.seq}`} className={cx("flex max-w-[80%] flex-col", r.m.author === "agent" ? "items-end self-end" : "items-start self-start")}>
              <span className={cx("type-body-sm whitespace-pre-wrap break-words rounded-2xl px-3.5 py-2.5",
                r.m.author === "agent" ? "rounded-br-sm bg-primary text-primary-foreground" : "rounded-bl-sm border border-border bg-surface")}>
                {r.m.body}
              </span>
              <span className="type-caption mt-1 text-muted">
                {r.m.author === "agent" ? r.m.author_name : d.owner_name || "Guest"} · {formatDateTime(r.m.created_at)}
                {r.m.author === "agent" && r.m.seq <= d.owner_read_seq ? " · Seen" : ""}
              </span>
            </li>
          ) : (
            <li key={`n${r.n.id}`} className="self-stretch rounded-md border-l-4 border-accent bg-accent/10 px-3 py-2">
              <span className="type-caption block font-semibold">Internal note · {r.n.author_name} · {formatDateTime(r.n.created_at)}</span>
              <span className="type-body-sm whitespace-pre-wrap">{r.n.body}</span>
            </li>
          ),
        )}
      </ol>
    </>
  );
}

function Context({ d, admin, agents }: { d: ChatDetail; admin: Parameters<typeof can>[0]; agents: { id: string; name: string }[] }) {
  return (
    <>
      <section className="rounded-lg border border-border bg-surface p-4">
        <h2 className="type-h4">{d.contact.kind === "guest" ? "Guest" : "Customer"}</h2>
        <p className="type-body-sm mt-2 font-semibold">{d.contact.name || "No name given"}</p>
        {d.contact.email ? (
          <p className="type-body-sm break-all text-muted">
            {d.contact.email}
            {d.contact.email_verified === true ? " (verified)" : ""}
          </p>
        ) : d.contact.kind === "customer" ? (
          <p className="type-caption text-muted">Email hidden: your role cannot see customer details.</p>
        ) : null}
        {d.contact.customer_id && can(admin, "customers.read") ? (
          <Link href={`/customers/${d.contact.customer_id}`} className="type-body-sm mt-2 inline-block font-semibold underline underline-offset-2">
            Open Customer 360
          </Link>
        ) : null}
        {d.contact.kind === "guest" ? <p className="type-caption mt-2 text-muted">Guests are not signed in: do not share account or order details.</p> : null}
        {d.feedback ? (
          <p className="type-body-sm mt-3">
            Rated {d.feedback.rating} of 5{d.feedback.comment ? `: "${d.feedback.comment}"` : ""}
          </p>
        ) : null}
      </section>

      {can(admin, "support_chat.resolve") ? (
        <section className="rounded-lg border border-border bg-surface p-4">
          <KForm action={setConversationStatus} submit={d.status === "resolved" ? "Reopen" : "Resolve"} pending="Saving…" label="Conversation status" variant={d.status === "resolved" ? "secondary" : "primary"} inline>
            <input type="hidden" name="conversation_id" value={d.id} />
            <input type="hidden" name="action" value={d.status === "resolved" ? "reopen" : "resolve"} />
          </KForm>
        </section>
      ) : null}

      {can(admin, "support_chat.assign") || can(admin, "support_chat.transfer") ? (
        <section className="rounded-lg border border-border bg-surface p-4">
          <h2 className="type-h4">Assignment</h2>
          <p className="type-body-sm mt-1 text-muted">
            {d.assignee_name ? `Assigned to ${d.assignee_name}` : "Unassigned"}
            {d.team ? ` · Team: ${d.team}` : ""}
          </p>
          <div className="mt-3">
            {/* Keyed by the current values: a live refresh must not leave stale defaults in the form. */}
            <KForm key={`assign-${d.assignee_id ?? ""}-${d.team ?? ""}`} action={assignConversation} submit={d.assignee_id && can(admin, "support_chat.transfer") ? "Transfer" : "Assign"} pending="Saving…" label="Assign the conversation" variant="secondary">
              <input type="hidden" name="conversation_id" value={d.id} />
              <input type="hidden" name="mode" value={d.assignee_id && can(admin, "support_chat.transfer") ? "transfer" : "assign"} />
              <Labelled label="Agent">
                <SelectInput name="assignee_id" defaultValue={d.assignee_id ?? ""}>
                  <option value="">Nobody</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Team (optional)">
                <TextInput name="team" maxLength={60} defaultValue={d.team ?? ""} />
              </Labelled>
              <Labelled label="Note for the history (optional)">
                <TextInput name="note" maxLength={1000} />
              </Labelled>
            </KForm>
          </div>
        </section>
      ) : null}

      {can(admin, "support_chat.reply") ? (
        <section className="rounded-lg border border-border bg-surface p-4">
          <h2 className="type-h4">Details</h2>
          <div className="mt-3">
            <KForm key={`details-${d.priority}-${d.tags.join(",")}-${d.order_id ?? ""}-${d.subscription_id ?? ""}`} action={updateConversation} submit="Save" pending="Saving…" label="Priority, tags and links" variant="secondary">
              <input type="hidden" name="conversation_id" value={d.id} />
              <Labelled label="Priority">
                <SelectInput name="priority" defaultValue={d.priority}>
                  {["low", "normal", "high", "urgent"].map((p) => (
                    <option key={p} value={p}>
                      {humanize(p)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Tags" hint="Separated by commas.">
                <TextInput name="tags" defaultValue={d.tags.join(", ")} maxLength={600} />
              </Labelled>
              {d.contact.kind === "customer" ? (
                <>
                  <Labelled label="Order id" hint="This customer's own order only.">
                    <TextInput name="order_id" defaultValue={d.order_id ?? ""} />
                  </Labelled>
                  <Labelled label="Subscription id">
                    <TextInput name="subscription_id" defaultValue={d.subscription_id ?? ""} />
                  </Labelled>
                </>
              ) : (
                <>
                  <input type="hidden" name="order_id" value="" />
                  <input type="hidden" name="subscription_id" value="" />
                </>
              )}
            </KForm>
          </div>
        </section>
      ) : null}

      {!d.case_id && can(admin, "cases.manage") && can(admin, "support_chat.reply") ? (
        <section className="rounded-lg border border-border bg-surface p-4">
          <h2 className="type-h4">Escalate to a support case</h2>
          <div className="mt-3">
            <KForm action={escalateConversation} submit="Open a case" pending="Opening…" label="Escalate to a support case" variant="secondary">
              <input type="hidden" name="conversation_id" value={d.id} />
              <Labelled label="Category">
                <SelectInput name="category" defaultValue="general">
                  {CASE_CATEGORIES.map((c) => (
                    <option key={c} value={c}>
                      {humanize(c)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Note (optional)">
                <TextArea name="note" rows={2} maxLength={2000} />
              </Labelled>
            </KForm>
          </div>
        </section>
      ) : null}

      <section className="rounded-lg border border-border bg-surface p-4">
        <h2 className="type-h4">History</h2>
        <ol className="type-caption mt-2 grid gap-1.5 text-muted">
          {d.events.map((e, i) => (
            <li key={i}>
              {formatDateTime(e.created_at)} · {humanize(e.type)}
              {e.actor_name ? ` by ${e.actor_name}` : ""}
            </li>
          ))}
        </ol>
      </section>
    </>
  );
}
