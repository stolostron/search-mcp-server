# ACM Search MCP Server

A Model Context Protocol (MCP) server that provides secure access to ACM (Advanced Cluster Management) search databases. Enables AI assistants and MCP clients to query and analyze Kubernetes resources across managed clusters through a standardized interface.

## Features

- **ACM Resource Analysis**: Advanced search across Kubernetes resources in managed clusters
- **Wildcard Namespace Filtering**: Support for shell-style patterns like `open-cluster-management*`
- **Multiple Output Modes**: Tables, counts, health analysis, and summaries
- **Security**: Kubernetes token authentication with ACM admin authorization
- **Performance**: Fast queries with configurable limits and optimization
- **Multiple Transport Modes**: SSE server and stdio support

## Quick Start

### Prerequisites
- OpenShift CLI (`oc`) installed and configured
- Access to an OpenShift cluster with Red Hat ACM deployed
- User account with proper permissions (see [Authorization](#authorization))

### Deployment
```bash
# 1. Login to cluster
oc login https://your-cluster-url

# 2. Generate database connection secret (auto-discovers ACM)
./scripts/create-secret.sh

# 3. Deploy using pre-built images
make deploy-prebuilt

# 4. Get connection info
make status
```

### Connection
```bash
# Get your connection details
export TOKEN=$(oc whoami -t)
export ROUTE_URL=$(oc get route acm-search-mcp-server-route -n acm-search -o jsonpath='{.spec.host}')

# Option A: HTTPS Route (requires certificate handling)
# this env has to be exported if using the https route
export NODE_TLS_REJECT_UNAUTHORIZED=0
claude mcp add --env NODE_TLS_REJECT_UNAUTHORIZED=0 --scope project \
  --transport sse acm-search \
  https://$ROUTE_URL/sse \
  --header "Authorization: Bearer $TOKEN"

# Option B: HTTP Port-forward (no certificate issues)
oc port-forward service/acm-search-mcp-server-service 8080:80 -n acm-search
claude mcp add --scope project --transport sse acm-search \
  http://localhost:8080/sse --header "Authorization: Bearer $TOKEN"
```

## Tools & Usage

### Available Tools

**Default Mode** (secure):
- `find_resources` - Advanced search across ACM managed cluster resources

### Wildcard Namespace Filtering

```json
// Single wildcard - all open-cluster-management namespaces
{
  "name": "find_resources",
  "arguments": {
    "kind": "Pod",
    "namespace": "open-cluster-management*"
  }
}

// Multiple patterns - mix exact and wildcard
{
  "name": "find_resources",
  "arguments": {
    "kind": "Pod",
    "namespace": "kube-*,default,openshift-*"
  }
}
```

**Supported patterns**: `*` (any chars), `?` (single char), comma-separated lists

### Output Modes

- **Default**: Table format with resource details
- **count**: Resource counts with percentages
- **health**: Health analysis with status breakdown
- **summary**: Overview statistics by cluster/namespace/kind

## Authentication & Authorization

### User Requirements

Access is granted to users who have **ANY** of the following:

1. **System Cluster Admin Groups**:
   - `system:masters` (traditional cluster admins)
   - `system:cluster-admins` (OpenShift cluster admins like `kube:admin`)

2. **Custom Groups with Cluster Admin Role**:
   - Any group granted `cluster-admin` via ClusterRoleBindings
   - Example: Custom LDAP groups like "ACM-Demo", "Platform-Admins"

3. **ACM Admin Permissions**:
   - Users who can create ManagedClusters (`managedclusters.cluster.open-cluster-management.io`)
   - Users with `open-cluster-management:cluster-manager-admin` ClusterRole

### Granting Access

**For LDAP/Corporate Users:**
```bash
# Grant ACM admin role
oc adm policy add-cluster-role-to-user open-cluster-management:cluster-manager-admin user@company.com

# Or create custom group with cluster-admin (for broader access)
oc adm policy add-cluster-role-to-group cluster-admin platform-admins
```

**For Service Accounts:**
```bash
oc create sa acm-search-automation -n acm-search
oc adm policy add-cluster-role-to-user open-cluster-management:cluster-manager-admin \
  system:serviceaccount:acm-search:acm-search-automation
export TOKEN=$(oc create token acm-search-automation -n acm-search --duration=8760h)
```

### MCP Server RBAC Requirements

The MCP server deployment requires these **specific permissions** (NOT `system:auth-delegator`):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: acm-search-mcp-auth-role
rules:
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews"]
  verbs: ["create"]                          # Token validation only
# Note: Uses SelfSubjectAccessReview with user's token for permission checking
```

This ClusterRole is automatically created by `make deploy-prebuilt`.

### Authorization Flow

1. **Token Validation** → Kubernetes TokenReview API validates bearer token
2. **Cluster Admin Check** → SelfSubjectAccessReview with user's token for `*` `*` permissions
3. **ACM Admin Check** → SelfSubjectAccessReview with user's token for ManagedCluster creation
4. **Grant/Deny Access** → Allow if either check passes (either/or logic)

## Troubleshooting

### Common Issues

**Access Denied Error:**
```bash
# Check your permissions
oc auth can-i create managedclusters.cluster.open-cluster-management.io
oc auth can-i "*" "*" --all-namespaces

# Grant ACM admin if needed
oc adm policy add-cluster-role-to-user open-cluster-management:cluster-manager-admin $(oc whoami)
```

**Database Connection Issues:**
```bash
# Check ACM namespace and database secret
oc get secret --all-namespaces | grep search-postgres

# Regenerate secret if wrong namespace detected
./scripts/create-secret.sh
oc rollout restart deployment/acm-search-mcp-server -n acm-search
```

**Pod CrashLoopBackOff:**
```bash
# Check logs for errors
make logs

# Usually database auth - regenerate secret with current credentials
./scripts/create-secret.sh
oc rollout restart deployment/acm-search-mcp-server -n acm-search
```

**Token Authentication Failed:**
```bash
# Verify token and headers
export TOKEN=$(oc whoami -t)
curl -k -H "Authorization: Bearer $TOKEN" https://your-route/info

# Test both header formats
curl -k -H "kubernetes-authorization: Bearer $TOKEN" https://your-route/info
```

### ACM Namespace Discovery

The deployment automatically discovers ACM namespace, but if it fails:

```bash
# Find ACM manually
oc get secret --all-namespaces | grep search-postgres
oc get namespace | grep -E "(acm|ocm|open-cluster|multicluster)"
oc get multiclusterhub -A --no-headers | awk '{print $1;}'

# Common ACM namespaces: open-cluster-management, ocm, rhacm, multicluster-engine
```

## Development

### Build & Deploy
```bash
# 1. Install dependencies and compile TypeScript
npm install                 # Install Node.js dependencies
npm run build               # Compile TypeScript to JavaScript (dist/)

# 2. Deploy
./scripts/create-secret.sh  # Generate database connection
make deploy                 # Build container + deploy custom image
make rebuild                # Clean everything + rebuild from scratch
```

**Important**: Always run `npm run build` after modifying TypeScript files, as the container uses compiled JavaScript from `dist/`.

### Testing & Operations
```bash
make status                 # Deployment health + connection info
make test                   # Test all endpoints
make logs                   # View server logs
make clean-all              # Remove everything
```

### Project Structure
```
src/
├── auth/token-validator.ts     # Kubernetes token validation + ACM authorization
├── find-resources/             # ACM resource search functionality
├── utils/cross-resource.ts     # Resource filtering with wildcard support
├── server.ts                   # Core MCP server implementation
└── http-server.ts              # SSE server entry point
```

## Reference

### Quick Commands
| Task | Command |
|------|---------|
| **Deploy** | `./scripts/create-secret.sh && make deploy-prebuilt` |
| **Export** | `export NODE_TLS_REJECT_UNAUTHORIZED=0` |
| **Connect** | `claude mcp add --scope project --transport sse acm-search https://route/sse --header "Authorization: Bearer $TOKEN"` |
| **Status** | `make status` |
| **Logs** | `make logs` |
| **Clean** | `make clean-all` |

### Connection URLs
- **HTTPS Route**: `https://acm-search-mcp-server-route-acm-search.apps.CLUSTER_DOMAIN/sse`
- **HTTP Port-forward**: `http://localhost:8080/sse` (with `oc port-forward service/acm-search-mcp-server-service 8080:80 -n acm-search`)

### Registry
- **Image**: `quay.io/stolostron/search-mcp-server:dev-preview`
- **Repository**: `git@github.com:stolostron/search-mcp-server.git`

## License

MIT License