package findrelated

import (
	"context"
	"testing"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestNewFindRelatedCore(t *testing.T) {
	core := NewFindRelatedCore(nil)
	assert.NotNil(t, core)
	assert.NotNil(t, core.sanitizer)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 1, DefaultMaxHops)
	assert.Equal(t, 3, ApplicationMaxHops)
	assert.Equal(t, 200, DefaultRelatedLimit)
	assert.Equal(t, 1000, MaxRelatedLimit)
	assert.Equal(t, "list", OutputModeList)
	assert.Equal(t, "count", OutputModeCount)
}

func TestValidateArgs(t *testing.T) {
	core := NewFindRelatedCore(nil)

	tests := []struct {
		name    string
		args    FindRelatedArgs
		wantErr string
	}{
		{
			name:    "empty UIDs",
			args:    FindRelatedArgs{},
			wantErr: "uids is required",
		},
		{
			name:    "negative maxHops",
			args:    FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: -1},
			wantErr: "maxHops must be non-negative",
		},
		{
			name:    "multiple UIDs",
			args:    FindRelatedArgs{UIDs: []string{"uid-1", "uid-2"}},
			wantErr: "uids must contain exactly 1",
		},
		{
			name:    "maxHops exceeds max",
			args:    FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 4},
			wantErr: "maxHops must not exceed 3",
		},
		{
			name:    "negative limit",
			args:    FindRelatedArgs{UIDs: []string{"uid-1"}, Limit: -1},
			wantErr: "limit must be non-negative",
		},
		{
			name: "valid args",
			args: FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 2, Limit: 100},
		},
		{
			name: "valid with zero maxHops and limit",
			args: FindRelatedArgs{UIDs: []string{"uid-1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.validateArgs(tt.args)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNormalizeArgs(t *testing.T) {
	core := NewFindRelatedCore(nil)

	tests := []struct {
		name     string
		args     FindRelatedArgs
		expected FindRelatedArgs
	}{
		{
			name: "defaults applied",
			args: FindRelatedArgs{UIDs: []string{"uid-1"}},
			expected: FindRelatedArgs{
				UIDs:       []string{"uid-1"},
				MaxHops:    DefaultMaxHops,
				Limit:      DefaultRelatedLimit,
				OutputMode: OutputModeList,
			},
		},
		{
			name: "limit capped to max",
			args: FindRelatedArgs{UIDs: []string{"uid-1"}, Limit: 5000},
			expected: FindRelatedArgs{
				UIDs:       []string{"uid-1"},
				MaxHops:    DefaultMaxHops,
				Limit:      MaxRelatedLimit,
				OutputMode: OutputModeList,
			},
		},
		{
			name: "explicit values preserved",
			args: FindRelatedArgs{
				UIDs:       []string{"uid-1"},
				MaxHops:    3,
				Limit:      50,
				OutputMode: OutputModeCount,
			},
			expected: FindRelatedArgs{
				UIDs:       []string{"uid-1"},
				MaxHops:    3,
				Limit:      50,
				OutputMode: OutputModeCount,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.normalizeArgs(context.Background(), tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveDefaultHops_NilDB(t *testing.T) {
	core := NewFindRelatedCore(nil)
	assert.Equal(t, DefaultMaxHops, core.resolveDefaultHops(context.Background(), []string{"uid-1"}))
}

func TestBuildRelatedQuery_SingleHop(t *testing.T) {
	core := NewFindRelatedCore(nil)

	args := FindRelatedArgs{
		UIDs:    []string{"uid-1"},
		MaxHops: 1,
	}

	query, params := core.buildRelatedQuery(args, nil)

	assert.Contains(t, query, "search.edges")
	assert.Contains(t, query, "INNER JOIN search.resources")
	assert.NotContains(t, query, "RECURSIVE")
	assert.Contains(t, query, "$1")
	assert.Equal(t, []interface{}{"uid-1"}, params)
}

func TestBuildRelatedQuery_MultiHop(t *testing.T) {
	core := NewFindRelatedCore(nil)

	args := FindRelatedArgs{
		UIDs:    []string{"uid-1"},
		MaxHops: 3,
	}

	query, params := core.buildRelatedQuery(args, nil)

	assert.Contains(t, query, "RECURSIVE")
	assert.Contains(t, query, "search_graph")
	assert.Contains(t, query, "sg.level")
	assert.Contains(t, query, "INNER JOIN search.resources")
	assert.Contains(t, query, "Node")
	assert.Contains(t, query, "Channel")
	assert.Contains(t, query, "interCluster")
	assert.Equal(t, 2, len(params))
	assert.Equal(t, "uid-1", params[0])
	assert.Equal(t, 3, params[1])
}

func TestBuildRelatedQuery_WithRBAC(t *testing.T) {
	core := NewFindRelatedCore(nil)

	args := FindRelatedArgs{
		UIDs:    []string{"uid-1"},
		MaxHops: 1,
	}

	userCtx := &auth.UserContext{
		QueryFilters: &auth.QueryFilters{
			HubClusterName: "local-cluster",
			PermissionSources: []auth.PermissionSource{
				{
					Source: "hub-kubernetes",
					ClusterScopedKinds: map[string][]auth.ResourcePermission{
						"local-cluster": {{Kind: "Pod", APIGroup: ""}},
					},
				},
			},
		},
	}

	query, params := core.buildRelatedQuery(args, userCtx)

	assert.Contains(t, query, "WHERE")
	assert.Contains(t, query, "r.cluster")
	assert.Contains(t, query, "r.data->>'kind'")
	assert.True(t, len(params) > 1)
}

func TestBuildRelatedQuery_NilUserCtx(t *testing.T) {
	core := NewFindRelatedCore(nil)

	args := FindRelatedArgs{
		UIDs:    []string{"uid-1"},
		MaxHops: 1,
	}

	query, params := core.buildRelatedQuery(args, nil)

	// No RBAC WHERE on the outer query — only the inner edge WHERE exists.
	assert.NotContains(t, query, "1 = 0")
	assert.NotContains(t, query, "r.cluster")
	assert.NotContains(t, query, "r.data->>'kind'")
	assert.Equal(t, 1, len(params))
}

func TestBuildRelatedQuery_FailClosedNilQueryFilters(t *testing.T) {
	core := NewFindRelatedCore(nil)
	args := FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 1}
	userCtx := &auth.UserContext{QueryFilters: nil}

	query, _ := core.buildRelatedQuery(args, userCtx)
	assert.Contains(t, query, "1 = 0")
}

func TestBuildRelatedQuery_EmptyAPIGroupUsesISNULL(t *testing.T) {
	core := NewFindRelatedCore(nil)
	args := FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 1}
	userCtx := &auth.UserContext{
		QueryFilters: &auth.QueryFilters{
			PermissionSources: []auth.PermissionSource{
				{
					Source: "hub-kubernetes",
					ClusterScopedKinds: map[string][]auth.ResourcePermission{
						"local-cluster": {{Kind: "Pod", APIGroup: ""}},
					},
				},
			},
		},
	}

	query, _ := core.buildRelatedQuery(args, userCtx)
	assert.Contains(t, query, "IS NULL", "empty apiGroup must use IS NULL for core group resources")
}

func TestBuildRelatedQuery_WildcardKindRestrictsAPIGroup(t *testing.T) {
	core := NewFindRelatedCore(nil)
	args := FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 1}
	userCtx := &auth.UserContext{
		QueryFilters: &auth.QueryFilters{
			PermissionSources: []auth.PermissionSource{
				{
					Source: "hub-kubernetes",
					ClusterScopedKinds: map[string][]auth.ResourcePermission{
						"local-cluster": {{Kind: "*", APIGroup: "apps"}},
					},
				},
			},
		},
	}

	query, params := core.buildRelatedQuery(args, userCtx)
	assert.NotContains(t, query, "r.data->>'kind'", "wildcard kind should not filter on kind")
	assert.Contains(t, query, "r.data->>'apigroup'", "specific apiGroup must still be enforced")
	assert.Contains(t, params, "apps")
}

func TestBuildRelatedQuery_MixedClusterAndNamespaced(t *testing.T) {
	core := NewFindRelatedCore(nil)
	args := FindRelatedArgs{UIDs: []string{"uid-1"}, MaxHops: 1}
	userCtx := &auth.UserContext{
		QueryFilters: &auth.QueryFilters{
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
		},
	}

	query, params := core.buildRelatedQuery(args, userCtx)
	assert.Contains(t, query, "r.cluster")
	assert.Contains(t, query, "r.data->>'kind'")
	assert.Contains(t, query, "r.data->>'namespace'")
	assert.Contains(t, params, "local-cluster")
	assert.Contains(t, params, "Node")
	assert.Contains(t, params, "Pod")
	assert.Contains(t, params, "default")
}

func TestBuildItemQuery(t *testing.T) {
	core := NewFindRelatedCore(nil)
	uids := []interface{}{"uid-1", "uid-2", "uid-3"}
	query, params := core.buildItemQuery(uids, 100)

	assert.Contains(t, query, "SELECT uid, cluster, data FROM search.resources")
	assert.Contains(t, query, "WHERE")
	assert.Contains(t, query, "IN")
	assert.Contains(t, query, "LIMIT 100")
	assert.Equal(t, 3, len(params))
}

func makeRelatedQueryResult(rows ...[]interface{}) *types.QueryResult {
	rowCount := len(rows)
	return &types.QueryResult{
		Columns:  []string{"uid", "cluster", "data"},
		Rows:     rows,
		RowCount: &rowCount,
	}
}

func TestProcessItems_GroupsByKind(t *testing.T) {
	core := NewFindRelatedCore(nil)

	ns := "default"
	qr := makeRelatedQueryResult(
		[]interface{}{"uid-1", "cluster-1", map[string]interface{}{
			"name": "pod-1", "kind": "Pod", "namespace": ns,
		}},
		[]interface{}{"uid-2", "cluster-1", map[string]interface{}{
			"name": "pod-2", "kind": "Pod", "namespace": ns,
		}},
		[]interface{}{"uid-3", "cluster-1", map[string]interface{}{
			"name": "rs-1", "kind": "ReplicaSet", "namespace": ns,
		}},
	)

	related := []uidInfo{
		{uid: "uid-1", kind: "Pod"},
		{uid: "uid-2", kind: "Pod"},
		{uid: "uid-3", kind: "ReplicaSet"},
	}

	groups := core.processItems(qr, related)

	assert.Len(t, groups, 2)

	kindCounts := map[string]int{}
	for _, g := range groups {
		kindCounts[g.Kind] = g.Count
	}
	assert.Equal(t, 2, kindCounts["Pod"])
	assert.Equal(t, 1, kindCounts["ReplicaSet"])
}

func TestProcessItems_SetsFields(t *testing.T) {
	core := NewFindRelatedCore(nil)

	ns := "kube-system"
	qr := makeRelatedQueryResult(
		[]interface{}{"uid-1", "cluster-1", map[string]interface{}{
			"name": "my-pod", "kind": "Pod", "namespace": ns,
		}},
	)

	related := []uidInfo{{uid: "uid-1", kind: "Pod"}}
	groups := core.processItems(qr, related)

	assert.Len(t, groups, 1)
	assert.Len(t, groups[0].Items, 1)
	item := groups[0].Items[0]

	assert.Equal(t, "uid-1", item.UID)
	assert.Equal(t, "cluster-1", item.Cluster)
	assert.Equal(t, "my-pod", item.Name)
	assert.Equal(t, "Pod", item.Kind)
	assert.NotNil(t, item.Namespace)
	assert.Equal(t, ns, *item.Namespace)
}

func TestProcessItems_ClusterScopedNoNamespace(t *testing.T) {
	core := NewFindRelatedCore(nil)

	qr := makeRelatedQueryResult(
		[]interface{}{"uid-1", "cluster-1", map[string]interface{}{
			"name": "my-node", "kind": "Node",
		}},
	)

	related := []uidInfo{{uid: "uid-1", kind: "Node"}}
	groups := core.processItems(qr, related)

	assert.Len(t, groups, 1)
	item := groups[0].Items[0]
	assert.Nil(t, item.Namespace)
}

func TestProcessItems_FallsBackToDataKind(t *testing.T) {
	core := NewFindRelatedCore(nil)

	qr := makeRelatedQueryResult(
		[]interface{}{"uid-1", "cluster-1", map[string]interface{}{
			"name": "something", "kind": "Service",
		}},
	)

	related := []uidInfo{{uid: "uid-1", kind: ""}}
	groups := core.processItems(qr, related)

	assert.Len(t, groups, 1)
	assert.Equal(t, "Service", groups[0].Kind)
}

func TestProcessItems_SkipsMalformedRows(t *testing.T) {
	core := NewFindRelatedCore(nil)

	qr := makeRelatedQueryResult(
		[]interface{}{"uid-1"},
		[]interface{}{123, "cluster", map[string]interface{}{}},
		[]interface{}{"uid-2", 456, map[string]interface{}{}},
		[]interface{}{"uid-3", "cluster", "not-a-map"},
	)

	related := []uidInfo{{uid: "uid-3", kind: "Pod"}}
	groups := core.processItems(qr, related)

	assert.Len(t, groups, 0)
}

func TestGroupCounts(t *testing.T) {
	core := NewFindRelatedCore(nil)

	related := []uidInfo{
		{uid: "1", kind: "Pod"},
		{uid: "2", kind: "Pod"},
		{uid: "3", kind: "Pod"},
		{uid: "4", kind: "ReplicaSet"},
		{uid: "5", kind: "Deployment"},
		{uid: "6", kind: "Deployment"},
	}

	groups := core.groupCounts(related)

	kindCounts := map[string]int{}
	for _, g := range groups {
		kindCounts[g.Kind] = g.Count
		assert.Nil(t, g.Items)
	}
	assert.Equal(t, 3, kindCounts["Pod"])
	assert.Equal(t, 1, kindCounts["ReplicaSet"])
	assert.Equal(t, 2, kindCounts["Deployment"])
}

func TestGroupCounts_Empty(t *testing.T) {
	core := NewFindRelatedCore(nil)
	groups := core.groupCounts(nil)
	assert.Len(t, groups, 0)
}

