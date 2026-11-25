# Mise Chat and the help centre

Sprint 9.1 (25 November 2026). Live conversations between customers (or guests) and the support team, an agent inbox in the admin
console, and a public help centre. Built inside the Go modular monolith; PostgreSQL is the only store; no paid messaging service.

Security and privacy: [MESSAGING_SECURITY.md](../security/MESSAGING_SECURITY.md). Mobile contracts: [FLUTTER_CONTRACTS.md](../api/FLUTTER_CONTRACTS.md).

## 1. Pieces

| Where | What |
|---|---|
| `services/api/internal/supportchat/` | `supportchat.go` (guests, conversations, messages, read positions, feedback, reply emails), `agent.go` (inbox, detail, reply, notes, assign/transfer, resolve/reopen, update, escalate, agents), `hub.go` (LISTEN/NOTIFY fan-out), `settings.go` (hours, availability, saved replies), `help.go` (help centre), `email.go`, `http.go` (routes and SSE streams). |
| `services/api/migrations/00016_support_chat.sql` | Tables, triggers, categories, permissions. |
| `services/api/internal/notifications/chat.go` | "You have a reply" email job. |
| `services/api/internal/analytics` | 10 `support.*` metrics and the "Support chat" dashboard. |
| `packages/chat` (`@mise/chat`) | `server.ts` (the relay each Next app mounts at `/api/chat/[...path]`), `launcher.tsx`, `panel.tsx`, `conversation.tsx`, `client.ts`. |
| `apps/marketing` | Guest chat (relay + launcher). |
| `apps/web` | Customer or guest chat, `/help`, `/help/[slug]`, `/support/chat/[id]`. |
| `apps/admin` | `/support/chat` (inbox), `/support/chat/settings`, `/help-centre`, `/api/chat/stream` (relay of the staff stream). |

The brief suggested files named `conversations.go`, `participants.go`, `presence.go`, `attachments.go`… The module is grouped by
audience instead (owner actions, agent actions, transport). Attachments are not built (§9); presence is limited to typing.

## 2. Data model

