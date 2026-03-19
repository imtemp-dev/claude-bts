BINARY_NAME := bts
MODULE := github.com/jlim/bts
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X $(MODULE)/pkg/version.Version=$(VERSION) -X $(MODULE)/pkg/version.Commit=$(COMMIT) -X $(MODULE)/pkg/version.Date=$(DATE)"

.PHONY: build install test clean release-local

build: ## Build the binary
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/bts

install: ## Install to GOPATH/bin
	go install $(LDFLAGS) ./cmd/bts

test: ## Run tests
	go test -race ./...

clean: ## Remove build artifacts
	rm -rf bin/ dist/

release-local: ## Test goreleaser locally (no publish)
	goreleaser release --snapshot --clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
