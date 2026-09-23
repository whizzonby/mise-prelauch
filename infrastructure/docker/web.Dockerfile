# Builds one of the Next.js apps (marketing or admin) as a standalone server.
# Build context is the repository root.
#
#   docker build -f infrastructure/docker/web.Dockerfile --build-arg APP=marketing \
#     --build-arg NEXT_PUBLIC_API_URL=https://api.example.com \
#     --build-arg NEXT_PUBLIC_SITE_URL=https://example.com .
#
# NEXT_PUBLIC_* values are compiled into the JavaScript, so the marketing image
# is built once per environment.
FROM node:24-alpine AS build
RUN npm install --global pnpm@11.9.0
WORKDIR /repo

# Manifests first, so dependency installation is cached between code changes.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/marketing/package.json apps/marketing/
COPY apps/admin/package.json apps/admin/
COPY e2e/package.json e2e/
COPY packages/config/package.json packages/config/
COPY packages/ui/package.json packages/ui/
COPY packages/validation/package.json packages/validation/
COPY packages/api-client/package.json packages/api-client/
COPY packages/analytics/package.json packages/analytics/
ARG APP
RUN pnpm install --frozen-lockfile --filter "@mise/${APP}..."

COPY packages packages
COPY apps/${APP} apps/${APP}

ARG NEXT_PUBLIC_API_URL
ARG NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL} \
    NEXT_PUBLIC_SITE_URL=${NEXT_PUBLIC_SITE_URL} \
    NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter "@mise/${APP}" build

FROM node:24-alpine
ARG APP
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    APP=${APP}
WORKDIR /app
COPY --from=build --chown=node:node /repo/apps/${APP}/.next/standalone ./
COPY --from=build --chown=node:node /repo/apps/${APP}/.next/static ./apps/${APP}/.next/static
COPY --from=build --chown=node:node /repo/apps/${APP}/public ./apps/${APP}/public
USER node
CMD ["sh", "-c", "exec node apps/${APP}/server.js"]
