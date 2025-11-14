SHELL := /bin/bash

GO ?= go
MIN_COVERAGE ?= 95.0
COVERAGE_PROFILE ?= coverage.out
COVERAGE_SUMMARY ?= coverage.txt

MODULE := $(shell $(GO) list -m 2>/dev/null)
PKGS := $(shell $(GO) list ./... 2>/dev/null)
COVERAGE_EXCLUDES ?= $(if $(MODULE),$(MODULE)/cmd/agentscheck% $(MODULE)/hack/%,)
PROD_PATTERNS := ./cmd/... ./internal/... ./pkg/...
PROD_PKGS := $(shell $(GO) list $(PROD_PATTERNS) 2>/dev/null)
COVERAGE_PKGS := $(filter-out $(COVERAGE_EXCLUDES),$(PROD_PKGS))
INTEGRATION_PKGS_RAW := $(shell $(GO) list ./tests/integration/... 2>/dev/null)
INTEGRATION_PKGS := $(filter-out $(COVERAGE_EXCLUDES),$(INTEGRATION_PKGS_RAW))
E2E_PKGS_RAW := $(shell $(GO) list ./tests/e2e/... 2>/dev/null)
E2E_PKGS := $(filter-out $(COVERAGE_EXCLUDES),$(E2E_PKGS_RAW))
COVERAGE_TAGS ?= integration e2e
COVERAGE_TAG_ARGS := $(if $(strip $(COVERAGE_TAGS)),-tags "$(strip $(COVERAGE_TAGS))",)

GOLANGCI_LINT_VERSION ?= v2.6.1
GOFUMPT_VERSION ?= 0.9.2
GOVULNCHECK_VERSION ?= v1.1.4
ACTIONLINT_VERSION ?= v1.7.8

GO_BIN_PATH := $(shell $(GO) env GOBIN)
ifeq ($(GO_BIN_PATH),)
GO_BIN_PATH := $(shell $(GO) env GOPATH)/bin
endif

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
GOVULNCHECK_CACHE_DIR := $(ROOT_DIR)/.cache/govulncheck
GOCACHE_DIR := $(ROOT_DIR)/.cache/go
GOLANGCI_LINT_CACHE_DIR := $(ROOT_DIR)/.cache/golangci

GOLANGCI_LINT_BIN ?= $(GO_BIN_PATH)/golangci-lint
GOLANGCI_LINT ?= $(GOLANGCI_LINT_BIN)
GOFUMPT_BIN ?= $(GO_BIN_PATH)/gofumpt
GOFUMPT ?= $(GOFUMPT_BIN)
ACTIONLINT_BIN ?= $(GO_BIN_PATH)/actionlint
ACTIONLINT ?= $(ACTIONLINT_BIN)
ACTIONLINT_FLAGS ?=
ACTIONLINT_PATHS ?=

.PHONY: fmt lint test build check tools ensure-golangci-lint ensure-gofumpt ensure-actionlint agents coverage govulncheck integration e2e actionlint lint-workflows

tools: ensure-golangci-lint ensure-gofumpt ensure-actionlint

ensure-golangci-lint:
	@set -euo pipefail; \
	BIN="$(GOLANGCI_LINT_BIN)"; \
	CURRENT_VERSION=""; \
	if [ -x "$$BIN" ]; then \
		CURRENT_VERSION="$$($$BIN version --short 2>/dev/null || true)"; \
	fi; \
	if [ "$$CURRENT_VERSION" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(GO_BIN_PATH) $(GOLANGCI_LINT_VERSION); \
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
	@mkdir -p "$(GOLANGCI_LINT_CACHE_DIR)"
	@GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE_DIR)" $(GOLANGCI_LINT) run

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

ensure-actionlint:
	@set -euo pipefail; \
	BIN="$(ACTIONLINT_BIN)"; \
	CURRENT_VERSION=""; \
	if [ -x "$$BIN" ]; then \
		CURRENT_VERSION="$$($$BIN -version 2>/dev/null | head -n1)"; \
	fi; \
	if [ "$$CURRENT_VERSION" != "$(ACTIONLINT_VERSION)" ]; then \
		echo "Installing actionlint $(ACTIONLINT_VERSION)"; \
		$(GO) install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION); \
	fi

test:
	@if [ -z "$(strip $(PKGS))" ]; then \
		echo "No Go packages found; skipping tests."; \
	else \
		mkdir -p "$(GOCACHE_DIR)"; \
		GOCACHE="$(GOCACHE_DIR)" $(GO) test -race $(PKGS); \
	fi

