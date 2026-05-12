package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/pkg/types"
)

// HTTPTransport implements MCP-compliant HTTP transport using mark3labs/mcp-go
type HTTPTransport struct {
	server        *http.Server
	config        *ServerConfig
	mcpServer     *PostgresMCPServer
	mcpLib        *server.MCPServer
	authMiddleware *auth.AuthMiddleware
	requestCount  int64
	errorCount    int64
	streamCount   int64
}

// NewHTTPTransport creates a new MCP-compliant HTTP transport
func NewHTTPTransport(config *ServerConfig) *HTTPTransport {
	return &HTTPTransport{
		config: config,
	}
}

// Start starts the MCP-compliant HTTP transport
func (t *HTTPTransport) Start(ctx context.Context, mcpServer *PostgresMCPServer) error {
	t.mcpServer = mcpServer

	// Enable streaming for HTTP transport
	if mcpServer != nil {
		mcpServer.SetStreamingMode(true)
	}

	// Create MCP server using mark3labs library
	t.mcpLib = server.NewMCPServer(t.config.AppDisplayName, t.config.AppVersion)

	// Remove auto-generated database resources to maintain transport consistency
	t.mcpLib.DeleteResources("database://tables", "database://stats")

	// Register tools using centralized definitions
	if err := t.registerTools(); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}

	// Create AuthConfig directly from ServerConfig values (clean, no duplication)
	authConfig := auth.NewAuthConfigFromServerValues(
		t.config.EnableAuth,
		t.config.AuthTimeout,
		t.config.AuthCacheEnabled,
		t.config.AuthCacheTTL,
		t.config.KubernetesURL,
		t.config.ServiceAccountToken,
		t.config.TokenPath,
		t.config.KubeconfigPath,
		t.config.SkipTLSVerify,
		t.config.DiscoveryTTL,
		t.config.DiscoverySource,
	)

	var err error
	t.authMiddleware, err = auth.NewAuthMiddleware(authConfig, t.mcpServer.dbConn)
	if err != nil {
		return fmt.Errorf("failed to initialize auth middleware: %w", err)
	}

	log.Printf("Authentication middleware initialized: enabled=%t", authConfig.EnableAuth)

	mux := http.NewServeMux()

	// Health and metrics endpoints
	mux.HandleFunc("/health", t.handleHealth)
	mux.HandleFunc("/metrics", t.handleMetrics)

	// Single MCP endpoint (compliant with MCP standard)
	mux.HandleFunc("/mcp", t.handleMCP)

	// Create server with auth and CORS middleware chain
	handler := t.corsMiddleware(mux)
	handler = t.authMiddleware.Handler(handler)

	t.server = &http.Server{
		Addr:              t.config.GetHTTPAddr(),
		Handler:           handler,
		ReadTimeout:       t.config.RequestTimeout,
		WriteTimeout:      t.config.RequestTimeout * 2,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Starting MCP-compliant HTTP transport on %s", t.server.Addr)

	// Start server in a goroutine
	errChan := make(chan error, 1)
	started := make(chan struct{})

	go func() {
		// Signal that we're about to start listening
		close(started)
		if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("HTTP server failed: %w", err)
		} else {
			errChan <- nil
		}
	}()

	// Wait for server to start listening
	<-started
	log.Printf("🚀 MCP Server READY - accepting connections on %s", t.server.Addr)

	// Wait for either context cancellation or server error
	select {
	case <-ctx.Done():
		log.Printf("Context cancelled, shutting down HTTP server...")
		// Gracefully shutdown the server
		err := t.Stop(context.Background())
		log.Printf("HTTP transport shutdown completed with result: %v", err)
		return err
	case err := <-errChan:
		return err
	}
}

// Stop gracefully stops the HTTP transport
func (t *HTTPTransport) Stop(ctx context.Context) error {
	log.Printf("Stopping HTTP transport...")

	// Cleanup auth middleware
	if t.authMiddleware != nil {
		log.Printf("Closing auth middleware...")
		t.authMiddleware.Close()
		log.Printf("Auth middleware closed")
	}

	if t.server != nil {
		log.Printf("Shutting down HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		err := t.server.Shutdown(shutdownCtx)
		if err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		} else {
			log.Printf("HTTP server shutdown complete")
		}
		return err
	}
	log.Printf("HTTP transport stopped")
	return nil
}

