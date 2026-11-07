# Project identity comes from the GitHub origin and frozen IDEA.md identifier.
PROJECT_NAME := $(shell remote=$$(git remote get-url origin 2>/dev/null); if [ -n "$$remote" ]; then printf '%s\n' "$$remote" | sed -E 's|.*/([^/]+)(\.git)?$$|\1|'; else basename "$(PWD)"; fi)
PROJECT_ORG := $(shell remote=$$(git remote get-url origin 2>/dev/null); if [ -n "$$remote" ]; then printf '%s\n' "$$remote" | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$$|\1|'; else basename "$(shell dirname "$(PWD)")"; fi)
INTERNAL_NAME := $(shell sed -n 's/^internal_name:[[:space:]]*//p' IDEA.md | head -n 1)

VERSION := $(shell cat release.txt 2>/dev/null || echo "0.1.0")
BUILD_EPOCH := $(shell date -u +%s)
COMMIT_ID := $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo N/A)
OFFICIAL_SITE := $(shell [ -f site.txt ] && cat site.txt || echo "$${OFFICIAL_SITE:-}")

BINDIR := binaries
RELDIR := releases
PLATFORMS := linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64,freebsd/amd64,freebsd/arm64
REGISTRY ?= ghcr.io/$(PROJECT_ORG)/$(PROJECT_NAME)
GO_CACHE ?= $(shell printf '%s' "$${TMPDIR:-/tmp}")/$(PROJECT_ORG)/$(INTERNAL_NAME)-go-cache/pkg/mod
GO_BUILD ?= $(shell printf '%s' "$${TMPDIR:-/tmp}")/$(PROJECT_ORG)/$(INTERNAL_NAME)-go-cache/build
DOCKER_MEM ?= 4g
DOCKER_CPUS ?= 2

LDFLAGS := -s -w -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT_ID)' -X 'main.BuildEpoch=$(BUILD_EPOCH)' -X 'main.OfficialSite=$(OFFICIAL_SITE)'
GO_DOCKER_RUN := docker run --rm \
	--name $(PROJECT_NAME)-$$(tr -dc 'a-z0-9' </dev/urandom | head -c8) \
	--memory=$(DOCKER_MEM) --cpus=$(DOCKER_CPUS) \
	-v $(PWD):/app \
	-v $(GO_CACHE):/usr/local/share/go/pkg/mod \
	-v $(GO_BUILD):/usr/local/share/go/cache \
	-w /app \
	-e CGO_ENABLED=0 \
	-e GOFLAGS=-buildvcs=false \
	-e PROJECT_ORG=$(PROJECT_ORG) \
	-e INTERNAL_NAME=$(INTERNAL_NAME)
GO_DOCKER := $(GO_DOCKER_RUN) casjaysdev/go:latest

.PHONY: dev local build test release docker clean

dev:
	@mkdir -p "$(GO_CACHE)" "$(GO_BUILD)" "$${TMPDIR:-/tmp}/$(PROJECT_ORG)"
	@BUILD_DIR=$$(mktemp -d "$${TMPDIR:-/tmp}/$(PROJECT_ORG)/$(INTERNAL_NAME)-XXXXXX") && \
		$(GO_DOCKER_RUN) -v "$$BUILD_DIR:/build" casjaysdev/go:latest \
		go build -buildvcs=false -o /build/$(PROJECT_NAME) ./src && \
		$(GO_DOCKER_RUN) -v "$$BUILD_DIR:/build" casjaysdev/go:latest \
		go build -buildvcs=false -o /build/$(PROJECT_NAME)-cli ./src/client && \
		echo "Development binaries: $$BUILD_DIR"

