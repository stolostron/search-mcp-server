package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNamespaceBypassPrevention validates that the critical security fix from Phase 5 works correctly
// This ensures users cannot gain unauthorized access to namespaces across different resource types
func TestNamespaceBypassPrevention(t *testing.T) {
	t.Run("resource-specific namespace filtering", func(t *testing.T) {
		// Simulate a user with mixed permissions:
		// - VirtualMachine: specific namespace access (analytics-jobs)
		// - ManagedCluster: wildcard namespace access (*)
		filters := &QueryFilters{
			AllowedClusters:   []string{"cluster-1"},
			AllowedNamespaces: []string{"analytics-jobs", "*"}, // This combination was the vulnerability
			AllowedResources:  []string{"VirtualMachine", "ManagedCluster"},
			ResourceNamespaces: map[string][]string{
				"VirtualMachine":  []string{"analytics-jobs"},     // Specific namespace only
				"ManagedCluster":  []string{"*"},                  // Wildcard access
			},
		}

		// Test 1: VirtualMachine should only access analytics-jobs namespace
		t.Run("VirtualMachine namespace isolation", func(t *testing.T) {
			assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachine"),
				"VirtualMachine should NOT have wildcard namespace access")

			allowedNS := filters.GetAllowedNamespacesForResource("VirtualMachine")
			assert.Equal(t, []string{"analytics-jobs"}, allowedNS,
				"VirtualMachine should only access analytics-jobs namespace")

			// Verify specific namespace checks
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "analytics-jobs"),
				"VirtualMachine should access analytics-jobs")
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachine", "auth-proxy"),
				"VirtualMachine should NOT access auth-proxy")
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachine", "backend-services"),
				"VirtualMachine should NOT access backend-services")
		})

		// Test 2: ManagedCluster should have wildcard access
		t.Run("ManagedCluster wildcard access preserved", func(t *testing.T) {
			assert.True(t, filters.HasNamespaceWildcardForResource("ManagedCluster"),
				"ManagedCluster should have wildcard namespace access")

			allowedNS := filters.GetAllowedNamespacesForResource("ManagedCluster")
			assert.Equal(t, []string{"*"}, allowedNS,
				"ManagedCluster should have wildcard access")

			// Verify wildcard allows all namespaces
			assert.True(t, filters.isNamespaceAllowedForResource("ManagedCluster", "any-namespace"),
				"ManagedCluster should access any namespace")
		})

		// Test 3: Unauthorized resource type should be blocked
		t.Run("unauthorized resource blocked", func(t *testing.T) {
			assert.False(t, filters.HasNamespaceWildcardForResource("Pod"),
				"Pod should NOT have any access")

			allowedNS := filters.GetAllowedNamespacesForResource("Pod")
			assert.Empty(t, allowedNS,
				"Pod should have no allowed namespaces")
		})
	})

	t.Run("privilege escalation prevention", func(t *testing.T) {
		// Test scenario: User tries to leverage wildcard access for one resource
		// to gain access to unauthorized namespaces for another resource
		filters := &QueryFilters{
			AllowedClusters:   []string{"*"},
			AllowedNamespaces: []string{"dev-*", "*"}, // Mixed patterns + wildcard
			AllowedResources:  []string{"ConfigMap", "Secret"},
			ResourceNamespaces: map[string][]string{
				"ConfigMap": []string{"dev-app1", "dev-app2"},  // Specific dev namespaces only
				"Secret":    []string{"*"},                     // Admin-level access to secrets
			},
		}

		// ConfigMap should be restricted despite global wildcard
		assert.False(t, filters.HasNamespaceWildcardForResource("ConfigMap"))
		assert.False(t, filters.isNamespaceAllowedForResource("ConfigMap", "prod-database"),
			"ConfigMap should not access production namespace")
		assert.True(t, filters.isNamespaceAllowedForResource("ConfigMap", "dev-app1"),
			"ConfigMap should access allowed dev namespace")

		// Secret should have wildcard access
		assert.True(t, filters.HasNamespaceWildcardForResource("Secret"))
		assert.True(t, filters.isNamespaceAllowedForResource("Secret", "prod-database"),
			"Secret should access any namespace with wildcard permission")
	})

	t.Run("edge cases and security boundaries", func(t *testing.T) {
		// Test empty/nil scenarios to ensure fail-secure behavior
		t.Run("nil QueryFilters", func(t *testing.T) {
			var filters *QueryFilters = nil

			assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachine"),
				"nil QueryFilters should deny wildcard access")
			assert.Empty(t, filters.GetAllowedNamespacesForResource("VirtualMachine"),
				"nil QueryFilters should return empty namespaces")
		})

		t.Run("empty ResourceNamespaces", func(t *testing.T) {
			filters := &QueryFilters{
				AllowedClusters:    []string{"cluster-1"},
				AllowedNamespaces:  []string{"*"},
				AllowedResources:   []string{"Pod"},
				ResourceNamespaces: nil, // Empty/nil resource namespaces
			}

			assert.False(t, filters.HasNamespaceWildcardForResource("Pod"),
				"Empty ResourceNamespaces should deny access")
			assert.Empty(t, filters.GetAllowedNamespacesForResource("Pod"),
				"Empty ResourceNamespaces should return no namespaces")
		})

		t.Run("unknown resource type", func(t *testing.T) {
			filters := &QueryFilters{
				AllowedClusters:   []string{"cluster-1"},
				AllowedNamespaces: []string{"*"},
				AllowedResources:  []string{"Pod"},
				ResourceNamespaces: map[string][]string{
					"Pod": []string{"default"},
				},
			}

			assert.False(t, filters.HasNamespaceWildcardForResource("UnknownResource"),
				"Unknown resource should be denied access")
			assert.Empty(t, filters.GetAllowedNamespacesForResource("UnknownResource"),
				"Unknown resource should have no allowed namespaces")
		})
	})
}

