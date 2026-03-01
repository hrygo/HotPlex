# HotPlex Makefile
# A premium CLI experience for building and managing HotPlex

# Colors for UI
CYAN          := \033[0;36m
GREEN         := \033[0;32m
YELLOW        := \033[1;33m
RED           := \033[0;31m
PURPLE        := \033[0;35m
BOLD          := \033[1m
NC            := \033[0m

# Metadata
BINARY_NAME   := hotplexd
CMD_PATH      := ./cmd/hotplexd
DIST_DIR      := ./dist
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS       := -X 'main.Version=$(VERSION)' -X 'main.Commit=$(COMMIT)' -X 'main.BuildTime=$(BUILD_TIME)'

LOG_DIR       := .logs
LOG_FILE      := $(LOG_DIR)/daemon.log

.PHONY: all help build build-all fmt vet test test-unit test-race test-integration test-all lint tidy clean install-hooks run stop restart docs svg2png

# Default target
all: help

svg2png: ## Manual utility: Convert all SVG files to 4K PNG (for external use)
	@printf "${CYAN}🖼️  Converting SVG to PNG...${NC}\n"
	@chmod +x scripts/svg2png.sh 2>/dev/null || true
	@./scripts/svg2png.sh
	@printf "${GREEN}✅ PNG assets generated in docs/images/png/${NC}\n"

## 📋 Help: Show available commands
help: ## Show this help message
	@printf "\n"
	@printf "${BOLD}${CYAN}🔥 HotPlex Build System${NC}\n"
	@printf "Usage: make ${YELLOW}<target>${NC}\n"
	@printf "\n"
	@printf "${BOLD}Management Targets:${NC}\n"
	@grep -E '^[a-zA-Z0-9_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  ${YELLOW}%-18s${NC} %s\n", $$1, $$2}'
	@printf "\n"

build: fmt vet tidy ## Compile the hotplexd daemon
	@printf "${GREEN}🚀 Building HotPlex Daemon (${VERSION})...${NC}\n"
	@mkdir -p $(DIST_DIR)
	@go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@printf "${GREEN}✅ Build complete: ${DIST_DIR}/$(BINARY_NAME)${NC}\n"

build-all: fmt vet tidy ## Compile for all major platforms (Linux, macOS, Windows)
	@printf "${GREEN}🚀 Building HotPlex Daemon for all platforms (${VERSION})...${NC}\n"
	@mkdir -p $(DIST_DIR)
	@GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)
	@GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(CMD_PATH)
	@GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_PATH)
	@GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(CMD_PATH)
	@GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_PATH)
	@printf "${GREEN}✅ Cross-compilation complete in ${DIST_DIR}/${NC}\n"

fmt: ## Format Go code
	@printf "${CYAN}🔧 Formatting code...${NC}\n"
	@go fmt ./...

vet: ## Check for suspicious constructs
	@printf "${CYAN}🔍 Vetting code...${NC}\n"
	@go vet ./...

test: test-unit ## Run fast unit tests (default)

test-unit: ## Run fast unit tests without race detection
	@printf "${CYAN}🧪 Running fast unit tests...${NC}\n"
	@go test -v -short ./...
	@printf "${GREEN}✅ Unit tests passed!${NC}\n"

test-race: ## Run unit tests with race detection
	@printf "${CYAN}🧪 Running unit tests with race detection...${NC}\n"
	@go test -v -race ./...
	@printf "${GREEN}✅ Race detection passed!${NC}\n"

test-integration: ## Run heavy integration tests
	@printf "${YELLOW}🏗️  Running heavy integration tests...${NC}\n"
	@go test -v -tags=integration ./...
	@printf "${GREEN}✅ Integration tests passed!${NC}\n"

test-all: test-race test-integration ## Run all tests

lint: ## Run golangci-lint
	@printf "${PURPLE}🔍 Linting code...${NC}\n"
	@if command -v golangci-lint &> /dev/null; then \
		golangci-lint run ./...; \
		printf "${GREEN}✅ Linting passed!${NC}\n"; \
	else \
		printf "${RED}❌ golangci-lint not found. Install it first.${NC}\n"; \
		exit 1; \
	fi

tidy: ## Clean up go.mod and dependencies
	@printf "${YELLOW}📦 Tidying up Go modules...${NC}\n"
	@go mod tidy
	@printf "${GREEN}✅ Modules synchronized.${NC}\n"

clean: ## Remove build artifacts
	@printf "${RED}🧹 Cleaning up build artifacts...${NC}\n"
	@rm -rf $(DIST_DIR)
	@go clean
	@printf "${GREEN}✅ Cleanup done.${NC}\n"

install-hooks: ## Install local Git hooks
	@printf "${CYAN}🔗 Installing HotPlex Git Hooks...${NC}\n"
	@chmod +x scripts/*.sh 2>/dev/null || true
	@if [ -d scripts ] && [ -f scripts/setup_hooks.sh ]; then ./scripts/setup_hooks.sh; fi
	@printf "${GREEN}✅ Hooks are active.${NC}\n"

run: build ## Start the daemon locally
	@printf "${PURPLE}🔥 Starting HotPlex Daemon...${NC}\n"
	@./$(DIST_DIR)/$(BINARY_NAME)

stop: ## Stop the running hotplexd daemon
	@printf "${YELLOW}🛑 Stopping HotPlex Daemon...${NC}\n"
	@if pgrep -f $(BINARY_NAME) > /dev/null 2>&1; then \
		pkill -f $(BINARY_NAME); \
		sleep 1; \
		if pgrep -f $(BINARY_NAME) > /dev/null 2>&1; then \
			pkill -9 -f $(BINARY_NAME); \
		fi; \
		printf "${GREEN}✅ Daemon stopped${NC}\n"; \
	else \
		printf "${YELLOW}⚠️  No running daemon found${NC}\n"; \
	fi

restart: stop build ## Restart the daemon and log to $(LOG_FILE)
	@mkdir -p $(LOG_DIR)
	@printf "${PURPLE}🔥 Starting HotPlex Daemon (logs: $(LOG_FILE))...${NC}\n"
	@nohup ./$(DIST_DIR)/$(BINARY_NAME) > $(LOG_FILE) 2>&1 &
	@printf "${GREEN}✅ Daemon started in background with PID: $$(pgrep -f $(BINARY_NAME))${NC}\n"
	@printf "${CYAN}📋 Tailing logs (Ctrl+C to stop tailing, daemon will keep running):${NC}\n"
	@tail -f $(LOG_FILE)
