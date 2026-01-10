# Forge Makefile
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
	@echo "$(COLOR_BLUE)Forge Makefile$(COLOR_RESET)"
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

.PHONY: ci-comprehensive
## ci-comprehensive: Run comprehensive CI checks (verify, test-coverage, security, build)
ci-comprehensive: verify test-coverage security vuln-check build
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
	@echo "$(COLOR_BLUE)Forge Project Information$(COLOR_RESET)"
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
# Multi-Module Release Management
# ==============================================================================

.PHONY: modules-check
## modules-check: Check all module versions and dependencies
modules-check:
	@echo "$(COLOR_GREEN)Checking module versions and dependencies...$(COLOR_RESET)"
	@./scripts/check-module-versions.sh

.PHONY: modules-fix-versions
## modules-fix-versions: Fix Go version mismatches across all modules
modules-fix-versions:
	@echo "$(COLOR_GREEN)Fixing Go version mismatches...$(COLOR_RESET)"
	@MAIN_VERSION=$$(grep "^go " go.mod | awk '{print $$2}'); \
	echo "Main module uses Go $$MAIN_VERSION"; \
	for ext in graphql grpc hls kafka mqtt; do \
		if [ -f "extensions/$$ext/go.mod" ]; then \
			EXT_VERSION=$$(grep "^go " extensions/$$ext/go.mod | awk '{print $$2}'); \
			if [ "$$EXT_VERSION" != "$$MAIN_VERSION" ]; then \
				echo "  Updating extensions/$$ext: $$EXT_VERSION -> $$MAIN_VERSION"; \
				cd extensions/$$ext && go mod edit -go=$$MAIN_VERSION && cd ../..; \
			else \
				echo "  extensions/$$ext: $$EXT_VERSION $(COLOR_GREEN)✓$(COLOR_RESET)"; \
			fi; \
		fi; \
	done
	@echo "$(COLOR_GREEN)✓ Go versions aligned$(COLOR_RESET)"

.PHONY: modules-update-deps
## modules-update-deps: Update extension dependencies to latest main module version
modules-update-deps:
	@echo "$(COLOR_GREEN)Updating extension dependencies...$(COLOR_RESET)"
	@LATEST_TAG=$$(git tag -l "v*" --sort=-version:refname | grep -v "extensions" | head -1); \
	if [ -z "$$LATEST_TAG" ]; then \
		echo "$(COLOR_RED)No main module release found. Release main module first.$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	echo "Latest main module version: $$LATEST_TAG"; \
	for ext in graphql grpc hls kafka mqtt; do \
		if [ -f "extensions/$$ext/go.mod" ]; then \
			echo "  Updating extensions/$$ext to $$LATEST_TAG..."; \
			cd extensions/$$ext && \
			go mod edit -require=github.com/xraph/forge@$$LATEST_TAG && \
			go mod tidy && \
			cd ../..; \
		fi; \
	done
	@echo "$(COLOR_GREEN)✓ Dependencies updated$(COLOR_RESET)"

.PHONY: modules-test
## modules-test: Run tests for all modules
modules-test:
	@echo "$(COLOR_GREEN)Testing all modules...$(COLOR_RESET)"
	@echo "  Testing main module..."
	@$(GOTEST) $(TEST_FLAGS) $(TEST_DIRS)
	@for ext in graphql grpc hls kafka mqtt; do \
		if [ -d "extensions/$$ext" ]; then \
			echo "  Testing extensions/$$ext..."; \
			cd extensions/$$ext && $(GOTEST) $(TEST_FLAGS) ./... && cd ../..; \
		fi; \
	done
	@echo "$(COLOR_GREEN)✓ All module tests passed$(COLOR_RESET)"

.PHONY: release-prepare
## release-prepare: Prepare for release (check versions, run tests, validate)
release-prepare: modules-check modules-test verify
	@echo "$(COLOR_GREEN)Running pre-release checks...$(COLOR_RESET)"
	@if ! git diff-index --quiet HEAD --; then \
		echo "$(COLOR_RED)Working directory is not clean. Commit changes first.$(COLOR_RESET)"; \
		git status --short; \
		exit 1; \
	fi
	@echo "$(COLOR_GREEN)✓ Ready for release$(COLOR_RESET)"

.PHONY: release-main
## release-main: Release main module (interactive)
release-main: release-prepare
	@echo "$(COLOR_GREEN)Releasing main module...$(COLOR_RESET)"
	@read -p "Enter version (e.g., 1.0.0): " VERSION; \
	if [ -z "$$VERSION" ]; then \
		echo "$(COLOR_RED)Version required$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	./scripts/release-modules.sh $$VERSION

