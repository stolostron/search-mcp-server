# Authentication & Authorization Architecture

## Overview

The ACM Search MCP Server implements a sophisticated **middleware chain pattern** for authentication and authorization, providing fine-grained access control for multi-cluster Kubernetes resources.

## Middleware Chain Architecture

### Request Flow

```
HTTP Request → Auth Middleware → CORS Middleware → Route Handler → Business Logic
```

### Detailed Flow Diagram

```
┌─────────────────┐    ┌──────────────────┐    ┌───────────────┐    ┌─────────────────┐
│   HTTP Request  │───▶│  Auth Middleware │───▶│ CORS Middleware│───▶│  Route Handler  │
│                 │    │                  │    │               │    │                 │
│ Bearer Token    │    │ 1. Validate Token│    │ 1. Set Headers│    │ 1. Extract User │
│ Headers         │    │ 2. Check RBAC    │    │ 2. Handle CORS│    │ 2. Apply Filters│
│ Request Body    │    │ 3. Set UserCtx   │    │ 3. Pass Through│    │ 3. Execute Logic│
└─────────────────┘    └──────────────────┘    └───────────────┘    └─────────────────┘
                                ▼                        ▼                       ▼
                       ┌──────────────────┐    ┌───────────────┐    ┌─────────────────┐
                       │   UserContext    │    │   Enhanced    │    │   Filtered      │
                       │   Attached to    │    │   Request     │    │   Response      │
                       │   Request        │    │   Context     │    │   Based on Role │
                       └──────────────────┘    └───────────────┘    └─────────────────┘
```

## Authentication Middleware Deep Dive

### 1. Token Extraction

The middleware supports multiple authentication headers for flexibility:

```go
func (m *AuthMiddleware) extractAuthHeader(r *http.Request) (string, string) {
    // Standard OAuth/Bearer token
    if authHeader := r.Header.Get("Authorization"); authHeader != "" {
        return authHeader, "Authorization"
    }

    // Custom Kubernetes header (for service accounts)
    if authHeader := r.Header.Get("kubernetes-authorization"); authHeader != "" {
        return authHeader, "kubernetes-authorization"
    }

    return "", ""
}
```

**Supported Headers:**
- `Authorization: Bearer <token>` - Standard OAuth/OIDC tokens
- `kubernetes-authorization: Bearer <token>` - Kubernetes service account tokens

### 2. Token Validation

The middleware validates tokens via Kubernetes `TokenReview` API:

```go
type TokenValidationResult struct {
    Valid bool         `json:"valid"`
    User  *UserContext `json:"user,omitempty"`
    Error string       `json:"error,omitempty"`
}
```

**Validation Process:**
1. **TokenReview API Call** - Kubernetes validates the token
2. **User Information Extraction** - Gets username, UID, groups
3. **Permission Checks** - Verifies ACM access rights
4. **Caching** - Optional token caching for performance

### 3. User Context Creation

The validated user information is structured as:

```go
type UserContext struct {
    Username     string    `json:"username"`      // "alice@company.com"
    UID          string    `json:"uid"`           // "8f8c1bd0-..."
    Groups       []string  `json:"groups"`        // ["admins", "developers"]
    AuthMethod   string    `json:"auth_method"`   // "bearer"
    HeaderSource string    `json:"header_source"` // "Authorization"
    ValidatedAt  time.Time `json:"validated_at"`
}
```

### 4. Context Injection ⭐

The **critical step** - injecting user context into the request:

```go
// Add user context to request context
ctx := WithUserContext(r.Context(), validationResult.User)
next.ServeHTTP(w, r.WithContext(ctx))
```

**Key Benefits:**
- **Thread-safe** - Each request gets its own context
- **Request-scoped** - User context travels with the request
- **Immutable** - Context cannot be modified downstream
- **Type-safe** - Strongly typed UserContext struct

## Authorization Flow

### 1. User Context Extraction

Any handler in the chain can access the authenticated user:

```go
func (t *HTTPTransport) handleMCP(w http.ResponseWriter, r *http.Request) {
    // Extract user context (set by auth middleware)
    userCtx := auth.UserFromContext(r.Context())

    // Route to appropriate handler with user context
    switch method {
    case "tools/list":
        t.handleToolsList(w, requestID, userCtx)
    case "tools/call":
        t.handleToolsCall(w, requestID, params, userCtx)
    }
}
```

### 2. Tool-Level Authorization

Different tools require different permission levels:

```go
func GetAuthorizedTools(userCtx *UserContext) []string {
    tools := []string{}

    // Everyone gets find_resources (with potential filtering)
    tools = append(tools, "find_resources")

    return tools
}
```

### 3. Permission Checking

