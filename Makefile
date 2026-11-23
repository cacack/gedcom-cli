.PHONY: build install test fmt vet check

build: ## Build ./bin/gedcom
	go build -o bin/gedcom ./cmd/gedcom

install: ## Install gedcom into GOBIN
	go install ./cmd/gedcom

test: ## Run tests with the race detector
	go test -race -cover ./...

fmt: ## Format code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

check: vet test ## What CI runs
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
