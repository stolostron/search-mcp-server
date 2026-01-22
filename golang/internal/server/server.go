package server

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/stolostron/search-mcp-server/internal/findresources"
	"github.com/stolostron/search-mcp-server/pkg/config"
	"github.com/stolostron/search-mcp-server/pkg/database"
)

// isVerboseLoggingEnabled checks if verbose server logging is enabled
func isVerboseServerLoggingEnabled() bool {
	return os.Getenv("TEST_MCP_VERBOSE") == "true"
}

// logIfVerboseServer prints debug messages only if verbose logging is enabled
func logIfVerboseServer(format string, args ...interface{}) {
	if isVerboseServerLoggingEnabled() {
		log.Printf("[MCP-SERVER-DEBUG] "+format, args...)
	}
}

// PostgresMCPServer represents the main MCP server with multi-transport support
type PostgresMCPServer struct {
	// Configuration
	config *ServerConfig

	// Database Dependencies
	dbConn    *database.DatabaseConnection
	dbQueries *database.DatabaseQueries

	// Business Logic
	findCore  *findresources.FindResourcesCore
	formatter *findresources.FindResourcesFormatter

	// Transport Management
	transportMgr *TransportManager

	// Runtime State
	streamingEnabled bool
}

// NewPostgresMCPServer creates a new MCP server instance
func NewPostgresMCPServer(databaseURL string) (*PostgresMCPServer, error) {
	// Load server configuration
	serverConfig := LoadServerConfig()
	if databaseURL != "" {
		serverConfig.DatabaseURL = databaseURL
	}

	// Validate configuration
	if err := serverConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid server configuration: %w", err)
	}

	return NewPostgresMCPServerWithConfig(serverConfig)
}

