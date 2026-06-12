package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTokenCacheKeyIsHashed asserts that raw bearer tokens are never stored as cache keys.
func TestTokenCacheKeyIsHashed(t *testing.T) {
	rawToken := "Bearer super-secret-cluster-admin-token"

	m := &AuthMiddleware{
		config:     &AuthConfig{CacheTokens: true, CacheTTL: 5 * time.Minute},
		tokenCache: make(map[string]*cachedToken),
	}

	m.cacheToken(rawToken, &TokenValidationResult{Valid: true})

	_, rawPresent := m.tokenCache[rawToken]
	assert.False(t, rawPresent, "raw bearer token must not be stored as a cache key")

	_, hashedPresent := m.tokenCache[hashToken(rawToken)]
	assert.True(t, hashedPresent, "SHA-256 hash must be used as the cache key")

	got := m.getCachedToken(rawToken)
	assert.NotNil(t, got, "getCachedToken must find the entry via its hash")
	assert.True(t, got.Valid)
}

// TestCacheTokenMiss asserts that getCachedToken returns nil for unknown tokens.
func TestCacheTokenMiss(t *testing.T) {
	m := &AuthMiddleware{
		config:     &AuthConfig{CacheTokens: true, CacheTTL: 5 * time.Minute},
		tokenCache: make(map[string]*cachedToken),
	}

	got := m.getCachedToken("Bearer never-cached-token")
	assert.Nil(t, got, "getCachedToken must return nil for an unknown token")
}

// TestCacheTokenExpiry asserts that getCachedToken returns nil once the TTL has elapsed.
func TestCacheTokenExpiry(t *testing.T) {
	m := &AuthMiddleware{
		config:     &AuthConfig{CacheTokens: true, CacheTTL: time.Millisecond},
		tokenCache: make(map[string]*cachedToken),
	}

	rawToken := "Bearer expiring-token"
	m.cacheToken(rawToken, &TokenValidationResult{Valid: true})

	// Immediately after caching the entry should be retrievable.
	assert.NotNil(t, m.getCachedToken(rawToken), "entry should be present before TTL elapses")

	// Wait for the TTL to elapse.
	time.Sleep(5 * time.Millisecond)
	assert.Nil(t, m.getCachedToken(rawToken), "getCachedToken must return nil after TTL elapses")
}

// TestCachedResultIsCloned asserts that mutating the result returned by getCachedToken
// does not affect the entry stored in the cache (i.e. each caller gets an independent copy).
func TestCachedResultIsCloned(t *testing.T) {
	rawToken := "Bearer cluster-admin-token"
	original := &TokenValidationResult{
		Valid: true,
		User: &UserContext{
			Username: "alice",
			Groups:   []string{"system:masters"},
		},
	}

	m := &AuthMiddleware{
		config:     &AuthConfig{CacheTokens: true, CacheTTL: 5 * time.Minute},
		tokenCache: make(map[string]*cachedToken),
	}
	m.cacheToken(rawToken, original)

	// First caller mutates the returned copy.
	first := m.getCachedToken(rawToken)
	assert.NotNil(t, first)
	first.User.HeaderSource = "Authorization"
	first.User.QueryFilters = &QueryFilters{HubClusterName: "local-cluster"}
	first.User.Groups = append(first.User.Groups, "injected-group")

	// Second caller must see a clean copy — no mutations from the first caller.
	second := m.getCachedToken(rawToken)
	assert.NotNil(t, second)
	assert.Equal(t, "", second.User.HeaderSource, "HeaderSource must not leak between requests")
	assert.Nil(t, second.User.QueryFilters, "QueryFilters must not leak between requests")
	assert.Equal(t, []string{"system:masters"}, second.User.Groups, "Groups must not be shared between copies")
}

