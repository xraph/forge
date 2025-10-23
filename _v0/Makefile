# Forge Framework Makefile
# Build system for the unified Forge CLI tool

# Variables
BINARY_NAME=forge
BINARY_PATH=cmd/$(BINARY_NAME)
BUILD_DIR=bin
DIST_DIR=dist
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# Go variables
GO=go
GOOS=$(shell $(GO) env GOOS)
GOARCH=$(shell $(GO) env GOARCH)
GOVERSION=$(shell $(GO) version | cut -d' ' -f3)

# Directories
PKG_DIR=./pkg/...
CMD_DIR=./cmd/...
INTERNAL_DIR=./internal/...

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m # No Color

# Default target
.DEFAULT_GOAL := build

# Help target
.PHONY: help
help: ## Show this help message
	@echo "$(GREEN)Forge Framework Build System$(NC)"
	@echo "$(BLUE)Version: $(VERSION)$(NC)"
	@echo "$(BLUE)Go Version: $(GOVERSION)$(NC)"
	@echo ""
	@echo "$(YELLOW)Available targets:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

# Build targets
.PHONY: build
build: clean deps ## Build the forge CLI for current platform
	@echo "$(YELLOW)Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./$(BINARY_PATH)
	@echo "$(GREEN)✓ Built $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

.PHONY: build-all
build-all: clean deps ## Build for all supported platforms
	@echo "$(YELLOW)Building for all platforms...$(NC)"
	@mkdir -p $(DIST_DIR)

	# Linux AMD64
	@echo "$(BLUE)Building for linux/amd64...$(NC)"
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./$(BINARY_PATH)

	# Linux ARM64
	@echo "$(BLUE)Building for linux/arm64...$(NC)"
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 ./$(BINARY_PATH)

	# macOS AMD64
	@echo "$(BLUE)Building for darwin/amd64...$(NC)"
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(BINARY_PATH)

	# macOS ARM64 (Apple Silicon)
	@echo "$(BLUE)Building for darwin/arm64...$(NC)"
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(BINARY_PATH)

	# Windows AMD64
	@echo "$(BLUE)Building for windows/amd64...$(NC)"
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(BINARY_PATH)

	@echo "$(GREEN)✓ Built all platform binaries in $(DIST_DIR)/$(NC)"

.PHONY: build-debug
build-debug: clean deps ## Build with debug symbols and race detector
	@echo "$(YELLOW)Building debug version...$(NC)"
	@mkdir -p $(BUILD_DIR)
	$(GO) build -race -gcflags="all=-N -l" -o $(BUILD_DIR)/$(BINARY_NAME)-debug ./$(BINARY_PATH)
	@echo "$(GREEN)✓ Built debug binary$(NC)"

.PHONY: install
install: build ## Install the forge CLI to $GOPATH/bin
	@echo "$(YELLOW)Installing $(BINARY_NAME)...$(NC)"
	$(GO) install -ldflags "$(LDFLAGS)" ./$(BINARY_PATH)
	@echo "$(GREEN)✓ Installed $(BINARY_NAME) to $(shell $(GO) env GOPATH)/bin$(NC)"

# Development targets
.PHONY: dev
dev: ## Start development mode with hot reload
	@echo "$(YELLOW)Starting development server...$(NC)"
	@if command -v air >/dev/null 2>&1; then \
		air -c .air.toml; \
	else \
		echo "$(RED)Error: 'air' not found. Install it with: go install github.com/cosmtrek/air@latest$(NC)"; \
		exit 1; \
	fi

.PHONY: run
run: build ## Build and run the forge CLI
	@echo "$(YELLOW)Running $(BINARY_NAME)...$(NC)"
	./$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

# Dependencies and modules
.PHONY: deps
deps: ## Download and verify dependencies
	@echo "$(YELLOW)Downloading dependencies...$(NC)"
	$(GO) mod download
	$(GO) mod verify

.PHONY: deps-update
deps-update: ## Update all dependencies to latest versions
	@echo "$(YELLOW)Updating dependencies...$(NC)"
	$(GO) get -u ./...
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: deps-check
deps-check: ## Check for outdated dependencies
	@echo "$(YELLOW)Checking for outdated dependencies...$(NC)"
	@if command -v go-mod-outdated >/dev/null 2>&1; then \
		$(GO) list -u -m -json all | go-mod-outdated -update -direct; \
	else \
		echo "$(BLUE)Install go-mod-outdated with: go install github.com/psampaz/go-mod-outdated@latest$(NC)"; \
		$(GO) list -u -m all; \
	fi

