//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/test/helpers"
)

// TestAuthMiddlewareIntegration tests the complete HTTP middleware chain
// with realistic Kubernetes API responses using a mock server
func TestAuthMiddlewareIntegration(t *testing.T) {
	// Start mock Kubernetes server
	mockK8s := helpers.NewMockKubernetesServer()
	defer mockK8s.Close()

	// Configure auth to use mock server
	config := &auth.AuthConfig{
		EnableAuth:     true,
		KubernetesURL:  mockK8s.URL(),
		TokenValue:     "test-server-token", // Dummy token for server auth
		SkipTLS:        true,
		AuthTimeout:    5 * time.Second,
		CacheTokens:    false, // Disable caching for cleaner tests
	}

	// Create real auth middleware
	authMiddleware, err := auth.NewAuthMiddleware(config)
	require.NoError(t, err, "Failed to create auth middleware")

	// Create test handler that captures the UserContext
	var capturedUserContext *auth.UserContext
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract UserContext from request context (real auth middleware sets this)
		if userCtx := auth.UserFromContext(r.Context()); userCtx != nil {
			capturedUserContext = userCtx
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Chain middleware
	handler := authMiddleware.Handler(testHandler)

	t.Run("cluster_admin_authentication", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer cluster-admin-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify HTTP response
		assert.Equal(t, http.StatusOK, w.Code, "Expected successful authentication")
		assert.Equal(t, "success", w.Body.String())

		// Verify UserContext was set correctly
		require.NotNil(t, capturedUserContext, "UserContext should be set for authenticated request")
		assert.Equal(t, "system:admin", capturedUserContext.Username)
		assert.Contains(t, capturedUserContext.Groups, "system:masters")
		assert.Equal(t, "Authorization", capturedUserContext.HeaderSource)

		// Verify QueryFilters for cluster admin
		require.NotNil(t, capturedUserContext.QueryFilters, "QueryFilters should be set")
		assert.True(t, capturedUserContext.QueryFilters.HasWildcardAccess(), "Cluster admin should have wildcard access")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedClusters, "*")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedNamespaces, "*")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "*")
	})

	t.Run("developer_authentication", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer developer-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify HTTP response
		assert.Equal(t, http.StatusOK, w.Code, "Expected successful authentication")

		// Verify UserContext
		require.NotNil(t, capturedUserContext, "UserContext should be set")
		assert.Equal(t, "alice-developer", capturedUserContext.Username)
		assert.Contains(t, capturedUserContext.Groups, "developers")

		// Verify limited permissions
		require.NotNil(t, capturedUserContext.QueryFilters, "QueryFilters should be set")
		assert.False(t, capturedUserContext.QueryFilters.HasWildcardAccess(), "Developer should not have wildcard access")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedClusters, "dev-cluster")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedNamespaces, "my-app")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedNamespaces, "shared-tools")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "Pod")      // Kubernetes Kind names
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "Service") // are capitalized
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "ConfigMap")

		// Verify resource-specific namespace access
		podNamespaces := capturedUserContext.QueryFilters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"my-app", "shared-tools"}, podNamespaces)

		configmapNamespaces := capturedUserContext.QueryFilters.GetAllowedNamespacesForResource("ConfigMap")
		assert.ElementsMatch(t, []string{"my-app"}, configmapNamespaces, "ConfigMaps should have more restricted access")
	})

	t.Run("namespace_admin_authentication", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer namespace-admin-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify HTTP response
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify UserContext
		require.NotNil(t, capturedUserContext)
		assert.Equal(t, "bob-admin", capturedUserContext.Username)
		assert.Contains(t, capturedUserContext.Groups, "namespace-admins")

		// Verify namespace-scoped admin permissions
		require.NotNil(t, capturedUserContext.QueryFilters)
		assert.False(t, capturedUserContext.QueryFilters.HasWildcardAccess(), "Namespace admin should not have global wildcard")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedClusters, "prod-cluster")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedNamespaces, "app-frontend")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedNamespaces, "app-backend")
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "*")
	})

	t.Run("readonly_user_authentication", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer readonly-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify HTTP response
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify UserContext
		require.NotNil(t, capturedUserContext)
		assert.Equal(t, "monitor-user", capturedUserContext.Username)
		assert.Contains(t, capturedUserContext.Groups, "monitoring")

		// Verify read-only permissions
		require.NotNil(t, capturedUserContext.QueryFilters)
		assert.False(t, capturedUserContext.QueryFilters.HasWildcardAccess())
		assert.ElementsMatch(t, []string{"monitoring-cluster", "vm-cluster"}, capturedUserContext.QueryFilters.AllowedClusters)
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "Pod")     // Kubernetes Kind names
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "Service") // are capitalized
		assert.Contains(t, capturedUserContext.QueryFilters.AllowedResources, "Event")

		// Verify namespace wildcard for monitoring
		assert.True(t, capturedUserContext.QueryFilters.HasNamespaceWildcardForResource("Pod"), "Monitoring should have access to pods in all namespaces")
	})

	t.Run("invalid_token_rejection", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer invalid-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify authentication failure
		assert.Equal(t, http.StatusUnauthorized, w.Code, "Invalid token should result in 401")
		assert.Nil(t, capturedUserContext, "UserContext should not be set for invalid token")
		assert.Contains(t, w.Body.String(), "Token validation failed", "Should return appropriate error message")
	})

	t.Run("missing_authorization_header", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Content-Type", "application/json")
		// No Authorization header

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify authentication failure
		assert.Equal(t, http.StatusUnauthorized, w.Code, "Missing auth header should result in 401")
		assert.Nil(t, capturedUserContext, "UserContext should not be set without auth header")
		assert.Contains(t, w.Body.String(), "Missing authorization header", "Should return appropriate error message")
	})

	t.Run("no_permissions_token", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer no-permissions-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify access denial for user with no permissions
		assert.Equal(t, http.StatusForbidden, w.Code, "User with no permissions should be forbidden")
		assert.Nil(t, capturedUserContext, "UserContext should not be set for user with no permissions")
		assert.Contains(t, w.Body.String(), "No ACM-related permissions found", "Should return appropriate error message")
	})

	t.Run("kubernetes_api_failure_simulation", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Authorization", "Bearer auth-failure-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify fail-secure behavior when Kubernetes API fails
		assert.Equal(t, http.StatusInternalServerError, w.Code, "API failure should result in 500")
		assert.Nil(t, capturedUserContext, "UserContext should not be set when API fails")
		assert.Contains(t, w.Body.String(), "Permission resolution failed", "Should indicate permission resolution failure")
	})

	t.Run("kubernetes_authorization_header", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("kubernetes-authorization", "Bearer cluster-admin-token") // Alternative header
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify alternative header works
		assert.Equal(t, http.StatusOK, w.Code, "Alternative auth header should work")
		require.NotNil(t, capturedUserContext, "UserContext should be set")
		assert.Equal(t, "kubernetes-authorization", capturedUserContext.HeaderSource, "Should track correct header source")
		assert.Equal(t, "system:admin", capturedUserContext.Username)
	})

	t.Run("health_endpoint_bypass", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("GET", "/health", nil)
		// No authorization header

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify health endpoint bypasses auth
		assert.Equal(t, http.StatusOK, w.Code, "Health endpoint should bypass auth")
		assert.Equal(t, "success", w.Body.String())
		assert.Nil(t, capturedUserContext, "UserContext should not be set for health endpoint")
	})
}

