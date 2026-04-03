.PHONY: build lint vet test check cover install-tools clean \
        snapshot release-check \
        mcp-up mcp-down mcp-logs mcp-ps

BIN     := bin/slack-tickets
PKG     := ./...
GOFLAGS := -race

# Pinned tool versions.
GOLANGCI_LINT_VERSION := v2.11.3
GORELEASER_VERSION    := v2.9.0

# ── Build ──────────────────────────────────────────────────────────────────────

build:
	go build -o $(BIN) ./cmd/slack-tickets/

# ── Quality gates ──────────────────────────────────────────────────────────────

vet:
	go vet $(PKG)

lint:
	golangci-lint run $(PKG)

test:
	go test $(GOFLAGS) -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out

# check runs all quality gates in order: vet → lint → test.
# This is the single command to run before committing.
check: vet lint test

# cover opens an HTML coverage report in the default browser.
cover: test
	go tool cover -html=coverage.out

# ── Release ────────────────────────────────────────────────────────────────────

# snapshot builds release artifacts for all platforms locally without publishing.
# Outputs are written to dist/. Useful for verifying the release config.
snapshot:
	goreleaser release --snapshot --clean

# release-check validates .goreleaser.yaml without building anything.
release-check:
	goreleaser check

# ── Tool installation ──────────────────────────────────────────────────────────

# install-tools downloads all developer tools needed to work on this project.
install-tools:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "Installing goreleaser $(GORELEASER_VERSION)..."
	go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
	@echo "All tools installed."

# ── MCP / Docker Compose targets ──────────────────────────────────────────────

MCP_DIR := docker/atlassian-mcp

# mcp-up starts the Atlassian MCP server in the background.
# Copy docker/atlassian-mcp/.env.example to docker/atlassian-mcp/.env and fill it in first.
mcp-up:
	docker compose -f $(MCP_DIR)/docker-compose.yml --env-file $(MCP_DIR)/.env up -d

# mcp-down stops and removes the Atlassian MCP server containers.
mcp-down:
	docker compose -f $(MCP_DIR)/docker-compose.yml --env-file $(MCP_DIR)/.env down

# mcp-logs tails the Atlassian MCP server logs.
mcp-logs:
	docker compose -f $(MCP_DIR)/docker-compose.yml --env-file $(MCP_DIR)/.env logs -f

# mcp-ps shows the status of MCP-related containers.
mcp-ps:
	docker compose -f $(MCP_DIR)/docker-compose.yml ps

# ── Housekeeping ───────────────────────────────────────────────────────────────

clean:
	rm -f $(BIN) coverage.out
	rm -rf dist/
