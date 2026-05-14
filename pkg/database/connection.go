// Package database provides PostgreSQL database connection and query functionality
package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stolostron/search-mcp-server/pkg/config"
	"github.com/stolostron/search-mcp-server/pkg/types"
)

// DatabaseConnection manages PostgreSQL connection pooling and query execution
type DatabaseConnection struct {
	pool   *pgxpool.Pool
	config types.DatabaseConfig
}


// NewDatabaseConnectionWithConfig creates a new database connection using the provided configuration
func NewDatabaseConnectionWithConfig(ctx context.Context, cfg config.Config) (*DatabaseConnection, error) {
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Use connection string from config if provided, otherwise construct from database config
	connectionString := cfg.ConnectionString
	if connectionString == "" {
		return nil, fmt.Errorf("connection string not provided in configuration")
	}

	// Parse the connection string to extract config
	dbConfig, err := parseConnectionString(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool using config values
	poolConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Apply configuration settings
	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MaxConnIdleTime = cfg.IdleTimeout
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	return &DatabaseConnection{
		pool:   pool,
		config: dbConfig,
	}, nil
}

// Connect returns a connection from the pool (equivalent to TypeScript connect method)
func (dc *DatabaseConnection) Connect(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := dc.pool.Acquire(ctx)
	if err != nil {
		// Log without potentially sensitive error details
		log.Printf("Database connection acquisition failed: %v", err)
		return nil, fmt.Errorf("database connection failed")
	}

	// Remove verbose success logging to reduce noise
	return conn, nil
}

// Query executes a SQL query with optional parameters and returns the result
func (dc *DatabaseConnection) Query(ctx context.Context, sql string, args ...interface{}) (*types.QueryResult, error) {
	conn, err := dc.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	// Track execution time (same as TypeScript implementation)
	startTime := time.Now()
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Convert pgx.Rows to QueryResult
	result, err := convertRowsToQueryResult(rows)
	if err != nil {
		return nil, err
	}

	// Add execution time
	executionTime := time.Since(startTime).Milliseconds()
	result.ExecutionTime = &executionTime

	return result, nil
}

// TestConnection verifies database connectivity (equivalent to TypeScript testConnection)
func (dc *DatabaseConnection) TestConnection(ctx context.Context) bool {
	result, err := dc.Query(ctx, "SELECT 1 as test")
	if err != nil {
		// Log without sensitive error details - connection test failures are expected
		log.Printf("Database connectivity test failed")
		return false
	}

	if len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
		if testValue, ok := result.Rows[0][0].(int32); ok && testValue == 1 {
			return true
		}
	}

	return false
}

// GetDatabaseInfo retrieves database metadata (equivalent to TypeScript getDatabaseInfo)
func (dc *DatabaseConnection) GetDatabaseInfo(ctx context.Context) (*types.DatabaseInfo, error) {
	// Get database version
	versionResult, err := dc.Query(ctx, "SELECT version()")
	if err != nil {
		return nil, fmt.Errorf("failed to get database version: %w", err)
	}

	// Get database size
	sizeResult, err := dc.Query(ctx, "SELECT pg_size_pretty(pg_database_size(current_database())) as size")
	if err != nil {
		return nil, fmt.Errorf("failed to get database size: %w", err)
	}

	version := "Unknown"
	if len(versionResult.Rows) > 0 && len(versionResult.Rows[0]) > 0 {
		if v, ok := versionResult.Rows[0][0].(string); ok {
			version = v
		}
	}

	size := "Unknown"
	if len(sizeResult.Rows) > 0 && len(sizeResult.Rows[0]) > 0 {
		if s, ok := sizeResult.Rows[0][0].(string); ok {
			size = s
		}
	}

	return &types.DatabaseInfo{
		Name:    dc.config.Database,
		Version: version,
		Size:    size,
	}, nil
}

// PoolStats returns current connection pool statistics
func (dc *DatabaseConnection) PoolStats() *types.PoolStats {
	if dc.pool == nil {
		return &types.PoolStats{
			Status:          "disconnected",
			IsHealthy:       false,
			LastHealthCheck: time.Now(),
		}
	}

	stat := dc.pool.Stat()
	now := time.Now()

	// Calculate utilization percentage
	utilizationPercent := 0.0
	if stat.MaxConns() > 0 {
		utilizationPercent = float64(stat.AcquiredConns()) / float64(stat.MaxConns()) * 100.0
	}

	// Determine health status and warnings
	isHealthy := true
	var warnings []string
	status := "healthy"

	// Check for high utilization (>80% is warning, >95% is critical)
	if utilizationPercent > 95 {
		status = "critical"
		isHealthy = false
		warnings = append(warnings, "Pool utilization critical (>95%)")
	} else if utilizationPercent > 80 {
		status = "warning"
		warnings = append(warnings, "Pool utilization high (>80%)")
	}

	// Check for no available connections
	if stat.AcquiredConns() >= stat.MaxConns() {
		status = "critical"
		isHealthy = false
		warnings = append(warnings, "Pool fully saturated - no available connections")
	}

	return &types.PoolStats{
		MaxConnections:      stat.MaxConns(),
		MinConnections:      0, // pgx doesn't expose MinConns in Stat
		AcquiredConnections: stat.AcquiredConns(),
		IdleConnections:     stat.IdleConns(),
		TotalConnections:    stat.TotalConns(),
		IsHealthy:           isHealthy,
		UtilizationPercent:  utilizationPercent,
		LastHealthCheck:     now,
		Status:              status,
		Warnings:            warnings,
	}
}