```go
func (u *UserContext) HasACMAdmin() bool {
    clusterAdminGroups := []string{"system:masters", "cluster-admins"}
    for _, group := range u.Groups {
        for _, adminGroup := range clusterAdminGroups {
            if group == adminGroup {
                return true
            }
        }
    }
    return false
}
```

## Current Permission Model

### Binary Access Control

The current implementation uses **binary access control**:

```
┌─────────────────┐    ┌─────────────────────┐    ┌──────────────────┐
│   User Token    │───▶│   ACM Admin Check   │───▶│   Tool Access    │
│                 │    │                     │    │                  │
│ Bearer eyJ0...  │    │ ✓ system:masters    │    │ ✓ find_resources │
│                 │    │ ✓ cluster-admins    │    │ ✓ find_resources │
│                 │    │ ✗ regular user      │    │ ✓ find_resources │
└─────────────────┘    └─────────────────────┘    └──────────────────┘
```

**Current Logic:**
- ✅ **All authenticated users** → `find_resources` access
- ❌ **Unauthenticated** → No access

## Enhanced RBAC Architecture (Proposed)

### Granular Resource-Based Access Control

Your proposed enhancement moves to **granular RBAC**:

```
┌─────────────────┐    ┌─────────────────────┐    ┌──────────────────┐
│   User Token    │───▶│   Role Resolution   │───▶│ Resource Filters │
│                 │    │                     │    │                  │
│ Bearer eyJ0...  │    │ Role: prod-viewer   │    │ Clusters: prod-* │
│                 │    │ Scope: clusters/ns  │    │ Namespaces: app-*│
│                 │    │ Resources: pods     │    │ Resources: Pod   │
└─────────────────┘    └─────────────────────┘    └──────────────────┘
```

### Enhanced User Context

```go
type UserContext struct {
    // Current fields
    Username     string    `json:"username"`
    UID          string    `json:"uid"`
    Groups       []string  `json:"groups"`
    AuthMethod   string    `json:"auth_method"`
    HeaderSource string    `json:"header_source"`
    ValidatedAt  time.Time `json:"validated_at"`

    // NEW: Enhanced RBAC fields
    Roles        []Role    `json:"roles"`          // User's assigned roles
    Permissions  []Permission `json:"permissions"` // Computed permissions
}

type Role struct {
    Name            string   `json:"name"`            // "prod-viewer", "dev-admin"
    AllowedClusters []string `json:"allowed_clusters"` // ["prod-east", "prod-*"]
    AllowedNamespaces []string `json:"allowed_namespaces"` // ["app-*", "monitoring"]
    AllowedResources []string `json:"allowed_resources"`  // ["Pod", "Service"]
    Permissions     []string `json:"permissions"`     // ["read", "list"]
}
```

### Enhanced Permission Resolution

```go
func ResolveUserPermissions(userCtx *UserContext) *ResourcePermissions {
    permissions := &ResourcePermissions{
        AllowedClusters:   []string{},
        AllowedNamespaces: []string{},
        AllowedResources:  []string{},
        AllowedOperations: []string{},
    }

    // Aggregate permissions from all user roles
    for _, role := range userCtx.Roles {
        permissions.AllowedClusters = append(permissions.AllowedClusters, role.AllowedClusters...)
        permissions.AllowedNamespaces = append(permissions.AllowedNamespaces, role.AllowedNamespaces...)
        permissions.AllowedResources = append(permissions.AllowedResources, role.AllowedResources...)
    }

    return permissions
}
```

## Implementation Strategy

### 1. Where to Apply Filters

The filtering would happen in the **query building phase** of `find_resources`:

```go
func (f *FindResourcesCore) buildQuery(args FindResourcesArgs, targetClusters []string, userCtx *auth.UserContext) (*QueryPlan, error) {
    sqlBuilder := utils.NewSQLBuilder(1)

    // Apply user-based filters FIRST
    if err := f.applyUserFilters(userCtx, sqlBuilder); err != nil {
        return nil, fmt.Errorf("user filter failed: %w", err)
    }

    // Then apply requested filters
    if args.Kind != nil {
        err := f.buildKindConditions(args.Kind, sqlBuilder)
        // ... existing logic
    }
}

func (f *FindResourcesCore) applyUserFilters(userCtx *auth.UserContext, builder *utils.SQLBuilder) error {
    permissions := auth.ResolveUserPermissions(userCtx)

    // Filter by allowed clusters
    if len(permissions.AllowedClusters) > 0 {
        builder.AddCondition("cluster IN (%s)", permissions.AllowedClusters)
    }

    // Filter by allowed namespaces (with pattern matching)
    if len(permissions.AllowedNamespaces) > 0 {
        namespaceConditions := []string{}
        for _, pattern := range permissions.AllowedNamespaces {
            if strings.Contains(pattern, "*") {
                // Wildcard pattern: "app-*" becomes "data->>'namespace' LIKE 'app-%'"
                namespaceConditions = append(namespaceConditions,
                    fmt.Sprintf("data->>'namespace' LIKE '%s'", strings.ReplaceAll(pattern, "*", "%")))
            } else {
                // Exact match
                namespaceConditions = append(namespaceConditions,
                    fmt.Sprintf("data->>'namespace' = '%s'", pattern))
            }
        }
        builder.AddCondition("(%s)", strings.Join(namespaceConditions, " OR "))
    }

    // Filter by allowed resource kinds
    if len(permissions.AllowedResources) > 0 {
        builder.AddCondition("data->>'kind' IN (%s)", permissions.AllowedResources)
    }

    return nil
}
```

