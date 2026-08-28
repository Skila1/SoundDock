# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY web/ ./
RUN --mount=type=cache,target=/root/.npm \
    npm run build

FROM golang:1.24-bookworm AS build
WORKDIR /src
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache \
    && apt-get update && apt-get install -y --no-install-recommends git ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY openapi ./openapi
COPY web/embed.go ./web/embed.go
COPY VERSION ./
COPY --from=web /src/web/dist ./web/dist
ARG VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    ver="${VERSION}"; \
    if [ -z "${ver}" ] && [ -f VERSION ]; then ver="$(tr -d '[:space:]' < VERSION)"; fi; \
    if [ -z "${ver}" ]; then ver=0.0.1; fi; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/sounddock/sounddock/internal/version.Version=${ver}" -o /out/sounddock ./cmd/sounddock

FROM debian:bookworm-slim
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache \
    && apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates ffmpeg wget libopus0 python3 \
    && wget -qO /usr/local/bin/yt-dlp https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp \
    && useradd --system --home /data --uid 1000 sounddock \
    && mkdir -p /data/cache /data/backups /data/managed /data/managed/inbox /libraries \
    && chown -R sounddock:sounddock /data /libraries
COPY --from=build /out/sounddock /usr/local/bin/sounddock
USER sounddock
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=8 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["sounddock"]
CMD ["all"]
