# Messaging security and privacy

Sprint 9.1. How Mise Chat and the help centre are protected. Architecture: [SUPPORT_CHAT.md](../architecture/SUPPORT_CHAT.md).
Tests referenced are in `services/api/internal/server/support_chat_test.go` unless stated; the browser walkthrough is
`e2e/tools/chat-walkthrough.mjs`.

## 1. Threats and controls (brief §32)

| Threat | Control | Evidence |
|---|---|---|
| Unauthorised conversation access / IDOR | Every owner query filters by the caller's own customer id or guest id; an id that is not theirs is `404` (same as one that does not exist). Staff endpoints check `support_chat.*` permissions on every call. | `TestCustomerChatIsolationAndGuestLinking` (another customer: 404 on read, messages, send, stream); `TestGuestChatAndAgentReply` (another guest: 404); RBAC matrix (26 roles × 386 admin routes). |
| Cross-account leakage | No customer endpoint joins another owner's rows; guest conversations never show account data; links to orders and subscriptions must belong to the conversation's customer, and a guest conversation cannot be linked to one. | Tests above; link refusal in `TestGuestChatAndAgentReply`. |
| Internal notes reaching customers | Notes live in their own table, read only by staff endpoints that check `support_chat.internal_notes`; the owner stream reads messages only, so a note signal cannot carry content. | Note never in guest responses (raw JSON checked) and never on the guest stream (`TestChatLiveDeliveryOverSSE`); walkthrough "note visible to guest: false". |
| Real-time subscription abuse ("WebSocket origin attacks") | Streams are SSE behind the apps' own relays. A stream is authorised like a request (owner check or `support_chat.read`) before the first byte; events carry ids only and every delivery re-reads with the caller's rights. Browsers cannot read a cross-origin stream (no CORS headers are sent). | Unauthorised streams refused (`TestChatLiveDeliveryOverSSE`, `TestCustomerChatIsolationAndGuestLinking`). |
| Session hijacking / long-lived connections | Customer and staff tokens stay on the app servers (httpOnly cookies); the API never sees browser cookies. Streams re-check the session every 60 s (and staff permission and MFA) and end after at most 25 minutes. | Code: `ownerStream`, `agentStream`. The 60 s re-check is not covered by an automated test (interval fixed at start-up). |
| CSRF on the relays | The relay refuses any non-GET request that is not same-origin (`Sec-Fetch-Site`, else `Origin`). | Cross-origin POST → 403 (checked against the running apps). |
| Guest identity spoofing | The guest token is random (256 bits), stored only as sha256, sent only from the app server, held in an httpOnly cookie scoped to `/api/chat`; script on the page cannot read it. A guest's history joins an account only with the token **and** a verified, matching email. | Hash checked in `TestGuestChatAndAgentReply`; linking rules in `TestCustomerChatIsolationAndGuestLinking`; walkthrough "token visible to script: false". |
| Spam | Guest starts: 5 per email per hour, plus the per-address sign-up limit. Chat can be switched off in settings. | `TestGuestChatAndAgentReply` (6th start → 429); `TestChatSettingsAndAvailability` (off → 503). |
| Message flooding | 20 messages a minute per customer or guest (bursts of 10), plus the API-wide per-address limit; typing signals throttled to one per 3 s. | `TestChatMessageFloodingIsLimited`. |
| Stored XSS | Message bodies, notes and help articles are plain text. React escapes everything rendered; no HTML is rendered anywhere (`whitespace-pre-wrap` keeps line breaks). Help articles refuse `<` and `>` outright. | `TestHelpCentreWorkflow` (HTML refused); code review: no `dangerouslySetInnerHTML` in `packages/chat`, the inbox or the help pages. |
| Malicious attachments | Attachments are not built (no storage approved): no table, endpoint or upload control. | Schema; UI. |
| Duplicate or replayed sends | `client_message_id` unique per conversation; a resend returns the stored message. | Idempotency in `TestGuestChatAndAgentReply` and the retried start in `TestCustomerChatIsolationAndGuestLinking`. |
| Tampering with history | Messages, internal notes, conversation events and help-article events are immutable (triggers). | `UPDATE support_messages` refused in `TestGuestChatAndAgentReply`. |
| Message bodies in logs | Logs carry ids, routes, statuses and error codes only. Access logs record paths (never bodies); `supportchat` logs no content; NOTIFY payloads carry ids only. | Code review. |
| External AI services | Conversation content is never sent to any external service, AI or otherwise. The only outbound messages are the reply emails, which contain no message text. | Code review (`notifications/chat.go`). |
| Reply email leaking content or tokens | Email says only that there is a reply and links to it; customers sign in to read; guests are pointed to the site in the same browser. | Email content checked in `TestGuestChatAndAgentReply`. |

## 2. Permissions

| Permission | Granted to | Allows |
|---|---|---|
| `support_chat.read` | super_admin, admin, operations, support | Inbox, conversations, stream, agents, saved replies (read). |
| `support_chat.reply` | same | Reply, typing, priority/tags/links. |
| `support_chat.assign` / `.transfer` / `.resolve` | same | Assign, transfer, resolve/reopen. |
| `support_chat.internal_notes` | same | Read and write internal notes. |
| `support_chat.manage` | super_admin, admin | Settings, hours, saved replies. |
| `support_chat.analytics` | super_admin, admin, operations | Support dashboard. |
| `help_centre.edit` | super_admin, admin, support, marketing | Write and submit articles; revise. |
| `help_centre.publish` | super_admin, admin | Return, publish, archive. |
| (escalate) | needs `cases.manage` and `support_chat.reply` | Open a support case from a conversation. |
| Customer email in the inbox | needs `customers.read` | Otherwise hidden (guests' emails are shown: they gave them to chat). |

## 3. Content shown to whom

| Data | Customer / guest | Agent |
|---|---|---|
| Messages | Their own conversations | All (with `support_chat.read`) |
| Agent identity | Agent's first name or "Mise team" | Full name |
| Internal notes, events, tags, priority, assignment | Never | Yes |
| The team's read position ("Seen") | Yes | — |
| The customer's read position | — | Yes |

## 4. Retention and deletion

What exists today:

- Messages, notes and events are kept for the life of the conversation and cannot be edited.
- Deleting a customer record deletes their conversations, messages, notes, read positions and feedback (foreign-key cascade).
  **There is no customer account deletion feature in the platform yet** (only waitlist leads can be erased); deletion requests are
  handled by an administrator with database access until one exists.
- A guest session expires after 30 days without use (the token stops working); its conversations remain.
- Reply-email records keep the conversation id, seq and outcome only.

**Decision needed (business and legal).** Proposed policy, not yet implemented:

| Data | Proposed retention |
|---|---|
| Guest conversations (never joined to an account) | Delete 12 months after the last message. |
| Customer conversations | Keep while the account exists; delete with the account; or anonymise after 24 months of inactivity. |
| Internal notes | Same as the conversation. |
| Help article history | Keep (no personal data beyond staff names). |
| Reply-email records | Delete after 90 days. |

When decided, a scheduled job will purge by these rules (a small addition to the worker), and the privacy notice must say the same.

## 5. Residual risks

| Risk | Rating | Note |
|---|---|---|
| Retention not enforced | Medium (privacy) | Needs the decision above. |
| Message-rate and typing limits are per API instance | Low | Multiplied by instance count; the WAF rate rule is the global backstop. |
| Guest history on the marketing site does not follow a customer who signs in on the customer app | Low (product) | Different origins; documented. |
| Stream session re-check untested by automation | Low | Behaviour is simple and reviewed; add a test with a configurable interval. |
