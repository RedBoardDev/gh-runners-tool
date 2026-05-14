BINARY    := ghr
MODULE    := github.com/RedBoardDev/gh-runners-tool/v2
CMD       := ./cmd/ghr
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE      ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS   := -s -w -X '$(MODULE)/internal/cli.version=$(VERSION)' -X '$(MODULE)/internal/cli.commit=$(COMMIT)' -X '$(MODULE)/internal/cli.date=$(DATE)'

.PHONY: build test lint vet fmt fmt-check vuln clean install snapshot ci help

build: ## Build the binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

test: ## Run tests with race detector
	go test -race -count=1 ./...

lint: ## Run golangci-lint
	golangci-lint run

vet: ## Run go vet
	go vet ./...

fmt: ## Format code
	gofmt -w .

fmt-check: ## Check formatting (CI)
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:" && gofmt -l . && exit 1)

vuln: ## Run govulncheck
	govulncheck ./...

clean: ## Remove build artifacts
	rm -rf $(BINARY) dist/

install: ## Install locally via go install
	go install -ldflags "$(LDFLAGS)" $(CMD)

snapshot: ## Build a snapshot release (no publish)
	goreleaser release --snapshot --clean

ci: lint vet fmt-check build test vuln ## Run all CI checks locally

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
