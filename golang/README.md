# ACM Search MCP Server (Go)

Model Context Protocol (MCP) server providing access to Red Hat Advanced Cluster Management (ACM) search database and Kubernetes resources across managed clusters.

## Quick Start

### Production Deployment (Recommended)

```bash
# One-command deployment with ACM auto-discovery
helm install acm-mcp-server ./helm/acm-mcp-server --create-namespace --namespace acm-search

# Or with custom registry
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace --namespace acm-search \
  --set image.repository=quay.io/yourorg/acm-mcp-server-go
```

### Development/Testing

```bash
# Local development
DATABASE_URL="postgresql://user:pass@acm-hub:5432/search" go run ./cmd/server

# HTTP transport (for web/API access)
DATABASE_URL="your-db-url" go run ./cmd/server --transport=http --port=8080

# STDIO transport (for Claude Desktop integration)
DATABASE_URL="your-db-url" go run ./cmd/server --transport=stdio
```

## Available Tools

- **`find_resources`** - Advanced Kubernetes resource search across all managed clusters with comprehensive filtering:
  - **Basic filters**: kind, name, namespace, cluster, status
  - **Advanced filters**: labelSelector, clusterSelector, textSearch, ageNewerThan, ageOlderThan
  - **Output control**: outputMode (list/count/summary/health), groupBy, sortBy, sortOrder, limit, countOnly

## Authentication (Production)

Authentication auto-enables in Kubernetes environments:

```bash
# Zero-config production deployment (auth auto-enabled)
helm install acm-mcp-server ./helm/acm-mcp-server --create-namespace --namespace acm-search

# Disable auth for testing (not recommended in production)
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace --namespace acm-search \
  --set authentication.enabled=false

# Local testing with RBAC
MCP_ENABLE_AUTH=true MCP_KUBECONFIG=~/.kube/config DATABASE_URL="..." go run ./cmd/server
```

## Helm Deployment

Complete Helm deployment with ACM auto-discovery and authentication:

```bash
# Install (auto-discovers ACM database credentials)
helm install acm-mcp-server ./helm/acm-mcp-server --create-namespace --namespace acm-search

# Check status
helm status acm-mcp-server --namespace acm-search
kubectl get pods,svc,route -n acm-search

# Test deployment
make test-mcp-deployed

# Uninstall
helm uninstall acm-mcp-server --namespace acm-search
```

See [`helm-install.md`](helm-install.md) for complete Helm deployment guide.

### Makefile Targets

```bash
make help                   # Show all available targets
make build                  # Build Go binary
make run                    # Build and run locally
make container-build        # Build container image
make helm-install           # Deploy with Helm
make helm-upgrade          # Upgrade existing deployment
make test-mcp-deployed     # Test deployed server
```

## Configuration

All configuration via environment variables.

**Required:**
- `DATABASE_URL` - PostgreSQL connection to ACM search database

**Common Options:**
- `MCP_TRANSPORT_MODE=auto|http|stdio` (default: auto)
- `MCP_ENABLE_AUTH=true|false` (default: auto-detect)
- `MCP_HTTP_PORT=8080` (HTTP transport port)

## Examples

```bash
# Basic: Find all failing pods across fleet
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"kind":"Pod","status":"Failed,Error,CrashLoopBackOff"}}}' | go run ./cmd/server

# Advanced: Find pods with specific labels created in last hour
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"kind":"Pod","labelSelector":"app=nginx","ageNewerThan":"1h","outputMode":"count","groupBy":"status"}}}' | go run ./cmd/server

# Complex: Health analysis of resources across production clusters
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"clusterSelector":"env=prod","outputMode":"health","ageOlderThan":"1w"}}}' | go run ./cmd/server

# Web interface
curl -X POST http://localhost:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' -H "Content-Type: application/json"
```

Built for Red Hat Advanced Cluster Management search integration.