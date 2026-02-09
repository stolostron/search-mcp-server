package auth

import (
	"context"
	"time"
)

// UserContext represents an authenticated user with their permissions
type UserContext struct {
	Username    string   `json:"username"`
	UID         string   `json:"uid"`
	Groups      []string `json:"groups"`
	AuthMethod  string   `json:"auth_method"`  // "bearer", "k8s-auth", etc.
	HeaderSource string  `json:"header_source"` // "Authorization" or "kubernetes-authorization"
	ValidatedAt time.Time `json:"validated_at"`
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