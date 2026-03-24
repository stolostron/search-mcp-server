//go:build integration

package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterviewv1alpha1 "github.com/stolostron/cluster-lifecycle-api/clusterview/v1alpha1"
)

// MockKubernetesServer provides a mock Kubernetes API server for testing authentication
type MockKubernetesServer struct {
	server      *httptest.Server
	tokenConfig map[string]TokenConfig
}

// TokenConfig defines how the mock server should respond to a specific token
type TokenConfig struct {
	Valid          bool
	Username       string
	UID            string
	Groups         []string
	Permissions    []PermissionRule
	ShouldFailAuth bool // Simulate auth failures
}

// PermissionRule matches the structure used in the real auth code
type PermissionRule struct {
	Verbs      []string
	APIGroups  []string
	Resources  []string
	Clusters   []string
	Namespaces []string
}

// NewMockKubernetesServer creates a new mock Kubernetes API server
func NewMockKubernetesServer() *MockKubernetesServer {
	mock := &MockKubernetesServer{
		tokenConfig: make(map[string]TokenConfig),
	}

	// Set up default token configurations for common test scenarios
	mock.setupDefaultTokens()

	// Create HTTP server with routes
	mux := http.NewServeMux()
	mock.setupRoutes(mux)

	mock.server = httptest.NewTLSServer(mux)
	return mock
}

// URL returns the URL of the mock server
func (m *MockKubernetesServer) URL() string {
	return m.server.URL
}

// Close shuts down the mock server
func (m *MockKubernetesServer) Close() {
	m.server.Close()
}

// AddToken adds a custom token configuration
func (m *MockKubernetesServer) AddToken(token string, config TokenConfig) {
	m.tokenConfig[token] = config
}

// setupDefaultTokens configures realistic token scenarios for testing
func (m *MockKubernetesServer) setupDefaultTokens() {
	// Cluster admin token - full access
	m.tokenConfig["cluster-admin-token"] = TokenConfig{
		Valid:    true,
		Username: "system:admin",
		UID:      "cluster-admin-uid",
		Groups:   []string{"system:masters", "system:authenticated"},
		Permissions: []PermissionRule{
			{
				Verbs:      []string{"get", "list", "create", "update", "delete", "watch"},
				APIGroups:  []string{"*"},
				Resources:  []string{"*"},
				Clusters:   []string{"*"},
				Namespaces: []string{"*"},
			},
		},
	}

	// Developer token - limited namespace access
	m.tokenConfig["developer-token"] = TokenConfig{
		Valid:    true,
		Username: "alice-developer",
		UID:      "alice-uid",
		Groups:   []string{"system:authenticated", "developers"},
		Permissions: []PermissionRule{
			{
				Verbs:      []string{"get", "list"},
				APIGroups:  []string{""},
				Resources:  []string{"pods", "services"},
				Clusters:   []string{"dev-cluster"},
				Namespaces: []string{"my-app", "shared-tools"},
			},
			{
				Verbs:      []string{"get", "list", "create", "update"},
				APIGroups:  []string{""},
				Resources:  []string{"configmaps"},
				Clusters:   []string{"dev-cluster"},
				Namespaces: []string{"my-app"},
			},
		},
	}

	// Namespace admin token - full access to specific namespaces
	m.tokenConfig["namespace-admin-token"] = TokenConfig{
		Valid:    true,
		Username: "bob-admin",
		UID:      "bob-uid",
		Groups:   []string{"system:authenticated", "namespace-admins"},
		Permissions: []PermissionRule{
			{
				Verbs:      []string{"get", "list", "create", "update", "delete", "watch"},
				APIGroups:  []string{"*"},
				Resources:  []string{"*"},
				Clusters:   []string{"prod-cluster"},
				Namespaces: []string{"app-frontend", "app-backend"},
			},
		},
	}

	// Read-only monitoring user
	m.tokenConfig["readonly-token"] = TokenConfig{
		Valid:    true,
		Username: "monitor-user",
		UID:      "monitor-uid",
		Groups:   []string{"system:authenticated", "monitoring"},
		Permissions: []PermissionRule{
			{
				Verbs:      []string{"get", "list"},
				APIGroups:  []string{""},
				Resources:  []string{"pods", "services", "events"},
				Clusters:   []string{"monitoring-cluster", "vm-cluster"},
				Namespaces: []string{"monitoring", "prometheus", "grafana", "*"},
			},
		},
	}

	// Invalid token
	m.tokenConfig["invalid-token"] = TokenConfig{
		Valid: false,
	}

	// Valid token but no permissions
	m.tokenConfig["no-permissions-token"] = TokenConfig{
		Valid:       true,
		Username:    "no-access-user",
		UID:         "no-access-uid",
		Groups:      []string{"system:authenticated"},
		Permissions: []PermissionRule{}, // Empty permissions
	}

	// Token that causes auth API failure
	m.tokenConfig["auth-failure-token"] = TokenConfig{
		Valid:          true,
		Username:       "auth-fail-user",
		UID:            "auth-fail-uid",
		Groups:         []string{"system:authenticated"},
		ShouldFailAuth: true,
	}
}