local:
	@mkdir -p "$(BINDIR)" "$(GO_CACHE)" "$(GO_BUILD)"
	@os=$$(uname -s | tr '[:upper:]' '[:lower:]'); arch=$$(uname -m); \
		case "$$os" in darwin) os=darwin ;; mingw*|msys*|cygwin*) os=windows ;; esac; \
		case "$$arch" in x86_64|x86-64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac; \
		if [ "$$os" = windows ]; then ext=.exe; else ext=; fi; \
		$(GO_DOCKER_RUN) -e GOOS=$$os -e GOARCH=$$arch casjaysdev/go:latest \
		go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o /app/$(BINDIR)/$(PROJECT_NAME) ./src && \
		$(GO_DOCKER_RUN) -e GOOS=$$os -e GOARCH=$$arch casjaysdev/go:latest \
		go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o /app/$(BINDIR)/$(PROJECT_NAME)-cli ./src/client

build:
	@mkdir -p "$(BINDIR)" "$(GO_CACHE)" "$(GO_BUILD)"
	@$(GO_DOCKER) go mod download
	@set -e; for platform in $$(printf '%s' "$(PLATFORMS)" | tr ',' ' '); do \
		os=$${platform%/*}; arch=$${platform#*/}; ext=; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		$(GO_DOCKER_RUN) -e GOOS=$$os -e GOARCH=$$arch casjaysdev/go:latest \
		go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o /app/$(BINDIR)/$(PROJECT_NAME)-$$os-$$arch$$ext ./src; \
		$(GO_DOCKER_RUN) -e GOOS=$$os -e GOARCH=$$arch casjaysdev/go:latest \
		go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o /app/$(BINDIR)/$(PROJECT_NAME)-cli-$$os-$$arch$$ext ./src/client; \
	done

test:
	@mkdir -p "$(GO_CACHE)" "$(GO_BUILD)"
	@$(GO_DOCKER) sh -eu -c '\
		mkdir -p "$${TMPDIR:-/tmp}/$(PROJECT_ORG)"; \
		coverage_dir=$$(mktemp -d "$${TMPDIR:-/tmp}/$(PROJECT_ORG)/$(INTERNAL_NAME)-XXXXXX"); \
		trap '\''case "$$coverage_dir" in "$${TMPDIR:-/tmp}/$(PROJECT_ORG)/$(INTERNAL_NAME)-"*) rm -rf -- "$$coverage_dir" ;; esac'\'' EXIT; \
		go vet ./...; \
		govulncheck ./...; \
		go test -coverprofile="$$coverage_dir/coverage.out" ./...; \
		coverage=$$(go tool cover -func="$$coverage_dir/coverage.out" | awk '\''/^total:/ {gsub("%", "", $$3); print $$3}'\''); \
		echo "Coverage: $$coverage%"; \
		awk -v coverage="$$coverage" '\''BEGIN { if (coverage+0 < 60) exit 1 }'\'''

release: build
	@mkdir -p "$(RELDIR)"
	@printf '%s\n' "$(VERSION)" > "$(RELDIR)/version.txt"
	@tar --exclude=.git --exclude=.github --exclude=.gitea --exclude=binaries --exclude=releases --exclude='*.out' \
		-czf "$(RELDIR)/$(PROJECT_NAME)-$(VERSION)-source.tar.gz" .
	@cd "$(BINDIR)" && sha256sum $(PROJECT_NAME)-* > ../$(RELDIR)/sha256.txt
	@cd "$(BINDIR)" && sha512sum $(PROJECT_NAME)-* > ../$(RELDIR)/sha512.txt

docker:
	@docker buildx build -f docker/Dockerfile --platform linux/amd64,linux/arm64 --push \
		--build-arg VERSION="$(VERSION)" --build-arg BUILD_EPOCH="$(BUILD_EPOCH)" \
		--build-arg COMMIT_ID="$(COMMIT_ID)" \
		-t "$(REGISTRY):$(VERSION)" -t "$(REGISTRY):latest" .

clean:
	@mkdir -p "$(BINDIR)" "$(RELDIR)"
	@find "$(BINDIR)" -maxdepth 1 -type f -name '$(PROJECT_NAME)*' -delete
	@find "$(RELDIR)" -maxdepth 1 -type f -name '$(PROJECT_NAME)*' -delete
