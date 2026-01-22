# ACM MCP Server - Helm Installation Guide

## Overview

The ACM Search MCP (Model Context Protocol) Server provides access to ACM search data through a standardized MCP interface. This guide covers installation using Helm with **automatic ACM database discovery**.

## Prerequisites

- OpenShift/Kubernetes cluster with ACM (Advanced Cluster Management) installed
- Helm 3.x installed locally
- `oc` or `kubectl` access to the cluster

## Installation Methods

### 🚀 Current Installation (Local Chart)

Since the Helm chart is not yet published to a repository, use the local chart:

```bash
# Clone the repository (if not already done)
git clone https://github.com/stolostron/search-mcp-server.git
cd search-mcp-server/golang

# Install with auto-discovery (recommended)
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace \
  --namespace acm-search

# That's it! The chart automatically:
# - Discovers your ACM MultiClusterHub installation
# - Extracts database credentials from ACM
# - Builds the complete database connection URL
# - Deploys the MCP server with proper configuration
```

### 🎯 Future Installation (Helm Repository)

*Once the chart is published to a Helm repository:*

```bash
# Add the ACM MCP Helm repository
helm repo add acm-mcp https://charts.example.com  # (when published)
helm repo update

# Install with auto-discovery
helm install acm-mcp-server acm-mcp/acm-mcp-server \
  --create-namespace \
  --namespace acm-search

# ↑ This just works - no Makefile, no manual secrets!
```

## Configuration Options

### Auto-Discovery Mode (Default)

The chart automatically discovers ACM installation and database credentials:

```yaml
# Default behavior - no configuration needed
database:
  autoDiscover: true  # Default
```

**How it works:**
1. Finds `MultiClusterHub` custom resource anywhere in the cluster
2. Identifies the ACM namespace (where MCH is installed)
3. Extracts `search-postgres` secret from the same namespace
4. Builds complete database URL: `postgresql://user:pass@search-postgres.acm-namespace.svc.cluster.local:5432/search`

### Manual Override Mode

For custom setups or non-standard ACM installations:

```bash
# Disable auto-discovery and provide manual database URL
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace \
  --namespace acm-search \
  --set database.autoDiscover=false \
  --set database.url="postgresql://searchuser:password@custom-host:5432/search"
```

Or using a values file:

```yaml
# custom-values.yaml
database:
  autoDiscover: false
  url: "postgresql://searchuser:mypassword@custom-postgres.example.com:5432/search"
```

```bash
helm install acm-mcp-server ./helm/acm-mcp-server \
  -f custom-values.yaml \
  --namespace acm-search
```

## Verification

### Check Installation Status

```bash
# Check Helm release
helm status acm-mcp-server --namespace acm-search

# Check pod status
kubectl get pods -n acm-search

# Check service and route
kubectl get svc,route -n acm-search
```

### Test MCP Server Health

```bash
# Get the route URL
ROUTE_URL=$(oc get route acm-mcp-server -n acm-search -o jsonpath='{.spec.host}')

# Test health endpoint
curl -k "https://$ROUTE_URL/health"

# Expected response: {"status":"healthy", ...}
```

### Run Complete Test Suite

```bash
# Using the provided test script
./test-mcp-server.sh "https://$ROUTE_URL"

# Or using Make (for developers)
make test-mcp-deployed
```

## Troubleshooting

### ACM Not Found

**Error**: `ACM auto-discovery enabled but no MultiClusterHub found`

**Solution**:
- Verify ACM is installed: `oc get multiclusterhub --all-namespaces`
- If ACM is not installed, use manual mode: `--set database.autoDiscover=false`

### Database Secret Not Found

**Error**: `ACM MultiClusterHub found in namespace 'X', but search-postgres secret not found`

**Solution**:
- Check if ACM search component is enabled
- Verify secret exists: `oc get secret search-postgres -n <acm-namespace>`

### Permission Issues

**Error**: `lookup` permission errors during Helm install

**Solution**:
- Ensure your user has cluster-admin or sufficient RBAC permissions
- The Helm client needs to read MultiClusterHub and Secret resources cluster-wide

## Advanced Configuration

### Custom Image Repository

```bash
helm install acm-mcp-server ./helm/acm-mcp-server \
  --set image.repository=quay.io/your-org/acm-mcp-server-go \
  --set image.tag=v1.0.0
```

### Resource Limits

```bash
helm install acm-mcp-server ./helm/acm-mcp-server \
  --set resources.requests.memory=256Mi \
  --set resources.limits.memory=1Gi
```

### Authentication Settings

```bash
# Enable authentication (for production)
helm install acm-mcp-server ./helm/acm-mcp-server \
  --set authentication.enabled=true

# Disable authentication (for testing)
helm install acm-mcp-server ./helm/acm-mcp-server \
  --set authentication.enabled=false
```

## Uninstallation

```bash
# Remove the Helm release
helm uninstall acm-mcp-server --namespace acm-search

# Optionally remove the namespace
kubectl delete namespace acm-search
```

## Connection to Claude Code

After successful installation, connect to Claude Code MCP:

```bash
# Get the route URL
ROUTE_URL=$(oc get route acm-mcp-server -n acm-search -o jsonpath='{.spec.host}')

# Add to Claude Code (authentication disabled)
claude mcp add --transport http acm-search \
  "https://$ROUTE_URL/mcp"

# For authenticated setup (when enabled)
TOKEN=$(oc whoami -t)
claude mcp add --transport http acm-search \
  "https://$ROUTE_URL/mcp" \
  --header "Authorization: Bearer $TOKEN"
```

## Support

- **Issues**: Report at https://github.com/stolostron/search-mcp-server/issues
- **Documentation**: https://github.com/stolostron/search-mcp-server
- **ACM Documentation**: https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes