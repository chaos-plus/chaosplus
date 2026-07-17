FROM node:24.4.1-alpine AS builder
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_API_URL=
ARG VITE_DEFAULT_TENANT=platform
ENV VITE_API_URL=${VITE_API_URL}
ENV VITE_DEFAULT_TENANT=${VITE_DEFAULT_TENANT}
RUN npm run build

FROM nginx:1.29.0-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /src/dist /usr/share/nginx/html
RUN sed -i 's#pid[[:space:]].*;#pid /tmp/nginx.pid;#' /etc/nginx/nginx.conf && \
    chown -R 101:101 /var/cache/nginx /etc/nginx/conf.d /usr/share/nginx/html
USER 101:101
