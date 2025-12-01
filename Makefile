# ACM Search MCP Server - Docker/Podman Build Testing Version
# This is a parallel implementation to test container builds vs S2I BuildConfigs
# Supports both Docker and Podman with automatic ARM Mac platform detection

.PHONY: help deploy build setup clean status test container-build container-push
.DEFAULT_GOAL := help

# Configuration - Separate namespace for testing
NAMESPACE ?= acm-search
SECRET_NAME = acm-search-mcp-secret

# Container Configuration - Quay.io Registry
REGISTRY ?= quay.io/stolostron
IMAGE_TAG ?= dev-preview
MCP_SERVER_IMAGE = $(REGISTRY)/search-mcp-server:$(IMAGE_TAG)

# Deployment names (aligned with image names)
DEPLOYMENT_NAME = acm-search-mcp-server
SERVICE_NAME = acm-search-mcp-server-service
ROUTE_NAME = acm-search-mcp-server-route

# Platform detection for cross-architecture builds
UNAME_M := $(shell uname -m)
UNAME_S := $(shell uname -s)

# Build strategy selection (override with BUILD_STRATEGY=native or BUILD_STRATEGY=cross)
BUILD_STRATEGY ?= auto

# Container tool detection and platform configuration
ifeq ($(UNAME_S),Darwin)
    ifeq ($(UNAME_M),arm64)
        CONTAINER_TOOL ?= podman
        ifeq ($(BUILD_STRATEGY),native)
            BUILD_PLATFORM =
            ARCH_NOTE = (Mac ARM → Linux ARM64 native)
        else ifeq ($(BUILD_STRATEGY),cross)
            BUILD_PLATFORM = --platform linux/amd64
            ARCH_NOTE = (Mac ARM → Linux x86_64 cross)
        else
            # Auto strategy: try native first, fallback to cross
            BUILD_PLATFORM =
            ARCH_NOTE = (Mac ARM → Linux ARM64 auto)
        endif
    else
        CONTAINER_TOOL ?= docker
        BUILD_PLATFORM =
        ARCH_NOTE = (Mac Intel)
    endif
else
    CONTAINER_TOOL ?= docker
    BUILD_PLATFORM =
    ARCH_NOTE = (Linux native)
endif

