package auth

import (
	"context"
	"testing"
)

// BenchmarkResourceSpecificPermissionFiltering tests the performance of resource-specific
// permission filtering under various load conditions
func BenchmarkResourceSpecificPermissionFiltering(t *testing.B) {

	t.Run("single_resource_permission_check", func(b *testing.B) {
		// Simple case: single resource type with specific namespaces
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{
				{
					Source:              "userpermission",
					ClusterScopedKinds:  map[string][]ResourcePermission{}, // No cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"app-1": {{Kind: "Pod"}},
						"app-2": {{Kind: "Pod"}},
						"app-3": {{Kind: "Pod"}},
					},
					ManagedClusters: map[string]struct{}{
						"cluster-1": {},
					},
				},
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate permission checks that happen during query filtering
			_ = filters.IsNamespaceAllowedInCluster("cluster-1", "app-1")
		}
	})

	t.Run("multi_resource_permission_check", func(b *testing.B) {
		// Complex case: multiple resource types with different namespace permissions
		filters := createComplexPermissionFilters()

		clusters := []string{"prod-cluster", "staging-cluster"}
		namespaces := []string{"app-1", "app-2", "vm-prod", "monitoring", "default"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate checking permissions for multiple cluster/namespace combinations
			clusterIdx := i % len(clusters)
			namespaceIdx := i % len(namespaces)
			_ = filters.IsNamespaceAllowedInCluster(clusters[clusterIdx], namespaces[namespaceIdx])
		}
	})

	t.Run("wildcard_permission_check", func(b *testing.B) {
		// Wildcard case: resources with wildcard namespace access
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{
				{
					Source:              "hub-kubernetes",
					ClusterScopedKinds:  map[string][]ResourcePermission{"cluster-1": {{Kind: "Secret"}, {Kind: "ConfigMap"}, {Kind: "Service"}}},
					NamespacedKinds: map[string][]ResourcePermission{
						"*": {{Kind: "Secret"}, {Kind: "ConfigMap"}, {Kind: "Service"}},
					},
					ManagedClusters: map[string]struct{}{
						"*": {}, // Wildcard cluster access
					},
				},
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = filters.HasWildcardAccess()
		}
	})

	t.Run("pattern_matching_permission_check", func(b *testing.B) {
		// Pattern matching case: namespace patterns like "app-*"
		filters := &QueryFilters{
			PermissionSources: []PermissionSource{
				{
					Source:              "userpermission",
					ClusterScopedKinds:  map[string][]ResourcePermission{}, // No cluster-scoped access
					NamespacedKinds: map[string][]ResourcePermission{
						"app-*": {{Kind: "Pod"}, {Kind: "VirtualMachine", APIGroup: "kubevirt.io"}},
						"vm-*":  {{Kind: "Pod"}, {Kind: "VirtualMachine", APIGroup: "kubevirt.io"}},
					},
					ManagedClusters: map[string]struct{}{
						"cluster-1": {},
					},
				},
			},
		}

		namespaces := []string{"app-frontend", "app-backend", "vm-prod", "vm-staging", "other-namespace"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			namespaceIdx := i % len(namespaces)
			_ = filters.IsNamespaceAllowedInCluster("cluster-1", namespaces[namespaceIdx])
		}
	})

	t.Run("large_permission_set", func(b *testing.B) {
		// Large scale: many resources and namespaces (enterprise scenario)
		filters := createLargeScalePermissionFilters()

		// Simulate checking many different combinations
		clusters := []string{"prod-cluster-a", "prod-cluster-b", "dev-cluster-a"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			clusterIdx := i % len(clusters)
			namespaceIdx := i % 50  // 50 different namespaces
			namespace := generateNamespace(namespaceIdx)
			_ = filters.IsNamespaceAllowedInCluster(clusters[clusterIdx], namespace)
		}
	})

	t.Run("cluster_access_performance", func(b *testing.B) {
		// Test performance of cluster access checks
		filters := createComplexPermissionFilters()
		clusters := []string{"prod-cluster", "staging-cluster", "dev-cluster", "test-cluster"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			clusterIdx := i % len(clusters)
			_ = filters.IsClusterAllowed(clusters[clusterIdx])
		}
	})

	t.Run("resource_kind_access_performance", func(b *testing.B) {
		// Test performance of resource kind access checks
		filters := createComplexPermissionFilters()
		resourceTypes := []string{"Pod", "VirtualMachine", "Service", "Secret"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resourceIdx := i % len(resourceTypes)
			_ = filters.IsResourceKindAllowed(resourceTypes[resourceIdx])
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
		discovery := GetSharedResourceDiscovery(config, nil)

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
			_ = discovery.getFromCache(testResources[resourceIdx])
		}
	})

	t.Run("full_discovery_flow_performance", func(b *testing.B) {
		discovery := GetSharedResourceDiscovery(config, nil)
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
			_, _ = discovery.GetResourceKind(ctx, "Bearer test-token", "", testResources[resourceIdx])
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
		resolver := NewRBACResolver(config, nil) // nil database for tests
		resolver.resourceDiscovery = GetSharedResourceDiscovery(config, nil)

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
			_ = resolver.mapResourceToKindWithToken(context.Background(), tc.apiGroup, tc.resource, "Bearer test-token")
		}
	})

	t.Run("permission_source_validation_performance", func(b *testing.B) {
		// Test performance of validating permission sources
		resolver := NewRBACResolver(config, nil) // nil database for tests
		resolver.resourceDiscovery = GetSharedResourceDiscovery(config, nil)

		// Setup complex query filters for realistic testing
		complexFilters := createComplexPermissionFilters()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Test various permission checks that would happen during query processing
			_ = complexFilters.HasWildcardAccess()
			_ = complexFilters.IsClusterAllowed("prod-cluster")
			_ = complexFilters.IsResourceKindAllowed("Pod")
			_ = complexFilters.IsNamespaceAllowedInCluster("prod-cluster", "app-frontend")
		}
	})
}

