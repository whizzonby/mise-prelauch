# ADR-009: Lead identity without passwords

Status: accepted

## Context

People on the waitlist need to confirm their email, see their own referral page, answer
profiling questions and unsubscribe. They do not have, and should not need, an account.

## Decision

Stateless tokens signed with HMAC-SHA256 (`internal/platform/token`). Each token binds a
lead id to one **purpose** and an expiry:

| Purpose | Lifetime | Delivered |
|---|---|---|
| `verify` | 7 days | In the verification email. |
| `profile` | 30 days | In the signup response (kept in the browser) and in email links to the welcome page. |
| `unsub` | Never expires | In every email. |

A token issued for one purpose is rejected for any other.

## Why stateless

No token table to write, clean up or index. Verification is naturally single-use, because
verifying an already-verified lead changes nothing.

## Consequences

- A single token cannot be revoked. Rotating `TOKEN_SIGNING_KEY` revokes all of them,
  which invalidates every outstanding email link.
- When customer accounts arrive, a lead becomes a customer by setting a password or
  signing in with a provider; the lead id carries over and these tokens stop being used
  for anything but unsubscribe.

## Email identity

Two addresses are the same person if they match after lower-casing and removing a `+tag`
(and dots, for Gmail). The canonical form is unique in the database. This closes the
cheapest referral abuse, one inbox signing up many times, at the cost of treating
`a+b@example.com` and `a@example.com` as one lead on the rare mail system where they differ.
