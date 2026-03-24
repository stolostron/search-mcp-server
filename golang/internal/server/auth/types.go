package auth

import (
	"context"
	"time"
)

// QueryFilters represents authorization filters for database queries
type QueryFilters struct {
	AllowedClusters   []string `json:"allowed_clusters"`   // Clusters user can access
	AllowedNamespaces []string `json:"allowed_namespaces"` // Namespaces user can access (may include *)
	AllowedResources  []string `json:"allowed_resources"`  // Resource kinds user can access
	ResourceNamespaces map[string][]string `json:"resource_namespaces"` // Per-resource namespace permissions: map[Kind][]namespaces
}

// HasWildcardAccess returns true if the filters explicitly grant unrestricted access to everything
func (qf *QueryFilters) HasWildcardAccess() bool {
	// SECURITY: No QueryFilters means no authorization resolution occurred
	// This should not grant wildcard access - fail secure
	if qf == nil {
		return false // No explicit permissions = no wildcard access
	}

	// Check if ALL dimensions explicitly allow wildcard access (empty slice or "*")
	clustersUnrestricted := qf.isUnrestricted(qf.AllowedClusters)
	namespacesUnrestricted := qf.isUnrestricted(qf.AllowedNamespaces)
	resourcesUnrestricted := qf.isUnrestricted(qf.AllowedResources)

	// Must have unrestricted access in ALL dimensions for true wildcard access
	return clustersUnrestricted && namespacesUnrestricted && resourcesUnrestricted
}

// HasNamespaceWildcardForResource returns true if the specific resource type has wildcard namespace access
func (qf *QueryFilters) HasNamespaceWildcardForResource(resourceKind string) bool {
	if qf == nil || qf.ResourceNamespaces == nil {
		return false
	}

	// Check for specific resource type
	namespaces, exists := qf.ResourceNamespaces[resourceKind]
	if exists {
		// Check if this specific resource has wildcard namespace access
		for _, ns := range namespaces {
			if ns == "*" {
				return true
			}
		}
	}

	// Check for global wildcard resource ("*") that grants access to all resource types
	globalNamespaces, globalExists := qf.ResourceNamespaces["*"]
	if globalExists {
		for _, ns := range globalNamespaces {
			if ns == "*" {
				return true // Global wildcard grants wildcard namespace access to all resources
			}
		}
	}

	return false
}

// GetAllowedNamespacesForResource returns the allowed namespaces for a specific resource type
func (qf *QueryFilters) GetAllowedNamespacesForResource(resourceKind string) []string {
	if qf == nil {
		return []string{} // SECURITY: Fail secure for nil QueryFilters
	}
	if qf.ResourceNamespaces == nil {
		return []string{} // SECURITY: Fail secure if resource-specific permissions not populated
	}

	// Check for specific resource type first
	namespaces, exists := qf.ResourceNamespaces[resourceKind]
	if exists {
		return namespaces
	}

	// Check for global wildcard resource ("*") that grants access to all resource types
	globalNamespaces, globalExists := qf.ResourceNamespaces["*"]
	if globalExists {
		return globalNamespaces // Return the global namespaces for wildcard resources
	}

	return []string{} // No access to this resource type
}

// isNamespaceAllowedForResource checks if a specific namespace is allowed for a specific resource type
// This is used for resource-specific namespace filtering to prevent privilege escalation
func (qf *QueryFilters) isNamespaceAllowedForResource(resourceKind, namespace string) bool {
	if qf == nil || qf.ResourceNamespaces == nil {
		return false
	}

	// Check for specific resource type first
	allowedNamespaces, exists := qf.ResourceNamespaces[resourceKind]
	if exists {
		if qf.checkNamespaceAccess(allowedNamespaces, namespace) {
			return true
		}
	}

	// Check for global wildcard resource ("*") that grants access to all resource types
	globalNamespaces, globalExists := qf.ResourceNamespaces["*"]
	if globalExists {
		return qf.checkNamespaceAccess(globalNamespaces, namespace)
	}

	return false
}