.PHONY: release-all
## release-all: Release all modules with same version (interactive)
release-all: release-prepare
	@echo "$(COLOR_GREEN)Releasing all modules...$(COLOR_RESET)"
	@read -p "Enter version (e.g., 1.0.0): " VERSION; \
	if [ -z "$$VERSION" ]; then \
		echo "$(COLOR_RED)Version required$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	./scripts/release-modules.sh $$VERSION all

.PHONY: release-extensions
## release-extensions: Release specific extensions (interactive)
release-extensions: release-prepare
	@echo "$(COLOR_GREEN)Available extensions: graphql, grpc, hls, kafka, mqtt$(COLOR_RESET)"
	@read -p "Enter version (e.g., 1.0.0): " VERSION; \
	if [ -z "$$VERSION" ]; then \
		echo "$(COLOR_RED)Version required$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	read -p "Enter extensions (comma-separated, e.g., grpc,kafka): " EXTS; \
	if [ -z "$$EXTS" ]; then \
		echo "$(COLOR_RED)Extensions required$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	./scripts/release-modules.sh $$VERSION $$EXTS

# ==============================================================================
# Binary Distribution (Multi-Platform)
# ==============================================================================

.PHONY: dist-local
## dist-local: Build binaries for all platforms locally (without publishing)
dist-local:
	@echo "$(COLOR_GREEN)Building multi-platform binaries...$(COLOR_RESET)"
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "$(COLOR_YELLOW)Installing goreleaser...$(COLOR_RESET)"; \
		go install github.com/goreleaser/goreleaser@latest; \
	fi
	@goreleaser check
	@goreleaser release --snapshot --clean --skip=publish
	@echo "$(COLOR_GREEN)✓ Binaries built in dist/$(COLOR_RESET)"
	@ls -lh dist/ | grep -E "\.(tar\.gz|zip)$$"

.PHONY: dist-verify
## dist-verify: Verify GoReleaser configuration
dist-verify:
	@echo "$(COLOR_GREEN)Verifying GoReleaser configuration...$(COLOR_RESET)"
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "$(COLOR_RED)goreleaser not installed. Run 'make install-tools'$(COLOR_RESET)"; \
		exit 1; \
	fi
	@goreleaser check
	@echo "$(COLOR_GREEN)✓ Configuration is valid$(COLOR_RESET)"

.PHONY: dist-clean
## dist-clean: Clean distribution artifacts
dist-clean:
	@echo "$(COLOR_GREEN)Cleaning distribution artifacts...$(COLOR_RESET)"
	@rm -rf dist/
	@echo "$(COLOR_GREEN)✓ Distribution artifacts cleaned$(COLOR_RESET)"

# ==============================================================================
# Package Manager Integration
# ==============================================================================

.PHONY: brew-tap-check
## brew-tap-check: Check Homebrew tap configuration
brew-tap-check:
	@echo "$(COLOR_GREEN)Checking Homebrew tap configuration...$(COLOR_RESET)"
	@if grep -q "homebrew-tap" .goreleaser.yml; then \
		echo "$(COLOR_GREEN)✓ Homebrew tap configured in .goreleaser.yml$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)⚠ Homebrew tap not configured$(COLOR_RESET)"; \
	fi

.PHONY: scoop-check
## scoop-check: Check Scoop bucket configuration
scoop-check:
	@echo "$(COLOR_GREEN)Checking Scoop bucket configuration...$(COLOR_RESET)"
	@if grep -q "scoop-bucket" .goreleaser.yml; then \
		echo "$(COLOR_GREEN)✓ Scoop bucket configured in .goreleaser.yml$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)⚠ Scoop bucket not configured$(COLOR_RESET)"; \
	fi

.PHONY: pkg-managers-check
## pkg-managers-check: Check all package manager configurations
pkg-managers-check: brew-tap-check scoop-check
	@echo ""
	@echo "$(COLOR_BLUE)Package manager configuration status:$(COLOR_RESET)"
	@echo "  - Homebrew: Check output above"
	@echo "  - Scoop: Check output above"
	@echo "  - Docker: See .goreleaser.yml dockers section"
	@echo "  - Snapcraft: See .goreleaser.yml snapcrafts section"
	@echo "  - AUR: See .goreleaser.yml aurs section"

# ==============================================================================
# Conventional Commits Automation
# ==============================================================================

