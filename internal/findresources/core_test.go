package findresources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Note: Actual database mocking would require proper interfaces

func TestFindResourcesCore_validateArgs(t *testing.T) {
	core := NewFindResourcesCore(nil)

	tests := []struct {
		name    string
		args    FindResourcesArgs
		wantErr bool
	}{
		{
			name: "valid basic args",
			args: FindResourcesArgs{
				Kind:       "Pod",
				Namespace:  "default",
				OutputMode: OutputModeList,
				Limit:      50,
			},
			wantErr: false,
		},
		{
			name: "invalid output mode",
			args: FindResourcesArgs{
				OutputMode: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid limit too high",
			args: FindResourcesArgs{
				Limit: 2000,
			},
			wantErr: true,
		},
		{
			name: "invalid sort order",
			args: FindResourcesArgs{
				SortOrder: "invalid",
			},
			wantErr: true,
		},
		{
			name: "empty args should pass with defaults",
			args: FindResourcesArgs{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.validateArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFindResourcesCore_normalizeArgs(t *testing.T) {
	core := NewFindResourcesCore(nil)

	tests := []struct {
		name     string
		args     FindResourcesArgs
		expected FindResourcesArgs
	}{
		{
			name: "apply defaults",
			args: FindResourcesArgs{},
			expected: FindResourcesArgs{
				OutputMode: DefaultOutputMode,
				Limit:      DefaultLimit,
				SortOrder:  DefaultSortOrder,
				GroupBy:    "",
			},
		},
		{
			name: "count mode gets default groupBy",
			args: FindResourcesArgs{
				OutputMode: OutputModeCount,
			},
			expected: FindResourcesArgs{
				OutputMode: OutputModeCount,
				Limit:      DefaultLimit,
				SortOrder:  DefaultSortOrder,
				GroupBy:    "status",
			},
		},
		{
			name: "preserve existing values",
			args: FindResourcesArgs{
				OutputMode: OutputModeSummary,
				Limit:      100,
				SortOrder:  SortOrderDesc,
				GroupBy:    "cluster",
			},
			expected: FindResourcesArgs{
				OutputMode: OutputModeSummary,
				Limit:      100,
				SortOrder:  SortOrderDesc,
				GroupBy:    "cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.normalizeArgs(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesCore_combineClusterFilters(t *testing.T) {
	core := NewFindResourcesCore(nil)

	tests := []struct {
		name            string
		explicitCluster interface{}
		targetClusters  []string
		expected        []string
	}{
		{
			name:            "no filters",
			explicitCluster: nil,
			targetClusters:  nil,
			expected:        []string{},
		},
		{
			name:            "only explicit cluster (string)",
			explicitCluster: "cluster1",
			targetClusters:  nil,
			expected:        []string{"cluster1"},
		},
		{
			name:            "only explicit clusters (slice)",
			explicitCluster: []string{"cluster1", "cluster2"},
			targetClusters:  nil,
			expected:        []string{"cluster1", "cluster2"},
		},
		{
			name:            "only target clusters",
			explicitCluster: nil,
			targetClusters:  []string{"cluster1", "cluster2"},
			expected:        []string{"cluster1", "cluster2"},
		},
		{
			name:            "intersection of both",
			explicitCluster: []string{"cluster1", "cluster2", "cluster3"},
			targetClusters:  []string{"cluster2", "cluster3", "cluster4"},
			expected:        []string{"cluster2", "cluster3"},
		},
		{
			name:            "no intersection",
			explicitCluster: []string{"cluster1"},
			targetClusters:  []string{"cluster2"},
			expected:        []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.combineClusterFilters(tt.explicitCluster, tt.targetClusters)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestFindResourcesCore_createEmptyResult(t *testing.T) {
	core := NewFindResourcesCore(nil)

	tests := []struct {
		name       string
		args       FindResourcesArgs
		expectType string
	}{
		{
			name:       "list mode",
			args:       FindResourcesArgs{OutputMode: OutputModeList},
			expectType: "[]ResourceResult",
		},
		{
			name:       "count mode",
			args:       FindResourcesArgs{OutputMode: OutputModeCount},
			expectType: "[]CountResult",
		},
		{
			name:       "summary mode",
			args:       FindResourcesArgs{OutputMode: OutputModeSummary},
			expectType: "SummaryResult",
		},
		{
			name:       "health mode",
			args:       FindResourcesArgs{OutputMode: OutputModeHealth},
			expectType: "HealthResult",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.createEmptyResult(tt.args)
			assert.NotNil(t, result)
			assert.Equal(t, tt.args.OutputMode, result.Mode)
			assert.Equal(t, 0, result.Metadata.TotalCount)

			// Verify data type
			switch tt.expectType {
			case "[]ResourceResult":
				_, ok := result.Data.([]ResourceResult)
				assert.True(t, ok, "Expected []ResourceResult")
			case "[]CountResult":
				_, ok := result.Data.([]CountResult)
				assert.True(t, ok, "Expected []CountResult")
			case "SummaryResult":
				_, ok := result.Data.(SummaryResult)
				assert.True(t, ok, "Expected SummaryResult")
			case "HealthResult":
				_, ok := result.Data.(HealthResult)
				assert.True(t, ok, "Expected HealthResult")
			}
		})
	}
}

func TestFindResourcesCore_buildOrderByClause(t *testing.T) {
	core := NewFindResourcesCore(nil)

	tests := []struct {
		name      string
		sortBy    string
		sortOrder string
		expected  string
	}{
		{
			name:      "sort by name asc",
			sortBy:    "name",
			sortOrder: "asc",
			expected:  "data->>'name' ASC",
		},
		{
			name:      "sort by created desc",
			sortBy:    "created",
			sortOrder: "desc",
			expected:  "data->>'created' DESC",
		},
		{
			name:      "sort by namespace",
			sortBy:    "namespace",
			sortOrder: "asc",
			expected:  "data->>'namespace' ASC",
		},
		{
			name:      "default sort (unknown field)",
			sortBy:    "unknown",
			sortOrder: "desc",
			expected:  "data->>'name' DESC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.buildOrderByClause(tt.sortBy, tt.sortOrder)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test basic compilation and instantiation
func TestNewFindResourcesCore(t *testing.T) {
	core := NewFindResourcesCore(nil)
	assert.NotNil(t, core)
}

// Test that all output mode constants are defined
func TestOutputModeConstants(t *testing.T) {
	assert.Equal(t, "list", OutputModeList)
	assert.Equal(t, "count", OutputModeCount)
	assert.Equal(t, "summary", OutputModeSummary)
	assert.Equal(t, "health", OutputModeHealth)
}

// Test that default constants are reasonable
func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, "list", DefaultOutputMode)
	assert.Equal(t, 50, DefaultLimit)
	assert.Equal(t, 1000, MaxLimit)
	assert.Equal(t, "asc", DefaultSortOrder)
}