// SupportsStreaming returns whether HTTP supports streaming
func (t *HTTPTransport) SupportsStreaming() bool {
	return true
}

// GetName returns the transport name
func (t *HTTPTransport) GetName() string {
	return "http-mcp"
}

// GetStatus returns the current status of the HTTP transport
func (t *HTTPTransport) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"name":               "http-mcp",
		"supports_streaming": true,
		"addr":               t.config.GetHTTPAddr(),
		"requests_processed": atomic.LoadInt64(&t.requestCount),
		"errors":             atomic.LoadInt64(&t.errorCount),
		"stream_requests":    atomic.LoadInt64(&t.streamCount),
		"mcp_compliant":      true,
	}
}

// GetMetrics returns HTTP transport metrics
func (t *HTTPTransport) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"requests_total":     atomic.LoadInt64(&t.requestCount),
		"errors_total":       atomic.LoadInt64(&t.errorCount),
		"stream_requests":    atomic.LoadInt64(&t.streamCount),
		"transport":          "http-mcp",
		"address":            t.config.GetHTTPAddr(),
		"streaming_enabled":  true,
		"max_response_size":  t.config.MaxResponseSize,
		"stream_buffer_size": t.config.StreamBufferSize,
		"mcp_compliant":      true,
	}
}

// registerTools registers tools using centralized definitions
func (t *HTTPTransport) registerTools() error {
	definitions := GetCentralizedToolDefinitions()

	for _, def := range definitions {
		// Create tool with centralized definition
		options := []mcp.ToolOption{mcp.WithDescription(def.Description)}
		options = append(options, def.Options...)
		tool := mcp.NewTool(def.Name, options...)

		// Map to appropriate handler
		var handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		switch def.Name {
		case "find_resources":
			// Create wrapper to adapt the signature and extract user context
			handler = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				userCtx := auth.UserFromContext(ctx)
				return t.handleFindResources(ctx, request, userCtx)
			}
		default:
			log.Printf("Warning: No handler found for tool: %s", def.Name)
			continue
		}

		// Register tool with MCP server
		t.mcpLib.AddTool(tool, handler)
		log.Printf("Registered MCP tool: %s", def.Name)
	}

	return nil
}

// handleMCP handles all MCP protocol messages through a single endpoint
func (t *HTTPTransport) handleMCP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&t.requestCount, 1)

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse JSON-RPC request
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		atomic.AddInt64(&t.errorCount, 1)
		t.sendJSONRPCError(w, nil, -32700, "Parse error", http.StatusBadRequest)
		return
	}

	// Extract request components
	method, ok := request["method"].(string)
	if !ok {
		atomic.AddInt64(&t.errorCount, 1)
		t.sendJSONRPCError(w, request["id"], -32600, "Invalid Request", http.StatusBadRequest)
		return
	}

	requestID := request["id"]
	params, _ := request["params"].(map[string]interface{})

	// Extract user context (set by auth middleware)
	userCtx := auth.UserFromContext(r.Context())


	// Handle different MCP methods
	w.Header().Set("Content-Type", "application/json")

	switch method {
	case "initialize":
		t.handleInitialize(w, requestID, params)
	case "notifications/initialized":
		t.handleNotificationsInitialized(w, requestID)
	case "tools/list":
		t.handleToolsList(w, requestID, userCtx)
	case "tools/call":
		t.handleToolsCall(r.Context(), w, requestID, params, userCtx)
	default:
		t.sendJSONRPCError(w, requestID, -32601, fmt.Sprintf("Method not found: %s", method), http.StatusNotFound)
	}
}

// Tool Handlers (reusing the patterns from STDIO transport)


