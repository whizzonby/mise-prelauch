# Mise app: offline behaviour

## What is kept

| Area | Content | Cleared |
|---|---|---|
| `public` | First page of meals, meal details seen, categories, reference lists, weekly menus | Never automatically (public data) |
| `account` | Orders and order details seen, recipes opened, conversation history | At sign-out and when the session ends |

JSON files in the app's private support directory, excluded from backups (Android `allowBackup=false` and data-extraction rules; iOS
app-support data is not shared with other apps). When the network fails, a screen shows its saved copy with a banner: "You're seeing a
saved copy from <time>. Pull down to refresh." (shown when the copy is more than two minutes old; one day for recipes).

## What is never done offline

Payments, subscription changes, meal selections, address changes and order creation are only shown as done when the server answers.
Offline, they fail with "you seem to be offline" and keep what the customer entered (for example the meal draft stays, marked
"Changes not saved yet"). Chat messages show *Not sent · Retry*; a retry reuses the same message id, so nothing is sent twice.

## Tokens offline

An expired access token is kept with its refresh token while offline; nothing signs the customer out because the network is down.
A refused refresh (online) signs out and clears the account cache.

## Tested

`test/widget/screens_test.dart`: offline menu shows the labelled saved copy; sign-out removes the account cache. Unit tests: offline
refresh keeps the session; chat retry dedup; selection draft kept on failure.
