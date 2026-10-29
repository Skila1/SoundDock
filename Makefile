.PHONY: build web test tidy docker bump

VERSION ?= $(shell tr -d '[:space:]' < VERSION)

build: web
	go build -ldflags="-X github.com/sounddock/sounddock/internal/version.Version=$(VERSION)" -o bin/sounddock ./cmd/sounddock

web:
	cd web && npm install && npm run build

test:
	go test ./...

tidy:
	go mod tidy

docker:
	docker build -f docker/Dockerfile -t sounddock:local .

run-api:
	go run ./cmd/sounddock all

# Next release version: patch and minor count 0-9 (0.1.9 -> 0.2.0).
bump:
	sh scripts/bump-version.sh
