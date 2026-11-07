VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= opensearch-mcp

.PHONY: build test integration lint image up down

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/opensearch-mcp ./cmd/opensearch-mcp

test:
	go test -race ./...

# Requires a running cluster: make up
integration:
	OPENSEARCH_URL=$${OPENSEARCH_URL:-http://localhost:9200} go test -tags integration -count=1 ./test/integration/

lint:
	go vet ./...
	go vet -tags integration ./...

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

up:
	docker compose up -d --build

down:
	docker compose down -v
