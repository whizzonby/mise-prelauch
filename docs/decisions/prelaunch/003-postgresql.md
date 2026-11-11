# ADR-003: PostgreSQL, and nothing else yet

Status: accepted

## Decision

PostgreSQL is the only data store. No Redis, no SQS, no search engine.

- **Background jobs** use a `jobs` table (ADR-008).
- **Rate limiting** is held in process memory, with an AWS WAF rate rule as the shared
  backstop. Redis would make limits exact across instances; at waitlist scale that
  precision does not justify another service to run and pay for.
- **Sessions** for admins are rows.

## Why PostgreSQL

Relational integrity matters here: a lead's status, one referral per invited person,
unique canonical emails and unique referral codes are enforced by constraints rather than
by hoping the application always remembers. JSONB covers the few loosely shaped fields.
It is also what the full platform (orders, inventory, payments) will need.

## Revisit when

- More than a couple of API instances make per-instance rate limits meaningfully loose.
- Job volume makes polling the table measurable in database load.
