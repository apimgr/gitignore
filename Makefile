# Project identity comes from IDEA.md first, then the remote, then the directory.
PROJECT_NAME := $(shell sed -n '/^## Project variables$$/{:a;n;/^## /q;/^project_name:[[:space:]]*/{s/^project_name:[[:space:]]*//;p;q};ba}' IDEA.md 2>/dev/null | head -n1 | tr -d '[:space:]')
ifeq ($(PROJECT_NAME),)
PROJECT_NAME := $(shell remote=$$(git remote get-url origin 2>/dev/null); if [ -n "$$remote" ]; then printf '%s\n' "$$remote" | sed -E 's|.*/([^/]+)(\.git)?$$|\1|'; else basename "$(PWD)"; fi)
endif
PROJECT_ORG := $(shell sed -n '/^## Project variables$$/{:a;n;/^## /q;/^project_org:[[:space:]]*/{s/^project_org:[[:space:]]*//;p;q};ba}' IDEA.md 2>/dev/null | head -n1 | tr -d '[:space:]')
ifeq ($(PROJECT_ORG),)
PROJECT_ORG := $(shell remote=$$(git remote get-url origin 2>/dev/null); if [ -n "$$remote" ]; then printf '%s\n' "$$remote" | sed -E 's|.*/([^/]+)/[^/]+(\.git)?$$|\1|'; else basename "$(shell dirname "$(PWD)")"; fi)
endif
INTERNAL_NAME := $(shell sed -n 's/^internal_name:[[:space:]]*//p' IDEA.md | head -n 1)

VERSION ?= $(shell if [ -s release.txt ]; then cat release.txt; else git rev-parse --short=7 HEAD 2>/dev/null || printf 'devel'; fi)
BUILD_EPOCH := $(shell date -u +%s)
BUILD_DATE := $(shell date -u -d @$(BUILD_EPOCH) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r $(BUILD_EPOCH) +%Y-%m-%dT%H:%M:%SZ)
COMMIT_ID := $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo N/A)
OFFICIAL_SITE := $(shell [ -f site.txt ] && cat site.txt || echo "$${OFFICIAL_SITE:-}")

BINDIR := binaries
RELDIR := releases
PLATFORMS := linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64,freebsd/amd64,freebsd/arm64
REGISTRY ?= ghcr.io/$(PROJECT_ORG)/$(PROJECT_NAME)
GO_CACHE ?= $(HOME)/go/pkg/mod
GO_BUILD ?= $(HOME)/.cache/go-build/$(PROJECT_NAME)
DOCKER_MEM ?= 4g
DOCKER_CPUS ?= 2
DOCKER_CPU_SHARES ?= 256
DOCKER_TIMEOUT ?= 3600

LDFLAGS := -s -w -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT_ID)' -X 'main.BuildEpoch=$(BUILD_EPOCH)' -X 'main.OfficialSite=$(OFFICIAL_SITE)'
GO_DOCKER_RUN := timeout -k 30 $(DOCKER_TIMEOUT) docker run --rm --no-healthcheck \
	--name $(PROJECT_NAME)-$$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n') \
	--memory=$(DOCKER_MEM) --cpus=$(DOCKER_CPUS) \
	--cpu-shares=$(DOCKER_CPU_SHARES) \
	-v $(PWD):/app \
	-v $(GO_CACHE):/usr/local/share/go/pkg/mod \
	-v $(GO_BUILD):/usr/local/share/go/cache \
	-w /app \
	-e CGO_ENABLED=0 \
	-e GOFLAGS=-buildvcs=false \
	-e PROJECT_ORG=$(PROJECT_ORG) \
	-e INTERNAL_NAME=$(INTERNAL_NAME)
GO_DOCKER := $(GO_DOCKER_RUN) casjaysdev/go:latest

.PHONY: dev local build test release release-check beta docker clean

dev:
	@mkdir -p "$(GO_CACHE)" "$(GO_BUILD)" "$${TMPDIR:-/tmp}/$(PROJECT_ORG)"
	@BUILD_DIR=$$(mktemp -d "$${TMPDIR:-/tmp}/$(PROJECT_ORG)/$(INTERNAL_NAME)-XXXXXX") && \
		$(GO_DOCKER_RUN) -v "$$BUILD_DIR:/build" casjaysdev/go:latest sh -eu -c '\
			go mod tidy; \
			go build -buildvcs=false -o /build/$(PROJECT_NAME) ./src; \
			go build -buildvcs=false -o /build/$(PROJECT_NAME)-cli ./src/client' && \
		echo "Development binaries: $$BUILD_DIR"

local: clean
	@mkdir -p "$(BINDIR)" "$(GO_CACHE)" "$(GO_BUILD)"
	@os=$$(uname -s | tr '[:upper:]' '[:lower:]'); arch=$$(uname -m); \
		case "$$os" in darwin) os=darwin ;; mingw*|msys*|cygwin*) os=windows ;; esac; \
		case "$$arch" in x86_64|x86-64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac; \
		if [ "$$os" = windows ]; then ext=.exe; else ext=; fi; \
		$(GO_DOCKER_RUN) -v "$(abspath $(BINDIR)):/build-out" -e GOOS=$$os -e GOARCH=$$arch -e BUILD_LDFLAGS="$(LDFLAGS)" casjaysdev/go:latest sh -eu -c '\
			go mod tidy; \
			go mod download; \
			go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME) ./src; \
			go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME)-cli ./src/client'

