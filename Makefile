BIN := linear-tui
SCHEMA_URL := https://raw.githubusercontent.com/linear/linear/master/packages/sdk/src/schema.graphql
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build run test race lint generate schema check-generated clean

all: generate build test

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(BIN) ./cmd/linear-tui

run:
	go run ./cmd/linear-tui

test:
	go test ./...

race:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run

generate:
	go generate ./...

# Refresh the vendored Linear schema, then regenerate the typed client.
schema:
	curl -fsSL $(SCHEMA_URL) -o internal/linear/schema.graphql
	$(MAKE) generate

check-generated: generate
	git diff --exit-code -- internal/linear/generated.go

clean:
	rm -rf bin dist
