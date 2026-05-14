# Search MCP Server Authentication & Authorization

## Overview

The ACM Search MCP Server implements enterprise-grade authentication and authorization for multi-cluster Kubernetes resources. This system provides **granular RBAC** where users access only their authorized clusters, namespaces, and resource types.

## Architecture Principles

### Security-First Design
- **Fail secure**: When in doubt, deny access
- **No unsafe fallbacks**: Permission failures result in access denial
- **Real-time validation**: Live Kubernetes RBAC checks using user tokens
- **Zero trust**: Every query validated against current permissions

### Dual API Approach
The system combines two permission sources for comprehensive coverage:

1. **UserPermission CRs**: Cross-cluster managed resource permissions
2. **Hub Kubernetes API**: Native RBAC for hub cluster resources

Both sources are merged with OR logic - users get access if either API grants permissions.

## Authentication Flow

### Request Processing
```
Client Request → Token Validation → Permission Resolution → Query Authorization → Filtered Results
```

### Token Validation
- Supports `Authorization: Bearer <token>` and `kubernetes-authorization: Bearer <token>` headers
- Validates tokens via Kubernetes `TokenReview` API
- Caches validation results (configurable TTL)

**Implementation**: `internal/server/auth/middleware.go`

### Permission Resolution
- Queries UserPermission Custom Resources for managed cluster access
- Calls Hub Kubernetes API for hub cluster permissions  
- Converts permissions to database query filters
- Caches resolved permissions per user

**Implementation**: `internal/server/auth/rbac_resolver.go`

## Authorization Model

### Permission Sources

**UserPermission CRs (Managed Clusters)**
- Source: Direct CR queries via Kubernetes dynamic client
- Scope: All managed clusters in the ACM fleet
- Coverage: Pods, Deployments, VirtualMachines, etc. on managed clusters

**Hub Kubernetes API (Hub Cluster)**  
- Source: `SelfSubjectAccessReview` and `SelfSubjectRulesReview`
- Scope: Hub cluster only (dynamically detected)
- Coverage: ManagedCluster, MultiClusterHub, ACM components

### Query Filtering
User permissions are converted to SQL filters that restrict database queries:

```sql
-- Example: User sees only authorized resources
WHERE (cluster = 'prod-east' AND namespace = 'app-1' AND data->>'kind' = 'Pod')
   OR (cluster = 'local-cluster' AND data->>'kind' = 'ManagedCluster')
```

**Implementation**: `internal/findresources/core.go:applyAuthorizationFilters()`

## Configuration

### Authentication Settings
```go
type AuthConfig struct {
    EnableAuth      bool          // Enable/disable authentication (default: auto-detected in K8s)
    AuthTimeout     time.Duration // Timeout for auth API calls (default: 5 seconds)
    CacheTokens     bool          // Cache validated tokens (default: true)
    CacheTTL        time.Duration // Cache TTL for tokens (default: 5 minutes)
    DiscoveryTTL    time.Duration // Discovery cache TTL (default: 5 minutes)
    DiscoverySource string        // "database" or "kubernetes" (default: "database")
    
    // Kubernetes connection (auto-detected in production)
    KubernetesHost  string        // KUBERNETES_SERVICE_HOST
    KubernetesPort  string        // KUBERNETES_SERVICE_PORT
    
    // Manual overrides (testing/development)  
    KubernetesURL   string        // Full cluster URL
    TokenValue      string        // Direct service account token
    SkipTLS         bool          // Skip TLS verification (default: false, testing only)
}
```

### Environment Variables
- `KUBERNETES_SERVICE_HOST` / `KUBERNETES_SERVICE_PORT`: Auto-detected in cluster
- `LOG_LEVEL=debug`: Enables comprehensive auth logging

## Security Scenarios

| Scenario | Token Status | Permission Resolution | Access Decision |
|----------|--------------|---------------------|-----------------|
| Valid user + APIs succeed + Has ACM permissions | Valid | SUCCESS (with permissions) | ✅ **ALLOW ACCESS** |
| Valid user + APIs succeed + No ACM permissions | Valid | SUCCESS (empty permissions) | ❌ **DENY ACCESS** |
| Valid user + K8s APIs fail | Valid | FAIL (cannot verify) | ❌ **DENY ACCESS** |
| Invalid token | Invalid | Not attempted | ❌ **DENY ACCESS** |
| Admin user + K8s APIs fail | Valid | FAIL (cannot verify) | ❌ **DENY ACCESS** |

**Key Point**: No admin bypass - even cluster admins are denied access when permission APIs fail.

