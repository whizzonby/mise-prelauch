# System context

Who and what the Mise platform deals with. Solid lines exist today; dashed lines are
planned integration boundaries, deliberately not implemented until credentials, API
documentation and business terms are confirmed.

```mermaid
flowchart TB
  subgraph People
    V[Visitor / lead]
    C[Customer]
    S[Mise staff<br/>marketing, operations, support, finance, admins]
    CH[Chefs and kitchen staff]
    N[Nutritionists]
    F[Farmers and suppliers]
  end

  P[Mise platform]

  V -->|waitlist, referrals| P
  C -->|account; later orders and subscriptions| P
  S -->|admin console| P
  CH -.->|recipes, production| P
  N -.->|validation| P
  F -.->|supply, purchase orders| P

  P -->|transactional email| E[Email: SES / SMTP]
  P -.->|charges, refunds, webhooks| PAY[Payment provider: undecided]
  P -.->|delivery jobs, status| D[Delivery partner: Food Drop or others]
  P -.->|production runs| CP[Co-packer: CARIRI or own kitchen]
  P -.->|campaigns| CRM[CRM / marketing platform]
  P -.->|SMS / WhatsApp| MSG[Messaging provider]
  P -.->|events| AN[Analytics service]
```

## Integration rules

- Every external party sits behind an interface owned by one module (`notifications`
  for email, later `payments`, `delivery`, `crm`). Domain code never names a provider.
- Food Drop, CARIRI and any payment provider are **configuration of an adapter**, not
  assumptions spread through the code. Switching delivery partner, or bringing
  production in-house, replaces an adapter.
- No adapter is written against a guessed API. Each needs the provider's documentation,
  sandbox credentials and agreed business rules first. None exist yet.
- Inbound webhooks (later) are verified (signature), stored raw, deduplicated by provider
  event id, and processed by a job.