.PHONY: commit-check
## commit-check: Validate last commit message follows conventional commits
commit-check:
	@echo "$(COLOR_GREEN)Checking last commit message...$(COLOR_RESET)"
	@if ! command -v npx >/dev/null 2>&1; then \
		echo "$(COLOR_RED)npx not found. Install Node.js first.$(COLOR_RESET)"; \
		exit 1; \
	fi
	@COMMIT_MSG=$$(git log -1 --pretty=%B); \
	echo "$$COMMIT_MSG" | npx --yes @commitlint/cli@latest --config .github/workflows/commitlint.config.js || \
	(echo "$(COLOR_RED)Commit message does not follow conventional commits format$(COLOR_RESET)" && exit 1)

.PHONY: commit-types
## commit-types: Show conventional commit types
commit-types:
	@echo "$(COLOR_BLUE)Conventional Commit Types:$(COLOR_RESET)"
	@echo ""
	@echo "  $(COLOR_GREEN)feat:$(COLOR_RESET)      New feature (triggers minor version bump)"
	@echo "  $(COLOR_GREEN)fix:$(COLOR_RESET)       Bug fix (triggers patch version bump)"
	@echo "  $(COLOR_GREEN)docs:$(COLOR_RESET)      Documentation only"
	@echo "  $(COLOR_GREEN)style:$(COLOR_RESET)     Code style changes (formatting, etc.)"
	@echo "  $(COLOR_GREEN)refactor:$(COLOR_RESET)  Code refactoring"
	@echo "  $(COLOR_GREEN)perf:$(COLOR_RESET)      Performance improvement"
	@echo "  $(COLOR_GREEN)test:$(COLOR_RESET)      Adding or updating tests"
	@echo "  $(COLOR_GREEN)chore:$(COLOR_RESET)     Maintenance tasks"
	@echo "  $(COLOR_GREEN)ci:$(COLOR_RESET)        CI/CD changes"
	@echo "  $(COLOR_GREEN)build:$(COLOR_RESET)     Build system changes"
	@echo "  $(COLOR_GREEN)revert:$(COLOR_RESET)    Revert previous commit"
	@echo ""
	@echo "  $(COLOR_YELLOW)Breaking changes:$(COLOR_RESET) Add '!' or 'BREAKING CHANGE:' footer"
	@echo ""
	@echo "$(COLOR_BLUE)Examples:$(COLOR_RESET)"
	@echo "  feat(router): add middleware support"
	@echo "  fix(database): resolve connection timeout"
	@echo "  feat!: remove deprecated API"
	@echo "  docs: update installation guide"

.PHONY: commit-template
## commit-template: Set up git commit message template
commit-template:
	@echo "$(COLOR_GREEN)Setting up commit message template...$(COLOR_RESET)"
	@printf '%s\n' \
		'# <type>(<scope>): <subject>' \
		'#' \
		'# [optional body]' \
		'#' \
		'# [optional footer(s)]' \
		'#' \
		'# Types: feat, fix, docs, style, refactor, perf, test, chore, ci, build, revert' \
		'# Scopes: core, grpc, kafka, graphql, hls, mqtt, cli, docs, etc.' \
		'#' \
		'# Examples:' \
		'#   feat(router): add middleware support' \
		'#   fix(database): resolve connection timeout' \
		'#   feat!: remove deprecated API' \
		'#' \
		'# Breaking changes: Add ! after type or BREAKING CHANGE: in footer' \
		> .git/commit-template
	@git config commit.template .git/commit-template
	@echo "$(COLOR_GREEN)✓ Commit template configured$(COLOR_RESET)"

# ==============================================================================
# Security Scanning
# ==============================================================================

.PHONY: security-scan
## security-scan: Run comprehensive security scan
security-scan:
	@echo "$(COLOR_GREEN)Running security scans...$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BLUE)1. gosec - Security audit$(COLOR_RESET)"
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -exclude-dir=bk -exclude-dir=vendor -fmt=text ./...; \
	else \
		echo "$(COLOR_YELLOW)Installing gosec...$(COLOR_RESET)"; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
		gosec -exclude-dir=bk -exclude-dir=vendor -fmt=text ./...; \
	fi
	@echo ""
	@echo "$(COLOR_BLUE)2. govulncheck - Vulnerability scan$(COLOR_RESET)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "$(COLOR_YELLOW)Installing govulncheck...$(COLOR_RESET)"; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
		govulncheck ./...; \
	fi
	@echo ""
	@echo "$(COLOR_GREEN)✓ Security scans completed$(COLOR_RESET)"

.PHONY: security-deps
## security-deps: Check for known vulnerabilities in dependencies
security-deps:
	@echo "$(COLOR_GREEN)Checking dependencies for vulnerabilities...$(COLOR_RESET)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck -test ./...; \
	else \
		echo "$(COLOR_YELLOW)Installing govulncheck...$(COLOR_RESET)"; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
		govulncheck -test ./...; \
	fi

