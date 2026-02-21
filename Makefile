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
CMD_PATH      := ./cmd/hotplexd/main.go
DIST_DIR      := ./dist

.PHONY: all help build build-all test lint tidy clean install-hooks run

# Default target
all: help

## 📋 Help: Show available commands
help: ## Show this help message
	@printf "\n"
	@printf "${BOLD}${CYAN}🔥 HotPlex Build System${NC}\n"
	@printf "Usage: make ${YELLOW}<target>${NC}\n"
	@printf "\n"
	@printf "${BOLD}Management Targets:${NC}\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  ${YELLOW}%-15s${NC} %s\n", $$1, $$2}'
	@printf "\n"

build: ## Compile the hotplexd daemon
	@printf "${GREEN}🚀 Building HotPlex Daemon...${NC}\n"
	@mkdir -p $(DIST_DIR)
	@go build -o $(DIST_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@printf "${GREEN}✅ Build complete: ${DIST_DIR}/$(BINARY_NAME)${NC}\n"

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

test-all: test-unit test-integration ## Run all tests

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
	@printf "${GREEN}✅ Cleanup done.${NC}\n"

install-hooks: ## Install local Git hooks
	@printf "${CYAN}🔗 Installing HotPlex Git Hooks...${NC}\n"
	@chmod +x scripts/*.sh
	@./scripts/setup_hooks.sh
	@printf "${GREEN}✅ Hooks are active.${NC}\n"

run: build ## Start the daemon locally
	@printf "${PURPLE}🔥 Starting HotPlex Daemon...${NC}\n"
	@./$(DIST_DIR)/$(BINARY_NAME)