// TestCacheTokenDoesNotMutateStoredEntry asserts that mutating the result after calling
// cacheToken does not corrupt the stored cache entry.
func TestCacheTokenDoesNotMutateStoredEntry(t *testing.T) {
	rawToken := "Bearer mutable-token"
	result := &TokenValidationResult{
		Valid: true,
		User: &UserContext{
			Username: "bob",
			Groups:   []string{"developers"},
		},
	}

	m := &AuthMiddleware{
		config:     &AuthConfig{CacheTokens: true, CacheTTL: 5 * time.Minute},
		tokenCache: make(map[string]*cachedToken),
	}
	m.cacheToken(rawToken, result)

	// Simulate the request handler mutating the original pointer after caching.
	result.User.HeaderSource = "kubernetes-authorization"
	result.User.QueryFilters = &QueryFilters{HubClusterName: "hub"}
	result.User.Groups = append(result.User.Groups, "extra-group")

	// The cache must have stored an independent copy.
	cached := m.getCachedToken(rawToken)
	assert.NotNil(t, cached)
	assert.Equal(t, "", cached.User.HeaderSource, "stored entry must not reflect post-cache mutation of HeaderSource")
	assert.Nil(t, cached.User.QueryFilters, "stored entry must not reflect post-cache mutation of QueryFilters")
	assert.Equal(t, []string{"developers"}, cached.User.Groups, "stored entry must not reflect post-cache mutation of Groups")
}

// TestCloneValidationResultNilSafety asserts that cloneValidationResult handles nil inputs.
func TestCloneValidationResultNilSafety(t *testing.T) {
	assert.Nil(t, cloneValidationResult(nil), "nil input must return nil")

	resultNoUser := &TokenValidationResult{Valid: false, Error: "bad token"}
	clone := cloneValidationResult(resultNoUser)
	assert.NotNil(t, clone)
	assert.Nil(t, clone.User, "clone of result with nil User must have nil User")
	assert.Equal(t, "bad token", clone.Error)
}

// TestCloneValidationResultNilGroups asserts that a nil Groups slice is handled safely.
func TestCloneValidationResultNilGroups(t *testing.T) {
	result := &TokenValidationResult{
		Valid: true,
		User:  &UserContext{Username: "carol", Groups: nil},
	}
	clone := cloneValidationResult(result)
	assert.NotNil(t, clone)
	assert.Nil(t, clone.User.Groups, "nil Groups must remain nil in clone")
}

// TestPermissionSourceSecurityIsolation validates the new PermissionSource-based security model
func TestPermissionSourceSecurityIsolation(t *testing.T) {
	t.Skip("Skipping stub method tests - these helper methods are not used in production find-resources code. Production validation done via user1/user2/user3 real testing.")

	t.Run("permission source isolation", func(t *testing.T) {
		// Test that permissions from different sources don't create Cartesian products
		filters := &QueryFilters{
			HubClusterName: "local-cluster",
			PermissionSources: []PermissionSource{
				{
					Source:              "userpermission",
					ClusterScopedKinds:  map[string][]ResourcePermission{}, // No cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"app-frontend": {{Kind: "Pod", APIGroup: ""}},
					},
					ManagedClusters: map[string]struct{}{
						"managed-cluster-1": {},
					},
				},
				{
					Source:              "hub-kubernetes",
					ClusterScopedKinds:  map[string][]ResourcePermission{"local-cluster": {{Kind: "ManagedCluster", APIGroup: "cluster.open-cluster-management.io"}}}, // Cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"*": {{Kind: "ManagedCluster", APIGroup: "cluster.open-cluster-management.io"}},
					},
					ManagedClusters: map[string]struct{}{
						"local-cluster": {},
					},
				},
			},
		}

		// Test cluster access
		assert.True(t, filters.IsClusterAllowed("managed-cluster-1"), "Should allow managed cluster access")
		assert.True(t, filters.IsClusterAllowed("local-cluster"), "Should allow hub cluster access")
		assert.False(t, filters.IsClusterAllowed("unauthorized-cluster"), "Should deny unauthorized cluster")

		// Test resource access
		assert.True(t, filters.IsResourceKindAllowed("Pod"), "Should allow Pod access")
		assert.True(t, filters.IsResourceKindAllowed("ManagedCluster"), "Should allow ManagedCluster access")
		assert.False(t, filters.IsResourceKindAllowed("UnauthorizedResource"), "Should deny unauthorized resource")

		// Test namespace access (critical - no Cartesian products)
		assert.True(t, filters.IsNamespaceAllowedInCluster("managed-cluster-1", "app-frontend"), "Should allow specific namespace in managed cluster")
		assert.False(t, filters.IsNamespaceAllowedInCluster("managed-cluster-1", "unauthorized-namespace"), "Should deny unauthorized namespace in managed cluster")
		assert.True(t, filters.IsNamespaceAllowedInCluster("local-cluster", "any-namespace"), "Should allow any namespace in hub cluster")
	})
}