.PHONY: security-audit
## security-audit: Full security audit (gosec + vulncheck + mod verify)
security-audit:
	@echo "$(COLOR_GREEN)Running full security audit...$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BLUE)1. Verifying module integrity$(COLOR_RESET)"
	@go mod verify
	@echo ""
	@echo "$(COLOR_BLUE)2. Running gosec$(COLOR_RESET)"
	@$(MAKE) security 2>/dev/null || true
	@echo ""
	@echo "$(COLOR_BLUE)3. Running govulncheck$(COLOR_RESET)"
	@$(MAKE) vuln-check 2>/dev/null || true
	@echo ""
	@echo "$(COLOR_GREEN)✓ Security audit completed$(COLOR_RESET)"

# ==============================================================================
# Multi-Platform Testing
# ==============================================================================

.PHONY: test-platforms
## test-platforms: Test on multiple platforms (requires Docker)
test-platforms:
	@echo "$(COLOR_GREEN)Testing on multiple platforms...$(COLOR_RESET)"
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "$(COLOR_RED)Docker not installed$(COLOR_RESET)"; \
		exit 1; \
	fi
	@echo ""
	@echo "$(COLOR_BLUE)Testing on Linux (Alpine)$(COLOR_RESET)"
	@docker run --rm -v $(PWD):/app -w /app golang:1.24-alpine \
		sh -c "apk add --no-cache git && go test -v ./... | grep -v '/bk/'"
	@echo ""
	@echo "$(COLOR_BLUE)Testing on Linux (Ubuntu)$(COLOR_RESET)"
	@docker run --rm -v $(PWD):/app -w /app golang:1.24 \
		sh -c "go test -v ./... | grep -v '/bk/'"
	@echo ""
	@echo "$(COLOR_GREEN)✓ Multi-platform tests completed$(COLOR_RESET)"

.PHONY: test-versions
## test-versions: Test with multiple Go versions (requires Docker)
test-versions:
	@echo "$(COLOR_GREEN)Testing with multiple Go versions...$(COLOR_RESET)"
	@for version in 1.22 1.23 1.24; do \
		echo ""; \
		echo "$(COLOR_BLUE)Testing with Go $$version$(COLOR_RESET)"; \
		docker run --rm -v $(PWD):/app -w /app golang:$$version \
			sh -c "go test -short ./... | grep -v '/bk/'" || exit 1; \
	done
	@echo ""
	@echo "$(COLOR_GREEN)✓ Multi-version tests completed$(COLOR_RESET)"

.PHONY: test-matrix
## test-matrix: Run comprehensive test matrix (platforms + versions)
test-matrix:
	@echo "$(COLOR_GREEN)Running comprehensive test matrix...$(COLOR_RESET)"
	@$(MAKE) test-versions
	@$(MAKE) test-platforms
	@echo "$(COLOR_GREEN)✓ Test matrix completed$(COLOR_RESET)"

# ==============================================================================
# Workflow Standardization
# ==============================================================================

