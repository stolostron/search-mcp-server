package auth

import (
	"context"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
)

// BenchmarkResourceSpecificPermissionFiltering tests the performance of resource-specific
// permission filtering under various load conditions
func BenchmarkResourceSpecificPermissionFiltering(t *testing.B) {

	t.Run("single_resource_permission_check", func(b *testing.B) {
		// Simple case: single resource type with specific namespaces
		filters := &QueryFilters{
			AllowedClusters:   []string{"cluster-1"},
			AllowedNamespaces: []string{"app-1", "app-2", "app-3"},
			AllowedResources:  []string{"Pod"},
			ResourceNamespaces: map[string][]string{
				"Pod": []string{"app-1", "app-2", "app-3"},
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate permission checks that happen during query filtering
			_ = filters.isNamespaceAllowedForResource("Pod", "app-1")
		}
	})

	t.Run("multi_resource_permission_check", func(b *testing.B) {
		// Complex case: multiple resource types with different namespace permissions
		filters := createComplexPermissionFilters()

		resourceTypes := []string{"Pod", "VirtualMachine", "Service", "Secret", "ConfigMap"}
		namespaces := []string{"app-1", "app-2", "vm-prod", "monitoring", "default"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate checking permissions for multiple resource/namespace combinations
			resourceIdx := i % len(resourceTypes)
			namespaceIdx := i % len(namespaces)
			_ = filters.isNamespaceAllowedForResource(resourceTypes[resourceIdx], namespaces[namespaceIdx])
		}
	})

	t.Run("wildcard_permission_check", func(b *testing.B) {
		// Wildcard case: resources with wildcard namespace access
		filters := &QueryFilters{
			AllowedClusters:   []string{"*"},
			AllowedNamespaces: []string{"*"},
			AllowedResources:  []string{"Secret", "ConfigMap", "Service"},
			ResourceNamespaces: map[string][]string{
				"Secret":    []string{"*"},
				"ConfigMap": []string{"*"},
				"Service":   []string{"*"},
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = filters.HasNamespaceWildcardForResource("Secret")
		}
	})

	t.Run("pattern_matching_permission_check", func(b *testing.B) {
		// Pattern matching case: namespace patterns like "app-*"
		filters := &QueryFilters{
			AllowedClusters:   []string{"cluster-1"},
			AllowedNamespaces: []string{"app-*", "vm-*"},
			AllowedResources:  []string{"Pod", "VirtualMachine"},
			ResourceNamespaces: map[string][]string{
				"Pod":            []string{"app-*"},
				"VirtualMachine": []string{"vm-*"},
			},
		}

		namespaces := []string{"app-frontend", "app-backend", "vm-prod", "vm-staging", "other-namespace"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			namespaceIdx := i % len(namespaces)
			_ = filters.isNamespaceAllowedForResource("Pod", namespaces[namespaceIdx])
		}
	})

	t.Run("large_permission_set", func(b *testing.B) {
		// Large scale: many resources and namespaces (enterprise scenario)
		filters := createLargeScalePermissionFilters()

		// Simulate checking many different combinations
		resourceTypes := []string{"Pod", "Service", "Deployment", "ConfigMap", "Secret",
			"VirtualMachine", "VirtualMachineInstance", "PersistentVolumeClaim"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(resourceTypes)
			namespaceIdx := i % 50  // 50 different namespaces
			namespace := generateNamespace(namespaceIdx)
			_ = filters.isNamespaceAllowedForResource(resourceTypes[resourceIdx], namespace)
		}
	})

	t.Run("get_allowed_namespaces_performance", func(b *testing.B) {
		// Test performance of getting allowed namespaces for resources
		filters := createComplexPermissionFilters()
		resourceTypes := []string{"Pod", "VirtualMachine", "Service", "Secret"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(resourceTypes)
			_ = filters.GetAllowedNamespacesForResource(resourceTypes[resourceIdx])
		}
	})

	t.Run("has_wildcard_access_performance", func(b *testing.B) {
		// Test performance of wildcard access checks
		filters := createComplexPermissionFilters()
		resourceTypes := []string{"Pod", "VirtualMachine", "Service", "Secret"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(resourceTypes)
			_ = filters.HasNamespaceWildcardForResource(resourceTypes[resourceIdx])
		}
	})
}

// BenchmarkDiscoverySystemPerformance tests the performance of the resource discovery system
func BenchmarkDiscoverySystemPerformance(t *testing.B) {
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	t.Run("discovery_cache_performance", func(b *testing.B) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")

		// Populate cache with many resources
		largeMappings := make(map[string]string)
		for i := 0; i < 1000; i++ {
			resource := generateResourceName(i)
			kind := generateKindName(i)
			largeMappings[resource] = kind
		}
		discovery.updateCache(largeMappings)

		testResources := []string{"pods", "services", "deployments", "virtualmachines", "test500"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(testResources)
			_, _ = discovery.getCachedMapping(testResources[resourceIdx])
		}
	})

	t.Run("hardcoded_mapping_performance", func(b *testing.B) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")
		testResources := []string{"pods", "services", "virtualmachines", "deployments", "secrets"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(testResources)
			_ = discovery.getHardcodedMapping(testResources[resourceIdx])
		}
	})

	t.Run("algorithmic_mapping_performance", func(b *testing.B) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")
		testResources := []string{"unknownresources", "customoperators", "newcrds", "policies", "categories"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(testResources)
			_ = discovery.algorithmicMapping(testResources[resourceIdx])
		}
	})

	t.Run("full_discovery_flow_performance", func(b *testing.B) {
		discovery := NewResourceDiscovery(config, "Bearer test-token")
		ctx := context.Background()

		// Mix of cached, hardcoded, and unknown resources
		testResources := []string{"pods", "virtualmachines", "unknownresource1", "services", "unknownresource2"}

		// Pre-populate cache with some resources
		cacheMappings := map[string]string{
			"pods":             "Pod",
			"virtualmachines":  "VirtualMachine",
		}
		discovery.updateCache(cacheMappings)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(testResources)
			_, _ = discovery.GetResourceKind(ctx, "", testResources[resourceIdx])
		}
	})
}

