.PHONY: build test lint install clean help

# Build the binary
build:
	@echo "Building zap..."
	@go build -o zap ./cmd/zap
	@echo "✅ Build complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@golangci-lint run

# Install the binary
install:
	@echo "Installing zap..."
	@go install ./cmd/zap
	@echo "✅ Installation complete"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f zap
	@go clean
	@echo "✅ Clean complete"

# Run all checks (test + lint)
check: test lint

# Show help
help:
	@echo "Available targets:"
	@echo "  make build    - Build the binary"
	@echo "  make test     - Run tests"
	@echo "  make lint     - Run linter (requires golangci-lint)"
	@echo "  make install  - Install the binary"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make check    - Run tests and linter"