// TestResourceIsolation validates that resource-specific permissions work correctly
func TestResourceIsolation(t *testing.T) {
	t.Run("multi-resource permission isolation", func(t *testing.T) {
		// Complex scenario with multiple resources having different permission levels
		filters := &QueryFilters{
			AllowedClusters:   []string{"prod-cluster", "dev-cluster"},
			AllowedNamespaces: []string{"app-*", "monitoring", "*"},
			AllowedResources:  []string{"Pod", "VirtualMachine", "Service", "Secret"},
			ResourceNamespaces: map[string][]string{
				"Pod":            []string{"app-frontend", "app-backend"},        // App-specific pods only
				"VirtualMachine": []string{"app-*"},                             // Pattern-based access
				"Service":        []string{"monitoring"},                        // Monitoring services only
				"Secret":         []string{"*"},                                 // Admin-level secret access
			},
		}

		testCases := []struct {
			resourceType     string
			namespace        string
			shouldHaveAccess bool
			description      string
		}{
			// Pod access tests
			{"Pod", "app-frontend", true, "Pod should access app-frontend"},
			{"Pod", "app-backend", true, "Pod should access app-backend"},
			{"Pod", "app-database", false, "Pod should NOT access app-database (not in specific list)"},
			{"Pod", "monitoring", false, "Pod should NOT access monitoring"},

			// VirtualMachine access tests
			{"VirtualMachine", "app-frontend", true, "VirtualMachine should access app-frontend (pattern match)"},
			{"VirtualMachine", "app-database", true, "VirtualMachine should access app-database (pattern match)"},
			{"VirtualMachine", "monitoring", false, "VirtualMachine should NOT access monitoring (no pattern match)"},

			// Service access tests
			{"Service", "monitoring", true, "Service should access monitoring"},
			{"Service", "app-frontend", false, "Service should NOT access app-frontend"},

			// Secret access tests (wildcard)
			{"Secret", "app-frontend", true, "Secret should access app-frontend (wildcard)"},
			{"Secret", "monitoring", true, "Secret should access monitoring (wildcard)"},
			{"Secret", "any-namespace", true, "Secret should access any namespace (wildcard)"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				hasAccess := filters.isNamespaceAllowedForResource(tc.resourceType, tc.namespace)
				assert.Equal(t, tc.shouldHaveAccess, hasAccess, tc.description)
			})
		}
	})

	t.Run("cross-cluster resource isolation", func(t *testing.T) {
		// Test that resource-specific permissions work across different clusters
		filters := &QueryFilters{
			AllowedClusters:   []string{"prod-cluster"},  // Limited to production cluster
			AllowedNamespaces: []string{"*"},
			AllowedResources:  []string{"Pod", "Service"},
			ResourceNamespaces: map[string][]string{
				"Pod":     []string{"app-prod"},
				"Service": []string{"*"},
			},
		}

		// Verify cluster restrictions are respected regardless of namespace permissions
		assert.True(t, containsString(filters.AllowedClusters, "prod-cluster"))
		assert.False(t, containsString(filters.AllowedClusters, "dev-cluster"))

		// Verify resource-specific namespace rules still apply within allowed clusters
		assert.True(t, filters.isNamespaceAllowedForResource("Pod", "app-prod"))
		assert.False(t, filters.isNamespaceAllowedForResource("Pod", "app-dev"))
		assert.True(t, filters.isNamespaceAllowedForResource("Service", "any-namespace"))
	})
}