// setupRoutes configures the HTTP routes for the mock server
func (m *MockKubernetesServer) setupRoutes(mux *http.ServeMux) {
	// TokenReview API - validates bearer tokens (correct Kubernetes API path)
	mux.HandleFunc("/apis/authentication.k8s.io/v1/tokenreviews", m.handleTokenReview)

	// SelfSubjectAccessReview API - checks individual permissions
	mux.HandleFunc("/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", m.handleSelfSubjectAccessReview)

	// SelfSubjectRulesReview API - gets all user permissions
	mux.HandleFunc("/apis/authorization.k8s.io/v1/selfsubjectrulesreviews", m.handleSelfSubjectRulesReview)

	// UserPermission API from cluster-lifecycle-api - handle all possible endpoints
	mux.HandleFunc("/apis/cluster.open-cluster-management.io/v1alpha1/selfpermissions", m.handleUserPermissionAPI)
	mux.HandleFunc("/apis/clusterview.open-cluster-management.io/v1alpha1/userpermissions", m.handleUserPermissionAPI)
	mux.HandleFunc("/apis/clusterview.open-cluster-management.io/v1alpha1/", m.handleUserPermissionAPI)

	// Discovery API - for resource discovery
	mux.HandleFunc("/api", m.handleDiscoveryAPI)
	mux.HandleFunc("/apis", m.handleDiscoveryAPI)

	// Add catch-all handler for any UserPermission API variations (must be last)
	mux.HandleFunc("/", m.handleCatchAll)
}