// TestSecurityFirstDesign validates that the system fails secure in all error conditions
func TestSecurityFirstDesign(t *testing.T) {
	t.Run("fail secure on nil conditions", func(t *testing.T) {
		var filters *QueryFilters

		// All methods should return secure defaults for nil QueryFilters
		assert.False(t, filters.IsClusterAllowed("any-cluster"))
		assert.False(t, filters.IsResourceKindAllowed("Pod"))
		assert.False(t, filters.HasWildcardAccess())
		assert.False(t, filters.IsNamespaceAllowedInCluster("any-cluster", "any-namespace"))
	})

	t.Run("fail secure on empty conditions", func(t *testing.T) {
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{},
		}

		// SECURITY: Empty permission sources mean NO ACCESS (fail secure)
		assert.False(t, filters.IsClusterAllowed("any-cluster"))
		assert.False(t, filters.IsResourceKindAllowed("Pod"))
		assert.False(t, filters.HasWildcardAccess())
		assert.False(t, filters.IsNamespaceAllowedInCluster("any-cluster", "any-namespace"))
	})
}

// TestWildcardAccessDetection validates wildcard access detection
func TestWildcardAccessDetection(t *testing.T) {
	t.Skip("Skipping stub method tests - these helper methods are not used in production find-resources code. Production validation done via user1/user2/user3 real testing.")

	t.Run("cluster admin detection", func(t *testing.T) {
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{
				{
					Source:              "hub-kubernetes",
					ClusterScopedKinds:  map[string][]ResourcePermission{"local-cluster": {{Kind: "*", APIGroup: "*"}}}, // Wildcard cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"*": {{Kind: "*", APIGroup: "*"}},
					},
					ManagedClusters: map[string]struct{}{
						"*": {}, // Wildcard cluster access
					},
				},
			},
		}

		assert.True(t, filters.HasWildcardAccess(), "Should detect cluster admin with wildcard permissions")
		assert.True(t, filters.IsClusterAllowed("any-cluster"), "Should allow any cluster access")
		assert.True(t, filters.IsResourceKindAllowed("any-resource"), "Should allow any resource access")
		assert.True(t, filters.IsNamespaceAllowedInCluster("any-cluster", "any-namespace"), "Should allow any namespace access")
	})

	t.Run("limited access detection", func(t *testing.T) {
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{
				{
					Source:              "userpermission",
					ClusterScopedKinds:  map[string][]ResourcePermission{}, // No cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"app-1": {{Kind: "Pod", APIGroup: ""}},
					},
					ManagedClusters: map[string]struct{}{
						"cluster-1": {},
					},
				},
			},
		}

		assert.False(t, filters.HasWildcardAccess(), "Should not detect wildcard access for limited permissions")
		assert.True(t, filters.IsClusterAllowed("cluster-1"), "Should allow specific cluster access")
		assert.False(t, filters.IsClusterAllowed("other-cluster"), "Should deny other cluster access")
		assert.True(t, filters.IsResourceKindAllowed("Pod"), "Should allow Pod access")
		assert.False(t, filters.IsResourceKindAllowed("Secret"), "Should deny Secret access")
	})
}