// IsHealthy checks if the connection pool is healthy and operational
func (dc *DatabaseConnection) IsHealthy(ctx context.Context) bool {
	if dc.pool == nil {
		return false
	}

	// Check basic connectivity with a quick query
	connectivityOk := dc.TestConnection(ctx)
	if !connectivityOk {
		log.Printf("Pool health check failed: connectivity test failed")
		return false
	}

	// Get pool statistics
	stats := dc.PoolStats()
	if !stats.IsHealthy {
		log.Printf("Pool health check failed: %s", stats.Status)
		if len(stats.Warnings) > 0 {
			log.Printf("Pool warnings: %v", stats.Warnings)
		}
		return false
	}

	return true
}

// GetPoolStatus returns comprehensive pool health and status information
func (dc *DatabaseConnection) GetPoolStatus(ctx context.Context) *types.PoolStatus {
	now := time.Now()

	// Test connectivity and measure response time
	start := time.Now()
	connectivityOk := dc.TestConnection(ctx)
	responseTime := time.Since(start)

	// Get pool statistics
	stats := dc.PoolStats()

	// Determine overall status
	overallStatus := "healthy"
	isHealthy := stats.IsHealthy && connectivityOk
	issues := make([]string, 0)
	recommendations := make([]string, 0)

	if !connectivityOk {
		overallStatus = "critical"
		isHealthy = false
		issues = append(issues, "Database connectivity failed")
		recommendations = append(recommendations, "Check database server availability and network connectivity")
	}

	if !stats.IsHealthy {
		if overallStatus != "critical" {
			overallStatus = stats.Status
		}
		isHealthy = false
		issues = append(issues, stats.Warnings...)
	}

	// Add performance recommendations
	if stats.UtilizationPercent > 70 {
		recommendations = append(recommendations, "Consider increasing max connections if performance is impacted")
	}

	if responseTime > 100*time.Millisecond {
		recommendations = append(recommendations, "Database response time is elevated - check database performance")
	}

	// Health recommendations
	if stats.AcquiredConnections > 0 && stats.IdleConnections == 0 {
		recommendations = append(recommendations, "No idle connections available - consider optimizing connection usage")
	}

	return &types.PoolStatus{
		IsHealthy:       isHealthy,
		Status:          overallStatus,
		LastChecked:     now,
		Stats:           *stats,
		ResponseTime:    responseTime,
		ConnectivityOk:  connectivityOk,
		Issues:          issues,
		Recommendations: recommendations,
	}
}

// LogPoolStatus logs current pool status for monitoring purposes
func (dc *DatabaseConnection) LogPoolStatus(ctx context.Context) {
	status := dc.GetPoolStatus(ctx)

	log.Printf("Connection Pool Status: %s (healthy=%t)", status.Status, status.IsHealthy)
	log.Printf("Pool Stats: %d/%d connections acquired, %d idle, %.1f%% utilization",
		status.Stats.AcquiredConnections,
		status.Stats.MaxConnections,
		status.Stats.IdleConnections,
		status.Stats.UtilizationPercent)
	log.Printf("Response Time: %v, Connectivity: %t", status.ResponseTime, status.ConnectivityOk)

	if len(status.Issues) > 0 {
		log.Printf("Pool Issues: %v", status.Issues)
	}

	if len(status.Recommendations) > 0 {
		log.Printf("Pool Recommendations: %v", status.Recommendations)
	}
}

// Close closes the connection pool (equivalent to TypeScript close)
func (dc *DatabaseConnection) Close() {
	if dc.pool != nil {
		dc.pool.Close()
	}
}

// GetConfig returns a copy of the database configuration (equivalent to TypeScript getConfig)
func (dc *DatabaseConnection) GetConfig() types.DatabaseConfig {
	return dc.config
}

// parseConnectionString parses a PostgreSQL connection string and returns a DatabaseConfig
func parseConnectionString(connectionString string) (types.DatabaseConfig, error) {
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return types.DatabaseConfig{}, fmt.Errorf("invalid connection string: %w", err)
	}

	port := 5432 // default PostgreSQL port
	if parsedURL.Port() != "" {
		if p, err := strconv.Atoi(parsedURL.Port()); err == nil {
			port = p
		}
	}

	// Parse SSL configuration from query parameters
	ssl := false
	if sslmode := parsedURL.Query().Get("sslmode"); sslmode == "require" {
		ssl = true
	}
	if sslParam := parsedURL.Query().Get("ssl"); sslParam == "true" {
		ssl = true
	}

	// Extract password (handle empty password case)
	password := ""
	if parsedURL.User != nil {
		password, _ = parsedURL.User.Password()
	}

	// Extract username (handle empty username case)
	username := ""
	if parsedURL.User != nil {
		username = parsedURL.User.Username()
	}

	// Extract database name (remove leading slash)
	database := strings.TrimPrefix(parsedURL.Path, "/")

	return types.DatabaseConfig{
		Host:     parsedURL.Hostname(),
		Port:     port,
		Database: database,
		User:     username,
		Password: password,
		SSL:      ssl,
	}, nil
}

// convertRowsToQueryResult converts pgx.Rows to types.QueryResult
func convertRowsToQueryResult(rows pgx.Rows) (*types.QueryResult, error) {
	// Get column descriptions
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = fd.Name
	}

	// Read all rows
	var resultRows [][]interface{}
	var rowCount int

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to read row values: %w", err)
		}
		resultRows = append(resultRows, values)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	return &types.QueryResult{
		Columns:  columns,
		Rows:     resultRows,
		RowCount: &rowCount,
	}, nil
}