// Helper functions for creating test data

func createComplexPermissionFilters() *QueryFilters {
	return &QueryFilters{
		PermissionSources: []PermissionSource{
			{
				Source:              "userpermission",
				ClusterScopedKinds:  map[string][]ResourcePermission{},
				NamespacedKinds: map[string][]ResourcePermission{
					"app-frontend": {{Kind: "Pod"}, {Kind: "ConfigMap"}},
					"app-backend":  {{Kind: "Pod"}, {Kind: "ConfigMap"}},
					"app-database": {{Kind: "Pod"}, {Kind: "ConfigMap"}},
					"vm-*":         {{Kind: "VirtualMachine", APIGroup: "kubevirt.io"}},
				},
				ManagedClusters: map[string]struct{}{
					"prod-cluster":    {},
					"staging-cluster": {},
				},
			},
			{
				Source:              "hub-kubernetes",
				ClusterScopedKinds:  map[string][]ResourcePermission{"cluster-1": {{Kind: "Service"}, {Kind: "Secret"}}},
				NamespacedKinds: map[string][]ResourcePermission{
					"*": {{Kind: "Service"}, {Kind: "Secret"}},
				},
				ManagedClusters: map[string]struct{}{
					"local-cluster": {},
				},
			},
		},
	}
}

func createLargeScalePermissionFilters() *QueryFilters {
	// Simulate enterprise-scale permissions
	namespacedResources := make(map[string][]ResourcePermission)
	managedClusters := make(map[string]struct{})

	resources := []ResourcePermission{
		{Kind: "Pod"}, {Kind: "Service"}, {Kind: "Deployment", APIGroup: "apps"},
		{Kind: "ConfigMap"}, {Kind: "Secret"}, {Kind: "VirtualMachine", APIGroup: "kubevirt.io"},
		{Kind: "VirtualMachineInstance", APIGroup: "kubevirt.io"},
		{Kind: "PersistentVolumeClaim"}, {Kind: "Ingress", APIGroup: "networking.k8s.io"},
		{Kind: "NetworkPolicy", APIGroup: "networking.k8s.io"},
	}

	for i := 0; i < 10; i++ {
		cluster := generateClusterName(i)
		managedClusters[cluster] = struct{}{}

		for j := 0; j < 20; j++ {
			namespace := generateNamespace(j)
			namespacedResources[namespace] = resources
		}
	}

	return &QueryFilters{
		PermissionSources: []PermissionSource{
			{
				Source:              "userpermission",
				ClusterScopedKinds:  map[string][]ResourcePermission{}, // No cluster-scoped access for large scale test
				NamespacedKinds: namespacedResources,
				ManagedClusters:     managedClusters,
			},
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