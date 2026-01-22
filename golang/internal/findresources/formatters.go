package findresources

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FindResourcesFormatter handles formatting of results for MCP responses
type FindResourcesFormatter struct{}

// NewFindResourcesFormatter creates a new formatter instance
func NewFindResourcesFormatter() *FindResourcesFormatter {
	return &FindResourcesFormatter{}
}

// MCPResponse represents the standard MCP tool response format
type MCPResponse struct {
	Content []MCPContent `json:"content"`
}

// MCPContent represents content in an MCP response
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// FormatResult formats a FindResourcesResult into an MCP response
func (f *FindResourcesFormatter) FormatResult(result *FindResourcesResult) MCPResponse {
	switch result.Mode {
	case OutputModeList:
		return f.formatListResult(result)
	case OutputModeCount:
		return f.formatCountResult(result)
	case OutputModeSummary:
		return f.formatSummaryResult(result)
	case OutputModeHealth:
		return f.formatHealthResult(result)
	default:
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: fmt.Sprintf("Unsupported output mode: %s", result.Mode)},
			},
		}
	}
}

// formatListResult formats list mode results
func (f *FindResourcesFormatter) formatListResult(result *FindResourcesResult) MCPResponse {
	resources, ok := result.Data.([]ResourceResult)
	if !ok {
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: "Error: Invalid data format for list mode"},
			},
		}
	}

	var output strings.Builder

	// Header with execution info
	output.WriteString("# Find Resources Results\n\n")
	output.WriteString(fmt.Sprintf("**Found %d resources** (execution time: %dms)\n\n",
		result.Metadata.TotalCount, result.Metadata.ExecutionTime))

	if len(resources) == 0 {
		output.WriteString("No resources found matching the specified criteria.\n")
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: output.String()},
			},
		}
	}

	// Group resources by kind for better display
	kindGroups := f.groupResourcesByKind(resources)

	// Create table for each kind
	for _, kind := range getSortedKinds(kindGroups) {
		resourcesOfKind := kindGroups[kind]
		output.WriteString(fmt.Sprintf("## %s (%d)\n\n", kind, len(resourcesOfKind)))

		// Create markdown table
		output.WriteString("| Name | Namespace | Cluster | Age | Status |\n")
		output.WriteString("|------|-----------|---------|-----|--------|\n")

		for _, resource := range resourcesOfKind {
			name := f.escapeMarkdown(resource.Name)
			namespace := f.formatNamespace(resource.Namespace)
			cluster := f.escapeMarkdown(resource.Cluster)
			age := f.escapeMarkdown(resource.Age)
			status := f.formatStatus(resource.Status)

			output.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				name, namespace, cluster, age, status))
		}

		output.WriteString("\n")
	}

	// Footer with query info
	if result.Metadata.Query != "" {
		output.WriteString("---\n")
		output.WriteString(fmt.Sprintf("*Query executed in %dms*\n", result.Metadata.ExecutionTime))
	}

	return MCPResponse{
		Content: []MCPContent{
			{Type: "text", Text: output.String()},
		},
	}
}

// formatCountResult formats count mode results
func (f *FindResourcesFormatter) formatCountResult(result *FindResourcesResult) MCPResponse {
	counts, ok := result.Data.([]CountResult)
	if !ok {
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: "Error: Invalid data format for count mode"},
			},
		}
	}

	var output strings.Builder

	// Header
	output.WriteString("# Resource Count Analysis\n\n")
	output.WriteString(fmt.Sprintf("**Total Resources: %d** (execution time: %dms)\n\n",
		result.Metadata.TotalCount, result.Metadata.ExecutionTime))

	if len(counts) == 0 {
		output.WriteString("No resources found matching the specified criteria.\n")
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: output.String()},
			},
		}
	}

	// Create count table
	output.WriteString("| Label | Count | Percentage |\n")
	output.WriteString("|-------|-------|------------|\n")

	for _, count := range counts {
		label := f.escapeMarkdown(count.Label)
		percentage := fmt.Sprintf("%.1f%%", count.Percentage)

		output.WriteString(fmt.Sprintf("| %s | %d | %s |\n",
			label, count.Count, percentage))
	}

	return MCPResponse{
		Content: []MCPContent{
			{Type: "text", Text: output.String()},
		},
	}
}

