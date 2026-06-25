# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS web-builder

WORKDIR /src/web
COPY web/package.json ./
COPY web/index.html ./index.html
COPY web/favicon.ico web/favicon-16x16.png web/favicon-32x32.png web/apple-touch-icon.png web/icon-48.png web/icon-192.png web/icon-512.png ./
COPY web/scripts ./scripts
COPY web/src ./src
RUN npm run build

FROM golang:1.25.9-bookworm AS go-builder

WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY --from=web-builder /src/web/dist ./internal/admin/dist

RUN go build -tags with_utls -trimpath -ldflags="-s -w" -o /out/chijie ./cmd/gateway/

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/chijie
COPY --from=go-builder /out/chijie /opt/chijie/chijie

EXPOSE 8080 9090
VOLUME ["/config"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/opt/chijie/chijie"]
CMD ["-config", "/config"]
