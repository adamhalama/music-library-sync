.PHONY: build test install uninstall clean legacy-install legacy-uninstall

GO ?= go
PREFIX ?= $(shell brew --prefix 2>/dev/null || echo /usr/local)
BINDIR := $(PREFIX)/bin

NAME := udl
CMD := ./cmd/udl
OUT := $(CURDIR)/bin/$(NAME)
LEGACY_SCRIPT := $(abspath $(CURDIR)/bin/update-downloads)

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	@mkdir -p "$(CURDIR)/bin"
	@$(GO) build -ldflags "$(LDFLAGS)" -o "$(OUT)" $(CMD)
	@echo "Built -> $(OUT)"

test:
	@$(GO) test ./...

install: build
	@mkdir -p "$(BINDIR)"
	@cp "$(OUT)" "$(BINDIR)/$(NAME)"
	@chmod +x "$(BINDIR)/$(NAME)"
	@echo "Installed -> $(BINDIR)/$(NAME)"

uninstall:
	@rm -f "$(BINDIR)/$(NAME)"
	@echo "Removed $(BINDIR)/$(NAME)"

legacy-install:
	@mkdir -p "$(BINDIR)"
	@ln -sfn "$(LEGACY_SCRIPT)" "$(BINDIR)/update-downloads"
	@echo "Installed legacy script -> $(BINDIR)/update-downloads -> $(LEGACY_SCRIPT)"

legacy-uninstall:
	@rm -f "$(BINDIR)/update-downloads"
	@echo "Removed $(BINDIR)/update-downloads"

clean:
	@rm -f "$(OUT)"