func (t *HTTPTransport) handleFindResources(ctx context.Context, request mcp.CallToolRequest, userCtx *auth.UserContext) (*mcp.CallToolResult, error) {
	// Parse arguments using shared function (eliminates duplication)
	args, err := ParseFindResourcesArgs(request)
	if err != nil {
		return nil, fmt.Errorf("invalid find_resources arguments: %w", err)
	}

	// Execute find resources with user context for authorization filtering
	result, err := t.mcpServer.findCore.FindResources(ctx, args, userCtx)
	if err != nil {
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

// Helper functions (reused from STDIO transport)

func (t *HTTPTransport) formatQueryResult(result *types.QueryResult) string {
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


// MCP Protocol Handlers

func (t *HTTPTransport) sendJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string, httpStatus int) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	if id != nil {
		response["id"] = id
	}

	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(response)
}

func (t *HTTPTransport) sendJSONRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"result":  result,
	}
	if id != nil {
		response["id"] = id
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (t *HTTPTransport) handleInitialize(w http.ResponseWriter, requestID interface{}, params map[string]interface{}) {
	result := map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{"listChanged": true},
			"streaming": map[string]interface{}{"enabled": true},
		},
		"serverInfo": map[string]interface{}{
			"name":    t.config.AppDisplayName,
			"version": t.config.AppVersion,
		},
		"instructions": t.config.AppDescription,
	}

	t.sendJSONRPCResult(w, requestID, result)
}

func (t *HTTPTransport) handleNotificationsInitialized(w http.ResponseWriter, requestID interface{}) {
	// notifications/initialized is a JSON-RPC notification (not a request)
	// According to JSON-RPC 2.0 spec: "The Server MUST NOT reply to a Notification"
	// Just acknowledge receipt with HTTP 200 and no response body
	w.WriteHeader(http.StatusOK)
	// No JSON response - notifications are fire-and-forget
}

func (t *HTTPTransport) handleToolsList(w http.ResponseWriter, requestID interface{}, userCtx *auth.UserContext) {
	// Get authorized tools based on user context
	authorizedToolNames := auth.GetAuthorizedTools(userCtx)

	log.Printf("[TOOLS] User: %s, Authorized tools: %v",
		getUsernameForLogging(userCtx), authorizedToolNames)

	// Get all tool definitions and filter by authorization
	allDefinitions := GetCentralizedToolDefinitions()
	tools := []map[string]interface{}{}

	// Create a set for faster lookup
	authorizedSet := make(map[string]bool)
	for _, name := range authorizedToolNames {
		authorizedSet[name] = true
	}

	for _, def := range allDefinitions {
		// Only include tools the user is authorized to access
		if !authorizedSet[def.Name] {
			continue
		}

		// Convert tool definition to MCP format
		tool := map[string]interface{}{
			"name":        def.Name,
			"description": def.Description,
		}

		// Use the JSON schema from centralized definition
		if def.JSONSchema != nil {
			tool["inputSchema"] = def.JSONSchema
		}

		tools = append(tools, tool)
	}

	result := map[string]interface{}{
		"tools": tools,
	}

	t.sendJSONRPCResult(w, requestID, result)
}


