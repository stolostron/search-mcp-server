package findresources

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewFindResourcesFormatter(t *testing.T) {
	formatter := NewFindResourcesFormatter()
	assert.NotNil(t, formatter)
}

func TestFindResourcesFormatter_escapeMarkdown(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic text",
			input:    "simple text",
			expected: "simple text",
		},
		{
			name:     "markdown characters",
			input:    "text|with*special_chars",
			expected: "text\\|with\\*special\\_chars",
		},
		{
			name:     "newlines and tabs",
			input:    "text\nwith\ttabs",
			expected: "text with tabs",
		},
		{
			name:     "multiple spaces",
			input:    "text  with   spaces",
			expected: "text with spaces",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.escapeMarkdown(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesFormatter_formatNamespace(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	tests := []struct {
		name      string
		namespace *string
		expected  string
	}{
		{
			name:      "nil namespace",
			namespace: nil,
			expected:  "*(cluster-scoped)*",
		},
		{
			name:      "empty namespace",
			namespace: func() *string { s := ""; return &s }(),
			expected:  "*(cluster-scoped)*",
		},
		{
			name:      "valid namespace",
			namespace: func() *string { s := "default"; return &s }(),
			expected:  "default",
		},
		{
			name:      "namespace with special chars",
			namespace: func() *string { s := "kube-system"; return &s }(),
			expected:  "kube-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.formatNamespace(tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesFormatter_formatStatus(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	tests := []struct {
		name     string
		status   *string
		expected string
	}{
		{
			name:     "nil status",
			status:   nil,
			expected: "-",
		},
		{
			name:     "empty status",
			status:   func() *string { s := ""; return &s }(),
			expected: "-",
		},
		{
			name:     "valid status",
			status:   func() *string { s := "Running"; return &s }(),
			expected: "Running",
		},
		{
			name:     "status with special chars",
			status:   func() *string { s := "CrashLoopBackOff"; return &s }(),
			expected: "CrashLoopBackOff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.formatStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesFormatter_getStatusIcon(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{
			name:     "running status",
			status:   "running",
			expected: "✅",
		},
		{
			name:     "failed status",
			status:   "failed",
			expected: "❌",
		},
		{
			name:     "pending status",
			status:   "pending",
			expected: "⏳",
		},
		{
			name:     "unknown status",
			status:   "unknown",
			expected: "❓",
		},
		{
			name:     "other status",
			status:   "other",
			expected: "📊",
		},
		{
			name:     "case insensitive",
			status:   "RUNNING",
			expected: "✅",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.getStatusIcon(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesFormatter_formatValue(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "NULL",
		},
		{
			name:     "string value",
			input:    "test",
			expected: "test",
		},
		{
			name:     "bool true",
			input:    true,
			expected: "true",
		},
		{
			name:     "bool false",
			input:    false,
			expected: "false",
		},
		{
			name:     "int value",
			input:    42,
			expected: "42",
		},
		{
			name:     "float value",
			input:    3.14,
			expected: "3.14",
		},
		{
			name:     "slice value",
			input:    []interface{}{"a", "b", "c"},
			expected: "a, b, c",
		},
		{
			name:     "map value",
			input:    map[string]interface{}{"key": "value"},
			expected: "{object}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.formatValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindResourcesFormatter_groupResourcesByKind(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	resources := []ResourceResult{
		{Kind: "Pod", Name: "pod1"},
		{Kind: "Pod", Name: "pod2"},
		{Kind: "Service", Name: "svc1"},
		{Kind: "", Name: "unknown1"}, // Empty kind should become "Unknown"
	}

	result := formatter.groupResourcesByKind(resources)

	assert.Len(t, result, 3) // Pod, Service, Unknown
	assert.Len(t, result["Pod"], 2)
	assert.Len(t, result["Service"], 1)
	assert.Len(t, result["Unknown"], 1)

	// Test that resources are sorted by name within groups
	assert.Equal(t, "pod1", result["Pod"][0].Name)
	assert.Equal(t, "pod2", result["Pod"][1].Name)
}

func TestFindResourcesFormatter_FormatResult_InvalidData(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	// Test with invalid data for list mode
	result := &FindResourcesResult{
		Mode: OutputModeList,
		Data: "invalid", // Should be []ResourceResult
		Metadata: Metadata{
			TotalCount:    0,
			ExecutionTime: 100,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	assert.Contains(t, response.Content[0].Text, "Error: Invalid data format")
}

func TestFindResourcesFormatter_FormatResult_EmptyResults(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	// Test with empty results
	result := &FindResourcesResult{
		Mode: OutputModeList,
		Data: []ResourceResult{},
		Metadata: Metadata{
			TotalCount:    0,
			ExecutionTime: 50,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	assert.Contains(t, response.Content[0].Text, "No resources found")
	assert.Contains(t, response.Content[0].Text, "Found 0 resources")
	assert.Contains(t, response.Content[0].Text, "execution time: 50ms")
}

func TestFindResourcesFormatter_FormatResult_ListMode(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	// Create test data
	namespace := "default"
	status := "Running"
	created := time.Now()

	resources := []ResourceResult{
		{
			Name:      "test-pod",
			Namespace: &namespace,
			Kind:      "Pod",
			Cluster:   "cluster1",
			Age:       "1h30m",
			Status:    &status,
			Created:   &created,
			Labels:    map[string]string{"app": "test"},
			Data:      map[string]interface{}{"name": "test-pod"},
		},
	}

	result := &FindResourcesResult{
		Mode: OutputModeList,
		Data: resources,
		Metadata: Metadata{
			TotalCount:    1,
			ExecutionTime: 150,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	text := response.Content[0].Text

	// Check that markdown table is properly formatted
	assert.Contains(t, text, "# Find Resources Results")
	assert.Contains(t, text, "Found 1 resources")
	assert.Contains(t, text, "execution time: 150ms")
	assert.Contains(t, text, "## Pod (1)")
	assert.Contains(t, text, "| Name | Namespace | Cluster | Age | Status |")
	assert.Contains(t, text, "| test-pod | default | cluster1 | 1h30m | Running |")
}

func TestFindResourcesFormatter_FormatResult_CountMode(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	counts := []CountResult{
		{Label: "Running", Count: 5, Percentage: 50.0},
		{Label: "Pending", Count: 3, Percentage: 30.0},
		{Label: "Failed", Count: 2, Percentage: 20.0},
	}

	result := &FindResourcesResult{
		Mode: OutputModeCount,
		Data: counts,
		Metadata: Metadata{
			TotalCount:    10,
			ExecutionTime: 75,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	text := response.Content[0].Text

	assert.Contains(t, text, "# Resource Count Analysis")
	assert.Contains(t, text, "Total Resources: 10")
	assert.Contains(t, text, "execution time: 75ms")
	assert.Contains(t, text, "| Label | Count | Percentage |")
	assert.Contains(t, text, "| Running | 5 | 50.0% |")
	assert.Contains(t, text, "| Pending | 3 | 30.0% |")
}

func TestFindResourcesFormatter_FormatResult_SummaryMode(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	summary := SummaryResult{
		TotalResources: 100,
		TotalClusters:  3,
		ResourcesByCluster: []CountResult{
			{Label: "cluster1", Count: 60, Percentage: 60.0},
			{Label: "cluster2", Count: 40, Percentage: 40.0},
		},
		ResourcesByKind: []CountResult{
			{Label: "Pod", Count: 80, Percentage: 80.0},
			{Label: "Service", Count: 20, Percentage: 20.0},
		},
		ResourcesByNamespace: []CountResult{
			{Label: "default", Count: 50, Percentage: 50.0},
			{Label: "kube-system", Count: 30, Percentage: 30.0},
		},
	}

	result := &FindResourcesResult{
		Mode: OutputModeSummary,
		Data: summary,
		Metadata: Metadata{
			TotalCount:    100,
			ExecutionTime: 200,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	text := response.Content[0].Text

	assert.Contains(t, text, "# Resource Fleet Summary")
	assert.Contains(t, text, "**Total Resources:** 100")
	assert.Contains(t, text, "**Total Clusters:** 3")
	assert.Contains(t, text, "execution time: 200ms")
	assert.Contains(t, text, "## 🖥️ Resources by Cluster")
	assert.Contains(t, text, "## 📦 Resources by Type")
	assert.Contains(t, text, "## 📁 Top Namespaces")
}

func TestFindResourcesFormatter_FormatResult_HealthMode(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	health := HealthResult{
		Total:     100,
		Healthy:   70,
		Unhealthy: 20,
		Unknown:   10,
		Details: []CountResult{
			{Label: "Running", Count: 70, Percentage: 70.0},
			{Label: "Failed", Count: 20, Percentage: 20.0},
			{Label: "Pending", Count: 10, Percentage: 10.0},
		},
		TopIssues: []string{"CrashLoopBackOff (15)", "ImagePullBackOff (5)"},
	}

	result := &FindResourcesResult{
		Mode: OutputModeHealth,
		Data: health,
		Metadata: Metadata{
			TotalCount:    100,
			ExecutionTime: 300,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	text := response.Content[0].Text

	assert.Contains(t, text, "# Fleet Health Analysis")
	assert.Contains(t, text, "✅ **Healthy:** 70 resources (70.0%)")
	assert.Contains(t, text, "❌ **Unhealthy:** 20 resources (20.0%)")
	assert.Contains(t, text, "❓ **Unknown:** 10 resources (10.0%)")
	assert.Contains(t, text, "execution time: 300ms")
	assert.Contains(t, text, "## 📋 Status Breakdown")
	assert.Contains(t, text, "## 🔴 Top Issues")
	assert.Contains(t, text, "1. 🚨 CrashLoopBackOff \\(15\\)")
}

func TestFindResourcesFormatter_UnsupportedOutputMode(t *testing.T) {
	formatter := NewFindResourcesFormatter()

	result := &FindResourcesResult{
		Mode: "invalid_mode",
		Data: nil,
		Metadata: Metadata{
			TotalCount:    0,
			ExecutionTime: 0,
		},
	}

	response := formatter.FormatResult(result)
	assert.Len(t, response.Content, 1)
	assert.Contains(t, response.Content[0].Text, "Unsupported output mode: invalid_mode")
}

// Test MCP response structure
func TestMCPResponseStructure(t *testing.T) {
	response := MCPResponse{
		Content: []MCPContent{
			{Type: "text", Text: "test content"},
		},
	}

	assert.Len(t, response.Content, 1)
	assert.Equal(t, "text", response.Content[0].Type)
	assert.Equal(t, "test content", response.Content[0].Text)
}