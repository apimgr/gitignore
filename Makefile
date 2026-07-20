.PHONY: help deps build dev test run clean docker docker-build docker-run docker-stop docker-test release

# Variables
BINARY_NAME=gitignore
PROJECTNAME := $(shell git remote get-url origin 2>/dev/null | sed -E 's|.*/([^/]+)(\.git)?$$|\1|' || basename "$$(pwd)")
PROJECTORG  := $(shell git remote get-url origin 2>/dev/null | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$$|\1|' || basename "$$(dirname "$$(pwd)")")

VERSION    := $(shell cat release.txt 2>/dev/null || echo "0.1.0")
COMMIT_ID  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitID=$(COMMIT_ID) -X main.BuildDate=$(BUILD_DATE)"

# Directories
BIN_DIR=./binaries
RELEASE_DIR=./release

# Platform-specific settings
PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64

# Docker toolchain
GO_CACHE  ?= $(HOME)/go/pkg/mod
GO_BUILD  ?= $(HOME)/.cache/go-build/$(PROJECTNAME)

DOCKER_MEM  ?= 4g
DOCKER_CPUS ?= 2

GO_DOCKER := docker run --rm \
	--name $(PROJECTNAME)-$$(tr -dc 'a-z0-9' </dev/urandom | head -c8) \
	--memory=$(DOCKER_MEM) --cpus=$(DOCKER_CPUS) \
	-v $(PWD):/app \
	-v $(GO_CACHE):/usr/local/share/go/pkg/mod \
	-v $(GO_BUILD):/usr/local/share/go/cache \
	-w /app \
	-e CGO_ENABLED=0 \
	-e GOFLAGS=-buildvcs=false \
	casjaysdev/go:latest

help: ## Show this help
	@echo "GitIgnore API Server - Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

deps: ## Download Go dependencies
	@echo "📦 Downloading dependencies..."
	@mkdir -p $(GO_CACHE) $(GO_BUILD)
	$(GO_DOCKER) go mod download
	$(GO_DOCKER) go mod tidy
	@echo "✅ Dependencies installed"

build: ## Build for current platform
	@echo "🔨 Building $(BINARY_NAME) for current platform..."
	@mkdir -p $(BIN_DIR) $(GO_CACHE) $(GO_BUILD)
	$(GO_DOCKER) go build -buildvcs=false -trimpath $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./src
	@echo "✅ Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-all: ## Build for all platforms
	@echo "🔨 Building for all platforms..."
	@mkdir -p $(BIN_DIR) $(GO_CACHE) $(GO_BUILD)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} ; \
		output=$(BIN_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH ; \
		if [ "$$GOOS" = "windows" ]; then output=$$output.exe; fi ; \
		echo "Building $$GOOS/$$GOARCH..." ; \
		$(GO_DOCKER) env GOOS=$$GOOS GOARCH=$$GOARCH go build -buildvcs=false -trimpath $(LDFLAGS) -o $$output ./src ; \
	done
	@echo "✅ All builds complete"

dev: ## Quick development build into a temp dir
	@mkdir -p $(GO_CACHE) $(GO_BUILD) "$${TMPDIR:-/tmp}/$(PROJECTORG)" && \
		BUILD_DIR=$$(mktemp -d "$${TMPDIR:-/tmp}/$(PROJECTORG)/$(PROJECTNAME)-XXXXXX") && \
		echo "Quick dev build..." && \
		$(GO_DOCKER) go build -buildvcs=false -o $$BUILD_DIR/$(BINARY_NAME) ./src && \
		echo "Built: $$BUILD_DIR/$(BINARY_NAME)"

test: ## Run tests
	@echo "🧪 Running tests..."
	@mkdir -p $(GO_CACHE) $(GO_BUILD)
	$(GO_DOCKER) go vet ./...
	$(GO_DOCKER) go test -v -race -cover ./...
	@echo "✅ Tests passed"

test-coverage: ## Run tests with coverage
	@echo "🧪 Running tests with coverage..."
	@mkdir -p $(GO_CACHE) $(GO_BUILD)
	$(GO_DOCKER) go test -v -coverprofile=coverage.out ./...
	$(GO_DOCKER) go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

run: build ## Build and run
	@echo "🚀 Starting server..."
	$(BIN_DIR)/$(BINARY_NAME) --dev --port 8080

run-dev: ## Run in development mode
	@echo "🚀 Starting server in dev mode..."
	@mkdir -p $(GO_CACHE) $(GO_BUILD)
	$(GO_DOCKER) go run ./src --dev --port 8080

docker: docker-build ## Build Docker image (alias for docker-build)

docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_ID=$(COMMIT_ID) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(BINARY_NAME):$(VERSION) \
		-t $(BINARY_NAME):latest \
		-f docker/Dockerfile \
		.
	@echo "✅ Docker image built: $(BINARY_NAME):$(VERSION)"

docker-run: ## Run Docker container
	@echo "🐳 Running Docker container..."
	docker run -d \
		--name $(BINARY_NAME) \
		-p 127.0.0.1:8080:8080 \
		-v $(PWD)/rootfs/config:/config \
		-v $(PWD)/rootfs/data:/data \
		-v $(PWD)/rootfs/logs:/logs \
		-e PORT=8080 \
		$(BINARY_NAME):latest
	@echo "✅ Container running: http://localhost:8080"
	@echo "📋 View credentials: cat rootfs/config/admin_credentials"

docker-stop: ## Stop Docker container
	@echo "🛑 Stopping Docker container..."
	docker stop $(BINARY_NAME) || true
	docker rm $(BINARY_NAME) || true
	@echo "✅ Container stopped"

docker-test: ## Test Docker build
	@echo "🧪 Testing Docker build..."
	./tests/test-docker.sh || echo "Test script not yet implemented"

release: build-all ## Create release artifacts
	@echo "📦 Creating release artifacts..."
	@mkdir -p $(RELEASE_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} ; \
		binary=$(BIN_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH ; \
		if [ "$$GOOS" = "windows" ]; then binary=$$binary.exe; fi ; \
		if [ -f "$$binary" ]; then \
			if [ "$$GOOS" = "windows" ]; then \
				zip -j $(RELEASE_DIR)/$(BINARY_NAME)-$(VERSION)-$$GOOS-$$GOARCH.zip $$binary ; \
			else \
				tar -czf $(RELEASE_DIR)/$(BINARY_NAME)-$(VERSION)-$$GOOS-$$GOARCH.tar.gz -C $(BIN_DIR) $(shell basename $$binary) ; \
			fi ; \
		fi ; \
	done
	@cd $(RELEASE_DIR) && sha256sum * > checksums.txt
	@echo "✅ Release artifacts created in $(RELEASE_DIR)/"

clean: ## Remove build artifacts
	@echo "🧹 Cleaning build artifacts..."
	rm -rf $(BIN_DIR)
	rm -rf $(RELEASE_DIR)
	rm -f coverage.out coverage.html
	@echo "✅ Clean complete"

clean-all: clean docker-stop ## Remove all artifacts and Docker containers
	@echo "🧹 Cleaning everything..."
	rm -rf rootfs/
	@echo "✅ All clean"

.DEFAULT_GOAL := help
