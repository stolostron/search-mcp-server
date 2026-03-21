package auth

import (
	"context"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKubeVirtResourceAccessControl validates the complete integration of KubeVirt resources
// with the granular RBAC system, including resource discovery and permission enforcement
func TestKubeVirtResourceAccessControl(t *testing.T) {
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	t.Run("kubevirt resource discovery integration", func(t *testing.T) {
		// Test that KubeVirt resources are properly mapped through the discovery system
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		kubeVirtResources := []struct {
			resource     string
			expectedKind string
		}{
			{"virtualmachines", "VirtualMachine"},
			{"virtualmachineinstances", "VirtualMachineInstance"},
			{"virtualmachineinstancepresets", "VirtualMachineInstancePreset"},
			{"virtualmachineinstancereplicasets", "VirtualMachineInstanceReplicaSet"},
			{"virtualmachineinstancemigrations", "VirtualMachineInstanceMigration"},
			{"kubevirts", "KubeVirt"},
			{"virtualmachinesnapshots", "VirtualMachineSnapshot"},
			{"virtualmachinesnapshotcontents", "VirtualMachineSnapshotContent"},
			{"virtualmachinerestores", "VirtualMachineRestore"},
			{"virtualmachinepools", "VirtualMachinePool"},
			{"virtualmachineclones", "VirtualMachineClone"},
			{"virtualmachineexports", "VirtualMachineExport"},
			{"virtualmachineinstancetypes", "VirtualMachineInstancetype"},
			{"virtualmachineclusterinstancetypes", "VirtualMachineClusterInstancetype"},
			{"virtualmachinepreferences", "VirtualMachinePreference"},
			{"virtualmachineclusterpreferences", "VirtualMachineClusterPreference"},
			{"migrationpolicies", "MigrationPolicy"},
		}

		for _, tc := range kubeVirtResources {
			t.Run(tc.resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(context.Background(), "kubevirt.io", tc.resource)

				assert.Equal(t, tc.expectedKind, kind,
					"Resource %s should map to Kind %s", tc.resource, tc.expectedKind)
				assert.NotNil(t, result)
				// Should use hardcoded fallback since no live cluster is available for tests
				assert.Contains(t, []string{"hardcoded", "cache"}, result.Source,
					"Should use hardcoded or cached mapping for %s", tc.resource)
			})
		}
	})

	t.Run("kubevirt rbac resolver integration", func(t *testing.T) {
		// Test the integration between KubeVirt resource discovery and RBAC resolution
		resolver := NewRBACResolver(config)
		userToken := "Bearer kubevirt-admin-token"

		// Initialize discovery with KubeVirt resources cached
		resolver.resourceDiscovery = NewResourceDiscovery(config, userToken)

		// Cache KubeVirt resources to simulate successful discovery
		kubeVirtMappings := map[string]string{
			"virtualmachines":           "VirtualMachine",
			"virtualmachineinstances":   "VirtualMachineInstance",
			"virtualmachinesnapshots":   "VirtualMachineSnapshot",
			"migrationpolicies":         "MigrationPolicy",
		}
		resolver.resourceDiscovery.updateCache(kubeVirtMappings)

		// Test resource-to-kind mapping through RBAC resolver
		testCases := []struct {
			apiGroup string
			resource string
			expected string
		}{
			{"kubevirt.io", "virtualmachines", "VirtualMachine"},
			{"kubevirt.io", "virtualmachineinstances", "VirtualMachineInstance"},
			{"snapshot.kubevirt.io", "virtualmachinesnapshots", "VirtualMachineSnapshot"},
			{"migrations.kubevirt.io", "migrationpolicies", "MigrationPolicy"},
		}

		for _, tc := range testCases {
			t.Run(tc.resource, func(t *testing.T) {
				kind := resolver.mapResourceToKind(tc.apiGroup, tc.resource)
				assert.Equal(t, tc.expected, kind,
					"RBAC resolver should correctly map %s/%s to %s", tc.apiGroup, tc.resource, tc.expected)
			})
		}
	})

	t.Run("kubevirt namespace isolation scenarios", func(t *testing.T) {
		// Test complex KubeVirt permission scenarios with namespace isolation

		t.Run("vm developer permissions", func(t *testing.T) {
			// User with VM development permissions in specific namespaces
			filters := &QueryFilters{
				AllowedClusters:   []string{"dev-cluster"},
				AllowedNamespaces: []string{"vm-dev", "vm-staging"},
				AllowedResources:  []string{"VirtualMachine", "VirtualMachineInstance"},
				ResourceNamespaces: map[string][]string{
					"VirtualMachine":         []string{"vm-dev", "vm-staging"},
					"VirtualMachineInstance": []string{"vm-dev"},              // More restrictive
				},
			}

			// VirtualMachine access tests
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "vm-dev"))
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "vm-staging"))
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachine", "vm-prod"))

			// VirtualMachineInstance access tests (more restrictive)
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "vm-dev"))
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "vm-staging"))
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "vm-prod"))

			// Verify no wildcard access
			assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
			assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachineInstance"))
		})

		t.Run("vm admin permissions", func(t *testing.T) {
			// User with admin-level VM permissions
			filters := &QueryFilters{
				AllowedClusters:   []string{"*"},
				AllowedNamespaces: []string{"*"},
				AllowedResources:  []string{"VirtualMachine", "VirtualMachineInstance", "VirtualMachineSnapshot", "MigrationPolicy"},
				ResourceNamespaces: map[string][]string{
					"VirtualMachine":         []string{"*"},
					"VirtualMachineInstance": []string{"*"},
					"VirtualMachineSnapshot": []string{"vm-*"},              // Pattern-based access
					"MigrationPolicy":        []string{"default", "system"}, // System namespaces only
				},
			}

			// VM and VMI should have full access
			assert.True(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
			assert.True(t, filters.HasNamespaceWildcardForResource("VirtualMachineInstance"))
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "any-namespace"))
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "any-namespace"))

			// Snapshot should have pattern-based access
			assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachineSnapshot"))
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "vm-prod"))
			assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "vm-dev"))
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "other-namespace"))

			// MigrationPolicy should be limited to system namespaces
			assert.False(t, filters.HasNamespaceWildcardForResource("MigrationPolicy"))
			assert.True(t, filters.isNamespaceAllowedForResource("MigrationPolicy", "default"))
			assert.True(t, filters.isNamespaceAllowedForResource("MigrationPolicy", "system"))
			assert.False(t, filters.isNamespaceAllowedForResource("MigrationPolicy", "vm-dev"))
		})

		t.Run("kubevirt readonly permissions", func(t *testing.T) {
			// User with read-only KubeVirt permissions across multiple namespaces
			filters := &QueryFilters{
				AllowedClusters:   []string{"prod-cluster", "staging-cluster"},
				AllowedNamespaces: []string{"vm-*", "kubevirt-system"},
				AllowedResources:  []string{"VirtualMachine", "VirtualMachineInstance", "KubeVirt"},
				ResourceNamespaces: map[string][]string{
					"VirtualMachine":         []string{"vm-prod", "vm-staging", "vm-demo"},
					"VirtualMachineInstance": []string{"vm-prod", "vm-staging"},
					"KubeVirt":              []string{"kubevirt-system"}, // System resource only
				},
			}

			// Verify specific namespace access for VMs
			vmNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachine")
			assert.ElementsMatch(t, []string{"vm-prod", "vm-staging", "vm-demo"}, vmNamespaces)

			// Verify more restrictive access for VMIs
			vmiNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachineInstance")
			assert.ElementsMatch(t, []string{"vm-prod", "vm-staging"}, vmiNamespaces)

			// Verify system-only access for KubeVirt CRD
			kubeVirtNamespaces := filters.GetAllowedNamespacesForResource("KubeVirt")
			assert.ElementsMatch(t, []string{"kubevirt-system"}, kubeVirtNamespaces)

			// Verify no unauthorized access
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachine", "unauthorized-ns"))
			assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "vm-demo"))
			assert.False(t, filters.isNamespaceAllowedForResource("KubeVirt", "vm-prod"))
		})
	})

	t.Run("cross-resource kubevirt privilege boundaries", func(t *testing.T) {
		// Test that permissions for one KubeVirt resource don't grant access to another
		filters := &QueryFilters{
			AllowedClusters:   []string{"kubevirt-cluster"},
			AllowedNamespaces: []string{"*"},
			AllowedResources:  []string{"VirtualMachine", "VirtualMachineSnapshot"},
			ResourceNamespaces: map[string][]string{
				"VirtualMachine":         []string{"*"},                    // Full VM access
				"VirtualMachineSnapshot": []string{"backup-namespace"},    // Limited snapshot access
			},
		}

		// VM access should be unrestricted
		assert.True(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
		assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "any-namespace"))

		// Snapshot access should be restricted
		assert.False(t, filters.HasNamespaceWildcardForResource("VirtualMachineSnapshot"))
		assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "backup-namespace"))
		assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "other-namespace"))

		// Unauthorized resource should have no access
		assert.Empty(t, filters.GetAllowedNamespacesForResource("VirtualMachineInstance"))
		assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineInstance", "any-namespace"))
	})

	t.Run("kubevirt resource types completeness", func(t *testing.T) {
		// Ensure all known KubeVirt resources are properly supported
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Core KubeVirt resources that should always be supported
		coreResources := []string{
			"virtualmachines",
			"virtualmachineinstances",
			"kubevirts",
		}

		// Extended KubeVirt resources
		extendedResources := []string{
			"virtualmachinesnapshots",
			"virtualmachinerestores",
			"virtualmachinepools",
			"migrationpolicies",
		}

		// Advanced KubeVirt resources
		advancedResources := []string{
			"virtualmachineinstancetypes",
			"virtualmachineclusterinstancetypes",
			"virtualmachinepreferences",
			"virtualmachineclusterpreferences",
			"virtualmachineexports",
		}

		allResources := append(coreResources, extendedResources...)
		allResources = append(allResources, advancedResources...)

		for _, resource := range allResources {
			t.Run(resource, func(t *testing.T) {
				kind, result := discovery.GetResourceKind(context.Background(), "kubevirt.io", resource)

				// All resources should get a valid Kind mapping
				assert.NotEmpty(t, kind, "Resource %s should have a valid Kind mapping", resource)
				assert.NotNil(t, result)

				// Kind should start with uppercase and be properly formatted
				assert.True(t, len(kind) > 0 && kind[0] >= 'A' && kind[0] <= 'Z',
					"Kind %s should start with uppercase letter", kind)

				// Verify the resource can be used in permission checks
				filters := &QueryFilters{
					AllowedClusters:   []string{"test-cluster"},
					AllowedNamespaces: []string{"test-namespace"},
					AllowedResources:  []string{kind},
					ResourceNamespaces: map[string][]string{
						kind: []string{"test-namespace"},
					},
				}

				assert.True(t, filters.isNamespaceAllowedForResource(kind, "test-namespace"),
					"Should be able to check permissions for %s", kind)
			})
		}
	})
}

