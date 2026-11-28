# Mise app: live support and help centre

The app is another client of Mise Chat (Sprint 9.1): the same API, the same admin inbox, the same people. There is no chatbot and no
separate support backend. Contract: `docs/api/FLUTTER_CONTRACTS.md`.

## Messaging

- Signed-in customers use `/me/chat/...` with their app token. Guests see the help centre and are asked to sign in to message
  (guest chat stays on the website for now).
- **Start:** `POST /me/chat/conversations` with a `client_message_id` created before the first attempt, `source: "app"`. A retry with
  the same id returns the same conversation.
- **Send:** the message appears at once as *Sending…*, becomes a normal message when the server stores it, or *Not sent · Retry*.
  Retry reuses the same `client_message_id`, so the server never stores it twice.
- **Deduplication** (`MessageLog`, unit-tested): stored messages are unique by `seq`; the customer's pending copy is replaced by the
  stored message with the same client id whether it arrives in the POST answer or on the stream first.
- **Read receipts and typing:** `POST …/read` when messages are on screen; "Seen" under the customer's last message once the team
  has read it (`team_read_seq`); "The Mise team is typing…" for 4 s after a `typing` event; typing sent at most every 3 s.
- **Resolution and feedback:** a resolved conversation offers a 1–5 rating; writing again reopens it.

## Real time

Server-Sent Events, not WebSocket (that is what the server provides). `SseConnection` (`core/realtime/sse.dart`):

- `GET …/stream` with the bearer token and `Last-Event-ID: <last seq>`; the server replays anything missed.
- Reconnects with backoff 1 s, 2 s, 5 s, then every 15 s; stops on 401/404 or an `end` event.
- Closed when the app goes to the background (a suspended app cannot keep it); on resume the app fetches
  `messages?after=<last seq>` (paging by 200) and reopens it.
- While disconnected, the screen says "Reconnecting…"; sending never depends on the stream.
- Replies that arrive while the app is closed reach the customer by push (when configured) and in the notification centre
  (`chat.reply`), which link straight to the conversation.

## Offline

The conversation is saved to the account cache and shown when offline. Messages are not queued silently while offline: an unsent
message stays visibly *Not sent* with Retry.

## Attachments

Not available: secure attachment storage is not approved (Sprint 9.1 left attachments disabled behind a storage interface). The app
asks for no camera or photo permission.

## Help centre

`/help/categories`, `/help/articles?q=&category=`, `/help/articles/{slug}`: published articles only, the same content as the
website. Bodies are plain text split into paragraphs and are never rendered as HTML. Every article ends with "Message the Mise team".
