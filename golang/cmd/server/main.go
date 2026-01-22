package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/stolostron/search-mcp-server/internal/server"
	"github.com/stolostron/search-mcp-server/pkg/config"
)

func main() {
	// Load configuration from environment variables and command line arguments
	cfg := config.LoadConfigWithArgs(os.Args)

	// Validate that database URL is provided
	if cfg.ConnectionString == "" {
		printUsage()
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Create MCP server
	mcpServer, err := server.NewPostgresMCPServer(cfg.ConnectionString)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal, stopping server...")
		cancel()
	}()

	// Print startup information
	serverConfig := mcpServer.GetConfig()
	log.Printf("=== ACM Search MCP Server ===")
	log.Printf("Version: 1.0.0")
	log.Printf("Transport Mode: %s", serverConfig.TransportMode)
	log.Printf("Database: %s", redactDatabaseURL(cfg.ConnectionString))
	log.Printf("Streaming Enabled: %t", mcpServer.IsStreamingEnabled())

	if serverConfig.TransportMode != "stdio" {
		if serverConfig.TransportMode == "http" {
			log.Printf("HTTP Server: %s", serverConfig.GetHTTPAddr())
		}
	}

	log.Printf("Starting server...")

	// Start the MCP server
	if err := mcpServer.Start(ctx); err != nil {
		log.Fatalf("Server failed: %v", err)
	}

	// Explicitly stop the server for cleanup
	if err := mcpServer.Stop(context.Background()); err != nil {
		log.Printf("Error during server cleanup: %v", err)
	}

	log.Printf("Server stopped gracefully")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `ACM Search MCP Server

Usage:
  %s <database_url>

Arguments:
  database_url    PostgreSQL connection string (required)
                  Example: postgresql://user:password@host:port/database

Environment Variables:
  DATABASE_URL              Database connection string (alternative to argument)
  MCP_TRANSPORT_MODE        Transport mode: auto, stdio, http (default: auto)
  MCP_HTTP_PORT             HTTP server port (default: 8080)
  MCP_ENABLE_STREAMING      Enable streaming for large datasets (default: true)
  MCP_STREAM_BUFFER         Resources per chunk (default: 100)
  MCP_MAX_RESPONSE_SIZE     Max resources before streaming (default: 1000)
  LOG_LEVEL                 Log level: debug, info, warn, error (default: info)

Examples:
  # STDIO mode (for Claude Desktop)
  %s "postgresql://user:pass@localhost:5432/search"

  # HTTP mode
  MCP_TRANSPORT_MODE=http %s "postgresql://user:pass@localhost:5432/search"

  # Using environment variable
  export DATABASE_URL="postgresql://user:pass@localhost:5432/search"
  export MCP_TRANSPORT_MODE="http"
  %s

For more information, see: https://github.com/stolostron/search-mcp-server
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

func redactDatabaseURL(url string) string {
	if url == "" {
		return "[NOT SET]"
	}

	// Find @ symbol to split user info from host
	atIndex := -1
	for i, char := range url {
		if char == '@' {
			atIndex = i
			break
		}
	}

	if atIndex == -1 {
		// No credentials in URL
		return url
	}

	// Find the start of user info (after ://)
	protocolEnd := 0
	if protocolIndex := findSubstring(url, "://"); protocolIndex != -1 {
		protocolEnd = protocolIndex + 3
	}

	// Redact the credentials part
	prefix := url[:protocolEnd]
	suffix := url[atIndex:]

	return prefix + "[REDACTED]" + suffix
}

func findSubstring(str, substr string) int {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}