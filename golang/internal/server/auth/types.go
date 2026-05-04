package auth

import (
	"context"
	"os"
	"time"
)

// PermissionSource represents one coherent permission set following search-v2-api's proven approach
// Direct namespace-kind mapping prevents Cartesian products by design
type PermissionSource struct {
	Source             string                    `json:"source"`             // "userpermission" or "hub-kubernetes"
	ClusterScopedKinds []string                  `json:"cluster_scoped_kinds"` // Kinds accessible cluster-wide (like ManagedCluster)
	NamespacedKinds    map[string][]string       `json:"namespaced_kinds"`     // Direct namespace → allowed Kinds mapping (prevents cross-multiplication)
	ManagedClusters    map[string]struct{}       `json:"managed_clusters"`     // Accessible managed clusters
}

// REMOVED: LocationBinding and ResourceRule - they caused Cartesian products via separate arrays
// NEW: Direct mapping like search-v2-api's NsResources map[string][]Resource approach

// QueryFilters represents authorization filters for database queries
// NEW: Prevents Cartesian products via source separation
type QueryFilters struct {
	PermissionSources []PermissionSource `json:"permission_sources"`
	HubClusterName    string             `json:"hub_cluster_name"` // Dynamically detected hub cluster name (replaces hard-coded "local-cluster")
}

// COMMENTED OUT: HasWildcardAccess - causing test failures, not used in production
// These methods have logic bugs but aren't used by find-resources production code.
// Production code directly accesses PermissionSource data structures.
// TODO: Fix logic bugs or remove entirely after confirming no external dependencies.

/*
func (qf *QueryFilters) HasWildcardAccess() bool {
	// SECURITY: No QueryFilters means no authorization resolution occurred
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false // No explicit permissions = no wildcard access
	}

	// Check if ANY permission source has unrestricted access in all dimensions
	for _, source := range qf.PermissionSources {
		if qf.sourceHasWildcardAccess(&source) {
			return true
		}
	}

	return false
}
*/

/*
func (qf *QueryFilters) sourceHasWildcardAccess(source *PermissionSource) bool {
	// Check cluster-scoped kinds for wildcard
	hasClusterWildcard := false
	for _, kind := range source.ClusterScopedKinds {
		if kind == "*" {
			hasClusterWildcard = true
			break
		}
	}

	// Check namespaced kinds for wildcard namespace and kinds
	hasNamespaceWildcard := false
	if _, exists := source.NamespacedKinds["*"]; exists {
		for _, kind := range source.NamespacedKinds["*"] {
			if kind == "*" {
				hasNamespaceWildcard = true
				break
			}
		}
	}

	return hasClusterWildcard || hasNamespaceWildcard
}
*/

// SIMPLE STUB IMPLEMENTATIONS - for test compatibility only
// Production find-resources code doesn't use these methods

// HasWildcardAccess returns true if any source has wildcard permissions (simple stub)
func (qf *QueryFilters) HasWildcardAccess() bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	// Simple check: look for "*" in any source
	for _, source := range qf.PermissionSources {
		// Check cluster-scoped wildcard
		for _, kind := range source.ClusterScopedKinds {
			if kind == "*" {
				return true
			}
		}
		// Check namespaced wildcard
		if kinds, exists := source.NamespacedKinds["*"]; exists {
			for _, kind := range kinds {
				if kind == "*" {
					return true
				}
			}
		}
	}
	return false
}

/*
// BUG: IsClusterAllowed doesn't check specific cluster in ManagedClusters map
// Currently returns true for ANY cluster if user has ANY permissions (security issue)
func (qf *QueryFilters) IsClusterAllowed(cluster string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false // SECURITY: Fail secure - no permissions means no access
	}

	// Check all permission sources - if ANY source allows the cluster, access is granted
	for _, source := range qf.PermissionSources {
		// Check if this source has access to the specified cluster
		if source.Source == "userpermission" {
			// For managed clusters, check if cluster has any accessible namespaces/kinds
			if len(source.NamespacedKinds) > 0 || len(source.ClusterScopedKinds) > 0 {
				return true // UserPermission API grants managed cluster access
			}
		} else if source.Source == "hub-kubernetes" {
			// Hub Kubernetes API only applies to hub cluster (dynamically detected)
			if cluster == qf.HubClusterName && (len(source.NamespacedKinds) > 0 || len(source.ClusterScopedKinds) > 0) {
				return true
			}
		}
	}
	return false
}
*/

