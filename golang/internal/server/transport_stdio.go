package server

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stolostron/search-mcp-server/pkg/types"
)

// STDIOTransport implements MCP-compliant STDIO transport using mark3labs/mcp-go
type STDIOTransport struct {
	server      *server.MCPServer
	mcpServer   *PostgresMCPServer
	requestCount int64
	errorCount  int64
}

// NewSTDIOTransport creates a new MCP-compliant STDIO transport
func NewSTDIOTransport() *STDIOTransport {
	return &STDIOTransport{}
}

// Start starts the STDIO transport (blocking)
func (t *STDIOTransport) Start(ctx context.Context, mcpServer *PostgresMCPServer) error {
	t.mcpServer = mcpServer

	// Create MCP server
	serverConfig := mcpServer.GetConfig()
	t.server = server.NewMCPServer(serverConfig.AppDisplayName, serverConfig.AppVersion)

	// Register MCP tools
	if err := t.registerTools(); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}

	// STDIO doesn't support streaming - ensure it's disabled
	if mcpServer != nil {
		mcpServer.SetStreamingMode(false)
	}

	log.Printf("Starting STDIO transport...")

	// Start the STDIO server (this blocks)
	return server.ServeStdio(t.server)
}

// Stop gracefully stops the STDIO transport
func (t *STDIOTransport) Stop(ctx context.Context) error {
	// Note: STDIO transport doesn't have a graceful shutdown mechanism
	// The server will stop when the stdio connection is closed
	return nil
}

// SupportsStreaming returns whether STDIO supports streaming (it doesn't)
func (t *STDIOTransport) SupportsStreaming() bool {
	return false
}

// GetName returns the transport name
func (t *STDIOTransport) GetName() string {
	return "stdio"
}

// GetStatus returns the current status of the STDIO transport
func (t *STDIOTransport) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"name":               "stdio",
		"supports_streaming": false,
		"protocol":           "MCP",
		"requests_processed": t.requestCount,
		"errors":             t.errorCount,
	}
}

// GetMetrics returns STDIO transport metrics
func (t *STDIOTransport) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"requests_total": t.requestCount,
		"errors_total":   t.errorCount,
		"transport":      "stdio",
	}
}

// registerTools registers MCP tools using centralized definitions
func (t *STDIOTransport) registerTools() error {
	// Get centralized tool definitions
	definitions := GetCentralizedToolDefinitions()

	// Register each tool
	for _, def := range definitions {
		// Create tool with centralized definition
		options := []mcp.ToolOption{mcp.WithDescription(def.Description)}
		options = append(options, def.Options...)
		tool := mcp.NewTool(def.Name, options...)

		// Map to appropriate handler
		var handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		switch def.Name {
		case "find_resources":
			handler = t.handleFindResources
		default:
			log.Printf("Warning: No handler found for tool: %s", def.Name)
			continue
		}

		// Register tool with server
		t.server.AddTool(tool, handler)
		log.Printf("Registered MCP tool: %s", def.Name)
	}

	return nil
}

// Tool Handlers


func (t *STDIOTransport) handleFindResources(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	t.requestCount++

	// Parse arguments using shared function (eliminates duplication)
	args, err := ParseFindResourcesArgs(request)
	if err != nil {
		t.errorCount++
		return nil, fmt.Errorf("invalid find_resources arguments: %w", err)
	}

	// Execute find resources (STDIO transport has no auth middleware, so userCtx is nil)
	result, err := t.mcpServer.findCore.FindResources(ctx, args, nil)
	if err != nil {
		t.errorCount++
		return nil, fmt.Errorf("find_resources execution failed: %w", err)
	}

	// Format result using the formatter
	formatted := t.mcpServer.formatter.FormatResult(result)

	// Convert the formatted result to MCP format
	if len(formatted.Content) > 0 {
		return mcp.NewToolResultText(formatted.Content[0].Text), nil
	}

	return mcp.NewToolResultText("No results found"), nil
}

// Helper functions

func (t *STDIOTransport) formatQueryResult(result *types.QueryResult) string {
	if result == nil {
		return "No results"
	}

	rowCount := 0
	if result.RowCount != nil {
		rowCount = *result.RowCount
	}

	output := fmt.Sprintf("Query executed successfully.\nRows returned: %d\n", rowCount)

	if len(result.Columns) > 0 {
		output += fmt.Sprintf("Columns: %v\n", result.Columns)
	}

	if len(result.Rows) > 0 && len(result.Rows) <= 10 {
		output += "\nFirst few rows:\n"
		for i, row := range result.Rows {
			if i >= 5 { // Show max 5 rows
				output += "...\n"
				break
			}
			output += fmt.Sprintf("Row %d: %v\n", i+1, row)
		}
	}

	if result.ExecutionTime != nil {
		output += fmt.Sprintf("\nExecution time: %d ms", *result.ExecutionTime)
	}

	return output
}

