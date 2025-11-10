SHELL := /bin/bash

GO ?= go

GOLANGCI_LINT_VERSION ?= v1.64.8

GO_BIN_PATH := $(shell $(GO) env GOBIN)
ifeq ($(GO_BIN_PATH),)
GO_BIN_PATH := $(shell $(GO) env GOPATH)/bin
endif

GOLANGCI_LINT_BIN ?= $(GO_BIN_PATH)/golangci-lint
GOLANGCI_LINT ?= $(GOLANGCI_LINT_BIN)
PKGS := $(shell $(GO) list ./... 2>/dev/null)

.PHONY: fmt lint test build check tools ensure-golangci-lint

tools: ensure-golangci-lint

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

fmt:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping format."; \
	else \
		$(GO) fmt $(PKGS); \
	fi

lint: ensure-golangci-lint
	$(GOLANGCI_LINT) run

test:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping tests."; \
	else \
		$(GO) test -race $(PKGS); \
	fi

check: lint test

build:
	$(GO) build ./...