// TestKubeVirtPermissionConversion tests the conversion of raw Kubernetes permissions
// to QueryFilters specifically for KubeVirt resources
func TestKubeVirtPermissionConversion(t *testing.T) {
	// Simulate raw permissions that might come from the UserPermission API for KubeVirt
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	resolver := NewRBACResolver(config)
	resolver.resourceDiscovery = NewResourceDiscovery(config, "Bearer test-token")

	t.Run("convert kubevirt permissions with discovery", func(t *testing.T) {
		// Mock raw permissions for KubeVirt resources
		mockPermissions := []PermissionRule{
			{
				ResourceRule: authorizationv1.ResourceRule{
					Verbs:     []string{"get", "list"},
					APIGroups: []string{"kubevirt.io"},
					Resources: []string{"virtualmachines", "virtualmachineinstances"},
				},
				Clusters:   []string{"prod-cluster"},
				Namespaces: []string{"vm-prod"},
			},
			{
				ResourceRule: authorizationv1.ResourceRule{
					Verbs:     []string{"*"},
					APIGroups: []string{"snapshot.kubevirt.io"},
					Resources: []string{"virtualmachinesnapshots"},
				},
				Clusters:   []string{"*"},
				Namespaces: []string{"backup-*"},
			},
		}

		// Set up discovery cache with KubeVirt mappings
		kubeVirtMappings := map[string]string{
			"virtualmachines":         "VirtualMachine",
			"virtualmachineinstances": "VirtualMachineInstance",
			"virtualmachinesnapshots": "VirtualMachineSnapshot",
		}
		resolver.resourceDiscovery.updateCache(kubeVirtMappings)

		// Convert permissions to QueryFilters
		filters := resolver.convertPermissionsToFilters(mockPermissions)

		require.NotNil(t, filters)

		// Verify cluster permissions
		assert.ElementsMatch(t, []string{"prod-cluster", "*"}, filters.AllowedClusters)

		// Verify resource permissions
		expectedResources := []string{"VirtualMachine", "VirtualMachineInstance", "VirtualMachineSnapshot"}
		assert.ElementsMatch(t, expectedResources, filters.AllowedResources)

		// Verify resource-specific namespace permissions
		require.NotNil(t, filters.ResourceNamespaces)

		// VirtualMachine should have specific namespace access
		vmNamespaces := filters.ResourceNamespaces["VirtualMachine"]
		assert.ElementsMatch(t, []string{"vm-prod"}, vmNamespaces)

		// VirtualMachineInstance should have same access as VirtualMachine
		vmiNamespaces := filters.ResourceNamespaces["VirtualMachineInstance"]
		assert.ElementsMatch(t, []string{"vm-prod"}, vmiNamespaces)

		// VirtualMachineSnapshot should have pattern-based access
		snapshotNamespaces := filters.ResourceNamespaces["VirtualMachineSnapshot"]
		assert.ElementsMatch(t, []string{"backup-*"}, snapshotNamespaces)

		// Test actual permission checks
		assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachine", "vm-prod"))
		assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachine", "vm-dev"))

		assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "backup-prod"))
		assert.True(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "backup-dev"))
		assert.False(t, filters.isNamespaceAllowedForResource("VirtualMachineSnapshot", "other-namespace"))
	})

	t.Run("handle unknown kubevirt resources gracefully", func(t *testing.T) {
		// Test with a new KubeVirt resource that's not in the hardcoded map
		mockPermissions := []PermissionRule{
			{
				ResourceRule: authorizationv1.ResourceRule{
					Verbs:     []string{"get", "list"},
					APIGroups: []string{"kubevirt.io"},
					Resources: []string{"newkubevirtvmtypes"}, // New resource type
				},
				Clusters:   []string{"test-cluster"},
				Namespaces: []string{"test-namespace"},
			},
		}

		// Don't set up discovery cache - force algorithmic fallback
		filters := resolver.convertPermissionsToFilters(mockPermissions)

		require.NotNil(t, filters)

		// Should use algorithmic mapping: newkubevirtvmtypes -> Newkubevirtvmtype
		assert.Contains(t, filters.AllowedResources, "Newkubevirtvmtype")

		// Should still have proper namespace permissions
		require.NotNil(t, filters.ResourceNamespaces)
		namespaces := filters.ResourceNamespaces["Newkubevirtvmtype"]
		assert.ElementsMatch(t, []string{"test-namespace"}, namespaces)

		// Should work in permission checks
		assert.True(t, filters.isNamespaceAllowedForResource("Newkubevirtvmtype", "test-namespace"))
		assert.False(t, filters.isNamespaceAllowedForResource("Newkubevirtvmtype", "other-namespace"))
	})
}