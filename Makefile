.PHONY: all build test coverage lint clean

BINARY_NAME=bin/anythingllm-gateway

all: lint test build

build:
	mkdir -p bin
	go build -v -ldflags="-s -w" -o $(BINARY_NAME) .

test:
	go test -v -race -cover ./internal/...

coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	go tool cover -func=coverage.out

lint:
	go vet ./...

clean:
	rm -rf bin/ coverage.out
