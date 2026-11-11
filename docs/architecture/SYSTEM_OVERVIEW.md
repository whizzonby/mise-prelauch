# System overview

Mise is a Trinidad and Tobago meal-kit subscription business. The platform is a
subscription-commerce and food-production operations system, built in phases:

| Phase | What | State |
|---|---|---|
| Pre-launch | Marketing site, waitlist, referrals, internal admin | Live code, preserved |
| **Sprint 1** | Foundation: identity, RBAC, sessions, MFA, customer app shell, API contract, lead-to-customer link | **This sprint** |
| Next | Customer platform: catalog, menus, subscriptions, orders, payments, delivery | Later sprints |
| Then | Operations: recipes and nutrition, suppliers and farmers, procurement, inventory, production, CRM, analytics, AI assistance, loyalty | Later sprints |
| Last | Flutter apps on the same API | After the web platform |

## Shape

- **One Go API** (`services/api`): a modular monolith with three binaries (`api`,
  `worker`, `misectl`), one PostgreSQL database, a PostgreSQL job queue for background
  work, SMTP for email. ADR-001.
- **Three Next.js apps** sharing one design system (`packages/ui`):
  `marketing` (public, static-first), `web` (customers), `admin` (staff). The two
  signed-in apps are backends-for-frontend: the browser never holds an API token. ADR-003, ADR-004.
- **One contract**: `docs/api/openapi.yaml`, enforced by tests in Go and TypeScript.
- **AWS** (designed, not yet applied) or a single server; staging and production fully separate. ADR-005.

## Where to read next

| Topic | Document |
|---|---|
| What existed and what changed | `EXISTING_SYSTEM_AUDIT.md` |
| People and systems around Mise | `SYSTEM_CONTEXT.md` |
| Running pieces and how they talk | `CONTAINER_ARCHITECTURE.md` |
| Modules, ownership, dependency rules | `MODULE_BOUNDARIES.md` |
| Key flows step by step | `DATA_FLOW.md` |
| Data model | `../database/CONCEPTUAL_ERD.md`, `../database/SPRINT_1_SCHEMA.md` |
| API rules and contract | `../api/API_STANDARDS.md`, `../api/openapi.yaml` |
| Security | `../security/` |
| Decisions | `../decisions/` |
| Design system | `design-system.md` |
| Deploying | `deployment.md`, `deploy-single-server.md` |