func (t *HTTPTransport) handleToolsCall(ctx context.Context, w http.ResponseWriter, requestID interface{}, params map[string]interface{}, userCtx *auth.UserContext) {
	if params == nil {
		t.sendJSONRPCError(w, requestID, -32602, "Missing params", http.StatusBadRequest)
		return
	}

	name, ok := params["name"].(string)
	if !ok {
		t.sendJSONRPCError(w, requestID, -32602, "Missing tool name", http.StatusBadRequest)
		return
	}

	// Ensure authentication when auth is enabled
	if t.config.EnableAuth && userCtx == nil {
		log.Printf("[SECURITY] Unauthenticated tool call blocked: tool=%s", name)
		t.sendJSONRPCError(w, requestID, -32001, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Check if user is authorized to call this tool
	authorizedTools := auth.GetAuthorizedTools(userCtx)
	isAuthorized := false
	for _, authorizedTool := range authorizedTools {
		if authorizedTool == name {
			isAuthorized = true
			break
		}
	}

	if !isAuthorized {
		log.Printf("[AUTH] Tool access denied for user %s: tool=%s",
			getUsernameForLogging(userCtx), name)
		t.sendJSONRPCError(w, requestID, -32003,
			fmt.Sprintf("Access denied for tool: %s. Check your permissions.", name),
			http.StatusForbidden)
		return
	}

	log.Printf("[TOOL] Tool call authorized: user=%s, tool=%s",
		getUsernameForLogging(userCtx), name)

	arguments, _ := params["arguments"].(map[string]interface{})
	if arguments == nil {
		arguments = make(map[string]interface{})
	}

	// Create MCP CallToolRequest
	mcpRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: arguments,
		},
	}

	// Execute the tool
	var result *mcp.CallToolResult
	var err error

	switch name {
	case "find_resources":
		result, err = t.handleFindResources(ctx, mcpRequest, userCtx)
	default:
		t.sendJSONRPCError(w, requestID, -32601, fmt.Sprintf("Unknown tool: %s", name), http.StatusNotFound)
		return
	}

	if err != nil {
		t.sendJSONRPCError(w, requestID, -32603, fmt.Sprintf("Tool execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert MCP result to JSON-RPC result
	jsonResult := map[string]interface{}{
		"content": result.Content,
	}
	if result.IsError {
		jsonResult["isError"] = true
	}

	t.sendJSONRPCResult(w, requestID, jsonResult)
}


// Middleware and utility functions

func (t *HTTPTransport) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only set CORS headers if CORS is enabled
		if t.config.EnableCORS {
			origin := r.Header.Get("Origin")

			// Only set CORS headers for cross-origin requests (requests with Origin header)
			if origin != "" {
				// Check if origin is allowed
				if t.isOriginAllowed(origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				} else if len(t.config.AllowedOrigins) == 1 && t.config.AllowedOrigins[0] == "*" {
					// Only set wildcard if explicitly configured as "*"
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else if len(t.config.AllowedOrigins) == 1 && t.config.AllowedOrigins[0] == "*" {
				// For same-origin requests, set wildcard if configured
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			if origin != "" || (len(t.config.AllowedOrigins) == 1 && t.config.AllowedOrigins[0] == "*") {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
		}

		if r.Method == "OPTIONS" {
			if t.config.EnableCORS && t.isOriginAllowed(r.Header.Get("Origin")) {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (t *HTTPTransport) isOriginAllowed(origin string) bool {
	if !t.config.EnableCORS {
		return false
	}

	// Empty origin always allowed (for same-origin requests)
	if origin == "" {
		return true
	}

	for _, allowed := range t.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (t *HTTPTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&t.requestCount, 1)

	if t.mcpServer == nil {
		atomic.AddInt64(&t.errorCount, 1)
		http.Error(w, "Server not initialized", http.StatusInternalServerError)
		return
	}

	health := t.mcpServer.Health(r.Context())

	w.Header().Set("Content-Type", "application/json")

	// Set HTTP status code based on health status
	if health["status"] == "unhealthy" {
		w.WriteHeader(http.StatusInternalServerError)
	}

	responseData := map[string]interface{}{
		"status":        health["status"],
		"transport":     "http-mcp",
		"address":       t.config.GetHTTPAddr(),
		"mcp_compliant": true,
		"health":        health,
	}

	_ = json.NewEncoder(w).Encode(responseData)
}

func (t *HTTPTransport) handleMetrics(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&t.requestCount, 1)

	if t.mcpServer == nil {
		atomic.AddInt64(&t.errorCount, 1)
		http.Error(w, "Server not initialized", http.StatusInternalServerError)
		return
	}

	metrics := t.mcpServer.Metrics(r.Context())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics":   metrics,
		"transport": t.GetMetrics(),
	})
}

// Helper function to safely get username for logging
func getUsernameForLogging(userCtx *auth.UserContext) string {
	if userCtx == nil {
		return "<anonymous>"
	}
	if userCtx.Username == "" {
		return "<unknown>"
	}
	return userCtx.Username
}