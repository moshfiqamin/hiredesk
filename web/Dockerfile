# HireDesk web frontend. Three stages: install dependencies, build, and a slim
# runtime that holds only the standalone server and its static assets.

# ---- deps: install exactly what package-lock.json pins ----------------------
FROM node:25-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# `npm ci` fails if the lockfile and package.json disagree, so the image always
# matches what was tested locally.
RUN npm ci

# ---- build ------------------------------------------------------------------
FROM node:25-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# NEXT_PUBLIC_ values are inlined into the browser bundle by `next build`, so
# they must be known now, not when the container starts. The browser calls the
# API from the user's machine, which is why this is a localhost URL and not the
# Compose service name.
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ---- runtime ----------------------------------------------------------------
FROM node:25-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
# Listen on every interface inside the container, or Docker's port mapping
# cannot reach the server.
ENV HOSTNAME=0.0.0.0

# The node image ships an unprivileged `node` user; running as it limits what a
# compromised process could touch.
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public
USER node

EXPOSE 3000
CMD ["node", "server.js"]
