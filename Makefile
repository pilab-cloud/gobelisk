# Gobelisk Makefile

.PHONY: build test clean run lint vet fmt tidy help

# Variables
BINARY_NAME=gobelisk
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary
	@echo "Building ${BINARY_NAME}..."
	go build ${LDFLAGS} -o ${BINARY_NAME} .

build-all: ## Build for all platforms
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o dist/${BINARY_NAME}-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o dist/${BINARY_NAME}-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o dist/${BINARY_NAME}-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o dist/${BINARY_NAME}-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o dist/${BINARY_NAME}-windows-amd64.exe .

test: ## Run tests
	@echo "Running tests..."
	go test ./... -count=1

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	@echo "Running linter..."
	golangci-lint run

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

tidy: ## Tidy go.mod
	@echo "Tidying go.mod..."
	go mod tidy

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf ${BINARY_NAME} dist/ coverage.out coverage.html

run: build ## Build and run
	@./${BINARY_NAME} server

install: ## Install binary to GOPATH/bin
	@echo "Installing..."
	go install ${LDFLAGS} .

proto: ## Generate protobuf code
	@echo "Generating protobuf..."
	buf generate
