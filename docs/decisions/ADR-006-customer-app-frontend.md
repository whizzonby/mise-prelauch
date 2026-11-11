# ADR-006: Customer app forms, state and catalogue boundaries

Status: accepted (Sprint 2).

## Context

The Sprint 2 brief lists React Hook Form, TanStack Query, Motion and
shadcn/ui as the preferred frontend tools. The customer app (`apps/web`) is a
backend-for-frontend: the browser never holds an API token, and every change goes
through a Next.js server action that calls the Go API.

## Decisions

1. **Forms are server actions with `useActionState`, not React Hook Form.**
   - The forms work before JavaScript loads, and the session token never leaves the server.
   - The Go API is the one validator. Its field errors come back and are shown beside each
     field (`components/forms.tsx`), announced through `role="alert"` regions.
   - React Hook Form would add a second, client-side copy of the same rules.
2. **No TanStack Query.**
   - Pages are server components that fetch on the server.
   - The only immediate client update is saving a favourite. It uses React's
     `useOptimistic`, which returns to the real state if the server refuses or the
     connection fails.
3. **No Motion library.**
   - Interface motion is CSS using the design-system durations, applied only under
     `prefers-reduced-motion: no-preference` (`motion-safe:`).
   - The onboarding steps are the only animated forms.
4. **No shadcn/ui layer.** `packages/ui` already wraps Radix in the Mise styles; the
   customer app reuses it (Field, Choice, Select, Heading, Tag, Container, Wordmark).
5. **Catalogue versus recipes.**
   - A `meal` is the customer-facing presentation of a dish.
   - Production recipes (quantities, yields, costing, versions) belong to the future
     Chef ERP, which will reference `meals.id`. The catalogue deliberately does not model
     production.
6. **No uploads yet.**
   - Meal photographs come from the customer app's `/catalog/` folder or an allow-listed
     https host (`CATALOG_IMAGE_HOSTS`).
   - Object storage (S3) is a cost decision for a later sprint (ADR-005).

## Consequences

- A rule lives in Go only. The tests that pin the rules are the Go integration tests;
  Playwright checks that they reach the person correctly.
- If the app ever needs rich client-side editing (a meal planner, say), TanStack Query
  can be added then, behind the same server actions or a route handler.
