package auth

import (
	"context"
	"os"
	"strings"
	"time"
)

// ResourcePermission pairs a Kubernetes Kind with its API group.
// Prevents cross-API-group resource leakage (e.g., certificates.cert-manager.io vs certificates.networking.k8s.io).
type ResourcePermission struct {
	Kind     string `json:"kind"`
	APIGroup string `json:"apigroup"`
}

// PermissionSource represents one coherent permission set following search-v2-api's proven approach.
// Direct namespace-kind mapping prevents Cartesian products by design.
type PermissionSource struct {
	Source             string                          `json:"source"`               // "userpermission-cr" or "hub-kubernetes"
	ClusterScopedKinds map[string][]ResourcePermission `json:"cluster_scoped_kinds"` // cluster → allowed (Kind, APIGroup) pairs
	NamespacedKinds    map[string][]ResourcePermission `json:"namespaced_kinds"`     // "cluster/namespace" → allowed (Kind, APIGroup) pairs
	ManagedClusters    map[string]struct{}             `json:"managed_clusters"`     // Accessible managed clusters
}

// QueryFilters represents authorization filters for database queries.
type QueryFilters struct {
	PermissionSources []PermissionSource `json:"permission_sources"`
	HubClusterName    string             `json:"hub_cluster_name"` // Dynamically detected hub cluster name
}

// HasWildcardAccess returns true if user has wildcard permissions in any dimension.
func (qf *QueryFilters) HasWildcardAccess() bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if _, exists := source.ManagedClusters["*"]; exists {
			return true
		}

		for _, perms := range source.ClusterScopedKinds {
			for _, perm := range perms {
				if perm.Kind == "*" {
					return true
				}
			}
		}

		for namespaceKey, perms := range source.NamespacedKinds {
			if strings.HasSuffix(namespaceKey, "/*") {
				for _, perm := range perms {
					if perm.Kind == "*" {
						return true
					}
				}
			}
			if namespaceKey == "*" {
				for _, perm := range perms {
					if perm.Kind == "*" {
						return true
					}
				}
			}
		}
	}
	return false
}

// IsClusterAllowed checks if user has access to a specific cluster.
func (qf *QueryFilters) IsClusterAllowed(cluster string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" {
			if _, exists := source.ManagedClusters["*"]; exists {
				return true
			}
			if _, exists := source.ManagedClusters[cluster]; exists {
				return true
			}
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			return len(source.ClusterScopedKinds) > 0 || len(source.NamespacedKinds) > 0
		}
	}

	return false
}

// IsNamespaceAllowedInCluster checks if user has access to a specific namespace in a specific cluster.
func (qf *QueryFilters) IsNamespaceAllowedInCluster(cluster, namespace string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		if source.Source == "userpermission" || source.Source == "userpermission-cr" {
			hasClusterAccess := false
			if _, exists := source.ManagedClusters["*"]; exists {
				hasClusterAccess = true
			} else if _, exists := source.ManagedClusters[cluster]; exists {
				hasClusterAccess = true
			}

			if !hasClusterAccess {
				continue
			}

			if source.Source == "userpermission-cr" {
				clusterWildcardKey := cluster + "/*"
				if _, exists := source.NamespacedKinds[clusterWildcardKey]; exists {
					return true
				}
				clusterNamespaceKey := cluster + "/" + namespace
				if _, exists := source.NamespacedKinds[clusterNamespaceKey]; exists {
					return true
				}
			} else {
				if _, exists := source.NamespacedKinds["*"]; exists {
					return true
				}
				if _, exists := source.NamespacedKinds[namespace]; exists {
					return true
				}
			}
		} else if source.Source == "hub-kubernetes" && cluster == qf.HubClusterName {
			if _, exists := source.NamespacedKinds["*"]; exists {
				return true
			}
			if _, exists := source.NamespacedKinds[namespace]; exists {
				return true
			}
		}
	}
	return false
}

// IsResourceKindAllowed returns true if kind exists in any permission source.
func (qf *QueryFilters) IsResourceKindAllowed(kind string) bool {
	if qf == nil || len(qf.PermissionSources) == 0 {
		return false
	}

	for _, source := range qf.PermissionSources {
		for _, perms := range source.ClusterScopedKinds {
			for _, perm := range perms {
				if perm.Kind == "*" || perm.Kind == kind {
					return true
				}
			}
		}
		for _, perms := range source.NamespacedKinds {
			for _, perm := range perms {
				if perm.Kind == "*" || perm.Kind == kind {
					return true
				}
			}
		}
	}
	return false
}

// UserContext represents an authenticated user with their permissions.
type UserContext struct {
	Username     string        `json:"username"`
	UID          string        `json:"uid"`
	Groups       []string      `json:"groups"`
	AuthMethod   string        `json:"auth_method"`
	HeaderSource string        `json:"header_source"`
	ValidatedAt  time.Time     `json:"validated_at"`
	QueryFilters *QueryFilters `json:"query_filters,omitempty"`
}

// HasACMAdmin checks if user has ACM administrator permissions.
func (u *UserContext) HasACMAdmin() bool {
	clusterAdminGroups := []string{"system:masters", "cluster-admins"}
	for _, group := range u.Groups {
		for _, adminGroup := range clusterAdminGroups {
			if group == adminGroup {
				return true
			}
		}
	}

	adminUsers := []string{"kube:admin", "system:admin", "admin"}
	for _, adminUser := range adminUsers {
		if u.Username == adminUser {
			return true
		}
	}

	return false
}

// TokenValidationResult represents the result of token validation.
type TokenValidationResult struct {
	Valid bool         `json:"valid"`
	User  *UserContext `json:"user,omitempty"`
	Error string       `json:"error,omitempty"`
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	EnableAuth      bool
	KubernetesHost  string
	KubernetesPort  string
	KubernetesURL   string
	TokenValue      string
	TokenPath       string
	KubeconfigPath  string
	SkipTLS         bool
	AuthTimeout     time.Duration
	CacheTokens     bool
	CacheTTL        time.Duration
	DiscoveryTTL    time.Duration
	DiscoverySource string
}

// K8sConfig represents Kubernetes client configuration.
type K8sConfig struct {
	Host      string
	Port      string
	URL       string
	Token     string
	TokenPath string
	TLSVerify bool
	Timeout   time.Duration
}

// Permission represents a specific Kubernetes permission check.
type Permission struct {
	Verb     string `json:"verb"`
	Resource string `json:"resource"`
	Group    string `json:"group"`
}

// Standard permissions for ACM access.
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

// contextKey is a type for storing values in context.
type contextKey string

// UserContextKey is the context key for user context.
const UserContextKey contextKey = "user_context"

// UserFromContext extracts user context from request context.
func UserFromContext(ctx context.Context) *UserContext {
	if user, ok := ctx.Value(UserContextKey).(*UserContext); ok {
		return user
	}
	return nil
}

// WithUserContext adds user context to request context.
func WithUserContext(ctx context.Context, user *UserContext) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}

// NewAuthConfigFromServerValues creates an AuthConfig from server configuration values.
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
		KubernetesHost:  os.Getenv("KUBERNETES_SERVICE_HOST"),
		KubernetesPort:  os.Getenv("KUBERNETES_SERVICE_PORT"),
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
