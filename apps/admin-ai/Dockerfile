FROM oven/bun:1.3.12-alpine AS build
WORKDIR /workspace

COPY package.json bun.lock turbo.json tsconfig.json ./
COPY apps/platform/package.json apps/platform/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN bun install --frozen-lockfile

COPY apps/platform apps/platform
COPY packages/ui packages/ui

ARG VITE_API_URL=/api
ARG VITE_CONTROL_PROXY_TARGET=http://server-ai:8081
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_CONTROL_PROXY_TARGET=$VITE_CONTROL_PROXY_TARGET
RUN bun run build

FROM nginx:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /workspace/apps/platform/dist /usr/share/nginx/html
