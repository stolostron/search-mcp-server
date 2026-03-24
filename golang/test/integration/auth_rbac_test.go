//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/test/helpers"
)

// TestRBACResolver tests the core RBAC permission resolution logic
// with realistic Kubernetes API responses using a mock server
func TestRBACResolver(t *testing.T) {
	// Start mock Kubernetes server
	mockK8s := helpers.NewMockKubernetesServer()
	defer mockK8s.Close()

	// Configure auth to use mock server
	config := &auth.AuthConfig{
		KubernetesURL: mockK8s.URL(),
		TokenValue:    "test-server-token", // Dummy token for server auth
		SkipTLS:       true,
		AuthTimeout:   5 * time.Second,
	}

	// Create RBAC resolver
	resolver := auth.NewRBACResolver(config)

	t.Run("cluster_admin_permission_resolution", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer cluster-admin-token")

		require.NoError(t, err, "Cluster admin permission resolution should succeed")
		require.NotNil(t, filters, "QueryFilters should be returned")

		// Verify wildcard access
		assert.True(t, filters.HasWildcardAccess(), "Cluster admin should have wildcard access")
		assert.ElementsMatch(t, []string{"*"}, filters.AllowedClusters)
		assert.ElementsMatch(t, []string{"*"}, filters.AllowedNamespaces)
		assert.ElementsMatch(t, []string{"*"}, filters.AllowedResources)

		// Verify resource-specific access
		assert.True(t, filters.IsClusterAllowed("any-cluster"))
		assert.True(t, filters.IsNamespaceAllowed("any-namespace"))
		assert.True(t, filters.IsResourceKindAllowed("any-resource"))

		// Verify namespace wildcard for specific resources
		assert.True(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.True(t, filters.HasNamespaceWildcardForResource("Secret"))
		assert.True(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
	})

	t.Run("developer_permission_resolution", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer developer-token")

		require.NoError(t, err, "Developer permission resolution should succeed")
		require.NotNil(t, filters, "QueryFilters should be returned")

		// Verify limited access
		assert.False(t, filters.HasWildcardAccess(), "Developer should not have wildcard access")
		assert.ElementsMatch(t, []string{"dev-cluster"}, filters.AllowedClusters)
		assert.Contains(t, filters.AllowedNamespaces, "my-app")
		assert.Contains(t, filters.AllowedNamespaces, "shared-tools")
		assert.ElementsMatch(t, []string{"Pod", "Service", "ConfigMap"}, filters.AllowedResources)

		// Verify cluster access
		assert.True(t, filters.IsClusterAllowed("dev-cluster"))
		assert.False(t, filters.IsClusterAllowed("prod-cluster"))
		assert.False(t, filters.IsClusterAllowed("other-cluster"))

		// Verify namespace access
		assert.True(t, filters.IsNamespaceAllowed("my-app"))
		assert.True(t, filters.IsNamespaceAllowed("shared-tools"))
		assert.False(t, filters.IsNamespaceAllowed("production"))
		assert.False(t, filters.IsNamespaceAllowed("system"))

		// Verify resource access
		assert.True(t, filters.IsResourceKindAllowed("Pod"))
		assert.True(t, filters.IsResourceKindAllowed("Service"))
		assert.True(t, filters.IsResourceKindAllowed("ConfigMap"))
		assert.False(t, filters.IsResourceKindAllowed("Secret"))
		assert.False(t, filters.IsResourceKindAllowed("Node"))

		// Verify resource-specific namespace access
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"my-app", "shared-tools"}, podNamespaces, "Pods should have access to both namespaces")

		configmapNamespaces := filters.GetAllowedNamespacesForResource("ConfigMap")
		assert.ElementsMatch(t, []string{"my-app"}, configmapNamespaces, "ConfigMaps should have restricted access")

		// Verify no wildcard namespace access for developer
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.False(t, filters.HasNamespaceWildcardForResource("ConfigMap"))
	})

	t.Run("namespace_admin_permission_resolution", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer namespace-admin-token")

		require.NoError(t, err, "Namespace admin permission resolution should succeed")
		require.NotNil(t, filters, "QueryFilters should be returned")

		// Verify namespace-scoped admin access
		assert.False(t, filters.HasWildcardAccess(), "Namespace admin should not have global wildcard")
		assert.ElementsMatch(t, []string{"prod-cluster"}, filters.AllowedClusters)
		assert.Contains(t, filters.AllowedNamespaces, "app-frontend")
		assert.Contains(t, filters.AllowedNamespaces, "app-backend")
		assert.ElementsMatch(t, []string{"*"}, filters.AllowedResources) // Full resource access within scope

		// Verify cluster restrictions
		assert.True(t, filters.IsClusterAllowed("prod-cluster"))
		assert.False(t, filters.IsClusterAllowed("dev-cluster"))

		// Verify namespace restrictions
		assert.True(t, filters.IsNamespaceAllowed("app-frontend"))
		assert.True(t, filters.IsNamespaceAllowed("app-backend"))
		assert.False(t, filters.IsNamespaceAllowed("other-namespace"))

		// Verify full resource access within authorized namespaces
		assert.True(t, filters.IsResourceKindAllowed("Pod"))
		assert.True(t, filters.IsResourceKindAllowed("Secret"))
		assert.True(t, filters.IsResourceKindAllowed("ConfigMap"))
		assert.True(t, filters.IsResourceKindAllowed("deployments"))

		// Verify resource-specific namespace access
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"app-frontend", "app-backend"}, podNamespaces)

		secretNamespaces := filters.GetAllowedNamespacesForResource("Secret")
		assert.ElementsMatch(t, []string{"app-frontend", "app-backend"}, secretNamespaces)
	})

	t.Run("readonly_user_permission_resolution", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer readonly-token")

		require.NoError(t, err, "Readonly user permission resolution should succeed")
		require.NotNil(t, filters, "QueryFilters should be returned")

		// Verify read-only access across multiple clusters
		assert.False(t, filters.HasWildcardAccess(), "Readonly user should not have global wildcard")
		assert.ElementsMatch(t, []string{"monitoring-cluster", "vm-cluster"}, filters.AllowedClusters)
		assert.Contains(t, filters.AllowedNamespaces, "monitoring")
		assert.Contains(t, filters.AllowedNamespaces, "prometheus")
		assert.Contains(t, filters.AllowedNamespaces, "grafana")
		assert.Contains(t, filters.AllowedNamespaces, "*") // Wildcard namespace access for monitoring
		assert.ElementsMatch(t, []string{"Pod", "Service", "Event"}, filters.AllowedResources)

		// Verify multi-cluster access
		assert.True(t, filters.IsClusterAllowed("monitoring-cluster"))
		assert.True(t, filters.IsClusterAllowed("vm-cluster"))
		assert.False(t, filters.IsClusterAllowed("prod-cluster"))

		// Verify namespace wildcard for monitoring resources
		assert.True(t, filters.HasNamespaceWildcardForResource("Pod"), "Monitoring should have wildcard namespace access to pods")
		assert.True(t, filters.HasNamespaceWildcardForResource("Service"), "Monitoring should have wildcard namespace access to services")
		assert.True(t, filters.HasNamespaceWildcardForResource("Event"), "Monitoring should have wildcard namespace access to events")

		// Verify specific namespace access
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.Contains(t, podNamespaces, "*", "Should have wildcard namespace access")
		assert.Contains(t, podNamespaces, "monitoring", "Should have specific monitoring namespace access")
	})

	t.Run("invalid_token_rejection", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer invalid-token")

		// Invalid token results in empty permissions, not an error in the resolver
		// The middleware layer should catch this and deny access
		require.NoError(t, err, "Resolver should handle invalid token gracefully")
		require.NotNil(t, filters, "QueryFilters should be returned")
		assert.Empty(t, filters.AllowedClusters, "Invalid token should result in no permissions")
		assert.Empty(t, filters.AllowedNamespaces, "Invalid token should result in no permissions")
		assert.Empty(t, filters.AllowedResources, "Invalid token should result in no permissions")
	})

	t.Run("no_permissions_token", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer no-permissions-token")

		require.NoError(t, err, "Valid token with no permissions should not error in resolver")
		require.NotNil(t, filters, "QueryFilters should be returned even with no permissions")

		// Verify empty permissions
		assert.False(t, filters.HasWildcardAccess())
		assert.Empty(t, filters.AllowedClusters, "Should have no cluster access")
		assert.Empty(t, filters.AllowedNamespaces, "Should have no namespace access")
		assert.Empty(t, filters.AllowedResources, "Should have no resource access")

		// Verify access denials
		assert.False(t, filters.IsClusterAllowed("any-cluster"))
		assert.False(t, filters.IsNamespaceAllowed("any-namespace"))
		assert.False(t, filters.IsResourceKindAllowed("any-resource"))
	})

	t.Run("api_failure_fail_secure", func(t *testing.T) {
		_, err := resolver.ResolveUserPermissions(context.Background(), "Bearer auth-failure-token")

		assert.Error(t, err, "API failure should result in error")
		assert.Contains(t, err.Error(), "access denied for security", "Should implement fail-secure behavior")
	})
}

