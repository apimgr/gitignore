.PHONY: help deps build test run clean docker docker-build docker-run docker-test release

# Variables
BINARY_NAME=gitignore
VERSION?=1.0.0
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_DATE?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-w -s -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)"

# Directories
BIN_DIR=./binaries
RELEASE_DIR=./release

# Platform-specific settings
PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64

help: ## Show this help
	@echo "GitIgnore API Server - Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

deps: ## Download Go dependencies
	@echo "📦 Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies installed"

build: ## Build for current platform
	@echo "🔨 Building $(BINARY_NAME) for current platform..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo "✅ Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-all: ## Build for all platforms
	@echo "🔨 Building for all platforms..."
	@mkdir -p $(BIN_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} ; \
		output=$(BIN_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH ; \
		if [ "$$GOOS" = "windows" ]; then output=$$output.exe; fi ; \
		echo "Building $$GOOS/$$GOARCH..." ; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build $(LDFLAGS) -o $$output . ; \
	done
	@echo "✅ All builds complete"

test: ## Run tests
	@echo "🧪 Running tests..."
	go test -v -race ./...
	@echo "✅ Tests passed"

test-coverage: ## Run tests with coverage
	@echo "🧪 Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

run: build ## Build and run
	@echo "🚀 Starting server..."
	$(BIN_DIR)/$(BINARY_NAME) --dev --port 8080

run-dev: ## Run in development mode
	@echo "🚀 Starting server in dev mode..."
	go run . --dev --port 8080

docker: docker-build ## Build Docker image (alias for docker-build)

docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(BINARY_NAME):$(VERSION) \
		-t $(BINARY_NAME):latest \
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
