# ABOUTME: Build and development automation for the dynupdate CoreDNS plugin
# ABOUTME: Targets for building, testing, linting, proto generation, and code quality

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

.DEFAULT_GOAL := help

# ── Variables ────────────────────────────────────────────────────────────────
GOBIN       ?= $(shell go env GOPATH)/bin
MODULE      := github.com/mauromedda/coredns-updater-plugin
PROTO_DIR   := proto
COVERAGE    := coverage.out

# ── Colors ───────────────────────────────────────────────────────────────────
CYAN  := \033[0;36m
GREEN := \033[0;32m
RED   := \033[0;31m
BOLD  := \033[1m
NC    := \033[0m

define log_info
	@printf "$(CYAN)[INFO]$(NC) %s\n" "$(1)"
endef

define log_success
	@printf "$(GREEN)[OK]$(NC) %s\n" "$(1)"
endef

define log_error
	@printf "$(RED)[ERROR]$(NC) %s\n" "$(1)"
endef

##@ General
.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: build
build: ## Build the plugin (compile check)
	$(call log_info,Building plugin)
	@go build ./...
	$(call log_success,Build succeeded)

.PHONY: test
test: ## Run all tests
	$(call log_info,Running tests)
	@go test -v -race ./...
	$(call log_success,All tests passed)

.PHONY: test-cover
test-cover: ## Run tests with coverage report
	$(call log_info,Running tests with coverage)
	@go test -race -coverprofile=$(COVERAGE) -covermode=atomic ./...
	@go tool cover -func=$(COVERAGE)
	$(call log_success,Coverage report generated)

.PHONY: test-cover-html
test-cover-html: test-cover ## Generate HTML coverage report
	@go tool cover -html=$(COVERAGE) -o coverage.html
	$(call log_success,HTML coverage report: coverage.html)

.PHONY: lint
lint: ## Run golangci-lint
	$(call log_info,Running linter)
	@golangci-lint run ./...
	$(call log_success,Lint passed)

.PHONY: fmt
fmt: ## Format Go source files
	@gofmt -s -w .
	@goimports -w .
	$(call log_success,Formatted)

.PHONY: vet
vet: ## Run go vet
	@go vet ./...
	$(call log_success,Vet passed)

##@ Proto
.PHONY: proto
proto: ## Generate Go code from proto definitions
	$(call log_info,Generating proto code)
	@protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/dynupdate.proto
	$(call log_success,Proto code generated)

.PHONY: proto-lint
proto-lint: ## Lint proto files
	@protoc --lint_out=. $(PROTO_DIR)/dynupdate.proto 2>/dev/null || true

##@ Quality
.PHONY: check
check: vet lint test ## Run all quality checks (vet + lint + test)
	$(call log_success,All checks passed)

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	@go mod tidy
	$(call log_success,Modules tidied)

.PHONY: clean
clean: ## Remove build artifacts and coverage files
	@rm -f $(COVERAGE) coverage.html
	@go clean -testcache
	$(call log_success,Cleaned)