// BenchmarkRBACResolverPerformance tests the performance of the complete RBAC resolution system
func BenchmarkRBACResolverPerformance(t *testing.B) {
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	t.Run("resource_to_kind_mapping_performance", func(b *testing.B) {
		resolver := NewRBACResolver(config)
		resolver.resourceDiscovery = NewResourceDiscovery(config, "Bearer test-token")

		// Pre-populate discovery cache
		mappings := map[string]string{
			"pods":              "Pod",
			"services":          "Service",
			"deployments":       "Deployment",
			"virtualmachines":   "VirtualMachine",
			"secrets":           "Secret",
		}
		resolver.resourceDiscovery.updateCache(mappings)

		testCases := []struct {
			apiGroup string
			resource string
		}{
			{"", "pods"},
			{"apps", "deployments"},
			{"kubevirt.io", "virtualmachines"},
			{"", "services"},
			{"custom.io", "unknownresource"},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tc := testCases[i%len(testCases)]
			_ = resolver.mapResourceToKind(tc.apiGroup, tc.resource)
		}
	})

	t.Run("permission_conversion_performance", func(b *testing.B) {
		resolver := NewRBACResolver(config)
		resolver.resourceDiscovery = NewResourceDiscovery(config, "Bearer test-token")

		// Setup complex permissions for realistic testing
		complexPermissions := createComplexPermissionRules()

		// Pre-populate discovery cache
		mappings := map[string]string{
			"pods":              "Pod",
			"services":          "Service",
			"virtualmachines":   "VirtualMachine",
			"deployments":       "Deployment",
		}
		resolver.resourceDiscovery.updateCache(mappings)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = resolver.convertPermissionsToFilters(complexPermissions)
		}
	})
}

// Helper functions for creating test data

func createComplexPermissionFilters() *QueryFilters {
	return &QueryFilters{
		AllowedClusters:   []string{"prod-cluster", "staging-cluster"},
		AllowedNamespaces: []string{"app-*", "vm-*", "monitoring", "default"},
		AllowedResources:  []string{"Pod", "VirtualMachine", "Service", "Secret", "ConfigMap"},
		ResourceNamespaces: map[string][]string{
			"Pod":            []string{"app-frontend", "app-backend", "app-database"},
			"VirtualMachine": []string{"vm-*"},  // Pattern-based access
			"Service":        []string{"*"},     // Wildcard access
			"Secret":         []string{"monitoring", "default"},
			"ConfigMap":      []string{"app-*"},
		},
	}
}

func createLargeScalePermissionFilters() *QueryFilters {
	// Simulate enterprise-scale permissions
	clusters := make([]string, 10)
	for i := 0; i < 10; i++ {
		clusters[i] = generateClusterName(i)
	}

	resources := []string{"Pod", "Service", "Deployment", "ConfigMap", "Secret",
		"VirtualMachine", "VirtualMachineInstance", "PersistentVolumeClaim",
		"Ingress", "NetworkPolicy"}

	resourceNamespaces := make(map[string][]string)
	for _, resource := range resources {
		namespaces := make([]string, 20)
		for i := 0; i < 20; i++ {
			namespaces[i] = generateNamespace(i)
		}
		resourceNamespaces[resource] = namespaces
	}

	return &QueryFilters{
		AllowedClusters:    clusters,
		AllowedNamespaces:  []string{"*"}, // Global namespace access
		AllowedResources:   resources,
		ResourceNamespaces: resourceNamespaces,
	}
}

func createComplexPermissionRules() []PermissionRule {
	return []PermissionRule{
		{
			ResourceRule: authorizationv1.ResourceRule{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{""},
				Resources: []string{"pods", "services"},
			},
			Clusters:   []string{"prod-cluster"},
			Namespaces: []string{"app-frontend", "app-backend"},
		},
		{
			ResourceRule: authorizationv1.ResourceRule{
				Verbs:     []string{"*"},
				APIGroups: []string{"kubevirt.io"},
				Resources: []string{"virtualmachines", "virtualmachineinstances"},
			},
			Clusters:   []string{"*"},
			Namespaces: []string{"vm-*"},
		},
		{
			ResourceRule: authorizationv1.ResourceRule{
				Verbs:     []string{"get", "list", "create", "update", "delete"},
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "replicasets"},
			},
			Clusters:   []string{"staging-cluster"},
			Namespaces: []string{"*"},
		},
	}
}

func generateClusterName(index int) string {
	if index < 5 {
		return "prod-cluster-" + string(rune('a' + index))
	}
	return "dev-cluster-" + string(rune('a' + index - 5))
}

func generateNamespace(index int) string {
	switch index % 4 {
	case 0:
		return "app-" + string(rune('a' + index/4))
	case 1:
		return "vm-" + string(rune('a' + index/4))
	case 2:
		return "monitoring-" + string(rune('a' + index/4))
	default:
		return "default-" + string(rune('a' + index/4))
	}
}

func generateResourceName(index int) string {
	return "resource" + string(rune('0' + index%10)) + string(rune('0' + (index/10)%10))
}

func generateKindName(index int) string {
	return "Kind" + string(rune('0' + index%10)) + string(rune('0' + (index/10)%10))
}