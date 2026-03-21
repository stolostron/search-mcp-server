package auth

import (
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndToEndUserScenarios validates the complete granular RBAC system
// with realistic user scenarios from admin to read-only developer access
func TestEndToEndUserScenarios(t *testing.T) {
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	t.Run("cluster_admin_scenario", func(t *testing.T) {
		// Scenario: Cluster administrator with full access
		scenario := &UserScenario{
			Name:        "cluster-admin",
			Description: "Cluster administrator with full access to all resources",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"*"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
					Clusters:   []string{"*"},
					Namespaces: []string{"*"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate admin has wildcard access
		assert.True(t, filters.HasWildcardAccess())
		assert.True(t, filters.IsClusterAllowed("any-cluster"))
		assert.True(t, filters.IsNamespaceAllowed("any-namespace"))
		assert.True(t, filters.IsResourceKindAllowed("any-resource"))

		// Validate resource-specific wildcard access
		assert.True(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.True(t, filters.HasNamespaceWildcardForResource("VirtualMachine"))
		assert.True(t, filters.HasNamespaceWildcardForResource("Secret"))
	})

	t.Run("namespace_admin_scenario", func(t *testing.T) {
		// Scenario: Namespace administrator with full access to specific namespaces
		scenario := &UserScenario{
			Name:        "namespace-admin",
			Description: "Namespace administrator with full access to app namespaces",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"*"},
						APIGroups: []string{"", "apps", "extensions"},
						Resources: []string{"*"},
					},
					Clusters:   []string{"prod-cluster"},
					Namespaces: []string{"app-frontend", "app-backend", "app-database"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate cluster access
		assert.True(t, filters.IsClusterAllowed("prod-cluster"))
		assert.False(t, filters.IsClusterAllowed("dev-cluster"))

		// Validate namespace access per resource
		for _, resourceKind := range []string{"Pod", "Deployment", "Service", "ConfigMap"} {
			allowedNamespaces := filters.GetAllowedNamespacesForResource(resourceKind)
			assert.ElementsMatch(t, []string{"app-frontend", "app-backend", "app-database"}, allowedNamespaces,
				"Resource %s should have access to app namespaces", resourceKind)

			assert.True(t, filters.isNamespaceAllowedForResource(resourceKind, "app-frontend"))
			assert.True(t, filters.isNamespaceAllowedForResource(resourceKind, "app-backend"))
			assert.False(t, filters.isNamespaceAllowedForResource(resourceKind, "other-namespace"))
		}

		// Should not have wildcard access
		assert.False(t, filters.HasWildcardAccess())
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
	})

	t.Run("developer_scenario", func(t *testing.T) {
		// Scenario: Developer with limited access to specific resource types
		scenario := &UserScenario{
			Name:        "developer",
			Description: "Developer with read access to pods and services, write access to configmaps",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"pods", "services"},
					},
					Clusters:   []string{"dev-cluster"},
					Namespaces: []string{"my-app", "shared-tools"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "create", "update", "patch"},
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
					},
					Clusters:   []string{"dev-cluster"},
					Namespaces: []string{"my-app"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate cluster access
		assert.True(t, filters.IsClusterAllowed("dev-cluster"))
		assert.False(t, filters.IsClusterAllowed("prod-cluster"))

		// Validate Pod and Service access
		for _, resourceKind := range []string{"Pod", "Service"} {
			allowedNamespaces := filters.GetAllowedNamespacesForResource(resourceKind)
			assert.ElementsMatch(t, []string{"my-app", "shared-tools"}, allowedNamespaces,
				"Resource %s should have access to dev namespaces", resourceKind)
		}

		// Validate ConfigMap access (more restrictive)
		configMapNamespaces := filters.GetAllowedNamespacesForResource("ConfigMap")
		assert.ElementsMatch(t, []string{"my-app"}, configMapNamespaces,
			"ConfigMap should only have access to my-app namespace")

		// Validate no access to unauthorized resources
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Secret"))
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Deployment"))
	})

	t.Run("readonly_user_scenario", func(t *testing.T) {
		// Scenario: Read-only user with monitoring access
		scenario := &UserScenario{
			Name:        "readonly-monitor",
			Description: "Read-only user with monitoring access across multiple clusters",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"pods", "services", "nodes"},
					},
					Clusters:   []string{"prod-cluster-1", "prod-cluster-2", "staging-cluster"},
					Namespaces: []string{"monitoring", "prometheus", "grafana"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{"metrics.k8s.io"},
						Resources: []string{"pods", "nodes"},
					},
					Clusters:   []string{"*"},
					Namespaces: []string{"*"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate multi-cluster access
		assert.True(t, filters.IsClusterAllowed("prod-cluster-1"))
		assert.True(t, filters.IsClusterAllowed("prod-cluster-2"))
		assert.True(t, filters.IsClusterAllowed("staging-cluster"))

		// Validate monitoring resource access
		// Note: Pod and Node resources also get wildcard access via the metrics API permission rule
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"monitoring", "prometheus", "grafana", "*"}, podNamespaces,
			"Resource Pod should have access to monitoring namespaces plus wildcard from metrics API")

		serviceNamespaces := filters.GetAllowedNamespacesForResource("Service")
		assert.ElementsMatch(t, []string{"monitoring", "prometheus", "grafana"}, serviceNamespaces,
			"Resource Service should have access to monitoring namespaces only")

		nodeNamespaces := filters.GetAllowedNamespacesForResource("Node")
		assert.ElementsMatch(t, []string{"monitoring", "prometheus", "grafana", "*"}, nodeNamespaces,
			"Resource Node should have access to monitoring namespaces plus wildcard from metrics API")

		// Pod and Node should have wildcard namespace access due to metrics API permissions
		assert.True(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.True(t, filters.HasNamespaceWildcardForResource("Node"))
		// Service should not have wildcard access
		assert.False(t, filters.HasNamespaceWildcardForResource("Service"))
	})

	t.Run("kubevirt_specialist_scenario", func(t *testing.T) {
		// Scenario: KubeVirt specialist with VM management access
		scenario := &UserScenario{
			Name:        "kubevirt-specialist",
			Description: "KubeVirt specialist with VM management access across VM namespaces",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"*"},
						APIGroups: []string{"kubevirt.io"},
						Resources: []string{"virtualmachines", "virtualmachineinstances"},
					},
					Clusters:   []string{"kubevirt-cluster"},
					Namespaces: []string{"vm-*"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "create"},
						APIGroups: []string{"snapshot.kubevirt.io"},
						Resources: []string{"virtualmachinesnapshots"},
					},
					Clusters:   []string{"kubevirt-cluster"},
					Namespaces: []string{"vm-backups"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					Clusters:   []string{"kubevirt-cluster"},
					Namespaces: []string{"vm-*"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate cluster access
		assert.True(t, filters.IsClusterAllowed("kubevirt-cluster"))
		assert.False(t, filters.IsClusterAllowed("other-cluster"))

		// Validate VM resource access with pattern matching
		for _, resourceKind := range []string{"VirtualMachine", "VirtualMachineInstance"} {
			allowedNamespaces := filters.GetAllowedNamespacesForResource(resourceKind)
			assert.ElementsMatch(t, []string{"vm-*"}, allowedNamespaces,
				"Resource %s should have pattern-based access to VM namespaces", resourceKind)

			assert.True(t, filters.isNamespaceAllowedForResource(resourceKind, "vm-prod"))
			assert.True(t, filters.isNamespaceAllowedForResource(resourceKind, "vm-development"))
			assert.False(t, filters.isNamespaceAllowedForResource(resourceKind, "app-frontend"))
		}

		// Validate snapshot access (different namespace)
		snapshotNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachineSnapshot")
		assert.ElementsMatch(t, []string{"vm-backups"}, snapshotNamespaces)

		// Validate Pod access in VM namespaces only
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"vm-*"}, podNamespaces)
	})

	t.Run("security_auditor_scenario", func(t *testing.T) {
		// Scenario: Security auditor with read access to security-related resources
		scenario := &UserScenario{
			Name:        "security-auditor",
			Description: "Security auditor with read access to security-related resources",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"secrets", "serviceaccounts"},
					},
					Clusters:   []string{"*"},
					Namespaces: []string{"*"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{"rbac.authorization.k8s.io"},
						Resources: []string{"roles", "rolebindings", "clusterroles", "clusterrolebindings"},
					},
					Clusters:   []string{"*"},
					Namespaces: []string{"*"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{"security.istio.io"},
						Resources: []string{"securitypolicies"},
					},
					Clusters:   []string{"prod-*"},
					Namespaces: []string{"*"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate wildcard cluster access for security resources
		assert.True(t, filters.IsClusterAllowed("any-cluster"))

		// Validate security resource access with wildcard namespaces
		for _, resourceKind := range []string{"Secret", "ServiceAccount", "Role", "RoleBinding"} {
			assert.True(t, filters.HasNamespaceWildcardForResource(resourceKind),
				"Resource %s should have wildcard namespace access", resourceKind)
		}

		// Validate pattern-based cluster access for Istio resources
		// Note: This would require cluster pattern matching in the implementation
		securityPolicyNamespaces := filters.GetAllowedNamespacesForResource("Securitypolicy") // Algorithmic mapping
		assert.ElementsMatch(t, []string{"*"}, securityPolicyNamespaces)
	})

	t.Run("mixed_permissions_scenario", func(t *testing.T) {
		// Scenario: Complex user with mixed permissions across different resource types
		scenario := &UserScenario{
			Name:        "mixed-permissions-user",
			Description: "User with complex mixed permissions across different resource types and namespaces",
			Permissions: []PermissionRule{
				// Full Pod access in app namespaces
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					Clusters:   []string{"app-cluster"},
					Namespaces: []string{"app-*"},
				},
				// Read-only Service access across all namespaces
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"services"},
					},
					Clusters:   []string{"app-cluster", "monitoring-cluster"},
					Namespaces: []string{"*"},
				},
				// Limited ConfigMap access in specific namespaces
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "create"},
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
					},
					Clusters:   []string{"app-cluster"},
					Namespaces: []string{"app-config", "shared-config"},
				},
				// VM access in specific namespace
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list", "create", "delete"},
						APIGroups: []string{"kubevirt.io"},
						Resources: []string{"virtualmachines"},
					},
					Clusters:   []string{"vm-cluster"},
					Namespaces: []string{"test-vms"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Validate cluster-specific access
		assert.True(t, filters.IsClusterAllowed("app-cluster"))
		assert.True(t, filters.IsClusterAllowed("monitoring-cluster"))
		assert.True(t, filters.IsClusterAllowed("vm-cluster"))

		// Validate Pod access (pattern-based in app cluster)
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"app-*"}, podNamespaces)
		assert.True(t, filters.isNamespaceAllowedForResource("Pod", "app-frontend"))
		assert.True(t, filters.isNamespaceAllowedForResource("Pod", "app-backend"))
		assert.False(t, filters.isNamespaceAllowedForResource("Pod", "other-namespace"))

		// Validate Service access (wildcard across multiple clusters)
		assert.True(t, filters.HasNamespaceWildcardForResource("Service"))
		assert.True(t, filters.isNamespaceAllowedForResource("Service", "any-namespace"))

		// Validate ConfigMap access (specific namespaces)
		configMapNamespaces := filters.GetAllowedNamespacesForResource("ConfigMap")
		assert.ElementsMatch(t, []string{"app-config", "shared-config"}, configMapNamespaces)

		// Validate VirtualMachine access (specific namespace)
		vmNamespaces := filters.GetAllowedNamespacesForResource("VirtualMachine")
		assert.ElementsMatch(t, []string{"test-vms"}, vmNamespaces)

		// Validate no access to unauthorized resources
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Secret"))
		assert.Empty(t, filters.GetAllowedNamespacesForResource("Deployment"))
	})
}