// NewPostgresMCPServerWithConfig creates a new MCP server with custom configuration
func NewPostgresMCPServerWithConfig(serverConfig *ServerConfig) (*PostgresMCPServer, error) {
	// Create database configuration from server config
	dbConfig := config.DefaultConfig()
	dbConfig.ConnectionString = serverConfig.DatabaseURL
	dbConfig.DefaultQueryTimeout = serverConfig.RequestTimeout

	// Initialize database connection
	ctx := context.Background()
	dbConn, err := database.NewDatabaseConnectionWithConfig(ctx, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test database connection
	if !dbConn.TestConnection(ctx) {
		return nil, fmt.Errorf("database connection test failed")
	}

	// Initialize database queries
	dbQueries := database.NewDatabaseQueries(dbConn)

	// Initialize find resources core
	findCore := findresources.NewFindResourcesCore(dbQueries)
	formatter := findresources.NewFindResourcesFormatter()

	// Create server instance
	server := &PostgresMCPServer{
		config:           serverConfig,
		dbConn:           dbConn,
		dbQueries:        dbQueries,
		findCore:         findCore,
		formatter:        formatter,
		streamingEnabled: serverConfig.EnableStreaming,
	}

	// Initialize transport manager
	server.transportMgr = NewTransportManager(serverConfig)

	log.Printf("MCP Server initialized: %s", serverConfig.String())
	return server, nil
}

// Start starts the MCP server with the configured transports
func (s *PostgresMCPServer) Start(ctx context.Context) error {
	log.Printf("Starting MCP server with transport mode: %s", s.config.TransportMode)

	// Register transports based on configuration
	if err := s.transportMgr.RegisterTransports(s); err != nil {
		return fmt.Errorf("failed to register transports: %w", err)
	}

	// Start all registered transports
	log.Printf("Starting transport manager...")
	err := s.transportMgr.StartAll(ctx)
	log.Printf("Transport manager returned with: %v", err)
	return err
}

// Stop gracefully stops the MCP server
func (s *PostgresMCPServer) Stop(ctx context.Context) error {
	log.Printf("Stopping MCP server...")

	// Stop all transports
	log.Printf("Stopping transport manager...")
	if err := s.transportMgr.StopAll(ctx); err != nil {
		log.Printf("Error stopping transports: %v", err)
	} else {
		log.Printf("Transport manager stopped successfully")
	}

	// Close database connection
	log.Printf("Closing database connection...")
	if s.dbConn != nil {
		s.dbConn.Close()
		log.Printf("Database connection closed")
	}

	log.Printf("MCP server stop completed")
	return nil
}

// GetConfig returns the server configuration
func (s *PostgresMCPServer) GetConfig() *ServerConfig {
	return s.config
}

// GetDatabaseQueries returns the database queries instance
func (s *PostgresMCPServer) GetDatabaseQueries() *database.DatabaseQueries {
	return s.dbQueries
}

// GetFindResourcesCore returns the find resources core instance
func (s *PostgresMCPServer) GetFindResourcesCore() *findresources.FindResourcesCore {
	return s.findCore
}

// GetFormatter returns the formatter instance
func (s *PostgresMCPServer) GetFormatter() *findresources.FindResourcesFormatter {
	return s.formatter
}

// SetStreamingMode enables or disables streaming for this server instance
func (s *PostgresMCPServer) SetStreamingMode(enabled bool) {
	s.streamingEnabled = enabled
	log.Printf("Streaming mode: %t", enabled)
}

// IsStreamingEnabled returns whether streaming is enabled
func (s *PostgresMCPServer) IsStreamingEnabled() bool {
	return s.streamingEnabled
}

// GetStreamConfig returns streaming configuration parameters
func (s *PostgresMCPServer) GetStreamConfig() (bufferSize, maxResponseSize int) {
	return s.config.StreamBufferSize, s.config.MaxResponseSize
}

// Health returns the health status of the server
func (s *PostgresMCPServer) Health(ctx context.Context) map[string]interface{} {
	logIfVerboseServer("Health check starting...")

	health := map[string]interface{}{
		"status": "healthy",
		"database": map[string]interface{}{
			"connected": false,
		},
		"transports": s.transportMgr.GetStatus(),
		"configuration": map[string]interface{}{
			"transport_mode":     s.config.TransportMode,
			"streaming_enabled":  s.streamingEnabled,
			"stream_buffer_size": s.config.StreamBufferSize,
			"max_response_size":  s.config.MaxResponseSize,
		},
	}

	// Test database connectivity
	logIfVerboseServer("Testing database connectivity...")
	dbConnected := s.dbConn.TestConnection(ctx)
	logIfVerboseServer("Database connection test result: %t", dbConnected)

	if dbConnected {
		logIfVerboseServer("Database connection successful")
		health["database"].(map[string]interface{})["connected"] = true

		// Get database pool status
		poolStatus := s.dbConn.GetPoolStatus(ctx)
		logIfVerboseServer("Pool status retrieved: %v", poolStatus != nil)
		if poolStatus != nil {
			health["database"].(map[string]interface{})["pool"] = map[string]interface{}{
				"total_connections":     poolStatus.Stats.TotalConnections,
				"idle_connections":      poolStatus.Stats.IdleConnections,
				"acquired_connections":  poolStatus.Stats.AcquiredConnections,
				"max_connections":       poolStatus.Stats.MaxConnections,
			}
			logIfVerboseServer("Pool stats - Total: %d, Idle: %d, Acquired: %d, Max: %d",
				poolStatus.Stats.TotalConnections,
				poolStatus.Stats.IdleConnections,
				poolStatus.Stats.AcquiredConnections,
				poolStatus.Stats.MaxConnections)
		}
	} else {
		logIfVerboseServer("Database connection failed - setting status to unhealthy")
		health["status"] = "unhealthy"
		health["database"].(map[string]interface{})["error"] = "Database connection failed"
	}

	return health
}

// Metrics returns server metrics
func (s *PostgresMCPServer) Metrics(ctx context.Context) map[string]interface{} {
	metrics := map[string]interface{}{
		"server": map[string]interface{}{
			"transport_mode":     s.config.TransportMode,
			"streaming_enabled":  s.streamingEnabled,
			"stream_buffer_size": s.config.StreamBufferSize,
		},
		"database": map[string]interface{}{},
		"transports": s.transportMgr.GetMetrics(),
	}

	// Get database statistics
	if s.dbConn != nil {
		if poolStats := s.dbConn.PoolStats(); poolStats != nil {
			metrics["database"] = map[string]interface{}{
				"pool_stats": map[string]interface{}{
					"max_connections":       poolStats.MaxConnections,
					"min_connections":       poolStats.MinConnections,
					"acquired_connections":  poolStats.AcquiredConnections,
					"idle_connections":      poolStats.IdleConnections,
					"total_connections":     poolStats.TotalConnections,
					"status":                poolStats.Status,
					"average_response_time": poolStats.AverageResponseTime,
				},
			}
		}
	}

	return metrics
}