coverage:
	@set -euo pipefail; \
        if [ -z "$(strip $(PKGS))" ]; then \
                echo "No Go packages found; skipping coverage."; \
        elif [ -z "$(strip $(COVERAGE_PKGS))" ]; then \
                echo "No Go packages selected for coverage after exclusions; adjust COVERAGE_EXCLUDES."; \
                exit 1; \
        else \
                excluded="$(strip $(COVERAGE_EXCLUDES))"; \
                if [ -n "$$excluded" ]; then \
                        echo "Excluding packages from coverage: $$excluded"; \
                fi; \
                coverage_pkgs="$(strip $(COVERAGE_PKGS))"; \
                coverage_csv=$$(printf '%s' "$$coverage_pkgs" | tr ' \n' ',' | sed 's/,,*/,/g; s/^,//; s/,$$//'); \
                rm -f $(COVERAGE_PROFILE) $(COVERAGE_SUMMARY); \
                unit_profile="coverage-unit.out"; \
                $(GO) test -race -covermode=atomic -coverpkg="$$coverage_csv" -coverprofile="$$unit_profile" $(COVERAGE_PKGS); \
                cat "$$unit_profile" > $(COVERAGE_PROFILE); \
                rm -f "$$unit_profile"; \
                if [ -n "$(strip $(INTEGRATION_PKGS))" ]; then \
                        integration_profile="coverage-integration.out"; \
                        $(GO) test -race -covermode=atomic -tags=integration -coverpkg="$$coverage_csv" -coverprofile="$$integration_profile" $(INTEGRATION_PKGS); \
                        tail -n +2 "$$integration_profile" >> $(COVERAGE_PROFILE); \
                        rm -f "$$integration_profile"; \
                fi; \
                if [ -n "$(strip $(E2E_PKGS))" ]; then \
                        e2e_profile="coverage-e2e.out"; \
                        if $(GO) test -race -covermode=atomic -tags=e2e -coverpkg="$$coverage_csv" -coverprofile="$$e2e_profile" $(E2E_PKGS); then \
                                tail -n +2 "$$e2e_profile" >> $(COVERAGE_PROFILE); \
                        else \
                                echo "Skipping e2e coverage due to test failures"; \
                        fi; \
                        rm -f "$$e2e_profile"; \
                fi; \
                $(GO) tool cover -func=$(COVERAGE_PROFILE) | tee $(COVERAGE_SUMMARY); \
                TOTAL=$$(awk '/^total:/ {total=$$NF} END {print total}' $(COVERAGE_SUMMARY)); \
                if [ -n "$$TOTAL" ]; then \
                        echo "Total coverage: $$TOTAL"; \
                        COVERAGE_VALUE=$$(printf '%s' "$$TOTAL" | tr -d '%'); \
			if ! awk -v cov="$$COVERAGE_VALUE" -v min="$(MIN_COVERAGE)" 'BEGIN {if (cov+0 >= min+0) exit 0; exit 1}' ; then \
				echo "Coverage $${COVERAGE_VALUE}% is below required $(MIN_COVERAGE)%"; \
				exit 1; \
			fi; \
		else \
			echo "Coverage summary unavailable"; \
		fi; \
	fi
agents:
	@set -euo pipefail; \
	mkdir -p "$(GOCACHE_DIR)"; \
	GOCACHE="$(GOCACHE_DIR)" $(GO) run ./cmd/agentscheck

govulncheck:
	@set -euo pipefail; \
	mkdir -p "$(GOCACHE_DIR)" "$(GOVULNCHECK_CACHE_DIR)"; \
	GOCACHE="$(GOCACHE_DIR)" GOVULNCHECK_CACHE="$(GOVULNCHECK_CACHE_DIR)" \
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

check: lint test agents

actionlint: ensure-actionlint
	@set -euo pipefail; \
	if [ ! -d ".github/workflows" ]; then \
		echo "No workflows directory found; skipping workflow lint."; \
	else \
		if [ -n "$(strip $(ACTIONLINT_PATHS))" ]; then \
			$(ACTIONLINT) $(strip $(ACTIONLINT_FLAGS)) $(ACTIONLINT_PATHS); \
		else \
			$(ACTIONLINT) $(strip $(ACTIONLINT_FLAGS)); \
		fi; \
	fi

lint-workflows: actionlint

build:
	$(GO) build ./...

integration:
	@set -euo pipefail; \
	if ! command -v docker >/dev/null 2>&1; then \
		echo "integration suite requires the docker CLI"; \
		exit 1; \
	fi; \
	if ! docker info >/dev/null 2>&1; then \
		echo "failed to communicate with the Docker daemon"; \
		exit 1; \
	fi; \
	cgroup_version="$$(docker info --format '{{.CgroupVersion}}' 2>/dev/null || true)"; \
	if [ "$$cgroup_version" != "2" ]; then \
		echo "integration suite requires cgroup v2 (detected $${cgroup_version:-unknown})"; \
		exit 1; \
	fi; \
	echo "Docker cgroup version: $$cgroup_version"; \
	artifacts_dir="$(ROOT_DIR)/artifacts"; \
	log_file="$$artifacts_dir/integration.log"; \
	mkdir -p "$$artifacts_dir" "$(GOCACHE_DIR)"; \
	keep_logs="$${INTEGRATION_KEEP_LOGS:-0}"; \
	cleanup() { \
		status="$$?"; \
		if [ "$$status" -eq 0 ] && [ "$$keep_logs" != "1" ]; then \
			rm -f "$$log_file"; \
			rmdir "$$artifacts_dir" 2>/dev/null || true; \
		else \
			echo "Integration logs captured at $$log_file"; \
		fi; \
		exit "$$status"; \
	}; \
	trap 'cleanup' EXIT; \
	touch "$$log_file"; \
	GOCACHE="$(GOCACHE_DIR)" $(GO) test -tags=integration -v ./tests/integration/... | tee "$$log_file"
e2e:
	@set -euo pipefail; \
	if [ ! -d "$(ROOT_DIR)/tests/e2e" ]; then \
		echo "e2e suite not available"; \
		exit 0; \
	fi; \
	mkdir -p "$(GOCACHE_DIR)"; \
	GOCACHE="$(GOCACHE_DIR)" $(GO) test -tags=e2e -v ./tests/e2e/...
