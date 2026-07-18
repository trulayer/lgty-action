BINARY := lgty-action

.PHONY: help build fmt vet test tidy clean
help: ## List targets
	@grep -E '^[a-z0-9_-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | sort

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