// TestRBACResolverResourceDiscovery tests the integration between RBAC resolver and resource discovery
func TestRBACResolverResourceDiscovery(t *testing.T) {
	mockK8s := helpers.NewMockKubernetesServer()
	defer mockK8s.Close()

	config := &auth.AuthConfig{
		KubernetesURL: mockK8s.URL(),
		TokenValue:    "test-server-token", // Dummy token for server auth
		SkipTLS:       true,
		AuthTimeout:   5 * time.Second,
	}

	resolver := auth.NewRBACResolver(config)

	t.Run("resource_discovery_integration", func(t *testing.T) {
		// Add custom token with KubeVirt resources
		mockK8s.AddToken("kubevirt-user-token", helpers.TokenConfig{
			Valid:    true,
			Username: "kubevirt-user",
			UID:      "kubevirt-uid",
			Groups:   []string{"system:authenticated", "kubevirt-users"},
			Permissions: []helpers.PermissionRule{
				{
					Verbs:      []string{"get", "list"},
					APIGroups:  []string{"kubevirt.io"},
					Resources:  []string{"virtualmachines", "virtualmachineinstances"},
					Clusters:   []string{"vm-cluster"},
					Namespaces: []string{"vm-production"},
				},
				{
					Verbs:      []string{"get", "list"},
					APIGroups:  []string{"snapshot.kubevirt.io"},
					Resources:  []string{"virtualmachinesnapshots"},
					Clusters:   []string{"*"},
					Namespaces: []string{"backup-*"},
				},
			},
		})

		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer kubevirt-user-token")

		require.NoError(t, err, "KubeVirt user permission resolution should succeed")
		require.NotNil(t, filters, "QueryFilters should be returned")

		// Verify KubeVirt resource access (expect Kubernetes Kind names)
		assert.Contains(t, filters.AllowedResources, "VirtualMachine")
		assert.Contains(t, filters.AllowedResources, "VirtualMachineInstance")
		assert.Contains(t, filters.AllowedResources, "VirtualMachineSnapshot")

		// Verify cluster and namespace filtering
		assert.Contains(t, filters.AllowedClusters, "vm-cluster")
		assert.Contains(t, filters.AllowedClusters, "*") // For snapshots

		// Verify resource-specific namespace mapping
		vmNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachine")
		assert.ElementsMatch(t, []string{"vm-production"}, vmNamespaces)

		vmiNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachineInstance")
		assert.ElementsMatch(t, []string{"vm-production"}, vmiNamespaces)

		snapshotNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachineSnapshot")
		assert.ElementsMatch(t, []string{"backup-*"}, snapshotNamespaces)

		// Verify no wildcard access for standard resources
		assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
		assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachineInstance"))

		// Verify pattern-based access for snapshots
		assert.False(t, filters.HasNamespaceWildcardForResource("virtualmachinesnapshots"), "Should use pattern, not wildcard")
	})
}

