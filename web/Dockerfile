FROM node:24-alpine AS dependencies

WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM node:24-alpine AS build

WORKDIR /app
RUN corepack enable
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
ARG NEXT_PUBLIC_APP_NAME=CargoFlows
ARG NEXT_PUBLIC_PUBLIC_APP_URL=https://www.cargoflows.cc
ENV NEXT_PUBLIC_APP_NAME=${NEXT_PUBLIC_APP_NAME}
ENV NEXT_PUBLIC_PUBLIC_APP_URL=${NEXT_PUBLIC_PUBLIC_APP_URL}
RUN pnpm build

FROM node:24-alpine AS runtime

ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
WORKDIR /app
RUN addgroup -S -g 1001 cargoflows && adduser -S -u 1001 -G cargoflows cargoflows
COPY --from=build --chown=cargoflows:cargoflows /app/public ./public
COPY --from=build --chown=cargoflows:cargoflows /app/.next/standalone ./
COPY --from=build --chown=cargoflows:cargoflows /app/.next/static ./.next/static
USER cargoflows
EXPOSE 3000
CMD ["node", "server.js"]
