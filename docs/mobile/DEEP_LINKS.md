# Mise app: deep links

Flutter's built-in deep linking is **off** (Android `flutter_deeplinking_enabled=false`, iOS `FlutterDeepLinkingEnabled=false`). Every
incoming link arrives through `app_links` and is checked by `SafeLinks` (`lib/core/security/links.dart`) before routing.

## Accepted

| Link | App route |
|---|---|
| `mise://menu/meal/<slug>`, `https://<approved host>/meals/<slug>` | Meal detail |
| `mise://menu/week/<YYYY-MM-DD>` | Weekly menu |
| `mise://orders/<uuid>`, `…/delivery`, `…/cook/<uuid>` | Order, delivery, cooking |
| `mise://support/conversations/<uuid>` | Conversation |
| `mise://box`, `mise://subscription`, `mise://notifications`, `mise://account/...` | Tabs and pages |
| `mise://verify-email?token=…`, `mise://reset-password?token=…` | Email links (token 16–128 URL-safe characters) |
| `mise://checkout/return?checkout=<uuid>` | Payment return (handled by the payment session, then the server is asked) |

Anything else — unknown paths, malformed ids, `..`, other schemes or hosts, external URLs — goes to Home. Query strings are dropped
except tokens on token links. Notification payloads use the same allow-list. A valid link never grants access: signed-in routes still
redirect to sign-in (and back afterwards, via a validated `from`), and the API still checks ownership (`404` for anything not the
customer's).

## Schemes per build

`mise` (production), `mise-staging`, `mise-local` (dev). The Android main activity accepts only the hosts above; `checkout` goes to
the payment callback activity.

## Universal Links and App Links

Not configured: **Blocked** on an approved domain (go-live blocker 1). When a domain exists: publish
`/.well-known/assetlinks.json` (package `tt.mise.customer`, release signing certificate SHA-256) and
`/.well-known/apple-app-site-association` (team id + bundle id), add an `autoVerify` https intent filter and the Associated Domains
entitlement, and set `MISE_DEEP_LINK_HOST`. Email links can then open the app directly; until then they open the website.
