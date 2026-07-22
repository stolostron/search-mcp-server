# MCP Server for Red Hat ACM - Helm Installation Guide

## Overview

The MCP (Model Context Protocol) Server for Red Hat ACM  provides access to ACM search data through a standardized MCP interface. This guide covers installation using Helm with **automatic ACM database discovery**.

## Prerequisites

- OpenShift/Kubernetes cluster with ACM (Advanced Cluster Management) installed
- Helm 3.x installed locally
- `oc` or `kubectl` access to the cluster

## Installation Methods

### 🚀 Recommended Installation (Helm Repository)

Install from the published Helm repository:

```bash
# Add the ACM MCP Helm repository
helm repo add acm-search https://raw.githubusercontent.com/stolostron/search-mcp-server/main/charts
helm repo update

# Install with auto-discovery (recommended)
helm install acm-mcp-server acm-search/acm-mcp-server \
  --create-namespace \
  --namespace acm-search

# That's it! The chart automatically:
# - Discovers your ACM MultiClusterHub installation
# - Extracts database credentials from ACM
# - Builds the complete database connection URL
# - Deploys the MCP server with proper configuration
```

### 🔧 Local Development Installation

For development or when using local chart modifications:

```bash
# Clone the repository
git clone https://github.com/stolostron/search-mcp-server.git
cd search-mcp-server

# Install with auto-discovery from local chart
helm install acm-mcp-server ./helm/acm-mcp-server \
  --create-namespace \
  --namespace acm-search
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
3. Extracts database credentials from the same namespace, preferring the dedicated
   read-only `search-postgres-mcp-readonly` secret (`search_mcp_ro` role). If that secret
   doesn't exist yet — e.g. the hub's `search-v2-operator` hasn't been upgraded to a
   version that provisions it ([ACM-32474](https://issues.redhat.com/browse/ACM-32474)) —
   it falls back to the legacy read-write `search-postgres` admin secret so the chart still
   installs. This fallback will be removed once the minimum supported ACM/MCE version
   always provisions the read-only secret.
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

### Database Connection URL Not Found

**Error (auto-discovery mode)**: `acm-mcp-server: could not determine the database connection URL. With database.autoDiscover=true, neither the search-postgres-mcp-readonly nor the search-postgres Secret was found with non-empty database-user, database-password, and database-name fields (or no MultiClusterHub resource exists) in the ACM namespace ...`

**Solution**:
- Check if ACM search component is enabled
- Verify one of the secrets exists (preferred): `oc get secret search-postgres-mcp-readonly -n <acm-namespace>`
- Or the legacy admin secret (older `search-v2-operator`): `oc get secret search-postgres -n <acm-namespace>`

**Error (manual mode)**: `acm-mcp-server: database.url is required when database.autoDiscover=false ...`

**Solution**:
- Provide a valid PostgreSQL connection URL: `--set database.url=postgresql://user:password@host:5432/dbname`

### Permission Issues

**Error**: `lookup` permission errors during Helm install

**Solution**:
- Ensure your user has cluster-admin or sufficient RBAC permissions
- The Helm client needs to read MultiClusterHub and Secret resources cluster-wide

### Pod Startup Issues

**Problem**: Pod fails to start or shows connection errors

**Debug Steps**:
1. **Enable debug logging**:
   ```bash
   helm upgrade acm-mcp-server acm-search/acm-mcp-server --set logLevel=debug -n acm-search
   ```

2. **Check configuration dump**:
   ```bash
   kubectl logs -l app.kubernetes.io/name=acm-mcp-server -n acm-search | head -20
   # Look for: "MCP Server initialized: ServerConfig{...}"
   ```

3. **Verify database connectivity**:
   ```bash
   kubectl logs -l app.kubernetes.io/name=acm-mcp-server -n acm-search | grep -i "database\|connection"
   # Look for: "[MCP-SERVER-DEBUG] Database connection test result: true"
   ```

## Debugging and Logging

### Enable Debug Logging

For troubleshooting, enable debug logging to see detailed configuration and database connectivity:

```bash
# Install with debug logging
helm install acm-mcp-server acm-search/acm-mcp-server \
  --set logLevel=debug \
  --namespace acm-search

# Or upgrade existing installation
helm upgrade acm-mcp-server acm-search/acm-mcp-server \
  --set logLevel=debug \
  --namespace acm-search

# Check debug logs
kubectl logs -l app.kubernetes.io/name=acm-mcp-server -n acm-search --tail=50
```

**Debug logging shows:**
- Complete configuration dump at startup
- Database connectivity test results
- Pool statistics and health checks
- Detailed MCP request/response logging

### Log Level Options

```yaml
# values.yaml or --set logLevel=<value>
logLevel: "info"    # Default: standard operational logs
logLevel: "debug"   # Verbose: includes configuration dump, connectivity details
```

## Advanced Configuration

### Custom Image Repository

```bash
helm install acm-mcp-server ./helm/acm-mcp-server \
  --set image.repository=quay.io/stolostron/search-mcp-server \
  --set image.tag=v0.1.0
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
helm install acm-mcp-server acm-search/acm-mcp-server \
  --set authentication.enabled=true

# Disable authentication (for testing)
helm install acm-mcp-server acm-search/acm-mcp-server \
  --set authentication.enabled=false
```

### Chart-Driven Configuration

The application metadata is automatically sourced from Chart.yaml, ensuring consistency:

- **App Name**: `acm-mcp-server` (from Chart name)
- **Display Name**: `MCP Server for Red Hat ACM` (from Chart metadata)
- **Description**: Auto-sourced from Chart description
- **Version**: Always matches Chart `appVersion`

This eliminates hardcoded values and ensures version consistency across deployments.

**Environment Variables Set:**
```bash
APP_NAME=acm-mcp-server
APP_DISPLAY_NAME=MCP Server for Red Hat ACM
APP_DESCRIPTION=MCP server for Red Hat Advanced Cluster Management...
APP_VERSION=0.1.0  # Matches Chart appVersion
LOG_LEVEL=info     # From values.yaml
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

# For authenticated setup (when enabled)
TOKEN=$(oc whoami -t)
claude mcp add --transport http -s project acm-search \
  "https://$ROUTE_URL/mcp" \
  --header "Authorization: Bearer $TOKEN"
```

## Support

- **Issues**: Report at https://github.com/stolostron/search-mcp-server/issues
- **Documentation**: https://github.com/stolostron/search-mcp-server
- **ACM Documentation**: https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes