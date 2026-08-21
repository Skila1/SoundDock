.PHONY: build web test tidy docker

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
