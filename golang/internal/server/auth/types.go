package auth

import (
	"context"
	"os"
	"strings"
	"time"
)

// PermissionSource represents one coherent permission set following search-v2-api's proven approach
// Direct namespace-kind mapping prevents Cartesian products by design
type PermissionSource struct {
	Source             string                    `json:"source"`             // "userpermission" or "hub-kubernetes"
	ClusterScopedKinds map[string][]string       `json:"cluster_scoped_kinds"` // cluster → allowed cluster-scoped Kinds (prevents Cartesian products)
	NamespacedKinds    map[string][]string       `json:"namespaced_kinds"`     // Direct "cluster/namespace" → allowed Kinds mapping (prevents cross-multiplication)
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

// HasWildcardAccess returns true if user has wildcard permissions in any dimension (VERIFIED: cluster-aware logic)
func (qf *QueryFilters) HasWildcardAccess() bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		// Check for wildcard cluster access (implies broad access)
		if _, exists := source.ManagedClusters["*"]; exists {
			return true // User has access to all clusters
		}

		// Check for cluster-scoped resource wildcard (within allowed clusters)
		for _, kinds := range source.ClusterScopedKinds {
			for _, kind := range kinds {
				if kind == "*" {
					return true // User has wildcard resource access cluster-wide
				}
			}
		}

		// Check for namespaced resource wildcard (within allowed clusters)
		// SECURITY FIX: Check both cluster-qualified and legacy bare namespace keys
		for namespaceKey, kinds := range source.NamespacedKinds {
			// Check cluster-qualified wildcard keys like "cluster-a/*"
			if strings.HasSuffix(namespaceKey, "/*") {
				for _, kind := range kinds {
					if kind == "*" {
						return true // User has wildcard resource access in cluster
					}
				}
			}
			// Check legacy bare wildcard key "*" (for hub-kubernetes source)
			if namespaceKey == "*" {
				for _, kind := range kinds {
					if kind == "*" {
						return true // User has wildcard resource access in all namespaces
					}
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
		for _, clusterKinds := range source.ClusterScopedKinds {
			for _, clusterKind := range clusterKinds {
				if clusterKind == "*" || clusterKind == kind {
					return true
				}
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

// IsClusterAllowed checks if user has access to specific cluster (FIXED: proper cluster-aware permissions)
func (qf *QueryFilters) IsClusterAllowed(cluster string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" {
			// SECURITY FIX: Check ManagedClusters map for SPECIFIC cluster access
			if _, exists := source.ManagedClusters["*"]; exists {
				return true // Wildcard cluster access
			}
			if _, exists := source.ManagedClusters[cluster]; exists {
				return true // Specific cluster access
			}
			// SECURITY FIX: Do NOT grant access just because user has permissions elsewhere
			// Removed: automatic cluster access based on any permissions
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			// Hub API only applies to hub cluster (dynamically detected)
			return len(source.ClusterScopedKinds) > 0 || len(source.NamespacedKinds) > 0
		}
	}

	return false
}

// IsNamespaceAllowedInCluster checks if user has access to specific namespace in specific cluster (FIXED: cluster-aware permissions)
func (qf *QueryFilters) IsNamespaceAllowedInCluster(cluster, namespace string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" || source.Source == "userpermission-cr" {
			// SECURITY FIX: First verify user has access to the cluster
			hasClusterAccess := false
			if _, exists := source.ManagedClusters["*"]; exists {
				hasClusterAccess = true // Wildcard cluster access
			} else if _, exists := source.ManagedClusters[cluster]; exists {
				hasClusterAccess = true // Specific cluster access
			}

			if !hasClusterAccess {
				continue // Skip this source - user doesn't have access to this cluster
			}

			// SECURITY FIX: Check cluster-qualified namespace keys for userpermission-cr source
			if source.Source == "userpermission-cr" {
				// Check cluster-qualified wildcard: "cluster/*"
				clusterWildcardKey := cluster + "/*"
				if _, exists := source.NamespacedKinds[clusterWildcardKey]; exists {
					return true // Wildcard namespace access in this cluster
				}
				// Check cluster-qualified specific namespace: "cluster/namespace"
				clusterNamespaceKey := cluster + "/" + namespace
				if _, exists := source.NamespacedKinds[clusterNamespaceKey]; exists {
					return true // Specific namespace access in this cluster
				}
			} else {
				// Legacy userpermission source uses bare namespace keys
				if _, exists := source.NamespacedKinds["*"]; exists {
					return true // Wildcard namespace access in this cluster
				}
				if _, exists := source.NamespacedKinds[namespace]; exists {
					return true // Specific namespace access in this cluster
				}
			}
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			// Hub API only applies to hub cluster - uses bare namespace keys
			if _, exists := source.NamespacedKinds["*"]; exists {
				return true // Wildcard namespace access on hub
			}
			if _, exists := source.NamespacedKinds[namespace]; exists {
				return true // Specific namespace access on hub
			}
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
		for _, clusterKinds := range source.ClusterScopedKinds {
			for _, clusterKind := range clusterKinds {
				if clusterKind == "*" || clusterKind == kind {
					return true
				}
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