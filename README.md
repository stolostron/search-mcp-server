# MCP Server for Red Hat ACM

Model Context Protocol (MCP) server providing access to Red Hat Advanced Cluster Management (ACM) search database and Kubernetes resources across managed clusters.

**This project is Tech Preview. Do not deploy in production environments.**

## Quick Start

**Pre-requisites:** 
- Red Hat Advanced Cluster Management
- [Helm](https://helm.sh/)

### Deploy from Helm repository

```bash
helm repo add acm-mcp-server https://raw.githubusercontent.com/stolostron/search-mcp-server/main/charts
helm install acm-mcp-server acm-mcp-server/acm-mcp-server --create-namespace --namespace acm-search
```
For additional deployment options see [Helm Deployment](#helm-deployment).

### Access the MCP server

#### Option 1: Connect from Claude Code

```bash
ROUTE_URL=$(oc get route acm-mcp-server -n acm-search -o jsonpath='{.spec.host}')
TOKEN=$(oc whoami -t)
claude mcp add --transport http acm-search \
  "https://$ROUTE_URL/mcp" \
  --header "Authorization: Bearer $TOKEN"

# NOTE: If your cluster uses a self-signed certificate, start claude code with
# NODE_TLS_REJECT_UNAUTHORIZED=0 claude
```

#### Option 2: Using cURL

```bash
# Get the OpenShift ROUTE_URL and TOKEN
ROUTE_URL=$(oc get route acm-mcp-server -n acm-search -o jsonpath='{.spec.host}')
TOKEN=$(oc whoami -t)

# Search for failing pods across the fleet
curl -k -X POST "https://$ROUTE_URL/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"kind":"Pod","status":"Failed,Error,CrashLoopBackOff","limit":5}}}'
```

## Available Tools

- **`find_resources`** - Advanced Kubernetes resource search across all managed clusters with comprehensive filtering:
  - **Basic filters**: kind, name, namespace, cluster, status
  - **Advanced filters**: labelSelector, clusterSelector, textSearch, ageNewerThan, ageOlderThan
  - **Output control**: outputMode (list/count/summary/health), groupBy, sortBy, sortOrder, limit, countOnly

## Development/Testing

```bash
# Local development
DATABASE_URL="postgresql://user:pass@acm-hub:5432/search" go run ./cmd/server

# HTTP transport (for web/API access)
DATABASE_URL="your-db-url" go run ./cmd/server --transport=http --port=8080

# STDIO transport (for Claude Desktop integration)
DATABASE_URL="your-db-url" go run ./cmd/server --transport=stdio
```

## Authentication (in-cluster)

Authentication auto-enables in Kubernetes environments:

```bash
# Zero-config deployment (auth auto-enabled)
helm install acm-mcp-server ./helm/acm-mcp-server --create-namespace --namespace acm-search

# Disable auth for testing (not recommended for production)
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace --namespace acm-search \
  --set authentication.enabled=false

# Local testing with RBAC
MCP_ENABLE_AUTH=true MCP_KUBECONFIG=~/.kube/config DATABASE_URL="..." go run ./cmd/server
```

## Helm Deployment

### Deployment alternatives and customization

```bash
# Deploy from local chart
helm install acm-mcp-server ./helm/acm-mcp-server --create-namespace --namespace acm-search

# With custom configuration
helm install acm-mcp-server acm-mcp-server/acm-mcp-server \
  --create-namespace --namespace acm-search \
  --set image.repository=quay.io/stolostron/search-mcp-server \
  --set app.logLevel=debug
```

### Complete Helm deployment with ACM auto-discovery and authentication

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

See [`docs/helm-install.md`](docs/helm-install.md) for complete Helm deployment guide.

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

All configuration via environment variables or Helm values.

**Required:**
- `DATABASE_URL` - PostgreSQL connection to ACM search database (auto-discovered in Helm)

**Common Options:**
- `MCP_TRANSPORT_MODE=auto|http|stdio` (default: auto)
- `MCP_ENABLE_AUTH=true|false` (default: auto-detect)
- `MCP_HTTP_PORT=8080` (HTTP transport port)
- `LOG_LEVEL=info|debug` (default: info) - Controls logging verbosity

**Chart.yaml-Driven Configuration:**
All app metadata is sourced from Chart.yaml:
- `APP_NAME` - Application name (from chart name)
- `APP_DISPLAY_NAME` - Display name (from chart metadata)
- `APP_DESCRIPTION` - Application description (from chart description)
- `APP_VERSION` - Version (from chart appVersion)

**Debug Configuration:**
```bash
# Enable debug logging for troubleshooting
helm install acm-mcp-server acm-mcp-server/acm-mcp-server \
  --create-namespace --namespace acm-search \
  --set app.logLevel=debug
# Shows: configuration dump, database connectivity details, health check logs
```

## Examples

```bash
# Basic: Find all failing pods across fleet
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"kind":"Pod","status":"Failed,Error,CrashLoopBackOff"}}}' | go run ./cmd/server

# Advanced: Find pods with specific labels created in last hour
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"kind":"Pod","labelSelector":"app=nginx","ageNewerThan":"1h","outputMode":"count","groupBy":"status"}}}' | go run ./cmd/server

# Complex: Health analysis of resources across production clusters
echo '{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"find_resources","arguments":{"clusterSelector":"env=prod","outputMode":"health","ageOlderThan":"1w"}}}' | go run ./cmd/server

# Web interface (auth disabled / local HTTP)
curl -X POST http://localhost:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' -H "Content-Type: application/json"
```

Built for Red Hat Advanced Cluster Management search integration.

Updated: 07/22/2026
