FROM golang:1.26.2-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chaosplus-server ./cmd/chaosplus-server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chaosplus-bootstrap ./cmd/chaosplus-bootstrap

FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates tzdata wget && addgroup -g 10001 chaosplus && adduser -D -H -u 10001 -G chaosplus chaosplus
COPY --from=builder /out/chaosplus-server /usr/local/bin/chaosplus-server
COPY --from=builder /out/chaosplus-bootstrap /usr/local/bin/chaosplus-bootstrap
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/chaosplus-server"]
