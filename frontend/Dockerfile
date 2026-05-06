# Multi-stage Dockerfile for any frontend app (shell or any mf-*). Build with:
#   docker build -f frontend/Dockerfile --build-arg APP=shell -t url-shortener/shell:dev frontend
ARG APP
FROM node:22-bookworm-slim AS builder
ARG APP
ENV CI=1 PNPM_HOME=/pnpm PATH="/pnpm:$PATH"
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
WORKDIR /work

# Install deps with the lockfile committed at the workspace root.
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY tsconfig.base.json biome.json ./
COPY packages ./packages
COPY apps ./apps

RUN pnpm install --frozen-lockfile

WORKDIR /work/apps/${APP}
RUN pnpm run build

FROM nginx:1.27-alpine AS runtime
ARG APP
COPY --from=builder /work/apps/${APP}/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