// checkNamespaceAccess is a helper method to check namespace access patterns
func (qf *QueryFilters) checkNamespaceAccess(allowedNamespaces []string, namespace string) bool {
	for _, allowedNS := range allowedNamespaces {
		if allowedNS == "*" {
			return true
		}
		if allowedNS == namespace {
			return true
		}
		// Pattern matching for namespaces like "app-*"
		if len(allowedNS) > 1 && allowedNS[len(allowedNS)-1] == '*' {
			prefix := allowedNS[:len(allowedNS)-1]
			if len(namespace) >= len(prefix) && namespace[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// isUnrestricted returns true if the slice represents unrestricted access
func (qf *QueryFilters) isUnrestricted(slice []string) bool {
	// SECURITY: Empty slice = NO ACCESS (fail secure), not unrestricted
	if len(slice) == 0 {
		return false
	}
	// Single "*" = explicit wildcard for this dimension
	return len(slice) == 1 && slice[0] == "*"
}

// IsClusterAllowed checks if a specific cluster is allowed
func (qf *QueryFilters) IsClusterAllowed(cluster string) bool {
	// SECURITY: Fail secure - no QueryFilters means no explicit permissions
	if qf == nil {
		return false
	}

	// SECURITY: Empty AllowedClusters list means NO ACCESS (fail secure)
	if len(qf.AllowedClusters) == 0 {
		return false
	}

	for _, allowed := range qf.AllowedClusters {
		if allowed == "*" || allowed == cluster {
			return true
		}
		// TODO: Add wildcard pattern matching if needed (e.g., "prod-*")
	}
	return false
}

// IsNamespaceAllowed checks if a specific namespace is allowed
func (qf *QueryFilters) IsNamespaceAllowed(namespace string) bool {
	// SECURITY: Fail secure - no QueryFilters means no explicit permissions
	if qf == nil {
		return false
	}

	// SECURITY: Empty AllowedNamespaces list means NO ACCESS (fail secure)
	if len(qf.AllowedNamespaces) == 0 {
		return false
	}

	for _, allowed := range qf.AllowedNamespaces {
		if allowed == "*" || allowed == namespace {
			return true
		}
		// TODO: Add wildcard pattern matching if needed (e.g., "app-*")
	}
	return false
}

// IsResourceKindAllowed checks if a specific resource kind is allowed
func (qf *QueryFilters) IsResourceKindAllowed(kind string) bool {
	// SECURITY: Fail secure - no QueryFilters means no explicit permissions
	if qf == nil {
		return false
	}

	// SECURITY: Empty AllowedResources list means NO ACCESS (fail secure)
	if len(qf.AllowedResources) == 0 {
		return false
	}

	for _, allowed := range qf.AllowedResources {
		if allowed == "*" || allowed == kind {
			return true
		}
	}
	return false
}

// UserContext represents an authenticated user with their permissions
type UserContext struct {
	Username     string        `json:"username"`
	UID          string        `json:"uid"`
	Groups       []string      `json:"groups"`
	AuthMethod   string        `json:"auth_method"`   // "bearer", "k8s-auth", etc.
	HeaderSource string        `json:"header_source"` // "Authorization" or "kubernetes-authorization"
	ValidatedAt  time.Time     `json:"validated_at"`
	QueryFilters *QueryFilters `json:"query_filters,omitempty"` // NEW: Granular permissions for database access
}

// HasACMAdmin checks if user has ACM administrator permissions
func (u *UserContext) HasACMAdmin() bool {
	// Check for cluster admin groups
	clusterAdminGroups := []string{"system:masters", "cluster-admins"}
	for _, group := range u.Groups {
		for _, adminGroup := range clusterAdminGroups {
			if group == adminGroup {
				return true
			}
		}
	}

	// Check for standard cluster admin users (OpenShift/Kubernetes defaults)
	adminUsers := []string{"kube:admin", "system:admin", "admin"}
	for _, adminUser := range adminUsers {
		if u.Username == adminUser {
			return true
		}
	}

	return false
}

// TokenValidationResult represents the result of token validation
type TokenValidationResult struct {
	Valid bool         `json:"valid"`
	User  *UserContext `json:"user,omitempty"`
	Error string       `json:"error,omitempty"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	// Environment-aware auth enablement
	EnableAuth bool

	// Kubernetes connection details (auto-detected in prod, manual for testing)
	KubernetesHost string // Auto-set by K8s: KUBERNETES_SERVICE_HOST
	KubernetesPort string // Auto-set by K8s: KUBERNETES_SERVICE_PORT

	// Local testing overrides
	KubernetesURL   string // Manual cluster URL: https://api.cluster.com:6443
	TokenValue      string // Direct service account token
	TokenPath       string // Custom token file path
	KubeconfigPath  string // Use kubeconfig file

	// Security options
	SkipTLS      bool          // Skip TLS verification (testing only)
	AuthTimeout  time.Duration // Timeout for auth API calls
	CacheTokens  bool          // Cache validated tokens
	CacheTTL     time.Duration // Cache TTL for tokens
}

// K8sConfig represents Kubernetes client configuration
type K8sConfig struct {
	Host           string
	Port           string
	URL            string // Full URL overrides Host+Port
	Token          string // Direct token value
	TokenPath      string // Path to token file
	KubeconfigPath string // Path to kubeconfig
	TLSVerify      bool   // Whether to verify TLS certificates
	Timeout        time.Duration
}

// Permission represents a specific Kubernetes permission check
type Permission struct {
	Verb     string `json:"verb"`     // create, get, list, etc.
	Resource string `json:"resource"` // managedclusters, pods, etc.
	Group    string `json:"group"`    // API group, empty for core
}

// Standard permissions for ACM access
var (
	ClusterAdminPermission = Permission{
		Verb:     "*",
		Resource: "*",
		Group:    "*",
	}

	ACMAdminPermission = Permission{
		Verb:     "create",
		Resource: "managedclusters",
		Group:    "cluster.open-cluster-management.io",
	}
)

// Context key type for storing user context in request context
type contextKey string

const UserContextKey contextKey = "user_context"

// UserFromContext extracts user context from request context
func UserFromContext(ctx context.Context) *UserContext {
	if user, ok := ctx.Value(UserContextKey).(*UserContext); ok {
		return user
	}
	return nil
}

// WithUserContext adds user context to request context
func WithUserContext(ctx context.Context, user *UserContext) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}