// handleTokenReview handles token validation requests
func (m *MockKubernetesServer) handleTokenReview(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("[MOCK-DEBUG] TokenReview handler called - Method: %s, URL: %s, Path: %s\n", r.Method, r.URL.String(), r.URL.Path)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tokenReview authenticationv1.TokenReview
	if err := json.NewDecoder(r.Body).Decode(&tokenReview); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	token := strings.TrimPrefix(tokenReview.Spec.Token, "Bearer ")
	config, exists := m.tokenConfig[token]

	response := authenticationv1.TokenReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authentication.k8s.io/v1",
			Kind:       "TokenReview",
		},
		Status: authenticationv1.TokenReviewStatus{
			Authenticated: config.Valid && exists,
		},
	}

	if config.Valid && exists {
		response.Status.User = authenticationv1.UserInfo{
			Username: config.Username,
			UID:      config.UID,
			Groups:   config.Groups,
		}
	} else {
		response.Status.Error = "Invalid token"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSelfSubjectAccessReview handles permission check requests
func (m *MockKubernetesServer) handleSelfSubjectAccessReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := m.extractToken(r)
	config, exists := m.tokenConfig[token]

	var accessReview authorizationv1.SelfSubjectAccessReview
	if err := json.NewDecoder(r.Body).Decode(&accessReview); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	response := authorizationv1.SelfSubjectAccessReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1",
			Kind:       "SelfSubjectAccessReview",
		},
		Status: authorizationv1.SubjectAccessReviewStatus{
			Allowed: false,
		},
	}

	if config.Valid && exists && !config.ShouldFailAuth {
		// Check if the requested permission is allowed
		response.Status.Allowed = m.checkPermission(config.Permissions, accessReview.Spec.ResourceAttributes)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSelfSubjectRulesReview handles requests for all user permissions
func (m *MockKubernetesServer) handleSelfSubjectRulesReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := m.extractToken(r)
	config, exists := m.tokenConfig[token]

	if config.ShouldFailAuth {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := authorizationv1.SelfSubjectRulesReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1",
			Kind:       "SelfSubjectRulesReview",
		},
		Status: authorizationv1.SubjectRulesReviewStatus{
			Incomplete: false,
		},
	}

	if config.Valid && exists {
		// Convert our PermissionRules to Kubernetes ResourceRules
		for _, perm := range config.Permissions {
			resourceRule := authorizationv1.ResourceRule{
				Verbs:     perm.Verbs,
				APIGroups: perm.APIGroups,
				Resources: perm.Resources,
			}
			response.Status.ResourceRules = append(response.Status.ResourceRules, resourceRule)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUserPermissionAPI handles cluster-lifecycle-api UserPermission requests
func (m *MockKubernetesServer) handleUserPermissionAPI(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("[MOCK-DEBUG] UserPermission handler called - Method: %s, URL: %s, Path: %s\n", r.Method, r.URL.String(), r.URL.Path)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := m.extractToken(r)
	config, exists := m.tokenConfig[token]

	// fmt.Printf("[MOCK-DEBUG] UserPermission API - Token extracted: '%s' (first 10 chars), exists in config: %v, valid: %v\n",
	//	token[:minInt(len(token), 10)], exists, config.Valid)

	// Special case: empty token should return empty permissions (graceful handling)
	if token == "" {
		// Return empty UserPermissionList for empty tokens
	} else if !exists {
		// Check for unknown tokens (should be rejected with 401)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Invalid tokens (exist in config but Valid=false) return empty permissions
	// This allows the RBAC layer to handle them gracefully

	if config.ShouldFailAuth {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert our PermissionRules to UserPermission objects
	var userPermissions []clusterviewv1alpha1.UserPermission

	if config.Valid && exists {
		// fmt.Printf("[MOCK-DEBUG] Processing %d permissions for token\n", len(config.Permissions))

		for i, perm := range config.Permissions {
			// fmt.Printf("[MOCK-DEBUG] Permission %d: Verbs=%v, APIGroups=%v, Resources=%v, Clusters=%v, Namespaces=%v\n",
			//	i, perm.Verbs, perm.APIGroups, perm.Resources, perm.Clusters, perm.Namespaces)

			// Create PolicyRule for ClusterRoleDefinition
			policyRule := rbacv1.PolicyRule{
				Verbs:     perm.Verbs,
				APIGroups: perm.APIGroups,
				Resources: perm.Resources,
			}

			// Create ClusterBindings for each cluster/namespace combination
			var bindings []clusterviewv1alpha1.ClusterBinding
			for _, cluster := range perm.Clusters {
				// Determine scope and namespaces
				var scope clusterviewv1alpha1.BindingScope
				var namespaces []string

				if len(perm.Namespaces) == 1 && perm.Namespaces[0] == "*" {
					scope = clusterviewv1alpha1.BindingScopeCluster
					namespaces = []string{"*"}
				} else {
					scope = clusterviewv1alpha1.BindingScopeNamespace
					namespaces = perm.Namespaces
				}

				binding := clusterviewv1alpha1.ClusterBinding{
					Cluster:    cluster,
					Scope:      scope,
					Namespaces: namespaces,
				}
				bindings = append(bindings, binding)
			}

			// Create UserPermission object
			userPermission := clusterviewv1alpha1.UserPermission{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "clusterview.open-cluster-management.io/v1alpha1",
					Kind:       "UserPermission",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("mock-role-%d", i),
				},
				Status: clusterviewv1alpha1.UserPermissionStatus{
					Bindings: bindings,
					ClusterRoleDefinition: clusterviewv1alpha1.ClusterRoleDefinition{
						Rules: []rbacv1.PolicyRule{policyRule},
					},
				},
			}
			userPermissions = append(userPermissions, userPermission)
		}
		// fmt.Printf("[MOCK-DEBUG] Converted to %d UserPermission objects\n", len(userPermissions))
	}

	// Wrap in proper Kubernetes UserPermissionList format
	response := clusterviewv1alpha1.UserPermissionList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "clusterview.open-cluster-management.io/v1alpha1",
			Kind:       "UserPermissionList",
		},
		Items: userPermissions,
	}

	w.Header().Set("Content-Type", "application/json")

	// Debug: show what we're returning
	// responseBytes, _ := json.Marshal(response)
	// fmt.Printf("[MOCK-DEBUG] UserPermission API response: %s\n", string(responseBytes))

	json.NewEncoder(w).Encode(response)
}

// handleDiscoveryAPI handles basic discovery requests (simplified)
func (m *MockKubernetesServer) handleDiscoveryAPI(w http.ResponseWriter, r *http.Request) {
	// Handle different discovery endpoints
	switch r.URL.Path {
	case "/api":
		// Core API discovery
		discoveryResponse := map[string]interface{}{
			"kind":       "APIVersions",
			"apiVersion": "v1",
			"versions":   []string{"v1"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discoveryResponse)

	case "/apis":
		// API group discovery - include cluster-lifecycle-api groups
		discoveryResponse := map[string]interface{}{
			"kind":       "APIGroupList",
			"apiVersion": "v1",
			"groups": []interface{}{
				map[string]interface{}{
					"name": "cluster.open-cluster-management.io",
					"versions": []interface{}{
						map[string]interface{}{
							"groupVersion": "cluster.open-cluster-management.io/v1alpha1",
							"version":      "v1alpha1",
						},
					},
					"preferredVersion": map[string]interface{}{
						"groupVersion": "cluster.open-cluster-management.io/v1alpha1",
						"version":      "v1alpha1",
					},
				},
				map[string]interface{}{
					"name": "clusterview.open-cluster-management.io",
					"versions": []interface{}{
						map[string]interface{}{
							"groupVersion": "clusterview.open-cluster-management.io/v1alpha1",
							"version":      "v1alpha1",
						},
					},
					"preferredVersion": map[string]interface{}{
						"groupVersion": "clusterview.open-cluster-management.io/v1alpha1",
						"version":      "v1alpha1",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discoveryResponse)

	default:
		// Default minimal response for other discovery endpoints
		discoveryResponse := map[string]interface{}{
			"kind":       "APIResourceList",
			"apiVersion": "v1",
			"resources":  []interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discoveryResponse)
	}
}

// Helper methods

// extractToken extracts the bearer token from the Authorization header
func (m *MockKubernetesServer) extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	return strings.TrimPrefix(authHeader, "Bearer ")
}

// checkPermission checks if a specific permission is allowed based on user's permission rules
func (m *MockKubernetesServer) checkPermission(permissions []PermissionRule, attrs *authorizationv1.ResourceAttributes) bool {
	if attrs == nil {
		return false
	}

	for _, perm := range permissions {
		// Check if this permission rule matches the request
		if m.matchesVerb(perm.Verbs, attrs.Verb) &&
			m.matchesAPIGroup(perm.APIGroups, attrs.Group) &&
			m.matchesResource(perm.Resources, attrs.Resource) {
			return true
		}
	}
	return false
}

// matchesVerb checks if the requested verb is allowed
func (m *MockKubernetesServer) matchesVerb(allowedVerbs []string, requestedVerb string) bool {
	for _, verb := range allowedVerbs {
		if verb == "*" || verb == requestedVerb {
			return true
		}
	}
	return false
}

// matchesAPIGroup checks if the requested API group is allowed
func (m *MockKubernetesServer) matchesAPIGroup(allowedGroups []string, requestedGroup string) bool {
	for _, group := range allowedGroups {
		if group == "*" || group == requestedGroup {
			return true
		}
	}
	return false
}

// matchesResource checks if the requested resource is allowed
func (m *MockKubernetesServer) matchesResource(allowedResources []string, requestedResource string) bool {
	for _, resource := range allowedResources {
		if resource == "*" || resource == requestedResource {
			return true
		}
	}
	return false
}

// handleCatchAll handles any unmatched requests - useful for debugging
func (m *MockKubernetesServer) handleCatchAll(w http.ResponseWriter, r *http.Request) {
	// DEBUG: Log what requests are hitting the catch-all
	// Debug: Log what requests are hitting the catch-all
	// fmt.Printf("[MOCK-DEBUG] Catch-all handler called - Method: %s, URL: %s, Path: %s\n", r.Method, r.URL.String(), r.URL.Path)

	// Check if this looks like a UserPermission API request
	if strings.Contains(r.URL.Path, "userpermissions") || strings.Contains(r.URL.Path, "selfpermissions") {
		m.handleUserPermissionAPI(w, r)
		return
	}

	// For other requests, return 404
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"not found","reason":"NotFound","code":404}`))
}

// Helper function since Go doesn't have min for int
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}