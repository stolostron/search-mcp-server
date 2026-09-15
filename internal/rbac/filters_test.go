package rbac

import (
	"strings"
	"testing"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stretchr/testify/assert"
)

func TestBuildConditions_NilFilters(t *testing.T) {
	cond, params := BuildConditions(nil, "")
	assert.Equal(t, "1 = 0", cond)
	assert.Nil(t, params)
}

func TestBuildConditions_EmptySources(t *testing.T) {
	filters := &auth.QueryFilters{PermissionSources: []auth.PermissionSource{}}
	cond, params := BuildConditions(filters, "")
	assert.Equal(t, "1 = 0", cond)
	assert.Nil(t, params)
}

func TestBuildConditions_ClusterScoped_SpecificKindAndAPIGroup(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Deployment", APIGroup: "apps"}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.Contains(t, cond, "data->>'kind' = %s")
	assert.Contains(t, cond, "data->>'apigroup' = %s")
	assert.Equal(t, []interface{}{"local-cluster", "Deployment", "apps"}, params)
}

func TestBuildConditions_ClusterScoped_WithPrefix(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "r.")

	assert.Contains(t, cond, "r.cluster = %s")
	assert.Contains(t, cond, "r.data->>'kind' = %s")
	assert.Contains(t, cond, "r.data->>'apigroup'")
	assert.Contains(t, params, "local-cluster")
	assert.Contains(t, params, "Pod")
}

func TestBuildConditions_FullWildcard(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "*", APIGroup: "*"}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.NotContains(t, cond, "data->>'kind'")
	assert.NotContains(t, cond, "data->>'apigroup'")
	assert.Equal(t, []interface{}{"local-cluster"}, params)
}

func TestBuildConditions_WildcardKindSpecificAPIGroup(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "*", APIGroup: "apps"}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.NotContains(t, cond, "data->>'kind'")
	assert.Contains(t, cond, "data->>'apigroup' = %s")
	assert.Contains(t, params, "apps")
}

func TestBuildConditions_SpecificKindWildcardAPIGroup(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Pod", APIGroup: "*"}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "data->>'kind' = %s")
	assert.NotContains(t, cond, "data->>'apigroup'")
	assert.Contains(t, params, "Pod")
}

func TestBuildConditions_EmptyAPIGroup_ISNULL(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, _ := BuildConditions(filters, "")

	assert.Contains(t, cond, "IS NULL")
	assert.Contains(t, cond, "data->>'apigroup' = %s")
}

func TestBuildConditions_Namespaced_HubKubernetes(t *testing.T) {
	filters := &auth.QueryFilters{
		HubClusterName: "local-cluster",
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"default": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.Contains(t, cond, "data->>'namespace' = %s")
	assert.Contains(t, cond, "data->>'kind' = %s")
	assert.Contains(t, params, "local-cluster")
	assert.Contains(t, params, "default")
	assert.Contains(t, params, "Pod")
}

func TestBuildConditions_Namespaced_UserPermissionCR(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "userpermission-cr",
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"prod-east/monitoring": {{Kind: "Deployment", APIGroup: "apps"}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.Contains(t, cond, "data->>'namespace' = %s")
	assert.Contains(t, params, "prod-east")
	assert.Contains(t, params, "monitoring")
	assert.Contains(t, params, "Deployment")
	assert.Contains(t, params, "apps")
}

func TestBuildConditions_WildcardNamespace(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "userpermission-cr",
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"prod-east/*": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.NotContains(t, cond, "data->>'namespace'")
	assert.Contains(t, params, "prod-east")
	assert.Contains(t, params, "Pod")
}

func TestBuildConditions_EmptyClusterSkipped(t *testing.T) {
	filters := &auth.QueryFilters{
		HubClusterName: "",
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"some-ns": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, _ := BuildConditions(filters, "")
	assert.Equal(t, "1 = 0", cond)
}

func TestBuildConditions_DualSource(t *testing.T) {
	filters := &auth.QueryFilters{
		HubClusterName: "local-cluster",
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Node", APIGroup: ""}},
				},
			},
			{
				Source: "userpermission-cr",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"prod-east": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.True(t, strings.Count(cond, "OR") >= 1)
	assert.Contains(t, params, "local-cluster")
	assert.Contains(t, params, "prod-east")
	assert.Contains(t, params, "Node")
	assert.Contains(t, params, "Pod")
}

func TestBuildConditions_MixedClusterAndNamespaced(t *testing.T) {
	filters := &auth.QueryFilters{
		HubClusterName: "local-cluster",
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Node", APIGroup: ""}},
				},
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"default": {{Kind: "Pod", APIGroup: ""}},
				},
			},
		},
	}
	cond, params := BuildConditions(filters, "")

	assert.Contains(t, cond, "cluster = %s")
	assert.Contains(t, cond, "data->>'kind' = %s")
	assert.Contains(t, cond, "data->>'namespace' = %s")
	assert.Contains(t, params, "local-cluster")
	assert.Contains(t, params, "Node")
	assert.Contains(t, params, "Pod")
	assert.Contains(t, params, "default")

	placeholderCount := strings.Count(cond, "%s")
	assert.Equal(t, len(params), placeholderCount, "placeholder count must match param count")
}