| Table | Purpose |
|---|---|
| `support_conversations` | Owner (`customer_id` **or** `guest_id`, enforced), number, source (site/app/help), status `open` (waiting for the team) / `pending` (waiting for the customer) / `resolved`, priority, tags, assignee, team, links to a support case, an order and a subscription (the customer's own), `last_seq`, first human response and resolution times. |
| `support_messages` | Immutable (trigger). `seq` is unique per conversation; `client_message_id` unique per conversation; author `customer`/`guest`/`agent`/`system`. |
| `support_read_positions` | Last seq read, per reader (customer, guest, or each agent). Read receipts come only from here. |
| `support_conversation_events` | Append-only history: created, assigned, unassigned, transferred, resolved, reopened, escalated, linked, priority, tags, guest_linked. |
| `support_internal_notes` | Staff notes, immutable, in their own table (never joined into customer responses). |
| `support_saved_replies` | Titles and texts agents can insert; archived ones are hidden from agents. |
| `support_feedback` | One rating (1–5) and optional comment per conversation. |
| `support_guest_sessions` | Guest name, email, **sha256 of the token**, expiry (30 days, sliding), link to the customer it joined. |
| `support_chat_settings`, `support_holidays` | On/off, time zone, weekly hours, closed days, messages, expectation text, email cool-down. All empty by default. |
| `support_chat_notifications` | One row per conversation per cool-down window (dedupe key) with its outcome. |
| `help_categories`, `help_articles`, `help_article_events` | Five structural categories; articles with a status workflow and full-text search; append-only history. |

## 3. Sending and receiving

**Sending (HTTP).** `POST …/conversations/{id}/messages` with `body` and a `client_message_id` chosen by the sender. In one
transaction the conversation row is locked, a repeat of the same `client_message_id` returns the stored message, otherwise the next
`seq` is assigned, the message is stored, and a NOTIFY is issued. The API answers only after the commit: **delivered means stored**.
A resolved conversation reopens when the customer writes; an agent reply sets it to waiting-for-customer and assigns the agent if
nobody had it.

**Live delivery (Server-Sent Events).**
- `GET /api/v1/me/chat/conversations/{id}/stream` and `/api/v1/chat/guest/conversations/{id}/stream` (owner): events `message`
  (SSE `id` = seq), `conversation` (status and the team's read position), `typing` (the team is typing), `end`. The client resumes with
  `Last-Event-ID` (or `?after=`); the server first sends every message after that seq, so nothing is missed across reconnects.
- `GET /api/v1/admin/chat/stream` (staff): events carry only the conversation id, seq, kind and actor; the inbox then fetches through
  the ordinary endpoints with the agent's own permissions.
- Each wake-up re-reads the database with the caller's authorisation. A heartbeat every 15 s also re-reads, so a missed signal only
  delays delivery. The session is re-checked every 60 s (stream ends with `end` if it is no longer valid); streams last at most 25
  minutes and the client reconnects.

**Why SSE and not WebSockets.** The customer and admin apps are BFFs: the browser never calls the API and never holds an API token.
Next.js route handlers can relay an SSE response but not a WebSocket upgrade; SSE also passes through Caddy and the ALB unchanged.
Messages are sent with ordinary POSTs, which are idempotent and work without a live connection, so SSE (server → client) plus HTTP
(client → server) covers the bidirectional need. If SSE is unavailable the widget polls every 15 s and the inbox refreshes every 20 s.

**Across API instances.** Every change is announced with PostgreSQL `NOTIFY` on channel `mise_support_chat` (payload: ids only,
delivered on commit). Each API instance holds one `LISTEN` connection, opened when its first stream starts and closed when its last
ends (outside the pool), and fans signals out to its streams. No Redis is deployed, and none is needed.

**Typing** is a NOTIFY only, throttled to one per sender every 3 s, never stored. **Read receipts** come from stored read positions.

## 4. Guests

A guest gives a name, an email and a first message. The API creates a session with a random token (`mgt_` + 32 bytes), stores only its
hash, and returns the token once; the app's relay puts it in an httpOnly cookie (`mise_chat_guest`, path `/api/chat`, SameSite=Lax,
Secure outside local) and strips it from the response. Starts are limited per email (5 an hour) and per client address. A guest sees
only their own conversations and never account data.

**Joining an account.** When a signed-in customer's request reaches the customer app's relay with a guest cookie, the relay asks the
API to link: allowed only if the customer's email is **verified** and equal to the guest's, and the caller presents the guest token
(same browser). Conversations move to the customer, the guest session is closed, and the cookie is cleared. Never by email match alone.
Guests who wrote on the marketing site and sign in on the customer app (another origin) keep the guest history on the marketing site.

## 5. The widget (`@mise/chat`)

- A launcher (round icon on phones; "Chat with us" pill from tablets up, with a light ring so it shows on dark sections) loads the panel
  code only when the page is idle or on first press. `?chat=open` opens it (reply emails and the customer app's `/support/chat`).
- Panel: **Home** (availability, continue the open conversation, start one, search help, "sign in" link on the marketing site),
  **Messages** (list with unread counts), **Help** (categories, search, articles). Full screen on phones, a card from tablets up.
- Conversation: optimistic send with a sender id, "Sending…", "Not sent. Try again" (resends the same id), "Delivered", "Seen"; typing;
  feedback (1–5) once resolved; Enter sends, Shift+Enter adds a line.
- Accessibility: dialog labelled, focus moves to the panel title on open and back to the launcher on close, Escape closes, live region
  for new messages, 44 px targets.
- Placement: hidden on `/subscribe`, `/test-checkout` and `/support/chat` (payment steps and the full-page conversation); above the
  phone tab bar when signed in.

## 6. Agent inbox (admin)

Three columns: the list (views: waiting for us — oldest first —, mine, unassigned, waiting for customer, resolved, all; search by name,
email, number or message text), the conversation (messages and internal notes in time order, notes visibly different; composer with
reply/note switch, saved replies, typing), and the context (contact; email only with `customers.read`; link to Customer 360;
resolve/reopen; assign/transfer; priority, tags, links to the customer's own order or subscription; escalate to a support case;
history). It stays live through the staff stream and records the agent's read position. Settings: availability, hours, closed days,
messages, email cool-down, saved replies. Unassigned conversations raise a staff alert, cleared on reply or assignment.

## 7. Help centre

Statuses: draft → in review → published → archived; "revise" returns a published or archived article to draft as a new version (and
takes it off the site). Writers (`help_centre.edit`) create and submit; publishers (`help_centre.publish`) return, publish and archive.
Bodies are plain text (no HTML; angle brackets refused). Public pages: `/help`, `/help/[slug]` in the customer app; search in the
widget. Only published articles are public. Five categories are seeded; **no article text was written** (it needs approved wording).

## 8. Offline, notifications, analytics

- **Offline.** With no hours configured the widget claims nothing ("We'll reply as soon as we can."). With hours, it shows online or
  offline from the configured time zone, hours and closed days, and the configured messages. Messages are accepted at any time.
- **Reply emails.** One per conversation per cool-down window (default 30 minutes), sent two minutes after the reply and skipped if the
  customer has read it; no message text, no token; customers are linked to `/support/chat/{id}`, guests to the site with `?chat=open`.
- **Analytics** (`support_chat.analytics`): new, resolved, waiting for the team, unassigned, backlog, first human response time,
  resolution time, satisfaction responses and the counts of low (1–2) and high (4–5) ratings. Counts only; no satisfaction score.

## 9. Not built

| Item | Why |
|---|---|
| Attachments | No approved private object storage (owner decision, 25 Nov 2026). Not created: no table, no endpoint, no upload control. |
| Push notifications | No mobile app yet; event names are reserved in `FLUTTER_CONTRACTS.md`. |
| Retention purge | Retention periods are a business/legal decision (see MESSAGING_SECURITY.md §4). |
| Business hours, expectation text, saved replies, help articles | Business content; the tools exist, the content is empty on purpose. |
