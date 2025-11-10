GO ?= go
GOLANGCI_LINT ?= golangci-lint
PKGS := $(shell $(GO) list ./... 2>/dev/null)

.PHONY: fmt lint test build

fmt:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping format."; \
	else \
		$(GO) fmt $(PKGS); \
	fi

lint:
	$(GOLANGCI_LINT) run

test:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping tests."; \
	else \
		$(GO) test -race $(PKGS); \
	fi

build:
	$(GO) build ./...