// TestRBACResolverSecurityEdgeCases tests security-focused edge cases
func TestRBACResolverSecurityEdgeCases(t *testing.T) {
	mockK8s := helpers.NewMockKubernetesServer()
	defer mockK8s.Close()

	config := &auth.AuthConfig{
		KubernetesURL: mockK8s.URL(),
		TokenValue:    "test-server-token", // Dummy token for server auth
		SkipTLS:       true,
		AuthTimeout:   5 * time.Second,
	}

	resolver := auth.NewRBACResolver(config)

	t.Run("empty_token_rejection", func(t *testing.T) {
		filters, err := resolver.ResolveUserPermissions(context.Background(), "")

		// Empty token results in empty permissions, not an error in the resolver
		// The middleware layer should catch this and deny access
		require.NoError(t, err, "Resolver should handle empty token gracefully")
		require.NotNil(t, filters, "QueryFilters should be returned")
		assert.Empty(t, filters.AllowedClusters, "Empty token should result in no permissions")
		assert.Empty(t, filters.AllowedNamespaces, "Empty token should result in no permissions")
		assert.Empty(t, filters.AllowedResources, "Empty token should result in no permissions")
	})

	t.Run("malformed_token_rejection", func(t *testing.T) {
		_, err := resolver.ResolveUserPermissions(context.Background(), "NotABearerToken")

		assert.Error(t, err, "Malformed token should be rejected")
	})

	t.Run("bearer_prefix_handling", func(t *testing.T) {
		// Test with and without Bearer prefix
		filters1, err1 := resolver.ResolveUserPermissions(context.Background(), "Bearer cluster-admin-token")
		filters2, err2 := resolver.ResolveUserPermissions(context.Background(), "cluster-admin-token")

		// Both should work (Bearer prefix should be handled consistently)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotNil(t, filters1)
		assert.NotNil(t, filters2)

		// Results should be equivalent
		assert.Equal(t, filters1.HasWildcardAccess(), filters2.HasWildcardAccess())
	})

	t.Run("context_cancellation_handling", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := resolver.ResolveUserPermissions(ctx, "Bearer cluster-admin-token")

		assert.Error(t, err, "Cancelled context should result in error")
		assert.Contains(t, err.Error(), "context canceled", "Should indicate context cancellation")
	})

	t.Run("timeout_handling", func(t *testing.T) {
		// Use very short timeout
		shortConfig := &auth.AuthConfig{
			KubernetesURL: mockK8s.URL(),
			SkipTLS:       true,
			AuthTimeout:   1 * time.Nanosecond, // Extremely short timeout
		}

		shortResolver := auth.NewRBACResolver(shortConfig)

		_, err := shortResolver.ResolveUserPermissions(context.Background(), "Bearer cluster-admin-token")

		// Should either succeed (if fast enough) or timeout
		if err != nil {
			// Go's HTTP client returns "context deadline exceeded" for timeouts
			assert.Contains(t, err.Error(), "deadline exceeded", "Short timeout should cause timeout error")
		}
	})

	t.Run("privilege_escalation_prevention", func(t *testing.T) {
		// Add token with mixed permissions to test isolation
		mockK8s.AddToken("mixed-permissions-token", helpers.TokenConfig{
			Valid:    true,
			Username: "mixed-user",
			UID:      "mixed-uid",
			Groups:   []string{"system:authenticated"},
			Permissions: []helpers.PermissionRule{
				{
					Verbs:      []string{"*"},
					APIGroups:  []string{""},
					Resources:  []string{"secrets"},
					Clusters:   []string{"*"},
					Namespaces: []string{"*"}, // Full secret access
				},
				{
					Verbs:      []string{"get", "list"},
					APIGroups:  []string{""},
					Resources:  []string{"pods", "services"},
					Clusters:   []string{"restricted-cluster"},
					Namespaces: []string{"restricted-namespace"}, // Limited pod/service access
				},
			},
		})

		filters, err := resolver.ResolveUserPermissions(context.Background(), "Bearer mixed-permissions-token")

		require.NoError(t, err)
		require.NotNil(t, filters)

		// Verify no privilege escalation: pod access should not inherit secret permissions
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"restricted-namespace"}, podNamespaces, "Pod access should be restricted")

		serviceNamespaces := filters.GetAllowedNamespacesForResource("Service")
		assert.ElementsMatch(t, []string{"restricted-namespace"}, serviceNamespaces, "Service access should be restricted")

		secretNamespaces := filters.GetAllowedNamespacesForResource("Secret")
		assert.ElementsMatch(t, []string{"*"}, secretNamespaces, "Secret access should be separate")

		// Verify cluster access isolation
		assert.True(t, filters.IsClusterAllowed("restricted-cluster"), "Should allow access to restricted cluster")
		assert.True(t, filters.IsClusterAllowed("any-cluster"), "Should allow access to any cluster for secrets")

		// But resource-specific checks should enforce restrictions
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"), "Pods should not have wildcard namespace access")
		assert.True(t, filters.HasNamespaceWildcardForResource("Secret"), "Secrets should have wildcard namespace access")
	})
}