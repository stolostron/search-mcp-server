package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// ToolDefinition represents a centralized tool definition
type ToolDefinition struct {
	Name        string
	Description string
	Options     []mcp.ToolOption
	JSONSchema  map[string]interface{} // JSON schema for HTTP transport
	Handler     func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// GetCentralizedToolDefinitions returns all tool definitions for the MCP server
// This ensures consistency across all transport types (STDIO, HTTP)
func GetCentralizedToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "find_resources",
			Description: "Find and analyze Kubernetes resources across ACM managed clusters with advanced filtering, counting, and health analysis",
			Options: []mcp.ToolOption{
				// Basic filters
				mcp.WithString("kind",
					mcp.Description("Resource kind (Pod, Deployment, Service, ManagedCluster, etc.)"),
				),
				mcp.WithString("name",
					mcp.Description("Resource name (exact match or shell-style pattern with * and ?)"),
				),
				mcp.WithString("namespace",
					mcp.Description("Namespace name, comma-separated list, or wildcard patterns. Examples: \"default\", \"kube-system,openshift-config\", \"open-cluster-management*\", \"kube-*,default\""),
				),
				mcp.WithString("cluster",
					mcp.Description("Cluster name or comma-separated list. Examples: \"jb-mc-1\", \"local-cluster\", \"jb-mc-1,local-cluster\""),
				),
				// Advanced filters
				mcp.WithString("labelSelector",
					mcp.Description("Kubernetes label selector: \"app=nginx,env!=test\""),
				),
				mcp.WithString("clusterSelector",
					mcp.Description("Filter by cluster labels: \"env=prod,cloud=AWS\""),
				),
				mcp.WithString("status",
					mcp.Description("Status filter: \"Running,Failed\" or \"CrashLoopBackOff\""),
				),
				mcp.WithString("textSearch",
					mcp.Description("Comprehensive text search across: (1) resource names, (2) namespaces, and (3) ALL JSON fields including labels, annotations, status, and nested data. Case-insensitive pattern matching. Examples: \"NonCompliant\" finds non-compliant policies, \"CrashLoopBackOff\" finds failing pods, \"prometheus\" finds monitoring resources. Performance: Slower than specific field filters but searches everything."),
				),
				// Time filters
				mcp.WithString("ageNewerThan",
					mcp.Description("Resources newer than: \"1h\", \"2d\", \"1w\""),
				),
				mcp.WithString("ageOlderThan",
					mcp.Description("Resources older than: \"1h\", \"2d\", \"1w\""),
				),
				// Output control
				mcp.WithString("outputMode",
					mcp.Description("Output format: list=detailed table, count=aggregated counts, summary=overview, health=status focus"),
					mcp.Enum("list", "count", "summary", "health"),
					mcp.DefaultString("list"),
				),
				mcp.WithString("groupBy",
					mcp.Description("Group results by: status, namespace, cluster, kind, or label:key"),
				),
				mcp.WithBoolean("countOnly",
					mcp.Description("Return only count numbers, no details"),
					mcp.DefaultBool(false),
				),
				mcp.WithNumber("limit",
					mcp.Description("Max results for list mode (1-1000)"),
					mcp.DefaultNumber(50),
					mcp.Min(1),
					mcp.Max(1000),
				),
				mcp.WithString("sortBy",
					mcp.Description("Sort by: name, created, namespace, cluster"),
					mcp.DefaultString("name"),
				),
				mcp.WithString("sortOrder",
					mcp.Description("Sort direction"),
					mcp.Enum("asc", "desc"),
					mcp.DefaultString("asc"),
				),
				mcp.WithBoolean("stream",
					mcp.Description("Enable streaming for large result sets (HTTP/SSE only)"),
					mcp.DefaultBool(false),
				),
			},
			JSONSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					// Basic filters
					"kind": map[string]interface{}{
						"type":        "string",
						"description": "Resource kind (Pod, Deployment, Service, ManagedCluster, etc.)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name (exact match or shell-style pattern with * and ?)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace name, comma-separated list, or wildcard patterns. Examples: \"default\", \"kube-system,openshift-config\", \"open-cluster-management*\", \"kube-*,default\"",
					},
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Cluster name or comma-separated list. Examples: \"jb-mc-1\", \"local-cluster\", \"jb-mc-1,local-cluster\"",
					},
					// Advanced filters
					"labelSelector": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes label selector: \"app=nginx,env!=test\"",
					},
					"clusterSelector": map[string]interface{}{
						"type":        "string",
						"description": "Filter by cluster labels: \"env=prod,cloud=AWS\"",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Status filter: \"Running,Failed\" or \"CrashLoopBackOff\"",
					},
					"textSearch": map[string]interface{}{
						"type":        "string",
						"description": "Comprehensive text search across: (1) resource names, (2) namespaces, and (3) ALL JSON fields including labels, annotations, status, and nested data. Case-insensitive pattern matching. Examples: \"NonCompliant\" finds non-compliant policies, \"CrashLoopBackOff\" finds failing pods, \"prometheus\" finds monitoring resources. Performance: Slower than specific field filters but searches everything.",
					},
					// Time filters
					"ageNewerThan": map[string]interface{}{
						"type":        "string",
						"description": "Resources newer than: \"1h\", \"2d\", \"1w\"",
					},
					"ageOlderThan": map[string]interface{}{
						"type":        "string",
						"description": "Resources older than: \"1h\", \"2d\", \"1w\"",
					},
					// Output control
					"outputMode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "count", "summary", "health"},
						"description": "Output format: list=detailed table, count=aggregated counts, summary=overview, health=status focus",
						"default":     "list",
					},
					"groupBy": map[string]interface{}{
						"type":        "string",
						"description": "Group results by: status, namespace, cluster, kind, or label:key",
					},
					"countOnly": map[string]interface{}{
						"type":        "boolean",
						"description": "Return only count numbers, no details",
						"default":     false,
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max results for list mode (1-1000)",
						"default":     50,
						"minimum":     1,
						"maximum":     1000,
					},
					"sortBy": map[string]interface{}{
						"type":        "string",
						"description": "Sort by: name, created, namespace, cluster",
						"default":     "name",
					},
					"sortOrder": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"asc", "desc"},
						"description": "Sort direction",
						"default":     "asc",
					},
					"stream": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable streaming for large result sets (HTTP/SSE only)",
						"default":     false,
					},
				},
			},
		},
	}
}

// GetMCPTools creates mcp.Tool instances from centralized definitions
func GetMCPTools() []mcp.Tool {
	definitions := GetCentralizedToolDefinitions()
	tools := make([]mcp.Tool, len(definitions))

	for i, def := range definitions {
		options := []mcp.ToolOption{mcp.WithDescription(def.Description)}
		options = append(options, def.Options...)
		tools[i] = mcp.NewTool(def.Name, options...)
	}

	return tools
}