### 2. Role Resolution Sources

Roles could be resolved from multiple sources:

```go
func (m *AuthMiddleware) resolveUserRoles(userCtx *UserContext, token string) ([]Role, error) {
    var roles []Role

    // Source 1: Kubernetes RBAC (ClusterRoles/Roles bound to user/groups)
    k8sRoles, err := m.validator.GetKubernetesRoles(userCtx, token)
    if err != nil {
        return nil, err
    }
    roles = append(roles, k8sRoles...)

    // Source 2: ConfigMap-based role definitions
    configRoles, err := m.loadRolesFromConfigMap(userCtx)
    if err != nil {
        return nil, err
    }
    roles = append(roles, configRoles...)

    // Source 3: External system (LDAP, OIDC claims, etc.)
    externalRoles, err := m.loadExternalRoles(userCtx)
    if err != nil {
        return nil, err
    }
    roles = append(roles, externalRoles...)

    return roles, nil
}
```

### 3. Example Role Definitions

```yaml
# ConfigMap: acm-search-roles
apiVersion: v1
kind: ConfigMap
metadata:
  name: acm-search-roles
  namespace: open-cluster-management
data:
  roles.yaml: |
    roles:
      - name: "production-viewer"
        description: "Read-only access to production clusters"
        allowedClusters: ["prod-east-*", "prod-west-*"]
        allowedNamespaces: ["app-*", "monitoring", "logging"]
        allowedResources: ["Pod", "Service", "Deployment", "ConfigMap"]
        permissions: ["read", "list"]

      - name: "development-admin"
        description: "Full access to development clusters"
        allowedClusters: ["dev-*", "test-*"]
        allowedNamespaces: ["*"]
        allowedResources: ["*"]
        permissions: ["*"]

      - name: "security-auditor"
        description: "Security-focused cross-cluster access"
        allowedClusters: ["*"]
        allowedNamespaces: ["kube-system", "security-*"]
        allowedResources: ["Pod", "Secret", "ServiceAccount"]
        permissions: ["read", "list"]

    userRoleBindings:
      - username: "alice@company.com"
        roles: ["production-viewer", "development-admin"]
      - group: "security-team"
        roles: ["security-auditor"]
```

## Benefits of Enhanced Architecture

### 1. **Seamless Integration**
- ✅ **Zero breaking changes** - existing auth flow remains intact
- ✅ **Additive enhancement** - new filtering layer adds capabilities
- ✅ **Backward compatible** - current admin/user model still works

### 2. **Performance Optimized**
- ✅ **Database-level filtering** - filters applied in SQL, not post-processing
- ✅ **Index-friendly** - cluster/namespace filters use indexed columns
- ✅ **Cached permissions** - role resolution cached per user session

### 3. **Security by Design**
- ✅ **Default deny** - users only see explicitly allowed resources
- ✅ **Multi-layer filtering** - user filters + request filters combined
- ✅ **Audit friendly** - all access decisions logged with user context

### 4. **Operational Flexibility**
- ✅ **Role-based segregation** - different teams see different views
- ✅ **Dynamic permissions** - roles can be updated without server restart
- ✅ **Multi-source roles** - Kubernetes RBAC + custom definitions

## Implementation Phases

### Phase 1: Enhanced User Context
1. Extend `UserContext` with roles/permissions
2. Add role resolution logic to auth middleware
3. Create role definition storage (ConfigMap)

### Phase 2: Query-Level Filtering
1. Modify `FindResourcesCore.buildQuery()` to accept UserContext
2. Add `applyUserFilters()` method
3. Update transport layers to pass UserContext

### Phase 3: Role Management
1. Create role definition API/CLI
2. Add role validation and testing
3. Implement role inheritance and composition

**Does this architecture work for your enhanced RBAC vision?** The current middleware chain pattern is perfectly positioned to support granular filtering at the query level! 🎯