// formatSummaryResult formats summary mode results
func (f *FindResourcesFormatter) formatSummaryResult(result *FindResourcesResult) MCPResponse {
	summary, ok := result.Data.(SummaryResult)
	if !ok {
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: "Error: Invalid data format for summary mode"},
			},
		}
	}

	var output strings.Builder

	// Header with overview
	output.WriteString("# Resource Fleet Summary\n\n")
	output.WriteString(fmt.Sprintf("📊 **Overview** (execution time: %dms)\n", result.Metadata.ExecutionTime))
	output.WriteString(fmt.Sprintf("- **Total Resources:** %d\n", summary.TotalResources))
	output.WriteString(fmt.Sprintf("- **Total Clusters:** %d\n\n", summary.TotalClusters))

	// Resources by Cluster
	if len(summary.ResourcesByCluster) > 0 {
		output.WriteString("## 🖥️ Resources by Cluster\n\n")
		for _, item := range summary.ResourcesByCluster {
			output.WriteString(fmt.Sprintf("- **%s:** %d resources (%.1f%%)\n",
				f.escapeMarkdown(item.Label), item.Count, item.Percentage))
		}
		output.WriteString("\n")
	}

	// Resources by Kind
	if len(summary.ResourcesByKind) > 0 {
		output.WriteString("## 📦 Resources by Type\n\n")
		for _, item := range summary.ResourcesByKind {
			output.WriteString(fmt.Sprintf("- **%s:** %d resources (%.1f%%)\n",
				f.escapeMarkdown(item.Label), item.Count, item.Percentage))
		}
		output.WriteString("\n")
	}

	// Resources by Namespace
	if len(summary.ResourcesByNamespace) > 0 {
		output.WriteString("## 📁 Top Namespaces\n\n")
		for _, item := range summary.ResourcesByNamespace {
			output.WriteString(fmt.Sprintf("- **%s:** %d resources (%.1f%%)\n",
				f.escapeMarkdown(item.Label), item.Count, item.Percentage))
		}
		output.WriteString("\n")
	}

	return MCPResponse{
		Content: []MCPContent{
			{Type: "text", Text: output.String()},
		},
	}
}

// formatHealthResult formats health mode results
func (f *FindResourcesFormatter) formatHealthResult(result *FindResourcesResult) MCPResponse {
	health, ok := result.Data.(HealthResult)
	if !ok {
		return MCPResponse{
			Content: []MCPContent{
				{Type: "text", Text: "Error: Invalid data format for health mode"},
			},
		}
	}

	var output strings.Builder

	// Header with health overview
	output.WriteString("# Fleet Health Analysis\n\n")
	output.WriteString(fmt.Sprintf("🏥 **Overall Health** (execution time: %dms)\n", result.Metadata.ExecutionTime))

	// Calculate percentages
	totalFloat := float64(health.Total)
	var healthyPct, unhealthyPct, unknownPct float64
	if health.Total > 0 {
		healthyPct = float64(health.Healthy) / totalFloat * 100
		unhealthyPct = float64(health.Unhealthy) / totalFloat * 100
		unknownPct = float64(health.Unknown) / totalFloat * 100
	}

	output.WriteString(fmt.Sprintf("- ✅ **Healthy:** %d resources (%.1f%%)\n", health.Healthy, healthyPct))
	output.WriteString(fmt.Sprintf("- ❌ **Unhealthy:** %d resources (%.1f%%)\n", health.Unhealthy, unhealthyPct))
	output.WriteString(fmt.Sprintf("- ❓ **Unknown:** %d resources (%.1f%%)\n", health.Unknown, unknownPct))
	output.WriteString(fmt.Sprintf("- 📊 **Total:** %d resources\n\n", health.Total))

	// Status breakdown table
	if len(health.Details) > 0 {
		output.WriteString("## 📋 Status Breakdown\n\n")
		output.WriteString("| Status | Count | Percentage |\n")
		output.WriteString("|--------|-------|------------|\n")

		for _, detail := range health.Details {
			status := f.escapeMarkdown(detail.Label)
			percentage := fmt.Sprintf("%.1f%%", detail.Percentage)
			icon := f.getStatusIcon(detail.Label)

			output.WriteString(fmt.Sprintf("| %s %s | %d | %s |\n",
				icon, status, detail.Count, percentage))
		}
		output.WriteString("\n")
	}

	// Top issues
	if len(health.TopIssues) > 0 {
		output.WriteString("## 🔴 Top Issues\n\n")
		for i, issue := range health.TopIssues {
			if i >= 5 { // Limit to top 5 for readability
				break
			}
			output.WriteString(fmt.Sprintf("%d. 🚨 %s\n", i+1, f.escapeMarkdown(issue)))
		}
		output.WriteString("\n")
	}

	// Health recommendations
	if health.Unhealthy > 0 {
		output.WriteString("## 💡 Recommendations\n\n")
		output.WriteString("- Review unhealthy resources for immediate action\n")
		output.WriteString("- Check logs and events for failing resources\n")
		if health.Unknown > 0 {
			output.WriteString("- Investigate resources with unknown status\n")
		}
		output.WriteString("\n")
	}

	return MCPResponse{
		Content: []MCPContent{
			{Type: "text", Text: output.String()},
		},
	}
}

