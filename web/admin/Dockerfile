FROM oven/bun:1.3.12-alpine AS build
WORKDIR /workspace

COPY package.json bun.lock turbo.json tsconfig.json ./
COPY apps/web/package.json apps/web/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN bun install --frozen-lockfile

COPY apps/web apps/web
COPY packages/ui packages/ui

ARG VITE_API_URL=/api
ARG VITE_DEFAULT_TENANT=platform
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_DEFAULT_TENANT=$VITE_DEFAULT_TENANT
RUN bun run build

FROM nginx:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /workspace/apps/web/dist /usr/share/nginx/html
EXPOSE 8080
