.PHONY: build test fmt lint ci

BIN_DIR := bin

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/safe-gmail ./cmd/safe-gmail
	go build -o $(BIN_DIR)/safe-gmaild ./cmd/safe-gmaild

test:
	go test ./...

fmt:
	gofmt -w cmd internal

lint:
	go test ./...

ci: fmt test

