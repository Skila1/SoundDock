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
    mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/sounddock/sounddock/internal/version.Version=${ver}" -o /out/sounddock ./cmd/sounddock && \
    CGO_ENABLED=0 go run ./cmd/schemahead > /out/schema-head

FROM debian:bookworm-slim
ARG YT_DLP_VERSION=2026.08.19
ARG YT_DLP_SHA256=1fa6733c37ea6fb51c99ad8fe785e7b7e5f3246c9b980230329d4fb72ed8d4d6
# yt-dlp needs a JavaScript runtime to solve YouTube's player challenges;
# without one, downloads fail with "content is not available on this app".
ARG DENO_VERSION=2.9.7
ARG DENO_SHA256_AMD64=c6527f24f4b16031d3ae4fa9f658d5f11534c8d84ce7dc8502420280919c3490
ARG DENO_SHA256_ARM64=c832298b1ad4422481334855f6003e0f54145762c5a134f20a489511d2f65bbf
ARG TARGETARCH
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache \
    && apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates ffmpeg wget unzip libopus0 python3 postgresql-client \
    && wget -qO /tmp/yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/${YT_DLP_VERSION}/yt-dlp" \
    && echo "${YT_DLP_SHA256}  /tmp/yt-dlp" | sha256sum -c - \
    && install -m 0755 /tmp/yt-dlp /usr/local/bin/yt-dlp \
    && rm -f /tmp/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp \
    && case "${TARGETARCH:-amd64}" in \
         arm64) deno_arch=aarch64; deno_sha="${DENO_SHA256_ARM64}" ;; \
         *) deno_arch=x86_64; deno_sha="${DENO_SHA256_AMD64}" ;; \
       esac \
    && wget -qO /tmp/deno.zip "https://github.com/denoland/deno/releases/download/v${DENO_VERSION}/deno-${deno_arch}-unknown-linux-gnu.zip" \
    && echo "${deno_sha}  /tmp/deno.zip" | sha256sum -c - \
    && unzip -q /tmp/deno.zip -d /usr/local/bin \
    && chmod a+rx /usr/local/bin/deno \
    && rm -f /tmp/deno.zip \
    && useradd --system --home /data --uid 1000 sounddock \
    && mkdir -p /data/cache /data/backups /data/managed /data/managed/inbox /libraries \
    && chown -R sounddock:sounddock /data /libraries
COPY --from=build /out/sounddock /usr/local/bin/sounddock
COPY --from=build /out/schema-head /etc/sounddock/schema-head
ARG SCHEMA_HEAD=25
LABEL org.sounddock.schema-head=${SCHEMA_HEAD}
USER sounddock
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=8 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["sounddock"]
CMD ["all"]