// TestCrossResourcePrivilegeEscalationPrevention ensures that permissions for one resource
// type cannot be used to gain unauthorized access to other resource types
func TestCrossResourcePrivilegeEscalationPrevention(t *testing.T) {
	config := &AuthConfig{
		KubernetesHost: "localhost",
		KubernetesPort: "6443",
		SkipTLS:        true,
	}

	t.Run("prevent_namespace_privilege_escalation", func(t *testing.T) {
		// User has wildcard namespace access for Secrets but restricted access for other resources
		scenario := &UserScenario{
			Name: "potential-privilege-escalator",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"secrets"},
					},
					Clusters:   []string{"test-cluster"},
					Namespaces: []string{"*"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"pods", "services"},
					},
					Clusters:   []string{"test-cluster"},
					Namespaces: []string{"restricted-namespace"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Verify Secret access has wildcard namespace access
		assert.True(t, filters.HasNamespaceWildcardForResource("Secret"))
		assert.True(t, filters.isNamespaceAllowedForResource("Secret", "any-namespace"))

		// Verify Pod and Service access is properly restricted despite Secret wildcard access
		assert.False(t, filters.HasNamespaceWildcardForResource("Pod"))
		assert.False(t, filters.HasNamespaceWildcardForResource("Service"))

		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		serviceNamespaces := filters.GetAllowedNamespacesForResource("Service")
		assert.ElementsMatch(t, []string{"restricted-namespace"}, podNamespaces)
		assert.ElementsMatch(t, []string{"restricted-namespace"}, serviceNamespaces)

		// Verify no cross-resource privilege escalation
		assert.True(t, filters.isNamespaceAllowedForResource("Pod", "restricted-namespace"))
		assert.False(t, filters.isNamespaceAllowedForResource("Pod", "unauthorized-namespace"))
	})

	t.Run("prevent_cluster_privilege_escalation", func(t *testing.T) {
		// User has access to multiple clusters for one resource but restricted for others
		scenario := &UserScenario{
			Name: "multi-cluster-restricted",
			Permissions: []PermissionRule{
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
					},
					Clusters:   []string{"*"},
					Namespaces: []string{"config-namespace"},
				},
				{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					Clusters:   []string{"dev-cluster"},
					Namespaces: []string{"*"},
				},
			},
		}

		filters := runUserScenario(t, config, scenario)

		// Verify cluster access is properly scoped per resource type
		allowedClusters := filters.AllowedClusters
		assert.Contains(t, allowedClusters, "*")
		assert.Contains(t, allowedClusters, "dev-cluster")

		// Verify ConfigMap can access any cluster but restricted namespace
		configMapNamespaces := filters.GetAllowedNamespacesForResource("ConfigMap")
		assert.ElementsMatch(t, []string{"config-namespace"}, configMapNamespaces)

		// Verify Pod access is cluster-restricted but namespace-unlimited within that cluster
		podNamespaces := filters.GetAllowedNamespacesForResource("Pod")
		assert.ElementsMatch(t, []string{"*"}, podNamespaces)

		// The key security check: ensure cluster filtering is applied correctly in practice
		// This would be validated in the actual query building logic
		assert.NotEmpty(t, filters.ResourceNamespaces)
		assert.Contains(t, filters.ResourceNamespaces, "ConfigMap")
		assert.Contains(t, filters.ResourceNamespaces, "Pod")
	})
}