build: clean
	@mkdir -p "$(BINDIR)" "$(GO_CACHE)" "$(GO_BUILD)"
	@$(GO_DOCKER_RUN) -v "$(abspath $(BINDIR)):/build-out" -e BUILD_LDFLAGS="$(LDFLAGS)" casjaysdev/go:latest sh -eu -c '\
		go mod tidy; \
		go mod download; \
		GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME) ./src; \
		GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME)-cli ./src/client; \
		for platform in $$(printf "%s" "$(PLATFORMS)" | tr "," " "); do \
			os=$${platform%/*}; arch=$${platform#*/}; ext=; \
			if [ "$$os" = windows ]; then ext=.exe; fi; \
			GOOS=$$os GOARCH=$$arch go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME)-$$os-$$arch$$ext ./src; \
			GOOS=$$os GOARCH=$$arch go build -buildvcs=false -trimpath -ldflags "$$BUILD_LDFLAGS" -o /build-out/$(PROJECT_NAME)-cli-$$os-$$arch$$ext ./src/client; \
		done'

test:
ifeq ($(TEST_E2E),1)
	@bash tests/e2e.sh
else
	@mkdir -p "$(GO_CACHE)" "$(GO_BUILD)"
	@network="$(PROJECT_NAME)-test-net"; created=false; \
		if ! docker network inspect "$$network" >/dev/null 2>&1; then docker network create --driver bridge "$$network" >/dev/null; created=true; fi; \
		trap '[ "$$created" = false ] || docker network rm "$$network" >/dev/null' EXIT; \
		$(GO_DOCKER_RUN) --network "$$network" casjaysdev/go:latest sh -eu -c '\
		temp_parent="$${TMPDIR:-/tmp}/$(PROJECT_ORG)"; \
		mkdir -p "$$temp_parent"; \
		test_tmp=$$(mktemp -d "$$temp_parent/$(INTERNAL_NAME)-XXXXXX"); \
		coverage_dir=$$(mktemp -d "$$temp_parent/$(INTERNAL_NAME)-XXXXXX"); \
		trap '\''for candidate in "$$test_tmp" "$$coverage_dir"; do case "$$candidate" in "$$temp_parent/$(INTERNAL_NAME)-"*) rm -rf -- "$$candidate" ;; esac; done'\'' EXIT; \
		export TMPDIR="$$test_tmp"; \
		go vet ./...; \
		govulncheck ./...; \
		go test -coverprofile="$$coverage_dir/coverage.out" ./...; \
		coverage=$$(go tool cover -func="$$coverage_dir/coverage.out" | awk '\''/^total:/ {gsub("%", "", $$3); print $$3}'\''); \
		echo "Coverage: $$coverage%"; \
		awk -v coverage="$$coverage" '\''BEGIN { if (coverage+0 < 60) exit 1 }'\'''
endif

release-check:
	@printf '%s\n' "$(VERSION)" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$$' >/dev/null || { echo "ERROR: make release needs a stable SemVer X.Y.Z in release.txt (got '$(VERSION)')" >&2; exit 78; }

beta:
	@[ "$$(git branch --show-current)" = main ] || { echo "beta: run it on main" >&2; exit 1; }
	@[ -z "$$(git status --porcelain)" ] || { echo "beta: the working tree is not clean" >&2; exit 1; }
	@tag=$$(date -u +%Y%m%d%H%M%S)-beta; git tag "$$tag" && git push origin "refs/tags/$$tag" && echo "Pushed $$tag"

release: release-check build
	@mkdir -p "$(RELDIR)"
	@printf '%s\n' "$(VERSION)" > "$(RELDIR)/version.txt"
	@cp "$(BINDIR)/$(PROJECT_NAME)-"* "$(RELDIR)/"
	@tar --exclude=.git --exclude=.github --exclude=.gitea --exclude=binaries --exclude=releases --exclude='*.out' \
		-czf "$(RELDIR)/$(PROJECT_NAME)-$(VERSION)-source.tar.gz" .
	@cd "$(RELDIR)" && sha256sum $(PROJECT_NAME)-* > sha256.txt
	@cd "$(RELDIR)" && sha512sum $(PROJECT_NAME)-* > sha512.txt
	@gh release delete "v$(VERSION)" --yes 2>/dev/null || true
	@git tag -d "v$(VERSION)" 2>/dev/null || true
	@gh api --method DELETE "repos/$(PROJECT_ORG)/$(PROJECT_NAME)/git/refs/tags/v$(VERSION)" 2>/dev/null || true
	@gh release create "v$(VERSION)" "$(RELDIR)"/* --title "v$(VERSION)" --notes "Release v$(VERSION)" --latest

docker:
	@docker buildx build -f docker/Dockerfile --platform linux/amd64,linux/arm64 --push \
		--build-arg VERSION="$(VERSION)" --build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg BUILD_EPOCH="$(BUILD_EPOCH)" --build-arg COMMIT_ID="$(COMMIT_ID)" \
		--build-arg OFFICIAL_SITE="$(OFFICIAL_SITE)" \
		-t "$(REGISTRY):$(VERSION)" -t "$(REGISTRY):latest" .

clean:
	@rm -rf "$(BINDIR)" "$(RELDIR)"
