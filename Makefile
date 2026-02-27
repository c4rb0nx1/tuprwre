.PHONY: all build build-all test test-coverage stress-output-race clean install install-system uninstall fmt lint deps verify dev run help

# Binary name
BINARY_NAME=tuprwre

# Build directory
BUILD_DIR=./build

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Install directories
# BIN_DIR can be overridden explicitly: make install BIN_DIR=/custom/bin
BIN_DIR ?=
SYSTEM_BIN_DIR ?= /usr/local/bin

# Build flags
LDFLAGS=-ldflags "-s -w"
BUILDFLAGS=-buildvcs=false

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tuprwre
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	# Linux AMD64
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/tuprwre
	# Linux ARM64
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/tuprwre
	# Darwin AMD64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/tuprwre
	# Darwin ARM64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(BUILDFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/tuprwre
	@echo "Multi-platform build complete"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

stress-output-race:
	@./scripts/stress-run-output-race.sh ubuntu:22.04 50

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

# Install binary to Go bin dir (GOBIN or GOPATH/bin)
install: build
	@bin_dir="$(BIN_DIR)"; \
	if [ -z "$$bin_dir" ]; then \
		bin_dir="$$($(GOCMD) env GOBIN 2>/dev/null)"; \
	fi; \
	if [ -z "$$bin_dir" ]; then \
		gopath="$$($(GOCMD) env GOPATH 2>/dev/null)"; \
		if [ -n "$$gopath" ]; then \
			bin_dir="$$gopath/bin"; \
		else \
			bin_dir="$$HOME/go/bin"; \
		fi; \
	fi; \
	echo "Installing $(BINARY_NAME) to $$bin_dir..."; \
	mkdir -p "$$bin_dir"; \
	install -m 0755 "$(BUILD_DIR)/$(BINARY_NAME)" "$$bin_dir/$(BINARY_NAME)"; \
	echo "Install complete: $$bin_dir/$(BINARY_NAME)"

# Install binary to system path (usually requires sudo)
install-system: build
	@echo "Installing $(BINARY_NAME) to $(SYSTEM_BIN_DIR)..."
	@install -m 0755 "$(BUILD_DIR)/$(BINARY_NAME)" "$(SYSTEM_BIN_DIR)/$(BINARY_NAME)"
	@echo "System install complete: $(SYSTEM_BIN_DIR)/$(BINARY_NAME)"

# Remove installed binary from user/system locations if present
uninstall:
	@bin_dir="$(BIN_DIR)"; \
	if [ -z "$$bin_dir" ]; then \
		bin_dir="$$($(GOCMD) env GOBIN 2>/dev/null)"; \
	fi; \
	if [ -z "$$bin_dir" ]; then \
		gopath="$$($(GOCMD) env GOPATH 2>/dev/null)"; \
		if [ -n "$$gopath" ]; then \
			bin_dir="$$gopath/bin"; \
		else \
			bin_dir="$$HOME/go/bin"; \
		fi; \
	fi; \
	echo "Removing installed $(BINARY_NAME) binary..."; \
	rm -f "$$bin_dir/$(BINARY_NAME)" 2>/dev/null || true; \
	rm -f "$(SYSTEM_BIN_DIR)/$(BINARY_NAME)" 2>/dev/null || true; \
	echo "Uninstall complete (checked: $$bin_dir, $(SYSTEM_BIN_DIR))"

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -w .

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

# Development build (with debug info)
dev:
	@echo "Building development version..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(BUILDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tuprwre
	@echo "Development build complete"

# Run the binary
run: build
	$(BUILD_DIR)/$(BINARY_NAME)

# Show help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  build-all     - Build for multiple platforms"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  clean         - Clean build artifacts"
	@echo "  install       - Install binary to GOBIN or GOPATH/bin"
	@echo "  install-system - Install binary to /usr/local/bin (sudo may be needed)"
	@echo "  uninstall     - Remove installed binary from user/system bin dirs"
	@echo "  fmt           - Format Go code"
	@echo "  lint          - Run linter"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  verify        - Verify dependencies"
	@echo "  dev           - Build development version"
	@echo "  run           - Build and run the binary"
	@echo "  help          - Show this help"
