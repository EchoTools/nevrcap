.PHONY: test lint fmt clean help

.DEFAULT_GOAL := test

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

test: ## Run tests
	go test ./...

lint: ## Format and vet
	go fmt ./...
	go vet ./...

fmt: ## Format code
	go fmt ./...

check: lint test ## Run all checks