## Resource Discovery

### Dynamic Resource Support
The system automatically discovers Kubernetes resource types without requiring code changes:

- **Cache-first approach**: 5-minute TTL for resource mappings
- **Database discovery** (Production): Fleet-wide resource types from `search.resources` table
- **Kubernetes discovery** (Testing only): Live API discovery, limited to hub cluster resources
- **Fail-secure design**: If database is unavailable, discovery fails rather than using unsafe defaults

**Production**: Always uses database discovery for complete fleet coverage.  
**Testing**: Can use Kubernetes API discovery when database is unavailable.

**Implementation**: `internal/server/auth/resource_discovery.go`

## Integration Guide

### Using Authentication Middleware
```go
// Create middleware
authMiddleware, err := auth.NewAuthMiddleware(config, db)
if err != nil {
    return fmt.Errorf("failed to create auth middleware: %w", err)
}

// Apply to HTTP handlers  
http.Handle("/mcp", authMiddleware.Handler(mcpHandler))
```

### Extracting User Context
```go
func handler(w http.ResponseWriter, r *http.Request) {
    userCtx := auth.GetUserContext(r)
    if userCtx == nil {
        // User not authenticated
        return
    }
    
    // Access user's permissions
    filters := userCtx.QueryFilters
    // Apply filters to database queries...
}
```

### Authorization in Database Queries
```go
// Apply user authorization filters to SQL queries
err := findResourcesCore.applyAuthorizationFilters(userCtx.QueryFilters, sqlBuilder)
```

## Troubleshooting

### Common Issues

**Permission Resolution Failures**
```
[RBAC-SECURITY] Permission resolution failed for user, denying access
```
- **Cause**: Kubernetes APIs (UserPermission or Hub RBAC) are unavailable
- **Solution**: Check K8s API server connectivity, verify service account permissions
- **Behavior**: Access denied for security (fail-secure design)

**No ACM Permissions Found**  
```
[RBAC] No ACM permissions found for user - denying access
```
- **Cause**: User has no UserPermission CRs and no hub cluster access
- **Solution**: Grant user appropriate RBAC permissions or UserPermission CRs

**Discovery Cache Misses**
```
[DISCOVERY-DEBUG] Resource not found in cache, performing discovery
```
- **Normal**: Cache miss triggers live discovery every 5 minutes
- **Action**: Monitor for excessive discovery calls, consider cache TTL tuning

### Debug Logging
Enable with `LOG_LEVEL=debug`:

- Permission resolution steps and API calls
- Resource discovery cache hits/misses  
- Security decisions and access denials
- User permission mappings and cluster access
- Database query filters applied per user

### Configuration Validation
```bash
# Test authentication is working
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/mcp

# Check service account token
kubectl auth can-i list pods --as=system:serviceaccount:default:search-mcp-server
```

## Deployment Considerations

### Production Security
- **Always enable authentication** in production environments
- **Use strong service account tokens** with minimal required permissions
- **Monitor permission resolution failures** - may indicate API server issues
- **Regular permission audits** - verify users have appropriate access

### Performance
- **Permission caching**: Reduces API calls, configurable TTL (default: varies by config)
- **Discovery caching**: 5-minute TTL for resource mappings
- **Database filtering**: Early query filtering reduces unauthorized data retrieval
- **Concurrent API calls**: Parallel permission checks with rate limiting

### High Availability
- **Database dependency**: System requires ACM database connectivity for full functionality
- **API dependencies**: Requires access to Kubernetes APIs for permission resolution
- **Graceful degradation**: Uses cached permissions when possible during API failures

## Security Benefits

1. **Zero Trust Architecture**: Every query validated against live Kubernetes RBAC
2. **Fail Secure Design**: Permission failures never result in privilege escalation
3. **Resource-Specific Permissions**: Users see only authorized resource types per namespace
4. **Real-time Validation**: No stale cached permissions can grant unintended access
5. **Comprehensive Audit Trail**: Full logging of authentication and authorization decisions

---

## File References

- **Middleware**: `internal/server/auth/middleware.go`
- **RBAC Resolution**: `internal/server/auth/rbac_resolver.go`  
- **Resource Discovery**: `internal/server/auth/resource_discovery.go`
- **Hub API Client**: `internal/server/auth/hub_rbac_client.go`
- **Type Definitions**: `internal/server/auth/types.go`
- **Query Integration**: `internal/findresources/core.go`

For implementation details, refer to the source code files above rather than embedded examples that may become outdated.