# Testing targets
.PHONY: test
test: ## Run all tests
	@echo "$(YELLOW)Running tests...$(NC)"
	$(GO) test -v ./...

.PHONY: test-race
test-race: ## Run tests with race detector
	@echo "$(YELLOW)Running tests with race detector...$(NC)"
	$(GO) test -race -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "$(YELLOW)Running tests with coverage...$(NC)"
	@mkdir -p coverage
	$(GO) test -coverprofile=coverage/coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "$(GREEN)✓ Coverage report generated: coverage/coverage.html$(NC)"

.PHONY: test-integration
test-integration: ## Run integration tests
	@echo "$(YELLOW)Running integration tests...$(NC)"
	$(GO) test -tags=integration -v ./...

.PHONY: benchmark
benchmark: ## Run benchmarks
	@echo "$(YELLOW)Running benchmarks...$(NC)"
	$(GO) test -bench=. -benchmem ./...

# Code quality targets
.PHONY: lint
lint: ## Run linters
	@echo "$(YELLOW)Running linters...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "$(RED)Error: golangci-lint not found$(NC)"; \
		echo "$(BLUE)Install it with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin$(NC)"; \
		exit 1; \
	fi

.PHONY: fmt
fmt: ## Format code
	@echo "$(YELLOW)Formatting code...$(NC)"
	$(GO) fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	fi

.PHONY: vet
vet: ## Run go vet
	@echo "$(YELLOW)Running go vet...$(NC)"
	$(GO) vet ./...

.PHONY: sec
sec: ## Run security scanner
	@echo "$(YELLOW)Running security scanner...$(NC)"
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "$(BLUE)Install gosec with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest$(NC)"; \
	fi

.PHONY: check
check: fmt vet lint test ## Run all code quality checks

# Documentation targets
.PHONY: docs
docs: ## Generate documentation
	@echo "$(YELLOW)Generating documentation...$(NC)"
	@if command -v godoc >/dev/null 2>&1; then \
		echo "$(BLUE)Starting documentation server at http://localhost:6060$(NC)"; \
		godoc -http=:6060; \
	else \
		echo "$(BLUE)Install godoc with: go install golang.org/x/tools/cmd/godoc@latest$(NC)"; \
	fi

.PHONY: docs-generate
docs-generate: ## Generate static documentation
	@echo "$(YELLOW)Generating static documentation...$(NC)"
	@mkdir -p docs/api
	@if command -v pkgsite >/dev/null 2>&1; then \
		echo "$(BLUE)Use 'pkgsite' to serve documentation$(NC)"; \
	else \
		echo "$(BLUE)Install pkgsite with: go install golang.org/x/pkgsite/cmd/pkgsite@latest$(NC)"; \
	fi

# Release targets
.PHONY: release
release: clean test lint build-all ## Create a release build
	@echo "$(YELLOW)Creating release archives...$(NC)"
	@mkdir -p $(DIST_DIR)/archives

	# Create tar.gz archives for Unix systems
	@for binary in $(shell ls $(DIST_DIR)/$(BINARY_NAME)-*); do \
		if [[ $$binary != *.exe ]]; then \
			platform=$$(basename $$binary | sed 's/$(BINARY_NAME)-//'); \
			echo "$(BLUE)Creating archive for $$platform...$(NC)"; \
			tar -czf $(DIST_DIR)/archives/$(BINARY_NAME)-$$platform.tar.gz -C $(DIST_DIR) $$(basename $$binary); \
		fi; \
	done

	# Create zip archives for Windows
	@for binary in $(shell ls $(DIST_DIR)/$(BINARY_NAME)-*.exe 2>/dev/null || true); do \
		if [[ $$binary == *.exe ]]; then \
			platform=$$(basename $$binary .exe | sed 's/$(BINARY_NAME)-//'); \
			echo "$(BLUE)Creating zip archive for $$platform...$(NC)"; \
			cd $(DIST_DIR) && zip archives/$(BINARY_NAME)-$$platform.zip $$(basename $$binary) && cd ..; \
		fi; \
	done

	@echo "$(GREEN)✓ Release archives created in $(DIST_DIR)/archives/$(NC)"

