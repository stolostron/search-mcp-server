.PHONY: build run test lint clean coverage integration container-build container-push helm-template helm-install helm-upgrade helm-uninstall

# Build Go binary
build:
	go build -o bin/acm-mcp-server-go cmd/server/main.go

# Run in development
run:
	go run cmd/server/main.go $(DB_URL)

# Run with specific database URL
dev:
	go run cmd/server/main.go $(DB_URL)

# Testing
test:
	go test ./... -v

# Integration tests
test-integration:
	go test ./test/integration -v -tags=integration

# Compatibility tests (compare with TypeScript)
test-compatibility:
	go test ./test/compatibility -v

# Linting
lint:
	golangci-lint run

# Test coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean
clean:
	rm -rf ../bin/ coverage.out coverage.html

# Download dependencies
deps:
	go mod download
	go mod tidy

# Container configuration
REGISTRY ?= quay.io/bjoydeep
IMAGE_NAME ?= acm-mcp-server-go
IMAGE_TAG ?= dev
IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)
PLATFORM ?= linux/amd64
CONTAINER_TOOL ?= podman

# Container build with podman
container-build:
	$(CONTAINER_TOOL) build --platform $(PLATFORM) -t $(REGISTRY)/$(IMAGE) -f Dockerfile .

# Push container to registry
container-push: container-build
	$(CONTAINER_TOOL) push $(REGISTRY)/$(IMAGE)

# Helm configuration
HELM_CHART_NAME ?= acm-mcp-server
HELM_RELEASE_NAME ?= acm-mcp-server
NAMESPACE ?= acm-search

# Helm targets
helm-template:
	helm template $(HELM_RELEASE_NAME) ./helm/$(HELM_CHART_NAME)

helm-install:
	helm install $(HELM_RELEASE_NAME) ./helm/$(HELM_CHART_NAME) \
		--create-namespace --namespace $(NAMESPACE) \
		--set image.repository=$(REGISTRY)/$(IMAGE_NAME) \
		--set image.tag=$(IMAGE_TAG)

helm-upgrade:
	helm upgrade $(HELM_RELEASE_NAME) ./helm/$(HELM_CHART_NAME) \
		--namespace $(NAMESPACE) \
		--set image.repository=$(REGISTRY)/$(IMAGE_NAME) \
		--set image.tag=$(IMAGE_TAG)

helm-uninstall:
	helm uninstall $(HELM_RELEASE_NAME) --namespace $(NAMESPACE)

# Testing targets
# Two different testing scenarios:
#
# 1. test-mcp-server (Generic/Flexible)
#    - Test ANY MCP server URL (dev, staging, prod, localhost, etc.)
#    - Use for: multiple environments, comparing servers, custom domains
#    - Usage: make test-mcp-server ROUTE_URL=https://your-server.com
#
# 2. test-mcp-deployed (Convenience/Auto-discovery)
#    - Test the server deployed by THIS Makefile (auto-finds route)
#    - Use for: quick testing after helm-install, CI/CD, integrated workflow
#    - Usage: make test-mcp-deployed (no parameters needed)

test-mcp-server: ## Test any MCP server (requires ROUTE_URL parameter)
	@if [ -z "$(ROUTE_URL)" ]; then \
		echo "Usage: make test-mcp-server ROUTE_URL=<url>"; \
		echo "Example: make test-mcp-server ROUTE_URL=https://acm-mcp-server-acm-search.apps.example.com"; \
		echo ""; \
		echo "Use cases:"; \
		echo "  - Testing development/staging servers"; \
		echo "  - Testing multiple environments"; \
		echo "  - Testing custom domains or localhost"; \
		exit 1; \
	fi
	./test-mcp-server.sh "$(ROUTE_URL)"

test-mcp-deployed: ## Test currently deployed MCP server (auto-discovers route)
	@echo "# Auto-discovering route for deployed server..."
	@ROUTE_URL=$$(oc get route $(HELM_RELEASE_NAME) -n $(NAMESPACE) -o jsonpath='{.spec.host}' 2>/dev/null || echo ""); \
	if [ -z "$$ROUTE_URL" ]; then \
		echo "No route found. Is the server deployed?"; \
		echo "Run: make helm-install"; \
		exit 1; \
	fi; \
	echo "Testing deployed server: https://$$ROUTE_URL"; \
	./test-mcp-server.sh "https://$$ROUTE_URL"

# Verify dependencies and project setup
verify:
	go mod verify
	go mod tidy
	@echo "Project structure:"
	@find . -name "*.go" -o -type d | grep -E '\.(go|/)$$' | sort

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# Full development workflow
check: fmt vet lint test

# Install development tools
install-tools:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin; \
	}

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Development:"
	@echo "  build           - Build the Go binary"
	@echo "  run             - Run in development mode"
	@echo "  dev             - Run with database URL"
	@echo "  test            - Run unit tests"
	@echo "  test-integration - Run integration tests"
	@echo "  test-compatibility - Run compatibility tests"
	@echo "  lint            - Run linter"
	@echo "  coverage        - Generate test coverage report"
	@echo "  fmt             - Format Go code"
	@echo "  vet             - Vet Go code"
	@echo "  check           - Run fmt, vet, lint, and test"
	@echo ""
	@echo "Container:"
	@echo "  container-build - Build container with podman"
	@echo "  container-push  - Push container to registry"
	@echo ""
	@echo "Deployment:"
	@echo "  helm-template   - Generate Helm templates"
	@echo "  helm-install    - Install with Helm"
	@echo "  helm-upgrade    - Upgrade with Helm"
	@echo "  helm-uninstall  - Uninstall with Helm"
	@echo ""
	@echo "Testing:"
	@echo "  test-mcp-server      - Test MCP server (requires ROUTE_URL=<url>)"
	@echo "  test-mcp-deployed    - Test currently deployed MCP server"
	@echo ""
	@echo "Utilities:"
	@echo "  clean           - Clean build artifacts"
	@echo "  deps            - Download and tidy dependencies"
	@echo "  verify          - Verify project setup"
	@echo "  install-tools   - Install development tools"
	@echo "  help            - Show this help message"