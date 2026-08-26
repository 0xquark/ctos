BINARY := ctos
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run test lint fmt vet tidy clean install check

build: ## Build the binary into ./ctos
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/ctos

run: ## Build and run
	go run ./cmd/ctos

test: ## Run all tests
	go test ./...

vet:
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

lint: ## Run golangci-lint (install: https://golangci-lint.run)
	golangci-lint run

tidy:
	go mod tidy

check: fmt vet test ## Everything CI runs

install: ## Install to $GOPATH/bin
	go install -ldflags "$(LDFLAGS)" ./cmd/ctos

clean:
	rm -f $(BINARY)
	go clean -testcache