// TestSecurityFirstDesign validates that the system fails secure in all error conditions
func TestSecurityFirstDesign(t *testing.T) {
	t.Run("fail secure on nil conditions", func(t *testing.T) {
		var filters *QueryFilters

		// All methods should return secure defaults for nil QueryFilters
		assert.False(t, filters.IsClusterAllowed("any-cluster"))
		assert.False(t, filters.IsNamespaceAllowed("any-namespace"))
		assert.False(t, filters.IsResourceKindAllowed("Pod"))
		assert.False(t, filters.HasWildcardAccess())
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Pod"))
	})

	t.Run("fail secure on empty conditions", func(t *testing.T) {
		filters := &QueryFilters{
			AllowedClusters:    []string{},
			AllowedNamespaces:  []string{},
			AllowedResources:   []string{},
			ResourceNamespaces: map[string][]string{},
		}

		// Empty global filters mean no restrictions (allow all) - this is by design
		assert.True(t, filters.IsClusterAllowed("any-cluster"))
		assert.True(t, filters.IsNamespaceAllowed("any-namespace"))
		assert.True(t, filters.IsResourceKindAllowed("Pod"))
		assert.True(t, filters.HasWildcardAccess())

		// But resource-specific permissions should be enforced (fail secure)
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Pod"))
	})

	t.Run("partial permission scenarios", func(t *testing.T) {
		// User has cluster access but no namespace permissions for a resource
		filters := &QueryFilters{
			AllowedClusters:   []string{"cluster-1"},
			AllowedNamespaces: []string{},
			AllowedResources:  []string{"Pod"},
			ResourceNamespaces: map[string][]string{
				"Pod": []string{}, // Empty namespace list
			},
		}

		assert.True(t, filters.IsClusterAllowed("cluster-1"))
		assert.True(t, filters.IsResourceKindAllowed("Pod"))
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Pod"))

		// Should deny namespace access even with cluster and resource permissions
		assert.False(t, filters.isNamespaceAllowedForResource("Pod", "any-namespace"))
	})
}

// Helper method is now defined in types.go as part of QueryFilters

// Helper functions for testing
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsWildcard(slice []string) bool {
	for _, s := range slice {
		if s == "*" || containsPattern(s) {
			return true
		}
	}
	return false
}

func containsPattern(s string) bool {
	return len(s) > 1 && s[len(s)-1] == '*'
}

func matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}
	return value == pattern
}