BINARY := lgty-action

.PHONY: help build fmt vet test tidy clean githooks
help: ## List targets
	@grep -E '^[a-z0-9_-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | sort

githooks: ## Install local git hooks (pre-commit: gofmt+vet · pre-push: build+test)
	git config core.hooksPath .githooks
	@echo "core.hooksPath -> .githooks (run once per clone)"

build: ## Build the static binary into dist/
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY) .

fmt: ## Format
	gofmt -s -w .

vet: ## Vet
	go vet ./...

test: ## Test
	go test ./...

tidy: ## Tidy modules
	go mod tidy

clean: ## Remove build artifacts
	rm -rf dist
