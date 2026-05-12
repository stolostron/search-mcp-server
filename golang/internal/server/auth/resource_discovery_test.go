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
		discovery := GetSharedResourceDiscovery(config, nil)

		assert.NotNil(t, discovery)
		assert.NotNil(t, globalResourceCache)
		assert.Equal(t, config, discovery.config)
		assert.Equal(t, 1*time.Hour, globalResourceCache.ttl)

		// Verify shared instance behavior
		discovery2 := GetSharedResourceDiscovery(config, nil)
		assert.Same(t, discovery, discovery2, "Should return the same shared instance")
	})

	t.Run("cache operations", func(t *testing.T) {
		discovery := GetSharedResourceDiscovery(config, nil)

		// Test cache miss on fresh instance
		result := discovery.getFromCache("pods")
		assert.False(t, result.Found)
		assert.Empty(t, result.Kind)

		// Test cache update and hit
		testMappings := map[string]string{
			"pods":        "Pod",
			"deployments": "Deployment",
		}
		discovery.updateCache(testMappings)

		result = discovery.getFromCache("pods")
		assert.True(t, result.Found)
		assert.True(t, result.CacheFresh)
		assert.Equal(t, "Pod", result.Kind)

		result = discovery.getFromCache("deployments")
		assert.True(t, result.Found)
		assert.True(t, result.CacheFresh)
		assert.Equal(t, "Deployment", result.Kind)

		// Test cache miss for unknown resource
		result = discovery.getFromCache("unknown")
		assert.False(t, result.Found)
		assert.True(t, result.CacheFresh)
		assert.Empty(t, result.Kind)
	})

	t.Run("cache expiration", func(t *testing.T) {
		discovery := GetSharedResourceDiscovery(config, nil)

		// Store original TTL and restore after test
		originalTTL := globalResourceCache.ttl
		defer func() {
			globalResourceCache.ttl = originalTTL
		}()

		globalResourceCache.ttl = 1 * time.Millisecond // Very short TTL for testing

		testMappings := map[string]string{"pods": "Pod"}
		discovery.updateCache(testMappings)

		// Should hit while fresh
		result := discovery.getFromCache("pods")
		assert.True(t, result.Found)
		assert.True(t, result.CacheFresh)
		assert.Equal(t, "Pod", result.Kind)

		// Wait for expiration
		time.Sleep(5 * time.Millisecond)

		// Should miss after expiration
		result = discovery.getFromCache("pods")
		assert.False(t, result.CacheFresh)  // Cache is stale
		assert.Equal(t, "Pod", result.Kind) // But data is still there
		assert.True(t, result.Found)
	})

	t.Run("cache stats", func(t *testing.T) {
		discovery := GetSharedResourceDiscovery(config, nil)

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

	discovery := GetSharedResourceDiscovery(config, nil)
	ctx := context.Background()

	t.Run("simplified discovery - no fallbacks", func(t *testing.T) {
		// Test that resources use cache when available, otherwise return not found
		// Fallbacks were removed to simplify discovery logic

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
				// Should use cache when available, otherwise not_found (no fallbacks)
				assert.Equal(t, tc.expectedSource, result.Source)
			})
		}
	})

	t.Run("unknown resources - no fallbacks", func(t *testing.T) {
		// Test that completely unknown resources return not_found
		// Algorithmic fallbacks were removed to simplify logic
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

	// Initialize discovery (this would normally happen in ResolveUserPermissions)
	resolver.resourceDiscovery = GetSharedResourceDiscovery(config, nil)

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
		// Explicitly set discovery to nil to simulate error condition
		errorResolver.resourceDiscovery = nil

		kind := errorResolver.mapResourceToKindWithToken(context.Background(), "", "pods", "Bearer test-token")
		// Should return empty string when discovery is unavailable
		assert.Equal(t, "", kind)
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

	discovery := GetSharedResourceDiscovery(config, nil)
	ctx := context.Background()

	// Populate cache to test cache performance
	testMappings := make(map[string]string)
	for i := 0; i < 1000; i++ {
		testMappings[string(rune(i))] = string(rune(i)) + "Kind"
	}
	discovery.updateCache(testMappings)

	b.Run("cache_hit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = discovery.getFromCache("0") // First entry, should always hit
		}
	})

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