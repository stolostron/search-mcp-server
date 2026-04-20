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

	t.Run("hardcoded fallback mapping", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Test standard Kubernetes resources
		assert.Equal(t, "Pod", discovery.getHardcodedMapping("pods"))
		assert.Equal(t, "Service", discovery.getHardcodedMapping("services"))
		assert.Equal(t, "Deployment", discovery.getHardcodedMapping("deployments"))

		// Test KubeVirt resources
		assert.Equal(t, "VirtualMachine", discovery.getHardcodedMapping("virtualmachines"))
		assert.Equal(t, "VirtualMachineInstance", discovery.getHardcodedMapping("virtualmachineinstances"))

		// Test operator resources
		assert.Equal(t, "Application", discovery.getHardcodedMapping("applications"))
		assert.Equal(t, "VirtualService", discovery.getHardcodedMapping("virtualservices"))

		// Test unknown resource
		assert.Equal(t, "", discovery.getHardcodedMapping("unknownresource"))
	})

	t.Run("algorithmic mapping", func(t *testing.T) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Test plural to singular conversion
		assert.Equal(t, "Pod", discovery.algorithmicMapping("pods"))
		assert.Equal(t, "Service", discovery.algorithmicMapping("services"))  // 'ss' preserved
		assert.Equal(t, "Deployment", discovery.algorithmicMapping("deployments"))

		// Test ies → y conversion
		assert.Equal(t, "Policy", discovery.algorithmicMapping("policies"))
		assert.Equal(t, "Category", discovery.algorithmicMapping("categories"))

		// Test words that don't end in 's'
		assert.Equal(t, "Node", discovery.algorithmicMapping("node"))

		// Test edge cases
		assert.Equal(t, "", discovery.algorithmicMapping(""))
		assert.Equal(t, "A", discovery.algorithmicMapping("a"))
	})

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

	t.Run("fallback hierarchy - hardcoded resources", func(t *testing.T) {
		// Test that hardcoded resources work even without live discovery
		// This simulates the scenario where discovery fails but we have hardcoded mappings

		testCases := []struct {
			resource     string
			expectedKind string
		}{
			{"pods", "Pod"},
			{"virtualmachines", "VirtualMachine"},
			{"applications", "Application"}, // ArgoCD
			{"virtualservices", "VirtualService"}, // Istio
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(ctx, "Bearer test-token", "", tc.resource)

				assert.Equal(t, tc.expectedKind, kind)
				assert.NotNil(t, result)
				// Should use hardcoded fallback when discovery isn't available
				assert.Contains(t, []string{"cache", "hardcoded"}, result.Source)
			})
		}
	})

	t.Run("fallback hierarchy - unknown resources", func(t *testing.T) {
		// Test algorithmic fallback for completely unknown resources
		testCases := []struct {
			resource     string
			expectedKind string
		}{
			{"customresources", "Customresource"}, // Simple s removal
			{"policies", "Policy"},                 // ies → y conversion
			{"newoperatorcrds", "Newoperatorcrd"}, // Unknown operator resource
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(ctx, "Bearer test-token", "", tc.resource)

				assert.Equal(t, tc.expectedKind, kind)
				assert.NotNil(t, result)
				// Should use algorithmic fallback for unknown resources
				assert.Equal(t, "algorithmic", result.Source)
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
				kind := resolver.mapResourceToKindWithToken(tc.apiGroup, tc.resource, "Bearer test-token")
				assert.Equal(t, tc.expected, kind)
			})
		}
	})

	t.Run("RBAC resolver handles discovery errors gracefully", func(t *testing.T) {
		// Test with a resolver that has no discovery initialized (error scenario)
		errorResolver := NewRBACResolver(config, nil) // nil database for tests
		// Don't initialize discovery to simulate error condition

		kind := errorResolver.mapResourceToKindWithToken("", "pods", "Bearer test-token")
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

	b.Run("hardcoded_fallback", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			discovery.getHardcodedMapping("pods")
		}
	})

	b.Run("algorithmic_fallback", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			discovery.algorithmicMapping("unknownresource")
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