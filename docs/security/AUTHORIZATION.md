# Authorization

Role-based access control held in the database and enforced by the API.
Code: `services/api/internal/identity/principal.go`, `http.go`. Schema: migration `00004_identity.sql`.

## Model

```mermaid
erDiagram
  users ||--o{ user_roles : holds
  roles ||--o{ user_roles : "assigned as"
  roles ||--o{ role_permissions : grants
  permissions ||--o{ role_permissions : "granted by"
```

- A **permission** is `<resource>.<action>` (`leads.export`). Handlers check permissions,
  never role names, so adding a role is data, not code.
- A **role** bundles permissions.
- A **role assignment** may carry a **scope** (`scope_type`, `scope_id`), for example a
  farmer for one supplier organisation, or kitchen staff for one kitchen. Global
  permission checks use **unscoped** assignments only; a scoped assignment grants nothing
  globally (`TestScopedRolesDoNotGrantGlobalAccess`). Scoped checks ("may this farmer see
  this supplier's purchase orders?") arrive with the modules that own those records,
  through a `CanIn(perm, scopeType, scopeID)` check in identity.
- Access is re-read on every request, so changes apply immediately.

## Permissions (Sprint 1)

Only permissions that guard something that exists:

| Permission | Guards |
|---|---|
| `admin.access` | Signing in to the admin console. Holders must use MFA outside local. |
| `analytics.view` | `GET /admin/stats/overview` |
| `leads.read` | `GET /admin/leads`, `GET /admin/leads/{id}` |
| `leads.update` | `PATCH /admin/leads/{id}` |
| `leads.export` | `GET /admin/leads/export.csv` |
| `leads.erase` | `DELETE /admin/leads/{id}` |
| `users.read` | Reserved for the staff screen (Sprint 2) |
| `users.manage` | Reserved for the staff screen; today `misectl` |

Renamed from pre-launch: `stats:read` → `analytics.view`, `leads:write` → `leads.update`,
`leads:*` → `leads.*`. A test fails if a permission the code checks is missing from the
database (`TestPermissionsInCodeExistInTheDatabase`, and `misectl doctor` in CI).

## Roles

| Role | Permissions today | Notes |
|---|---|---|
| `super_admin` | all eight | |
| `admin` | all except `users.manage` | |
| `marketing` | admin.access, analytics.view, leads.read, leads.export | |
| `operations` | admin.access, analytics.view, leads.read | |
| `support` | admin.access, leads.read, leads.update | |
| `finance`, `chef`, `kitchen_staff`, `nutritionist`, `farmer` | none yet | Created so assignments can be planned; granted permissions with their modules. `farmer` and `kitchen_staff` will be scoped. |
| `customer` | none | Customer endpoints are authorised by audience and ownership (a customer only ever acts on their own records). |

The pre-launch role map is carried over exactly.

## Enforcement

- **The API is the authority.** `identity.Service.RequireAdmin(permission, handler)` wraps
  every admin route; `RequireCustomer` wraps customer routes. The Next.js apps also hide
  what a role cannot use, as a convenience only.
- 401 `unauthorized`: no valid session for this audience. 403 `mfa_required`: second
  factor still due. 403 `forbidden`: signed in without the permission.
- Ownership: customer endpoints take no user id from the request; they act on the
  session's user (`DELETE /me/sessions/{id}` only matches the caller's sessions, tested).

## Managing access (until the staff screen exists)

```sh
MISE_ADMIN_PASSWORD='…' misectl admin create -email a@b.c -name "Ada" -role marketing
misectl admin grant  -email a@b.c -role support
misectl admin revoke -email a@b.c -role marketing
misectl admin disable -email a@b.c
```

Each is audited (`via: misectl`).