.PHONY: workflows-check
## workflows-check: Validate GitHub workflows
workflows-check:
	@echo "$(COLOR_GREEN)Checking GitHub workflows...$(COLOR_RESET)"
	@if command -v actionlint >/dev/null 2>&1; then \
		actionlint .github/workflows/*.yml; \
	else \
		echo "$(COLOR_YELLOW)actionlint not installed. Install with:$(COLOR_RESET)"; \
		echo "  go install github.com/rhysd/actionlint/cmd/actionlint@latest"; \
	fi

.PHONY: workflows-list
## workflows-list: List all GitHub workflows
workflows-list:
	@echo "$(COLOR_BLUE)GitHub Workflows:$(COLOR_RESET)"
	@for workflow in .github/workflows/*.yml; do \
		NAME=$$(grep "^name:" $$workflow | head -1 | cut -d':' -f2 | xargs); \
		FILE=$$(basename $$workflow); \
		echo "  - $$NAME ($$FILE)"; \
	done

.PHONY: workflows-update
## workflows-update: Update workflow action versions
workflows-update:
	@echo "$(COLOR_GREEN)Updating workflow action versions...$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)Note: This requires manual review of changes$(COLOR_RESET)"
	@echo ""
	@echo "Common updates needed:"
	@echo "  - actions/checkout@v3 → actions/checkout@v4"
	@echo "  - actions/setup-go@v4 → actions/setup-go@v5"
	@echo "  - actions/cache@v3 → actions/cache@v4"
	@echo ""
	@echo "Use: sed -i '' 's/actions\/checkout@v3/actions\/checkout@v4/g' .github/workflows/*.yml"

# ==============================================================================
# CI/CD Comprehensive Checks
# ==============================================================================

.PHONY: ci-full
## ci-full: Run all CI checks (format, lint, test, security, build)
ci-full: fmt-check vet tidy-check lint test security-scan build
	@echo "$(COLOR_GREEN)✓ All CI checks passed$(COLOR_RESET)"

.PHONY: ci-pre-release
## ci-pre-release: Run all pre-release checks
ci-pre-release: ci-full modules-check dist-verify
	@echo "$(COLOR_GREEN)✓ Pre-release checks passed$(COLOR_RESET)"

.PHONY: ci-quick
## ci-quick: Quick CI checks (format, lint, test-short)
ci-quick: fmt lint test-short
	@echo "$(COLOR_GREEN)✓ Quick CI checks passed$(COLOR_RESET)"

.PHONY: ci-status
## ci-status: Show CI/CD status and configuration
ci-status:
	@echo "$(COLOR_BLUE)=== CI/CD Status ===$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BLUE)Modules:$(COLOR_RESET)"
	@./scripts/check-module-versions.sh 2>&1 | head -20 || true
	@echo ""
	@echo "$(COLOR_BLUE)Tools:$(COLOR_RESET)"
	@$(MAKE) check-tools 2>/dev/null | grep "✓\|✗"
	@echo ""
	@echo "$(COLOR_BLUE)Workflows:$(COLOR_RESET)"
	@COUNT=$$(ls -1 .github/workflows/*.yml 2>/dev/null | wc -l); echo "  Total workflows: $$COUNT"
	@echo ""
	@echo "$(COLOR_BLUE)Git Status:$(COLOR_RESET)"
	@git status --short || echo "  Clean"
	@echo ""
	@echo "$(COLOR_BLUE)Latest Tags:$(COLOR_RESET)"
	@git tag -l "v*" --sort=-version:refname | head -5

# ==============================================================================
# Documentation Generation
# ==============================================================================

.PHONY: docs-ci
## docs-ci: Generate CI/CD documentation summary
docs-ci:
	@echo "$(COLOR_GREEN)Generating CI/CD documentation summary...$(COLOR_RESET)"
	@echo "Forge CI/CD Status Report" > CI_CD_STATUS.txt
	@echo "Generated: $$(date)" >> CI_CD_STATUS.txt
	@echo "" >> CI_CD_STATUS.txt
	@echo "MODULES:" >> CI_CD_STATUS.txt
	@./scripts/check-module-versions.sh 2>&1 >> CI_CD_STATUS.txt || true
	@echo "" >> CI_CD_STATUS.txt
	@echo "WORKFLOWS:" >> CI_CD_STATUS.txt
	@ls -1 .github/workflows/*.yml | xargs -I {} basename {} | sed 's/^/  - /' >> CI_CD_STATUS.txt
	@echo "" >> CI_CD_STATUS.txt
	@echo "TOOLS:" >> CI_CD_STATUS.txt
	@$(MAKE) check-tools 2>/dev/null >> CI_CD_STATUS.txt || true
	@echo "" >> CI_CD_STATUS.txt
	@echo "LATEST RELEASES:" >> CI_CD_STATUS.txt
	@git tag -l "v*" --sort=-version:refname | head -5 | sed 's/^/  - /' >> CI_CD_STATUS.txt
	@echo "" >> CI_CD_STATUS.txt
	@echo "For detailed documentation, see:" >> CI_CD_STATUS.txt
	@echo "  - .github/EXECUTIVE_SUMMARY.md" >> CI_CD_STATUS.txt
	@echo "  - .github/QUICK_REFERENCE.md" >> CI_CD_STATUS.txt
	@echo "  - .github/CI_CD_REVIEW.md" >> CI_CD_STATUS.txt
	@cat CI_CD_STATUS.txt
	@echo ""
	@echo "$(COLOR_GREEN)✓ Report saved to CI_CD_STATUS.txt$(COLOR_RESET)"

# ==============================================================================
# Quick Aliases for CI/CD
# ==============================================================================

.PHONY: release
## release: Alias for release-prepare (check before release)
release: release-prepare

.PHONY: scan
## scan: Alias for security-scan
scan: security-scan

.PHONY: dist
## dist: Alias for dist-local
dist: dist-local

.PHONY: matrix
## matrix: Alias for test-matrix
matrix: test-matrix

# ==============================================================================
# Default target
# ==============================================================================

.DEFAULT_GOAL := help