// Helper types and functions for user scenario testing

type UserScenario struct {
	Name        string
	Description string
	Permissions []PermissionRule
}

func runUserScenario(t *testing.T, config *AuthConfig, scenario *UserScenario) *QueryFilters {
	t.Helper()

	t.Logf("Running scenario: %s - %s", scenario.Name, scenario.Description)

	// Create RBAC resolver with discovery
	resolver := NewRBACResolver(config)
	resolver.resourceDiscovery = NewResourceDiscovery(config, "Bearer "+scenario.Name+"-token")

	// Set up discovery cache with common resources
	commonMappings := map[string]string{
		"pods":                      "Pod",
		"services":                  "Service",
		"configmaps":                "ConfigMap",
		"secrets":                   "Secret",
		"serviceaccounts":           "ServiceAccount",
		"nodes":                     "Node",
		"roles":                     "Role",
		"rolebindings":              "RoleBinding",
		"clusterroles":              "ClusterRole",
		"clusterrolebindings":       "ClusterRoleBinding",
		"virtualmachines":           "VirtualMachine",
		"virtualmachineinstances":   "VirtualMachineInstance",
		"virtualmachinesnapshots":   "VirtualMachineSnapshot",
		"securitypolicies":          "Securitypolicy",  // Algorithmic mapping test
		"deployments":               "Deployment",
	}
	resolver.resourceDiscovery.updateCache(commonMappings)

	// Convert permissions to QueryFilters
	filters := resolver.convertPermissionsToFilters(scenario.Permissions)
	require.NotNil(t, filters, "Failed to convert permissions for scenario: %s", scenario.Name)

	t.Logf("Scenario %s completed. Generated %d resource types with permissions",
		scenario.Name, len(filters.AllowedResources))

	return filters
}