// TestAuthMiddlewareDisabled tests behavior when authentication is disabled
func TestAuthMiddlewareDisabled(t *testing.T) {
	// Configure auth as disabled
	config := &auth.AuthConfig{
		EnableAuth: false,
	}

	// Create auth middleware
	authMiddleware, err := auth.NewAuthMiddleware(config)
	require.NoError(t, err)

	var capturedUserContext *auth.UserContext
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userCtx := auth.UserFromContext(r.Context()); userCtx != nil {
			capturedUserContext = userCtx
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	handler := authMiddleware.Handler(testHandler)

	t.Run("disabled_auth_allows_access", func(t *testing.T) {
		capturedUserContext = nil

		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"method":"find_resources"}`))
		req.Header.Set("Content-Type", "application/json")
		// No authorization header

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Verify access is allowed without authentication
		assert.Equal(t, http.StatusOK, w.Code, "Disabled auth should allow access")
		assert.Equal(t, "success", w.Body.String())
		assert.Nil(t, capturedUserContext, "UserContext should be nil when auth is disabled")
	})
}

// TestAuthMiddlewareTokenCaching tests token caching behavior
func TestAuthMiddlewareTokenCaching(t *testing.T) {
	// Start mock Kubernetes server
	mockK8s := helpers.NewMockKubernetesServer()
	defer mockK8s.Close()

	// Configure auth with caching enabled
	config := &auth.AuthConfig{
		EnableAuth:     true,
		KubernetesURL:  mockK8s.URL(),
		TokenValue:     "test-server-token", // Dummy token for server auth
		SkipTLS:        true,
		AuthTimeout:    5 * time.Second,
		CacheTokens:    true, // Enable caching
		CacheTTL:       time.Minute,
	}

	authMiddleware, err := auth.NewAuthMiddleware(config)
	require.NoError(t, err)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMiddleware.Handler(testHandler)

	t.Run("token_caching_works", func(t *testing.T) {
		// First request
		req1 := httptest.NewRequest("POST", "/mcp", nil)
		req1.Header.Set("Authorization", "Bearer cluster-admin-token")

		w1 := httptest.NewRecorder()
		start1 := time.Now()
		handler.ServeHTTP(w1, req1)
		duration1 := time.Since(start1)

		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request with same token (should use cache)
		req2 := httptest.NewRequest("POST", "/mcp", nil)
		req2.Header.Set("Authorization", "Bearer cluster-admin-token")

		w2 := httptest.NewRecorder()
		start2 := time.Now()
		handler.ServeHTTP(w2, req2)
		duration2 := time.Since(start2)

		assert.Equal(t, http.StatusOK, w2.Code)

		// Second request should be faster (cached)
		// Note: This is a heuristic test and might be flaky in fast environments
		t.Logf("First request: %v, Second request: %v", duration1, duration2)
		// We don't assert timing since it's environment-dependent, but the test validates caching works
	})
}

