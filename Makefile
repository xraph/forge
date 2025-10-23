# Forge v2 Makefile
# Production-ready development workflow for testing, building, and linting

# ==============================================================================
# Variables
# ==============================================================================

# Binary and output configuration
BINARY_NAME := forge
OUTPUT_DIR := bin
CLI_DIR := cmd/forge
COVERAGE_DIR := coverage
LINT_CONFIG := .golangci.yml

# Build metadata
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := $(shell go version | cut -d' ' -f3)

# Build flags
LDFLAGS := -ldflags "\
	-s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE) \
	-X main.goVersion=$(GO_VERSION)"

# Go commands and flags
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := $(GOCMD) fmt
GOVET := $(GOCMD) vet
GOINSTALL := $(GOCMD) install

# Test flags
TEST_FLAGS := -v -race -timeout=5m
COVERAGE_FLAGS := -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic
BENCH_FLAGS := -bench=. -benchmem -benchtime=5s

# Directories to test/lint (excluding vendor, generated, and bk/)
TEST_DIRS := $(shell go list ./... 2>/dev/null | grep -v '/bk/')
LINT_DIRS := ./...

# Tools
GOLANGCI_LINT := golangci-lint
GOFUMPT := gofumpt
GOIMPORTS := goimports

# Colors for output
COLOR_RESET := \033[0m
COLOR_GREEN := \033[32m
COLOR_YELLOW := \033[33m
COLOR_BLUE := \033[34m
COLOR_RED := \033[31m

# ==============================================================================
# Main targets
# ==============================================================================

.PHONY: all
## all: Run format, lint, test, and build (default target)
all: fmt lint test build

.PHONY: help
## help: Show this help message
help:
	@echo "$(COLOR_BLUE)Forge v2 Makefile$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_GREEN)Usage:$(COLOR_RESET)"
	@echo "  make [target]"
	@echo ""
	@echo "$(COLOR_GREEN)Targets:$(COLOR_RESET)"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'

# ==============================================================================
# Build targets
# ==============================================================================

.PHONY: build
## build: Build the Forge CLI binary
build:
	@echo "$(COLOR_GREEN)Building Forge CLI...$(COLOR_RESET)"
	@mkdir -p $(OUTPUT_DIR)
	@cd $(CLI_DIR) && $(GOBUILD) $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(BINARY_NAME) .
	@echo "$(COLOR_GREEN)✓ Built: $(OUTPUT_DIR)/$(BINARY_NAME)$(COLOR_RESET)"

.PHONY: build-debug
## build-debug: Build with debug symbols (no -s -w flags)
build-debug:
	@echo "$(COLOR_GREEN)Building Forge CLI (debug mode)...$(COLOR_RESET)"
	@mkdir -p $(OUTPUT_DIR)
	@cd $(CLI_DIR) && $(GOBUILD) -gcflags="all=-N -l" -o ../../$(OUTPUT_DIR)/$(BINARY_NAME)-debug .
	@echo "$(COLOR_GREEN)✓ Built: $(OUTPUT_DIR)/$(BINARY_NAME)-debug$(COLOR_RESET)"

.PHONY: build-all
## build-all: Build CLI and all examples
build-all: build build-examples
	@echo "$(COLOR_GREEN)✓ Built all targets$(COLOR_RESET)"

.PHONY: build-examples
## build-examples: Build all example applications
build-examples:
	@echo "$(COLOR_GREEN)Building examples...$(COLOR_RESET)"
	@for dir in examples/*/; do \
		if [ -f "$$dir/main.go" ]; then \
			example=$$(basename $$dir); \
			echo "  Building $$example..."; \
			cd $$dir && $(GOBUILD) -o ../../$(OUTPUT_DIR)/examples/$$example . || exit 1; \
			cd ../..; \
		fi \
	done
	@echo "$(COLOR_GREEN)✓ Built all examples$(COLOR_RESET)"

.PHONY: install
## install: Install the CLI to GOPATH/bin
install:
	@echo "$(COLOR_GREEN)Installing Forge CLI...$(COLOR_RESET)"
	@cd $(CLI_DIR) && $(GOINSTALL) $(LDFLAGS) .
	@echo "$(COLOR_GREEN)✓ Installed to: $$(go env GOPATH)/bin/$(BINARY_NAME)$(COLOR_RESET)"

# ==============================================================================
# Test targets
# ==============================================================================

.PHONY: test
## test: Run all tests with race detector
test:
	@echo "$(COLOR_GREEN)Running tests...$(COLOR_RESET)"
	@$(GOTEST) $(TEST_FLAGS) $(TEST_DIRS)
	@echo "$(COLOR_GREEN)✓ All tests passed$(COLOR_RESET)"

.PHONY: test-short
## test-short: Run tests with -short flag
test-short:
	@echo "$(COLOR_GREEN)Running short tests...$(COLOR_RESET)"
	@$(GOTEST) -short $(TEST_FLAGS) $(TEST_DIRS)

.PHONY: test-verbose
## test-verbose: Run tests with verbose output
test-verbose:
	@echo "$(COLOR_GREEN)Running tests (verbose)...$(COLOR_RESET)"
	@$(GOTEST) $(TEST_FLAGS) -v $(TEST_DIRS)

.PHONY: test-coverage
## test-coverage: Run tests with coverage report
test-coverage:
	@echo "$(COLOR_GREEN)Running tests with coverage...$(COLOR_RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) $(TEST_FLAGS) $(COVERAGE_FLAGS) $(TEST_DIRS)
	@$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "$(COLOR_GREEN)✓ Coverage report: $(COVERAGE_DIR)/coverage.html$(COLOR_RESET)"
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

.PHONY: test-coverage-text
## test-coverage-text: Run tests and show coverage summary
test-coverage-text:
	@echo "$(COLOR_GREEN)Running tests with coverage...$(COLOR_RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) $(TEST_FLAGS) $(COVERAGE_FLAGS) $(TEST_DIRS)
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out

.PHONY: test-integration
## test-integration: Run integration tests only
test-integration:
	@echo "$(COLOR_GREEN)Running integration tests...$(COLOR_RESET)"
	@$(GOTEST) $(TEST_FLAGS) -tags=integration $(TEST_DIRS)

.PHONY: test-unit
## test-unit: Run unit tests only (exclude integration)
test-unit:
	@echo "$(COLOR_GREEN)Running unit tests...$(COLOR_RESET)"
	@$(GOTEST) $(TEST_FLAGS) -short $(TEST_DIRS)

.PHONY: test-race
## test-race: Run tests with race detector
test-race:
	@echo "$(COLOR_GREEN)Running tests with race detector...$(COLOR_RESET)"
	@$(GOTEST) -race -timeout=10m $(TEST_DIRS)

.PHONY: test-cli
## test-cli: Run CLI tests
test-cli:
	@echo "$(COLOR_GREEN)Running CLI tests...$(COLOR_RESET)"
	@cd $(CLI_DIR) && $(GOTEST) $(TEST_FLAGS) ./...
	@cd cli && $(GOTEST) $(TEST_FLAGS) ./...

.PHONY: test-extensions
## test-extensions: Run extension tests
test-extensions:
	@echo "$(COLOR_GREEN)Running extension tests...$(COLOR_RESET)"
	@cd extensions && $(GOTEST) $(TEST_FLAGS) ./...

.PHONY: test-watch
## test-watch: Run tests in watch mode (requires entr)
test-watch:
	@echo "$(COLOR_YELLOW)Watching for changes (Ctrl+C to stop)...$(COLOR_RESET)"
	@find . -name '*.go' ! -path "./vendor/*" | entr -c make test-short

.PHONY: bench
## bench: Run benchmarks
bench:
	@echo "$(COLOR_GREEN)Running benchmarks...$(COLOR_RESET)"
	@$(GOTEST) $(BENCH_FLAGS) $(TEST_DIRS)

.PHONY: bench-compare
## bench-compare: Run benchmarks and save to file for comparison
bench-compare:
	@echo "$(COLOR_GREEN)Running benchmarks (saving to bench.txt)...$(COLOR_RESET)"
	@$(GOTEST) $(BENCH_FLAGS) $(TEST_DIRS) | tee bench.txt

# ==============================================================================
# Linting and formatting
# ==============================================================================

.PHONY: lint
## lint: Run golangci-lint
lint:
	@echo "$(COLOR_GREEN)Running linter...$(COLOR_RESET)"
	@if command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run $(LINT_DIRS) --timeout=5m; \
		echo "$(COLOR_GREEN)✓ Linting passed$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_RED)Error: golangci-lint not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
		exit 1; \
	fi

.PHONY: lint-fix
## lint-fix: Run golangci-lint with auto-fix
lint-fix:
	@echo "$(COLOR_GREEN)Running linter with auto-fix...$(COLOR_RESET)"
	@$(GOLANGCI_LINT) run $(LINT_DIRS) --fix --timeout=5m

.PHONY: fmt
## fmt: Format Go code
fmt:
	@echo "$(COLOR_GREEN)Formatting code...$(COLOR_RESET)"
	@$(GOFMT) $(TEST_DIRS)
	@echo "$(COLOR_GREEN)✓ Code formatted$(COLOR_RESET)"

.PHONY: fmt-check
## fmt-check: Check if code is formatted
fmt-check:
	@echo "$(COLOR_GREEN)Checking code format...$(COLOR_RESET)"
	@test -z "$$($(GOFMT) -l . | tee /dev/stderr)" || \
		(echo "$(COLOR_RED)Code is not formatted. Run 'make fmt'$(COLOR_RESET)" && exit 1)

.PHONY: vet
## vet: Run go vet
vet:
	@echo "$(COLOR_GREEN)Running go vet...$(COLOR_RESET)"
	@$(GOVET) $(TEST_DIRS)
	@echo "$(COLOR_GREEN)✓ Vet passed$(COLOR_RESET)"

.PHONY: tidy
## tidy: Tidy go modules
tidy:
	@echo "$(COLOR_GREEN)Tidying modules...$(COLOR_RESET)"
	@$(GOMOD) tidy
	@echo "$(COLOR_GREEN)✓ Modules tidied$(COLOR_RESET)"

.PHONY: tidy-check
## tidy-check: Check if go.mod is tidy
tidy-check:
	@echo "$(COLOR_GREEN)Checking if modules are tidy...$(COLOR_RESET)"
	@$(GOMOD) tidy
	@git diff --exit-code go.mod go.sum || \
		(echo "$(COLOR_RED)go.mod or go.sum is not tidy. Run 'make tidy'$(COLOR_RESET)" && exit 1)

.PHONY: verify
## verify: Run all verification checks (fmt-check, vet, tidy-check, lint)
verify: fmt-check vet tidy-check lint
	@echo "$(COLOR_GREEN)✓ All verification checks passed$(COLOR_RESET)"

# ==============================================================================
# CLI development
# ==============================================================================

.PHONY: dev
## dev: Build and run CLI with 'doctor' command
dev: build
	@echo "$(COLOR_GREEN)Running Forge CLI (doctor)...$(COLOR_RESET)"
	@./$(OUTPUT_DIR)/$(BINARY_NAME) doctor

.PHONY: dev-version
## dev-version: Build and show version
dev-version: build
	@./$(OUTPUT_DIR)/$(BINARY_NAME) --version

.PHONY: cli-examples
## cli-examples: Run all CLI examples
cli-examples:
	@echo "$(COLOR_GREEN)Running CLI examples...$(COLOR_RESET)"
	@for dir in cli/examples/*/; do \
		if [ -f "$$dir/main" ]; then \
			example=$$(basename $$dir); \
			echo "  Running $$example..."; \
			$$dir/main || echo "  $(COLOR_YELLOW)Warning: $$example failed$(COLOR_RESET)"; \
		fi \
	done

# ==============================================================================
# Dependency management
# ==============================================================================

.PHONY: deps
## deps: Download dependencies
deps:
	@echo "$(COLOR_GREEN)Downloading dependencies...$(COLOR_RESET)"
	@$(GOMOD) download
	@echo "$(COLOR_GREEN)✓ Dependencies downloaded$(COLOR_RESET)"

.PHONY: deps-update
## deps-update: Update all dependencies
deps-update:
	@echo "$(COLOR_GREEN)Updating dependencies...$(COLOR_RESET)"
	@$(GOGET) -u ./...
	@$(GOMOD) tidy
	@echo "$(COLOR_GREEN)✓ Dependencies updated$(COLOR_RESET)"

.PHONY: deps-vendor
## deps-vendor: Vendor dependencies
deps-vendor:
	@echo "$(COLOR_GREEN)Vendoring dependencies...$(COLOR_RESET)"
	@$(GOMOD) vendor
	@echo "$(COLOR_GREEN)✓ Dependencies vendored$(COLOR_RESET)"

# ==============================================================================
# Security and quality
# ==============================================================================

.PHONY: security
## security: Run security scan with gosec
security:
	@echo "$(COLOR_GREEN)Running security scan...$(COLOR_RESET)"
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -exclude-dir=vendor -exclude-dir=examples ./...; \
		echo "$(COLOR_GREEN)✓ Security scan completed$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)Warning: gosec not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
	fi

.PHONY: vuln-check
## vuln-check: Check for known vulnerabilities
vuln-check:
	@echo "$(COLOR_GREEN)Checking for vulnerabilities...$(COLOR_RESET)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
		echo "$(COLOR_GREEN)✓ Vulnerability check completed$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)Warning: govulncheck not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
	fi

.PHONY: complexity
## complexity: Check code complexity
complexity:
	@echo "$(COLOR_GREEN)Checking code complexity...$(COLOR_RESET)"
	@if command -v gocyclo >/dev/null 2>&1; then \
		gocyclo -over 15 .; \
	else \
		echo "$(COLOR_YELLOW)Warning: gocyclo not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
	fi

# ==============================================================================
# Code generation
# ==============================================================================

.PHONY: generate
## generate: Run go generate
generate:
	@echo "$(COLOR_GREEN)Running code generation...$(COLOR_RESET)"
	@$(GOCMD) generate $(TEST_DIRS)
	@echo "$(COLOR_GREEN)✓ Code generation completed$(COLOR_RESET)"

.PHONY: mocks
## mocks: Generate test mocks
mocks:
	@echo "$(COLOR_GREEN)Generating mocks...$(COLOR_RESET)"
	@if command -v mockgen >/dev/null 2>&1; then \
		$(GOCMD) generate -tags=mock $(TEST_DIRS); \
		echo "$(COLOR_GREEN)✓ Mocks generated$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)Warning: mockgen not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
	fi

# ==============================================================================
# Cleanup
# ==============================================================================

.PHONY: clean
## clean: Remove build artifacts and cache
clean:
	@echo "$(COLOR_GREEN)Cleaning...$(COLOR_RESET)"
	@rm -rf $(OUTPUT_DIR)
	@rm -rf $(COVERAGE_DIR)
	@rm -f bench.txt
	@$(GOCMD) clean -cache -testcache -modcache
	@echo "$(COLOR_GREEN)✓ Cleaned$(COLOR_RESET)"

.PHONY: clean-build
## clean-build: Remove only build artifacts
clean-build:
	@echo "$(COLOR_GREEN)Cleaning build artifacts...$(COLOR_RESET)"
	@rm -rf $(OUTPUT_DIR)
	@rm -rf $(COVERAGE_DIR)
	@echo "$(COLOR_GREEN)✓ Build artifacts cleaned$(COLOR_RESET)"

# ==============================================================================
# Documentation
# ==============================================================================

.PHONY: docs
## docs: Generate documentation
docs:
	@echo "$(COLOR_GREEN)Generating documentation...$(COLOR_RESET)"
	@if command -v godoc >/dev/null 2>&1; then \
		echo "Starting godoc server at http://localhost:6060"; \
		godoc -http=:6060; \
	else \
		echo "$(COLOR_YELLOW)Warning: godoc not found. Run 'make install-tools' to install$(COLOR_RESET)"; \
	fi

.PHONY: docs-generate
## docs-generate: Generate static documentation
docs-generate:
	@echo "$(COLOR_GREEN)Generating static documentation...$(COLOR_RESET)"
	@$(GOCMD) doc -all ./... > docs/API.md
	@echo "$(COLOR_GREEN)✓ Documentation generated$(COLOR_RESET)"

# ==============================================================================
# Tools installation
# ==============================================================================

.PHONY: install-tools
## install-tools: Install development tools
install-tools:
	@echo "$(COLOR_GREEN)Installing development tools...$(COLOR_RESET)"
	@echo "  Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "  Installing gosec..."
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "  Installing govulncheck..."
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "  Installing gocyclo..."
	@go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
	@echo "  Installing mockgen..."
	@go install go.uber.org/mock/mockgen@latest
	@echo "  Installing gofumpt..."
	@go install mvdan.cc/gofumpt@latest
	@echo "  Installing goimports..."
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "$(COLOR_GREEN)✓ All tools installed$(COLOR_RESET)"

.PHONY: check-tools
## check-tools: Check if required tools are installed
check-tools:
	@echo "$(COLOR_GREEN)Checking installed tools...$(COLOR_RESET)"
	@command -v golangci-lint >/dev/null 2>&1 && echo "  ✓ golangci-lint" || echo "  ✗ golangci-lint"
	@command -v gosec >/dev/null 2>&1 && echo "  ✓ gosec" || echo "  ✗ gosec"
	@command -v govulncheck >/dev/null 2>&1 && echo "  ✓ govulncheck" || echo "  ✗ govulncheck"
	@command -v gocyclo >/dev/null 2>&1 && echo "  ✓ gocyclo" || echo "  ✗ gocyclo"
	@command -v mockgen >/dev/null 2>&1 && echo "  ✓ mockgen" || echo "  ✗ mockgen"
	@command -v gofumpt >/dev/null 2>&1 && echo "  ✓ gofumpt" || echo "  ✗ gofumpt"
	@command -v goimports >/dev/null 2>&1 && echo "  ✓ goimports" || echo "  ✗ goimports"

# ==============================================================================
# CI/CD helpers
# ==============================================================================

.PHONY: ci
## ci: Run all CI checks (verify, test, build)
ci: verify test build
	@echo "$(COLOR_GREEN)✓ All CI checks passed$(COLOR_RESET)"

.PHONY: ci-full
## ci-full: Run comprehensive CI checks (verify, test-coverage, security, build)
ci-full: verify test-coverage security vuln-check build
	@echo "$(COLOR_GREEN)✓ All comprehensive CI checks passed$(COLOR_RESET)"

.PHONY: pre-commit
## pre-commit: Run checks before commit (fmt, lint, test-short)
pre-commit: fmt lint test-short
	@echo "$(COLOR_GREEN)✓ Pre-commit checks passed$(COLOR_RESET)"

.PHONY: pre-push
## pre-push: Run checks before push (verify, test)
pre-push: verify test
	@echo "$(COLOR_GREEN)✓ Pre-push checks passed$(COLOR_RESET)"

# ==============================================================================
# Release
# ==============================================================================

.PHONY: release-dry-run
## release-dry-run: Show what would be built for release
release-dry-run:
	@echo "$(COLOR_GREEN)Release information:$(COLOR_RESET)"
	@echo "  Version:    $(VERSION)"
	@echo "  Commit:     $(COMMIT)"
	@echo "  Build Date: $(BUILD_DATE)"
	@echo "  Go Version: $(GO_VERSION)"

.PHONY: release
## release: Build release binaries for multiple platforms
release: clean
	@echo "$(COLOR_GREEN)Building release binaries...$(COLOR_RESET)"
	@mkdir -p $(OUTPUT_DIR)/releases
	@for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			ext=""; \
			if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
			echo "  Building $$os/$$arch..."; \
			cd $(CLI_DIR) && GOOS=$$os GOARCH=$$arch $(GOBUILD) $(LDFLAGS) \
				-o ../../$(OUTPUT_DIR)/releases/$(BINARY_NAME)-$$os-$$arch$$ext . || exit 1; \
			cd ../..; \
		done \
	done
	@echo "$(COLOR_GREEN)✓ Release binaries built in $(OUTPUT_DIR)/releases$(COLOR_RESET)"

# ==============================================================================
# Docker
# ==============================================================================

.PHONY: docker-build
## docker-build: Build Docker image
docker-build:
	@echo "$(COLOR_GREEN)Building Docker image...$(COLOR_RESET)"
	@docker build -t forge:$(VERSION) -t forge:latest .
	@echo "$(COLOR_GREEN)✓ Docker image built$(COLOR_RESET)"

.PHONY: docker-test
## docker-test: Run tests in Docker
docker-test:
	@echo "$(COLOR_GREEN)Running tests in Docker...$(COLOR_RESET)"
	@docker run --rm -v $(PWD):/app -w /app golang:1.24 make test

# ==============================================================================
# Quick shortcuts
# ==============================================================================

.PHONY: t
## t: Alias for 'test'
t: test

.PHONY: b
## b: Alias for 'build'
b: build

.PHONY: l
## l: Alias for 'lint'
l: lint

.PHONY: f
## f: Alias for 'fmt'
f: fmt

.PHONY: r
## r: Alias for 'dev' (run)
r: dev

# ==============================================================================
# Info
# ==============================================================================

.PHONY: info
## info: Display project information
info:
	@echo "$(COLOR_BLUE)Forge v2 Project Information$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_GREEN)Version:$(COLOR_RESET)    $(VERSION)"
	@echo "$(COLOR_GREEN)Commit:$(COLOR_RESET)     $(COMMIT)"
	@echo "$(COLOR_GREEN)Build Date:$(COLOR_RESET) $(BUILD_DATE)"
	@echo "$(COLOR_GREEN)Go Version:$(COLOR_RESET) $(GO_VERSION)"
	@echo ""
	@echo "$(COLOR_GREEN)Directories:$(COLOR_RESET)"
	@echo "  Output:   $(OUTPUT_DIR)"
	@echo "  CLI:      $(CLI_DIR)"
	@echo "  Coverage: $(COVERAGE_DIR)"
	@echo ""
	@echo "$(COLOR_GREEN)Module:$(COLOR_RESET)     $$(head -1 go.mod | cut -d' ' -f2)"
	@echo ""

# ==============================================================================
# Default target
# ==============================================================================

.DEFAULT_GOAL := help

