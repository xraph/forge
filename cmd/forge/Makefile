# Forge CLI Makefile

# Binary name
BINARY_NAME=forge
OUTPUT_DIR=../../bin

# Build variables
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: all build install clean test help

all: build

## build: Build the CLI binary
build:
	@echo "Building Forge CLI..."
	@mkdir -p $(OUTPUT_DIR)
	go build $(LDFLAGS) -o $(OUTPUT_DIR)/$(BINARY_NAME) .
	@echo "✓ Built: $(OUTPUT_DIR)/$(BINARY_NAME)"

## install: Install the CLI to GOPATH/bin
install:
	@echo "Installing Forge CLI..."
	go install $(LDFLAGS) .
	@echo "✓ Installed to: $(shell go env GOPATH)/bin/$(BINARY_NAME)"

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(OUTPUT_DIR)/$(BINARY_NAME)
	@echo "✓ Cleaned"

## test: Run tests
test:
	@echo "Running tests..."
	go test -v ./...

## lint: Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

## dev: Build and run the CLI
dev: build
	@$(OUTPUT_DIR)/$(BINARY_NAME) doctor

## help: Show this help message
help:
	@echo "Forge CLI Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

