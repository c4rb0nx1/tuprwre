.PHONY: build test clean install fmt lint

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

# Install binary to GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME) 2>/dev/null || \
		cp $(BUILD_DIR)/$(BINARY_NAME) ~/go/bin/$(BINARY_NAME) 2>/dev/null || \
		echo "Please ensure ~/go/bin is in your PATH"
	@echo "Install complete"

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
	@echo "  install       - Install binary to GOPATH/bin"
	@echo "  fmt           - Format Go code"
	@echo "  lint          - Run linter"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  verify        - Verify dependencies"
	@echo "  dev           - Build development version"
	@echo "  run           - Build and run the binary"
	@echo "  help          - Show this help"