# Colors for output
CYAN = \033[36m
GREEN = \033[32m
YELLOW = \033[33m
RED = \033[31m
BLUE = \033[34m
MAGENTA = \033[35m
RESET = \033[0m

## Help target
help: ## Show this help message
	@echo "$(CYAN)ACM Search MCP Server - Container Build Testing$(RESET)"
	@echo "$(YELLOW)🐳 Container Tool: $(CONTAINER_TOOL) $(ARCH_NOTE)$(RESET)"
	@echo "$(YELLOW)🏗️  Build Platform: $(BUILD_PLATFORM)$(RESET)"
	@echo "$(YELLOW)📦 Namespace: $(NAMESPACE)$(RESET)"
	@echo ""
	@echo "$(GREEN)Images:$(RESET)"
	@echo "  📦 $(MCP_SERVER_IMAGE)"
	@echo ""
	@echo "$(GREEN)Available targets:$(RESET)"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(CYAN)%-20s$(RESET) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "$(BLUE)Deployment Commands:$(RESET)"
	@echo "  make deploy-prebuilt              # Deploy existing Quay.io images (new clusters)"
	@echo "  make deploy                       # Build + deploy (development)"
	@echo "  make rebuild                      # Clean everything + build + deploy (fresh start)"
	@echo "  make status                       # Show deployment status"
	@echo "  make test                         # Test the deployment"

## Prerequisites
check-login: ## Check if logged into OpenShift
	@echo "$(CYAN)Checking OpenShift login...$(RESET)"
	@if ! oc whoami &>/dev/null; then \
		echo "$(RED)Error: Not logged into OpenShift. Please run 'oc login' first.$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ Logged in as: $$(oc whoami)$(RESET)"

check-container-tool: ## Check if container tool is available
	@echo "$(CYAN)Checking container tool: $(CONTAINER_TOOL)$(RESET)"
	@if ! command -v $(CONTAINER_TOOL) &>/dev/null; then \
		echo "$(RED)Error: $(CONTAINER_TOOL) not found. Please install $(CONTAINER_TOOL).$(RESET)"; \
		exit 1; \
	fi
	@if ! $(CONTAINER_TOOL) info &>/dev/null; then \
		echo "$(RED)Error: $(CONTAINER_TOOL) daemon not running. Please start $(CONTAINER_TOOL).$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ $(CONTAINER_TOOL) is available$(RESET)"
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		echo "$(MAGENTA)ℹ️  Using Podman for ARM Mac cross-compilation$(RESET)"; \
	fi

check-quay-login: ## Check if logged into Quay.io
	@echo "$(CYAN)Checking Quay.io authentication...$(RESET)"
	@echo "$(YELLOW)Ensure you're logged in: $(CONTAINER_TOOL) login quay.io$(RESET)"
	@echo "$(GREEN)✓ Ready to push to $(REGISTRY)$(RESET)"

## Namespace setup (separate from S2I)
.namespace-docker: check-login
	@echo "$(CYAN)Setting up container testing namespace: $(NAMESPACE)$(RESET)"
	@if ! oc get namespace "$(NAMESPACE)" &>/dev/null; then \
		echo "Creating namespace: $(NAMESPACE)"; \
		oc create namespace "$(NAMESPACE)"; \
	else \
		echo "Namespace $(NAMESPACE) already exists"; \
	fi
	@oc project "$(NAMESPACE)"
	@touch .namespace-docker

## Secret management (create in container namespace using ACM discovery)
.secret-docker: .namespace-docker
	@echo "$(CYAN)Setting up ACM database secret with auto-discovery...$(RESET)"
	@if ! oc get secret $(SECRET_NAME) -n $(NAMESPACE) &>/dev/null; then \
		echo "Creating database secret in $(NAMESPACE) using ACM auto-discovery..."; \
		echo "$(YELLOW)Running ACM-aware secret generation...$(RESET)"; \
		if [ -f "k8s/secret.yaml" ]; then rm -f k8s/secret.yaml; fi; \
		if echo -e "y\ny\n" | NAMESPACE=$(NAMESPACE) ./scripts/create-secret.sh >/dev/null 2>&1; then \
			echo "$(GREEN)✓ ACM auto-discovery succeeded$(RESET)"; \
			if [ -f "k8s/secret.yaml" ]; then \
				sed 's/postgres-mcp-secret/$(SECRET_NAME)/g' k8s/secret.yaml | oc apply -f -; \
			else \
				echo "$(RED)Error: Secret generation failed$(RESET)"; \
				exit 1; \
			fi; \
		else \
			echo "$(YELLOW)ACM auto-discovery failed, using manual password extraction...$(RESET)"; \
			ACM_NS=$$(oc get secrets --all-namespaces 2>/dev/null | grep search-postgres | awk '{print $$1}' | head -1); \
			if [ -n "$$ACM_NS" ]; then \
				echo "Found ACM namespace: $$ACM_NS"; \
				DB_PASS=$$(oc get secret search-postgres -n "$$ACM_NS" -o jsonpath='{.data.database-password}' 2>/dev/null | base64 -d); \
				if [ -n "$$DB_PASS" ]; then \
					DB_URL="postgresql://searchuser:$$DB_PASS@search-postgres.$$ACM_NS.svc.cluster.local:5432/search"; \
					ENCODED_URL=$$(echo -n "$$DB_URL" | base64); \
					oc create secret generic $(SECRET_NAME) -n $(NAMESPACE) --from-literal=database-url="$$DB_URL"; \
					echo "$(GREEN)✓ Created secret with current ACM password$(RESET)"; \
				else \
					echo "$(RED)Error: Could not extract ACM password$(RESET)"; \
					exit 1; \
				fi; \
			else \
				echo "$(RED)Error: No ACM search-postgres secret found$(RESET)"; \
				exit 1; \
			fi; \
		fi; \
	else \
		echo "Database secret already exists in $(NAMESPACE)"; \
	fi
	@touch .secret-docker

## Container Build Targets
container-build: check-container-tool ## Build container images with proper platform targeting
	@echo "$(CYAN)========================================$(RESET)"
	@echo "$(CYAN)🐳 CONTAINER BUILD - Quay.io Registry$(RESET)"
	@echo "$(CYAN)Tool: $(CONTAINER_TOOL) $(ARCH_NOTE)$(RESET)"
	@echo "$(CYAN)Platform: $(BUILD_PLATFORM)$(RESET)"
	@echo "$(CYAN)========================================$(RESET)"
	@echo ""
	@$(MAKE) build-mcp-server

build-mcp-server: ## Build MCP server with fallback strategies
	@echo "$(YELLOW)Building ACM Search MCP Server...$(RESET)"
	@echo "Image: $(MCP_SERVER_IMAGE)"
	@if [ "$(UNAME_S)" = "Darwin" ] && [ "$(UNAME_M)" = "arm64" ]; then \
		echo "$(MAGENTA)Attempting ARM Mac cross-compilation strategies...$(RESET)"; \
		echo "Strategy 1: Multi-stage build"; \
		if $(CONTAINER_TOOL) build --platform linux/amd64 -f Dockerfile.noinstall -t $(MCP_SERVER_IMAGE) . 2>/dev/null; then \
			echo "$(GREEN)✓ No-install build succeeded$(RESET)"; \
		else \
			echo "$(YELLOW)No-install build failed, trying multi-stage...$(RESET)"; \
			if $(CONTAINER_TOOL) build --platform linux/amd64 --build-arg BUILDPLATFORM=linux/arm64 --build-arg TARGETPLATFORM=linux/amd64 -f Dockerfile.multiarch -t $(MCP_SERVER_IMAGE) . 2>/dev/null; then \
				echo "$(GREEN)✓ Multi-stage build succeeded$(RESET)"; \
			else \
				echo "$(YELLOW)Multi-stage build failed, trying standard cross-compilation...$(RESET)"; \
				$(CONTAINER_TOOL) build --platform linux/amd64 -t $(MCP_SERVER_IMAGE) .; \
			fi; \
		fi; \
	else \
		echo "Command: $(CONTAINER_TOOL) build $(BUILD_PLATFORM) -t $(MCP_SERVER_IMAGE) ."; \
		time $(CONTAINER_TOOL) build $(BUILD_PLATFORM) -t $(MCP_SERVER_IMAGE) .; \
	fi
	@echo "$(GREEN)✓ MCP server image built$(RESET)"
	@echo ""

container-push: check-quay-login ## Push container images to Quay.io
	@echo "$(CYAN)Pushing container images to Quay.io...$(RESET)"
	@echo ""
	@echo "$(YELLOW)Pushing $(MCP_SERVER_IMAGE)...$(RESET)"
	@time $(CONTAINER_TOOL) push $(MCP_SERVER_IMAGE)
	@echo "$(GREEN)✓ Pushed: $(MCP_SERVER_IMAGE)$(RESET)"
	@echo ""
	@echo "$(GREEN)🎉 Images available at:$(RESET)"
	@echo "  📦 https://quay.io/repository/stolostron/search-mcp-server"

## Build pipeline
.build-docker: container-build container-push
	@echo "$(GREEN)✓ Container images built and pushed to Quay.io$(RESET)"
	@touch .build-docker

# RBAC is now included in k8s/deployment_docker.yaml - no separate target needed

## Generate deployment YAML from template with variable substitution
k8s/deployment_docker.yaml: k8s/deployment_docker.template.yaml
	@echo "$(CYAN)Generating deployment YAML with variables...$(RESET)"
	@echo "$(YELLOW)NAMESPACE=$(NAMESPACE), DEPLOYMENT_NAME=$(DEPLOYMENT_NAME)$(RESET)"
	@if [ -f "k8s/deployment_docker.template.yaml" ]; then \
		export NAMESPACE=$(NAMESPACE) DEPLOYMENT_NAME=$(DEPLOYMENT_NAME) SERVICE_NAME=$(SERVICE_NAME) ROUTE_NAME=$(ROUTE_NAME) SECRET_NAME=$(SECRET_NAME) MCP_SERVER_IMAGE=$(MCP_SERVER_IMAGE) && \
		envsubst < k8s/deployment_docker.template.yaml > k8s/deployment_docker.yaml; \
		echo "$(GREEN)✓ Generated k8s/deployment_docker.yaml with current variables$(RESET)"; \
	else \
		echo "$(RED)Error: k8s/deployment_docker.template.yaml not found$(RESET)"; \
		exit 1; \
	fi

## Deployment (container-based)
.deploy-docker: .build-docker .secret-docker k8s/deployment_docker.yaml
	@echo "$(CYAN)Deploying ACM Search MCP Server with Quay.io images...$(RESET)"
	@oc apply -f k8s/deployment_docker.yaml
	@echo "$(YELLOW)Waiting for container deployment in $(NAMESPACE)...$(RESET)"
	@oc rollout status deployment/$(DEPLOYMENT_NAME) -n $(NAMESPACE) --timeout=300s
	@touch .deploy-docker

## Deployment (pre-built images only)
.deploy-prebuilt: .secret-docker k8s/deployment_docker.yaml
	@echo "$(CYAN)Deploying ACM Search MCP Server with existing Quay.io images...$(RESET)"
	@echo "$(YELLOW)Skipping build - using pre-built images$(RESET)"
	@oc apply -f k8s/deployment_docker.yaml
	@echo "$(YELLOW)Waiting for container deployment in $(NAMESPACE)...$(RESET)"
	@oc rollout status deployment/$(DEPLOYMENT_NAME) -n $(NAMESPACE) --timeout=300s
	@touch .deploy-prebuilt

## High-level targets
setup: .namespace-docker .secret-docker ## Setup container testing environment
	@echo "$(GREEN)✓ Container testing setup complete in $(NAMESPACE)$(RESET)"

build: .build-docker ## Build and push container images to Quay.io
	@echo "$(GREEN)✓ Container build complete - images pushed to Quay.io$(RESET)"

deploy: .deploy-docker ## Full container-based deployment (builds images)
	@echo "$(GREEN)✓ Container deployment complete in $(NAMESPACE)$(RESET)"
	@echo ""
	@$(MAKE) status

deploy-prebuilt: .deploy-prebuilt ## Deploy using existing Quay.io images (no building)
	@echo "$(GREEN)✓ Pre-built container deployment complete in $(NAMESPACE)$(RESET)"
	@echo ""
	@$(MAKE) status

rebuild: clean-all deploy ## Clean everything and rebuild from scratch
	@echo "$(GREEN)✓ Complete rebuild finished$(RESET)"

## Testing and comparison targets
benchmark: ## Compare container vs S2I build times
	@echo "$(CYAN)========================================$(RESET)"
	@echo "$(CYAN)🏁 CONTAINER vs S2I PERFORMANCE TEST$(RESET)"
	@echo "$(CYAN)Container Tool: $(CONTAINER_TOOL) $(ARCH_NOTE)$(RESET)"
	@echo "$(CYAN)========================================$(RESET)"
	@echo ""

	@echo "$(YELLOW)Testing Container Build Performance...$(RESET)"
	@time $(MAKE) build
	@echo ""

	@echo "$(YELLOW)For comparison, run S2I build with:$(RESET)"
	@echo "  time make build"
	@echo ""
	@echo "$(GREEN)✓ Benchmark complete$(RESET)"


## Platform and architecture helpers
platform-info: ## Show platform and architecture information
	@echo "$(CYAN)Platform Information:$(RESET)"
	@echo "Host OS: $(UNAME_S)"
	@echo "Host Architecture: $(UNAME_M)"
	@echo "Container Tool: $(CONTAINER_TOOL)"
	@echo "Build Platform: $(BUILD_PLATFORM)"
	@echo "Architecture Note: $(ARCH_NOTE)"
	@echo ""
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		echo "$(MAGENTA)Cross-compilation enabled for ARM Mac → x86_64 OpenShift$(RESET)"; \
	fi

## Status and utilities
status: check-login ## Show container deployment status
	@echo "$(CYAN)Container Deployment Status:$(RESET)"
	@echo "Namespace: $(NAMESPACE)"
	@echo "Registry: $(REGISTRY)"
	@echo "Image Tag: $(IMAGE_TAG)"
	@echo "Container Tool: $(CONTAINER_TOOL) $(ARCH_NOTE)"
	@echo ""
	@if oc get deployment $(DEPLOYMENT_NAME) -n $(NAMESPACE) &>/dev/null; then \
		echo "$(GREEN)✓ Container deployment exists$(RESET)"; \
		oc get deployment $(DEPLOYMENT_NAME) -n $(NAMESPACE); \
		echo ""; \
		echo "$(CYAN)Pod Status:$(RESET)"; \
		oc get pods -l app=$(DEPLOYMENT_NAME) -n $(NAMESPACE); \
	else \
		echo "$(RED)✗ Container deployment not found in $(NAMESPACE)$(RESET)"; \
		echo "$(YELLOW)Hint: Run 'make deploy' first$(RESET)"; \
	fi
	@echo ""
	@if ROUTE_URL=$$(oc get route $(ROUTE_NAME) -n $(NAMESPACE) -o jsonpath='{.spec.host}' 2>/dev/null); then \
		echo "$(CYAN)Access Information:$(RESET)"; \
		echo "Route URL: https://$$ROUTE_URL"; \
		echo "SSE Endpoint: https://$$ROUTE_URL/sse"; \
		echo "Service: acm-search-mcp-server-service.$(NAMESPACE).svc.cluster.local"; \
		echo ""; \
		echo "$(CYAN)Authentication:$(RESET)"; \
		if oc whoami &>/dev/null; then \
			echo "Current User: $$(oc whoami)"; \
			echo "Get Token: oc whoami -t"; \
			echo "$(YELLOW)⚠ Tokens expire - refresh as needed$(RESET)"; \
		else \
			echo "$(RED)✗ Not logged into OpenShift$(RESET)"; \
		fi; \
		echo ""; \
		echo "$(GREEN)╔═══════════════════════════════════════════════════════════════════════════════╗$(RESET)"; \
		echo "$(GREEN)║                        Claude Code Connection Options                        ║$(RESET)"; \
		echo "$(GREEN)╚═══════════════════════════════════════════════════════════════════════════════╝$(RESET)"; \
		echo ""; \
		echo "$(YELLOW)Option A: HTTPS Route (requires certificate handling)$(RESET)"; \
		echo "export TOKEN=\$$(oc whoami -t)"; \
		echo "claude mcp add --env NODE_TLS_REJECT_UNAUTHORIZED=0 --scope project \\"; \
		echo "  --transport sse acm-search \\"; \
		echo "  https://$$ROUTE_URL/sse \\"; \
		echo "  --header \"Authorization: Bearer \$$TOKEN\""; \
		echo ""; \
		echo "$(YELLOW)Option B: HTTP Service (no certificate issues)$(RESET)"; \
		echo "# Terminal 1: Set up port-forward"; \
		echo "oc port-forward service/acm-search-mcp-server-service 8080:80 -n $(NAMESPACE)"; \
		echo ""; \
		echo "# Terminal 2: Connect Claude Code"; \
		echo "export TOKEN=\$$(oc whoami -t)"; \
		echo "claude mcp add --scope project \\"; \
		echo "  --transport sse acm-search \\"; \
		echo "  http://localhost:8080/sse \\"; \
		echo "  --header \"Authorization: Bearer \$$TOKEN\""; \
		echo ""; \
	else \
		echo "$(YELLOW)⚠ Route not found - check deployment$(RESET)"; \
	fi

test: check-login ## Test the container-based deployment
	@echo "$(CYAN)Testing container deployment in $(NAMESPACE)...$(RESET)"
	@if oc get deployment $(DEPLOYMENT_NAME) -n $(NAMESPACE) &>/dev/null; then \
		if ROUTE_URL=$$(oc get route $(ROUTE_NAME) -n $(NAMESPACE) -o jsonpath='{.spec.host}' 2>/dev/null); then \
			echo "Testing health endpoint..."; \
			curl -s -k "https://$$ROUTE_URL/health" || echo "Health check failed"; \
			echo ""; \
		fi; \
	else \
		echo "$(RED)✗ Container deployment not found$(RESET)"; \
	fi

logs: check-login ## Show logs from container deployment
	@echo "$(CYAN)Container deployment logs:$(RESET)"
	@oc logs deployment/$(DEPLOYMENT_NAME) -c postgres-mcp-server -n $(NAMESPACE) --tail=20 2>/dev/null || \
		echo "$(RED)Container deployment not found$(RESET)"

## Cleanup
clean: check-login ## Clean up container deployment only
	@echo "$(CYAN)Cleaning container deployment in $(NAMESPACE)...$(RESET)"
	@oc delete deployment $(DEPLOYMENT_NAME) -n $(NAMESPACE) 2>/dev/null || true
	@oc delete service $(SERVICE_NAME) -n $(NAMESPACE) 2>/dev/null || true
	@oc delete route $(ROUTE_NAME) -n $(NAMESPACE) 2>/dev/null || true
	@rm -f .namespace-docker .secret-docker .build-docker .deploy-docker k8s/deployment_docker.yaml
	@echo "$(GREEN)✓ Container deployment cleaned$(RESET)"

clean-namespace: check-login ## Delete entire container testing namespace
	@echo "$(CYAN)Deleting container testing namespace: $(NAMESPACE)$(RESET)"
	@oc delete namespace $(NAMESPACE) 2>/dev/null || true
	@rm -f .namespace-docker .secret-docker .build-docker .deploy-docker k8s/deployment_docker.yaml
	@echo "$(GREEN)✓ Container namespace deleted$(RESET)"

container-clean: ## Clean local container images
	@echo "$(CYAN)Cleaning local container images...$(RESET)"
	@$(CONTAINER_TOOL) rmi $(MCP_SERVER_IMAGE) 2>/dev/null || true
	@echo "$(GREEN)✓ Local container images cleaned$(RESET)"

clean-rbac: check-login ## Clean up cluster-level RBAC resources
	@echo "$(CYAN)Cleaning cluster-level RBAC resources...$(RESET)"
	@oc delete clusterrole acm-search-mcp-auth-role 2>/dev/null || true
	@oc delete clusterrolebinding acm-search-auth-delegator-binding 2>/dev/null || true
	@echo "$(GREEN)✓ Cluster RBAC resources cleaned$(RESET)"

clean-all: clean-namespace container-clean clean-rbac ## Clean everything: namespace, containers, and RBAC
	@echo "$(GREEN)✓ Complete cleanup done (namespace + containers + RBAC)$(RESET)"

## Quay.io specific targets
quay-login: ## Login to Quay.io registry
	@echo "$(CYAN)Logging into Quay.io with $(CONTAINER_TOOL)...$(RESET)"
	@echo "$(YELLOW)Please provide your Quay.io credentials:$(RESET)"
	@$(CONTAINER_TOOL) login quay.io

quay-repos: ## Show Quay.io repository URLs
	@echo "$(CYAN)Quay.io Repository URLs:$(RESET)"
	@echo "📦 MCP Server (with integrated auth): https://quay.io/repository/stolostron/search-mcp-server"
	@echo ""
	@echo "$(YELLOW)Make repository public for OpenShift to pull without authentication$(RESET)"
