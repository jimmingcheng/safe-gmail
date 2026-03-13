.PHONY: build install test fmt lint ci

GO ?= go
INSTALL ?= install
BIN_DIR := bin
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/safe-gmail ./cmd/safe-gmail
	$(GO) build -o $(BIN_DIR)/safe-gmaild ./cmd/safe-gmaild

install: build
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) -m 0755 $(BIN_DIR)/safe-gmail $(DESTDIR)$(BINDIR)/safe-gmail
	$(INSTALL) -m 0755 $(BIN_DIR)/safe-gmaild $(DESTDIR)$(BINDIR)/safe-gmaild

test:
	$(GO) test ./...

fmt:
	gofmt -w cmd internal

lint:
	$(GO) test ./...

ci: fmt test
