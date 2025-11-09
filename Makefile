.PHONY: build install test version

# Build the binary with version from git tags
build:
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	echo "Building with version: $$VERSION"; \
	go build -ldflags "-X github.com/hugoev/zap/internal/version.Version=$$VERSION -X github.com/hugoev/zap/internal/version.Commit=$$COMMIT -X github.com/hugoev/zap/internal/version.Date=$$DATE" -o zap ./cmd/zap

# Install with version from git tags
install:
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	go install -ldflags "-X github.com/hugoev/zap/internal/version.Version=$$VERSION -X github.com/hugoev/zap/internal/version.Commit=$$COMMIT -X github.com/hugoev/zap/internal/version.Date=$$DATE" ./cmd/zap

# Show current version
version:
	@go run -ldflags "-X github.com/hugoev/zap/internal/version.Version=$$(git describe --tags --always 2>/dev/null || echo 'dev')" ./cmd/zap version

