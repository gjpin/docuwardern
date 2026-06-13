.PHONY: test build e2e

GOCACHE ?= /tmp/docuwarden-go-cache
COMPOSE ?= podman compose

test:
	GOCACHE=$(GOCACHE) go test ./...

build:
	GOCACHE=$(GOCACHE) go build -o bin/docuwarden ./cmd/docuwarden

e2e:
	$(COMPOSE) up -d --wait qdrant
	GOCACHE=$(GOCACHE) go test -tags=integration ./integration
