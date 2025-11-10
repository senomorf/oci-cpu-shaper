SHELL := /bin/bash

GO ?= go

GOLANGCI_LINT_VERSION ?= v1.64.8
GOFUMPT_VERSION ?= 0.6.0

GO_BIN_PATH := $(shell $(GO) env GOBIN)
ifeq ($(GO_BIN_PATH),)
GO_BIN_PATH := $(shell $(GO) env GOPATH)/bin
endif

GOLANGCI_LINT_BIN ?= $(GO_BIN_PATH)/golangci-lint
GOLANGCI_LINT ?= $(GOLANGCI_LINT_BIN)
GOFUMPT_BIN ?= $(GO_BIN_PATH)/gofumpt
GOFUMPT ?= $(GOFUMPT_BIN)
PKGS := $(shell $(GO) list ./... 2>/dev/null)

.PHONY: fmt lint test build check tools ensure-golangci-lint ensure-gofumpt agents coverage

tools: ensure-golangci-lint ensure-gofumpt

ensure-golangci-lint:
	@set -euo pipefail; \
	BIN="$(GOLANGCI_LINT_BIN)"; \
	CURRENT_VERSION=""; \
	if [ -x "$$BIN" ]; then \
		CURRENT_VERSION="$$($$BIN version --format short 2>/dev/null || true)"; \
	fi; \
	if [ "$$CURRENT_VERSION" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)"; \
		$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi

fmt: ensure-gofumpt
	@set -euo pipefail; \
	FILES="$$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './.git/*' 2>/dev/null)"; \
	if [ -z "$$FILES" ]; then \
		echo "No Go files found; skipping format."; \
	else \
		gofmt -w $$FILES; \
		$(GOFUMPT) -w $$FILES; \
	fi

lint: ensure-golangci-lint
	$(GOLANGCI_LINT) run

ensure-gofumpt:
	@set -euo pipefail; \
	BIN="$(GOFUMPT_BIN)"; \
	CURRENT_VERSION=""; \
	if [ -x "$$BIN" ]; then \
		CURRENT_VERSION="$$($$BIN -version 2>/dev/null | awk '{print $$2}')"; \
	fi; \
	if [ "$$CURRENT_VERSION" != "$(GOFUMPT_VERSION)" ]; then \
		echo "Installing gofumpt v$(GOFUMPT_VERSION)"; \
		$(GO) install mvdan.cc/gofumpt@v$(GOFUMPT_VERSION); \
	fi

test:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping tests."; \
	else \
		$(GO) test -race $(PKGS); \
	fi

coverage:
	@set -euo pipefail; \
	if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping coverage."; \
	else \
		$(GO) test -race -covermode=atomic -coverprofile=coverage.out $(PKGS); \
		TOTAL=$$($(GO) tool cover -func=coverage.out | awk '/^total:/ {print $$NF}'); \
		if [ -n "$$TOTAL" ]; then \
			echo "Total coverage: $$TOTAL"; \
		else \
			echo "Coverage summary unavailable"; \
		fi; \
	fi

agents:
	$(GO) run ./cmd/agentscheck

check: lint test agents

build:
	$(GO) build ./...
