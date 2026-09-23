# ADR-008: Background jobs in PostgreSQL

Status: accepted

## Context

Email must not be sent inside an HTTP request, and it must not be lost. The brief suggests
SQS "where appropriate".

## Decision

A `jobs` table, polled by the worker with `SELECT … FOR UPDATE SKIP LOCKED`, with retries,
exponential backoff and a permanent-failure state (`internal/platform/jobs`).

## Why not SQS, yet

A job is inserted **in the same transaction** as the change that caused it. A signup that
commits always has its email job; a signup that rolls back never does. With SQS that
guarantee needs an outbox table anyway, and then a relay process to move rows to the queue.
At waitlist volume the outbox *is* the queue.

It also means local development, CI and production run the same mechanism with no
emulator.

## Details

- Payloads carry ids only. The worker loads current data at send time, which is also when
  it re-checks that the lead still wants email.
- A job stuck in `running` for ten minutes (a worker died) is returned to the queue.
  Handlers must therefore tolerate running twice; sending a duplicate email is the worst case.
- Failures after the last attempt raise a CloudWatch alarm.

## Revisit when

Polling shows up in database load, or work needs to fan out to many consumers. The
`jobs.Enqueue` and `Runner.Register` interface is small enough to put SQS behind.
