package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResourceDiscovery(t *testing.T) {
	// Mock auth config for testing
	config := &AuthConfig{
		KubernetesHost:  "localhost",
		KubernetesPort:  "6443",
		SkipTLS:         true,
		DiscoveryTTL:    1 * time.Hour,     // Set proper TTL for tests
		DiscoverySource: "kubernetes",      // Use kubernetes source for tests (no db)
	}

	t.Run("initialization", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		assert.NotNil(t, discovery)
		assert.NotNil(t, globalResourceCache)
		assert.Equal(t, config, discovery.config)
		assert.Equal(t, 1*time.Hour, globalResourceCache.ttl)

		// Verify shared instance behavior
		discovery2 := NewResourceDiscovery(config, "Bearer other-token")
		assert.Same(t, discovery, discovery2, "Should return the same shared instance")
	})

	t.Run("cache operations", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Test cache miss on fresh instance
		kind, found := discovery.getCachedMapping("pods")
		assert.False(t, found)
		assert.Empty(t, kind)

		// Test cache update and hit
		testMappings := map[string]string{
			"pods":        "Pod",
			"deployments": "Deployment",
		}
		discovery.updateCache(testMappings)

		kind, found = discovery.getCachedMapping("pods")
		assert.True(t, found)
		assert.Equal(t, "Pod", kind)

		kind, found = discovery.getCachedMapping("deployments")
		assert.True(t, found)
		assert.Equal(t, "Deployment", kind)

		// Test cache miss for unknown resource
		kind, found = discovery.getCachedMapping("unknown")
		assert.False(t, found)
		assert.Empty(t, kind)
	})

	t.Run("cache expiration", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Store original TTL and restore after test
		originalTTL := globalResourceCache.ttl
		defer func() {
			globalResourceCache.ttl = originalTTL
		}()

		globalResourceCache.ttl = 1 * time.Millisecond // Very short TTL for testing

		testMappings := map[string]string{"pods": "Pod"}
		discovery.updateCache(testMappings)

		// Should hit while fresh
		kind, found := discovery.getCachedMapping("pods")
		assert.True(t, found)
		assert.Equal(t, "Pod", kind)

		// Wait for expiration
		time.Sleep(5 * time.Millisecond)

		// Should miss after expiration
		kind, found = discovery.getCachedMapping("pods")
		assert.False(t, found)
		assert.Empty(t, kind)
	})

	// Removed hardcoded and algorithmic mapping tests
	// per reviewer feedback to simplify discovery logic

	t.Run("cache stats", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Populate cache and check stats
		testMappings := map[string]string{
			"pods":        "Pod",
			"deployments": "Deployment",
			"services":    "Service",
		}
		discovery.updateCache(testMappings)

		// Check updated stats
		stats := discovery.GetCacheStats()
		assert.Equal(t, 3, stats["cache_size"])
		assert.False(t, stats["is_expired"].(bool))
		assert.InDelta(t, 0.0, stats["age_minutes"].(float64), 0.1) // Should be very recent
		assert.Equal(t, 1.0, stats["ttl_hours"].(float64))
	})
}

func TestResourceDiscoveryFallbackHierarchy(t *testing.T) {
	config := &AuthConfig{
		KubernetesHost:  "localhost",
		KubernetesPort:  "6443",
		SkipTLS:         true,
		DiscoveryTTL:    1 * time.Hour,     // Set proper TTL for tests
		DiscoverySource: "kubernetes",      // Use kubernetes source for tests (no db)
	}

	discovery := NewResourceDiscovery(config, "Bearer test-token")
	ctx := context.Background()

	t.Run("simplified discovery - no fallbacks", func(t *testing.T) {
		// Test that resources use cache when available, otherwise return not found
		// Fallbacks were removed per reviewer feedback (A2) to simplify discovery logic

		testCases := []struct {
			resource       string
			expectedKind   string
			expectedSource string
		}{
			{"pods", "Pod", "cache"},         // Should be in cache from setup
			{"virtualmachines", "", "not_found"}, // Not in cache, no fallbacks
			{"applications", "", "not_found"},    // Not in cache, no fallbacks
			{"virtualservices", "", "not_found"}, // Not in cache, no fallbacks
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(ctx, "Bearer test-token", "", tc.resource)

				assert.Equal(t, tc.expectedKind, kind)
				assert.NotNil(t, result)
				// Should use cache when available, otherwise not_found (no fallbacks per A2 fix)
				assert.Equal(t, tc.expectedSource, result.Source)
			})
		}
	})

	t.Run("unknown resources - no fallbacks", func(t *testing.T) {
		// Test that completely unknown resources return not_found
		// Algorithmic fallbacks were removed per reviewer feedback (A2) to simplify logic
		testCases := []struct {
			resource string
		}{
			{"customresources"}, // Unknown resource
			{"policies"},        // Unknown resource
			{"newoperatorcrds"}, // Unknown operator resource
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(ctx, "Bearer test-token", "", tc.resource)

				assert.Equal(t, "", kind) // No fallbacks = empty string
				assert.NotNil(t, result)
				// Should return not_found for unknown resources (no algorithmic fallback)
				assert.Equal(t, "not_found", result.Source)
			})
		}
	})
}