.PHONY: changelog
changelog: ## Generate changelog
	@echo "$(YELLOW)Generating changelog...$(NC)"
	@if command -v git-chglog >/dev/null 2>&1; then \
		git-chglog -o CHANGELOG.md; \
	else \
		echo "$(BLUE)Install git-chglog with: go install github.com/git-chglog/git-chglog/cmd/git-chglog@latest$(NC)"; \
		echo "$(BLUE)Or manually create CHANGELOG.md$(NC)"; \
	fi

# Docker targets
.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "$(YELLOW)Building Docker image...$(NC)"
	docker build -t forge:$(VERSION) -t forge:latest .

.PHONY: docker-run
docker-run: docker-build ## Run Docker container
	@echo "$(YELLOW)Running Docker container...$(NC)"
	docker run --rm -it forge:latest $(ARGS)

# Cleanup targets
.PHONY: clean
clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -rf coverage
	@$(GO) clean -cache
	@echo "$(GREEN)✓ Cleaned build artifacts$(NC)"

.PHONY: clean-deps
clean-deps: ## Clean dependency cache
	@echo "$(YELLOW)Cleaning dependency cache...$(NC)"
	$(GO) clean -modcache

.PHONY: clean-all
clean-all: clean clean-deps ## Clean everything

# Utility targets
.PHONY: version
version: ## Show version information
	@echo "$(GREEN)Forge CLI Build Information$(NC)"
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go Version: $(GOVERSION)"
	@echo "GOOS:       $(GOOS)"
	@echo "GOARCH:     $(GOARCH)"

.PHONY: env
env: ## Show build environment
	@echo "$(GREEN)Build Environment$(NC)"
	@echo "GO:         $(shell which $(GO))"
	@echo "GOPATH:     $(shell $(GO) env GOPATH)"
	@echo "GOROOT:     $(shell $(GO) env GOROOT)"
	@echo "GOOS:       $(GOOS)"
	@echo "GOARCH:     $(GOARCH)"
	@echo "CGO:        $(shell $(GO) env CGO_ENABLED)"

.PHONY: deps-graph
deps-graph: ## Generate dependency graph
	@echo "$(YELLOW)Generating dependency graph...$(NC)"
	@if command -v go-mod-graph-chart >/dev/null 2>&1; then \
		go mod graph | go-mod-graph-chart -o deps-graph.svg; \
		echo "$(GREEN)✓ Dependency graph saved to deps-graph.svg$(NC)"; \
	else \
		echo "$(BLUE)Install go-mod-graph-chart with: go install github.com/PaulXu-cn/go-mod-graph-chart/gmgc@latest$(NC)"; \
	fi

# IDE support
.PHONY: vscode
vscode: ## Setup VS Code workspace
	@echo "$(YELLOW)Setting up VS Code workspace...$(NC)"
	@mkdir -p .vscode
	@echo '{"go.toolsGopath": "$(shell $(GO) env GOPATH)"}' > .vscode/settings.json
	@echo "$(GREEN)✓ VS Code workspace configured$(NC)"

# Performance profiling
.PHONY: profile-cpu
profile-cpu: build ## Run CPU profiling
	@echo "$(YELLOW)Running CPU profiling...$(NC)"
	./$(BUILD_DIR)/$(BINARY_NAME) --cpuprofile=cpu.prof $(ARGS)
	$(GO) tool pprof cpu.prof

.PHONY: profile-mem
profile-mem: build ## Run memory profiling
	@echo "$(YELLOW)Running memory profiling...$(NC)"
	./$(BUILD_DIR)/$(BINARY_NAME) --memprofile=mem.prof $(ARGS)
	$(GO) tool pprof mem.prof

# Check if required tools are installed
.PHONY: check-tools
check-tools: ## Check if required development tools are installed
	@echo "$(YELLOW)Checking required tools...$(NC)"
	@echo "$(BLUE)Go:$(NC) $(shell $(GO) version 2>/dev/null || echo "❌ Not installed")"
	@echo "$(BLUE)golangci-lint:$(NC) $(shell golangci-lint version 2>/dev/null || echo "❌ Not installed")"
	@echo "$(BLUE)air:$(NC) $(shell air -v 2>/dev/null || echo "❌ Not installed")"
	@echo "$(BLUE)gosec:$(NC) $(shell gosec -version 2>/dev/null || echo "❌ Not installed")"
	@echo "$(BLUE)godoc:$(NC) $(shell command -v godoc >/dev/null 2>&1 && echo "✓ Installed" || echo "❌ Not installed")"