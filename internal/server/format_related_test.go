package server

import (
	"strings"
	"testing"

	"github.com/stolostron/search-mcp-server/internal/findrelated"
	"github.com/stretchr/testify/assert"
)

func TestFormatRelatedResult_Empty(t *testing.T) {
	result := &findrelated.FindRelatedResult{
		Groups: []findrelated.RelatedKindGroup{},
		Metadata: findrelated.RelatedMetadata{
			SeedUIDs:      []string{"uid-1"},
			MaxHops:       1,
			TotalRelated:  0,
			ExecutionTime: 5,
		},
	}

	output := FormatRelatedResult(result)
	assert.Contains(t, output, "Related Resources")
	assert.Contains(t, output, "No related resources found")
	assert.Contains(t, output, "Seeds:** 1")
}

func TestFormatRelatedResult_WithGroups(t *testing.T) {
	ns := "default"
	result := &findrelated.FindRelatedResult{
		Groups: []findrelated.RelatedKindGroup{
			{
				Kind:  "Pod",
				Count: 2,
				Items: []findrelated.RelatedItem{
					{UID: "uid-1", Name: "pod-1", Namespace: &ns, Kind: "Pod", Cluster: "cluster-1"},
					{UID: "uid-2", Name: "pod-2", Namespace: &ns, Kind: "Pod", Cluster: "cluster-1"},
				},
			},
			{
				Kind:  "ReplicaSet",
				Count: 1,
				Items: []findrelated.RelatedItem{
					{UID: "uid-3", Name: "rs-1", Namespace: &ns, Kind: "ReplicaSet", Cluster: "cluster-1"},
				},
			},
		},
		Metadata: findrelated.RelatedMetadata{
			SeedUIDs:      []string{"seed-1"},
			MaxHops:       1,
			TotalRelated:  3,
			ExecutionTime: 12,
		},
	}

	output := FormatRelatedResult(result)

	assert.Contains(t, output, "Pod (2)")
	assert.Contains(t, output, "ReplicaSet (1)")
	assert.Contains(t, output, "pod-1")
	assert.Contains(t, output, "pod-2")
	assert.Contains(t, output, "rs-1")
	assert.Contains(t, output, "default")
	assert.Contains(t, output, "cluster-1")
	assert.Contains(t, output, "Total related:** 3")
	assert.Contains(t, output, "12ms")
}

func TestFormatRelatedResult_ClusterScoped(t *testing.T) {
	result := &findrelated.FindRelatedResult{
		Groups: []findrelated.RelatedKindGroup{
			{
				Kind:  "Node",
				Count: 1,
				Items: []findrelated.RelatedItem{
					{UID: "uid-1", Name: "node-1", Kind: "Node", Cluster: "cluster-1"},
				},
			},
		},
		Metadata: findrelated.RelatedMetadata{
			SeedUIDs:      []string{"seed-1"},
			MaxHops:       1,
			TotalRelated:  1,
			ExecutionTime: 3,
		},
	}

	output := FormatRelatedResult(result)
	assert.Contains(t, output, "node-1")
	// Namespace should show "-" for cluster-scoped resources
	lines := strings.Split(output, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "node-1") {
			assert.Contains(t, line, "| - |")
			found = true
		}
	}
	assert.True(t, found, "should find the node-1 row")
}

func TestFormatRelatedResult_MarkdownEscaping(t *testing.T) {
	ns := "ns|with|pipes"
	result := &findrelated.FindRelatedResult{
		Groups: []findrelated.RelatedKindGroup{
			{
				Kind:  "ConfigMap",
				Count: 1,
				Items: []findrelated.RelatedItem{
					{UID: "uid-1", Name: "cm|special*chars", Namespace: &ns, Kind: "ConfigMap", Cluster: "cluster_1"},
				},
			},
		},
		Metadata: findrelated.RelatedMetadata{
			SeedUIDs:      []string{"seed-1"},
			MaxHops:       1,
			TotalRelated:  1,
			ExecutionTime: 1,
		},
	}

	output := FormatRelatedResult(result)

	assert.NotContains(t, output, "| cm|special")
	assert.Contains(t, output, "cm\\|special\\*chars")
	assert.Contains(t, output, "ns\\|with\\|pipes")
	assert.Contains(t, output, "cluster\\_1")
}

func TestEscapeRelatedMarkdown_WhitespaceCollapsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"newlines become spaces", "line1\nline2\nline3", "line1 line2 line3"},
		{"tabs become spaces", "col1\tcol2", "col1 col2"},
		{"carriage returns become spaces", "a\rb", "a b"},
		{"consecutive whitespace collapsed", "a\n\n\nb", "a b"},
		{"mixed whitespace collapsed", "a\t\n\rb", "a b"},
		{"leading/trailing trimmed", "\n  hello  \n", "hello"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeRelatedMarkdown(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatRelatedResult_CountOnly(t *testing.T) {
	result := &findrelated.FindRelatedResult{
		Groups: []findrelated.RelatedKindGroup{
			{Kind: "Pod", Count: 5},
			{Kind: "ReplicaSet", Count: 2},
		},
		Metadata: findrelated.RelatedMetadata{
			SeedUIDs:      []string{"seed-1"},
			MaxHops:       1,
			TotalRelated:  7,
			ExecutionTime: 2,
		},
	}

	output := FormatRelatedResult(result)

	assert.Contains(t, output, "Pod (5)")
	assert.Contains(t, output, "ReplicaSet (2)")
	// No table header since items are empty
	assert.NotContains(t, output, "| Name |")
}