func TestResourceDiscoveryIntegrationWithRBAC(t *testing.T) {
	// Test the integration between resource discovery and RBAC resolution

	config := &AuthConfig{
		KubernetesHost:  "localhost",
		KubernetesPort:  "6443",
		SkipTLS:         true,
		DiscoveryTTL:    1 * time.Hour,     // Set proper TTL for tests
		DiscoverySource: "kubernetes",      // Use kubernetes source for tests (no db)
	}

	resolver := NewRBACResolver(config, nil) // nil database for tests
	userToken := "Bearer test-token"

	// Initialize discovery (this would normally happen in ResolveUserPermissions)
	resolver.resourceDiscovery = NewResourceDiscovery(config, userToken)

	// Populate cache with some test mappings
	testMappings := map[string]string{
		"pods":             "Pod",
		"virtualmachines":  "VirtualMachine",
		"applications":     "Application",      // ArgoCD
		"virtualservices":  "VirtualService",   // Istio
		"pipelines":        "Pipeline",         // Tekton
		"customoperator":   "CustomOperator",   // Unknown operator
	}
	resolver.resourceDiscovery.updateCache(testMappings)

	t.Run("RBAC resolver uses discovery for known resources", func(t *testing.T) {
		testCases := []struct {
			apiGroup string
			resource string
			expected string
		}{
			{"", "pods", "Pod"},
			{"kubevirt.io", "virtualmachines", "VirtualMachine"},
			{"argoproj.io", "applications", "Application"},
			{"networking.istio.io", "virtualservices", "VirtualService"},
			{"tekton.dev", "pipelines", "Pipeline"},
			{"custom.io", "customoperator", "CustomOperator"},
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind := resolver.mapResourceToKindWithToken(context.Background(), tc.apiGroup, tc.resource, "Bearer test-token")
				assert.Equal(t, tc.expected, kind)
			})
		}
	})

	t.Run("RBAC resolver handles discovery errors gracefully", func(t *testing.T) {
		// Test with a resolver that has no discovery initialized (error scenario)
		errorResolver := NewRBACResolver(config, nil) // nil database for tests
		// Don't initialize discovery to simulate error condition

		kind := errorResolver.mapResourceToKindWithToken(context.Background(), "", "pods", "Bearer test-token")
		// Should use algorithmic fallback
		assert.Equal(t, "Pod", kind)
	})
}

// Benchmark the discovery performance
func BenchmarkResourceDiscovery(b *testing.B) {
	config := &AuthConfig{
		KubernetesHost:  "localhost",
		KubernetesPort:  "6443",
		SkipTLS:         true,
		DiscoveryTTL:    1 * time.Hour,     // Set proper TTL for tests
		DiscoverySource: "kubernetes",      // Use kubernetes source for tests (no db)
	}

	discovery := NewResourceDiscovery(config, "Bearer test-token")
	ctx := context.Background()

	// Populate cache to test cache performance
	testMappings := make(map[string]string)
	for i := 0; i < 1000; i++ {
		testMappings[string(rune(i))] = string(rune(i)) + "Kind"
	}
	discovery.updateCache(testMappings)

	b.Run("cache_hit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			discovery.getCachedMapping("0") // First entry, should always hit
		}
	})

	// Removed hardcoded and algorithmic fallback benchmarks
	// per reviewer feedback to simplify discovery logic

	b.Run("full_discovery_flow_cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			discovery.GetResourceKind(ctx, "Bearer test-token", "", "0") // Should hit cache
		}
	})

	b.Run("full_discovery_flow_hardcoded", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			discovery.GetResourceKind(ctx, "Bearer test-token", "", "pods") // Should hit hardcoded
		}
	})
}