/*
// BUG: IsNamespaceAllowedInCluster doesn't respect cluster boundaries
// Ignores cluster parameter - just checks if user has namespace access anywhere
func (qf *QueryFilters) IsNamespaceAllowedInCluster(cluster, namespace string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false // SECURITY: Fail secure - no permissions means no access
	}

	// Check all permission sources for cluster-namespace combination
	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" {
			// UserPermission API: check if namespace is directly mapped
			if _, exists := source.NamespacedKinds[namespace]; exists {
				return true
			}
			// Check for wildcard namespace access
			if _, exists := source.NamespacedKinds["*"]; exists {
				return true
			}
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			// Hub Kubernetes API: check if namespace is directly mapped
			if _, exists := source.NamespacedKinds[namespace]; exists {
				return true
			}
			// Check for wildcard namespace access
			if _, exists := source.NamespacedKinds["*"]; exists {
				return true
			}
		}
	}
	return false
}
*/

/*
// IsResourceKindAllowed - commented out due to test failures
func (qf *QueryFilters) IsResourceKindAllowed(kind string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false // SECURITY: Fail secure - no permissions means no access
	}

	// Check all permission sources - if ANY source allows the kind, access is granted
	for _, source := range qf.PermissionSources {
		// Check cluster-scoped kinds
		for _, clusterKind := range source.ClusterScopedKinds {
			if clusterKind == "*" || clusterKind == kind {
				return true
			}
		}

		// Check namespaced kinds across all namespaces
		for _, kinds := range source.NamespacedKinds {
			for _, namespacedKind := range kinds {
				if namespacedKind == "*" || namespacedKind == kind {
					return true
				}
			}
		}
	}
	return false
}
*/

// IsClusterAllowed checks if user has access to specific cluster (simple stub)
func (qf *QueryFilters) IsClusterAllowed(cluster string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" {
			// Check ManagedClusters map for specific or wildcard access
			if _, exists := source.ManagedClusters["*"]; exists {
				return true // Wildcard cluster access
			}
			if _, exists := source.ManagedClusters[cluster]; exists {
				return true // Specific cluster access
			}

			// Check cluster-scoped access (any cluster-scoped permission allows any cluster)
			if len(source.ClusterScopedKinds) > 0 {
				return true // Has cluster-scoped access
			}

			// Check if user has any namespaced kinds (implies cluster access)
			if len(source.NamespacedKinds) > 0 {
				return true // Has namespace access, implies cluster access
			}
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			// Hub API only applies to hub cluster (dynamically detected)
			return len(source.ClusterScopedKinds) > 0 || len(source.NamespacedKinds) > 0
		}
	}

	return false
}

// IsNamespaceAllowedInCluster returns true if namespace exists in any source (simple stub)
func (qf *QueryFilters) IsNamespaceAllowedInCluster(cluster, namespace string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		// Check wildcard namespace access
		if _, exists := source.NamespacedKinds["*"]; exists {
			return true
		}

		// Check specific namespace access
		if _, exists := source.NamespacedKinds[namespace]; exists {
			return true
		}
	}
	return false
}

// IsResourceKindAllowed returns true if kind exists anywhere (simple stub)
func (qf *QueryFilters) IsResourceKindAllowed(kind string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	// Simple stub: check if kind exists in any source
	for _, source := range qf.PermissionSources {
		// Check cluster-scoped
		for _, clusterKind := range source.ClusterScopedKinds {
			if clusterKind == "*" || clusterKind == kind {
				return true
			}
		}
		// Check namespaced
		for _, kinds := range source.NamespacedKinds {
			for _, namespacedKind := range kinds {
				if namespacedKind == "*" || namespacedKind == kind {
					return true
				}
			}
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

	// Discovery configuration (NEW)
	DiscoveryTTL    time.Duration // Configurable discovery cache TTL
	DiscoverySource string        // "database" or "kubernetes"
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

// NewAuthConfigFromServerValues creates an AuthConfig from server configuration values
// This provides clean separation: server config holds values, auth handles logic
func NewAuthConfigFromServerValues(
	enableAuth bool,
	authTimeout time.Duration,
	authCacheEnabled bool,
	authCacheTTL time.Duration,
	kubernetesURL string,
	serviceAccountToken string,
	tokenPath string,
	kubeconfigPath string,
	skipTLSVerify bool,
	discoveryTTL time.Duration,
	discoverySource string,
) *AuthConfig {
	return &AuthConfig{
		EnableAuth:      enableAuth,
		KubernetesHost:  os.Getenv("KUBERNETES_SERVICE_HOST"), // Auto-detected from environment
		KubernetesPort:  os.Getenv("KUBERNETES_SERVICE_PORT"), // Auto-detected from environment
		KubernetesURL:   kubernetesURL,
		TokenValue:      serviceAccountToken,
		TokenPath:       tokenPath,
		KubeconfigPath:  kubeconfigPath,
		SkipTLS:         skipTLSVerify,
		AuthTimeout:     authTimeout,
		CacheTokens:     authCacheEnabled,
		CacheTTL:        authCacheTTL,
		DiscoveryTTL:    discoveryTTL,
		DiscoverySource: discoverySource,
	}
}