// Helper methods

// groupResourcesByKind groups resources by their kind
func (f *FindResourcesFormatter) groupResourcesByKind(resources []ResourceResult) map[string][]ResourceResult {
	groups := make(map[string][]ResourceResult)

	for _, resource := range resources {
		kind := resource.Kind
		if kind == "" {
			kind = "Unknown"
		}
		groups[kind] = append(groups[kind], resource)
	}

	// Sort resources within each group by name
	for kind := range groups {
		sort.Slice(groups[kind], func(i, j int) bool {
			return groups[kind][i].Name < groups[kind][j].Name
		})
	}

	return groups
}

// getSortedKinds returns sorted list of resource kinds
func getSortedKinds(kindGroups map[string][]ResourceResult) []string {
	kinds := make([]string, 0, len(kindGroups))
	for kind := range kindGroups {
		kinds = append(kinds, kind)
	}

	// Sort by count descending, then by name ascending
	sort.Slice(kinds, func(i, j int) bool {
		countI := len(kindGroups[kinds[i]])
		countJ := len(kindGroups[kinds[j]])

		if countI != countJ {
			return countI > countJ // More resources first
		}
		return kinds[i] < kinds[j] // Alphabetical for same count
	})

	return kinds
}

// formatNamespace formats namespace value for display
func (f *FindResourcesFormatter) formatNamespace(namespace *string) string {
	if namespace == nil {
		return "*(cluster-scoped)*"
	}
	if *namespace == "" {
		return "*(cluster-scoped)*"
	}
	return f.escapeMarkdown(*namespace)
}

// formatStatus formats status value for display
func (f *FindResourcesFormatter) formatStatus(status *string) string {
	if status == nil {
		return "-"
	}
	if *status == "" {
		return "-"
	}
	return f.escapeMarkdown(*status)
}

// getStatusIcon returns an appropriate icon for a status
func (f *FindResourcesFormatter) getStatusIcon(status string) string {
	status = strings.ToLower(status)

	switch {
	case strings.Contains(status, "running"):
		return "✅"
	case strings.Contains(status, "completed"):
		return "✅"
	case strings.Contains(status, "succeeded"):
		return "✅"
	case strings.Contains(status, "ready"):
		return "✅"
	case strings.Contains(status, "active"):
		return "✅"
	case strings.Contains(status, "available"):
		return "✅"
	case strings.Contains(status, "failed"):
		return "❌"
	case strings.Contains(status, "error"):
		return "❌"
	case strings.Contains(status, "crashloop"):
		return "❌"
	case strings.Contains(status, "imagepull"):
		return "❌"
	case strings.Contains(status, "pending"):
		return "⏳"
	case strings.Contains(status, "terminating"):
		return "⏳"
	case strings.Contains(status, "unknown"):
		return "❓"
	default:
		return "📊"
	}
}

// escapeMarkdown escapes special markdown characters
func (f *FindResourcesFormatter) escapeMarkdown(text string) string {
	// Escape markdown special characters
	replacer := strings.NewReplacer(
		"|", "\\|",
		"*", "\\*",
		"_", "\\_",
		"`", "\\`",
		"#", "\\#",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"!", "\\!",
		"\n", " ",
		"\r", " ",
		"\t", " ",
	)

	escaped := replacer.Replace(text)

	// Collapse multiple spaces
	for strings.Contains(escaped, "  ") {
		escaped = strings.ReplaceAll(escaped, "  ", " ")
	}

	return strings.TrimSpace(escaped)
}

// formatValue formats any value for safe display in markdown
func (f *FindResourcesFormatter) formatValue(value interface{}) string {
	if value == nil {
		return "NULL"
	}

	switch v := value.(type) {
	case string:
		return f.escapeMarkdown(v)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []interface{}:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = f.formatValue(item)
		}
		return strings.Join(parts, ", ")
	case map[string]interface{}:
		// For complex objects, show a simplified representation
		return "{object}"
	default:
		return f.escapeMarkdown(fmt.Sprintf("%v", v))
	}
}