func TestAPIGroupCondition_Empty(t *testing.T) {
	cond, params := apiGroupCondition("", "")
	assert.Contains(t, cond, "IS NULL")
	assert.Contains(t, cond, "= %s")
	assert.Equal(t, []interface{}{""}, params)
}

func TestAPIGroupCondition_NonEmpty(t *testing.T) {
	cond, params := apiGroupCondition("", "apps")
	assert.NotContains(t, cond, "IS NULL")
	assert.Contains(t, cond, "= %s")
	assert.Equal(t, []interface{}{"apps"}, params)
}

func TestAPIGroupCondition_WithPrefix(t *testing.T) {
	cond, _ := apiGroupCondition("r.", "apps")
	assert.Contains(t, cond, "r.data->>'apigroup' = %s")
}

func TestBuildConditions_UserPermCR_NoSlashInNsKey_SkipsEmptyCluster(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source:             "userpermission-cr",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{},
				NamespacedKinds: map[string][]auth.ResourcePermission{
					"just-a-namespace": {{Kind: "Pod", APIGroup: ""}},
				},
				ManagedClusters: map[string]struct{}{},
			},
		},
	}
	cond, params := BuildConditions(filters, "")
	assert.Equal(t, "1 = 0", cond, "should deny when userpermission-cr nsKey has no '/' (empty cluster)")
	assert.Nil(t, params)
}

func TestBuildConditions_PermFilterDropsAll(t *testing.T) {
	filters := &auth.QueryFilters{
		PermissionSources: []auth.PermissionSource{
			{
				Source: "hub-kubernetes",
				ClusterScopedKinds: map[string][]auth.ResourcePermission{
					"local-cluster": {{Kind: "Pod", APIGroup: ""}},
				},
				NamespacedKinds: map[string][]auth.ResourcePermission{},
				ManagedClusters: map[string]struct{}{"local-cluster": {}},
			},
		},
		HubClusterName: "local-cluster",
	}
	cond, params := BuildConditionsWithOptions(filters, Options{
		PermFilter: func(perms []auth.ResourcePermission) []auth.ResourcePermission {
			return nil
		},
	})
	assert.Equal(t, "1 = 0", cond, "should deny when PermFilter drops all permissions")
	assert.Nil(t, params)
}

func TestBuildClusterPerms_MultiplePerms(t *testing.T) {
	perms := []auth.ResourcePermission{
		{Kind: "*", APIGroup: "*"},
		{Kind: "Pod", APIGroup: ""},
		{Kind: "*", APIGroup: "apps"},
	}
	cond, params := buildClusterPerms("", "my-cluster", "", perms)

	assert.Contains(t, cond, " OR ")

	placeholderCount := strings.Count(cond, "%s")
	assert.Equal(t, len(params), placeholderCount)
}
