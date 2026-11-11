# ADR-005: One repository, pnpm workspaces

Status: accepted

## Decision

Everything lives in one repository: the two Next.js apps, the Go services, shared
TypeScript packages, infrastructure and documentation. JavaScript workspaces are managed
by pnpm. Shared packages are consumed as TypeScript source (`transpilePackages`), so there
is no package build step.

## Why

- A change that touches the API, its client and a screen is one pull request and one review.
- The design system is shared by importing it, not by publishing it.
- No task runner (Turborepo, Nx). `pnpm -r` runs a script everywhere; with eight small
  workspaces, caching would save seconds.

## Toolchain pins

TypeScript 6 and ESLint 9 rather than the newest majors: at the time of writing,
`typescript-eslint` does not support TypeScript 7 and `eslint-plugin-react` does not
support ESLint 10. Move up when they do.

## Consequences

CI runs everything on every change. When that gets slow, add path filters or a task runner.
