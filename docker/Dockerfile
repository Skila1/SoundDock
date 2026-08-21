# syntax=docker/dockerfile:1

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/sounddock/sounddock/internal/version.Version=dev" -o /out/sounddock ./cmd/sounddock

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg wget libopus0 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --home /data --uid 1000 sounddock \
    && mkdir -p /data/cache /data/backups /data/managed /libraries \
    && chown -R sounddock:sounddock /data /libraries
COPY --from=build /out/sounddock /usr/local/bin/sounddock
USER sounddock
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=8 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["sounddock"]
CMD ["all"]
