// Package types defines core data structures for the ACM Search MCP Server
package types

import "time"

// DatabaseConfig represents PostgreSQL database connection configuration
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSL      bool   `json:"ssl"`
}

// QueryResult represents the result of a database query execution
type QueryResult struct {
	Columns       []string        `json:"columns"`
	Rows          [][]interface{} `json:"rows"`
	RowCount      *int            `json:"rowCount,omitempty"`
	ExecutionTime *int64          `json:"executionTime,omitempty"` // milliseconds
}

// TableInfo represents metadata about a database table
type TableInfo struct {
	TableName string `json:"tableName"`
	Schema    string `json:"schema"`
	RowCount  *int   `json:"rowCount,omitempty"`
	Size      string `json:"size,omitempty"`
}

// ColumnInfo represents metadata about a table column
type ColumnInfo struct {
	ColumnName   string  `json:"columnName"`
	DataType     string  `json:"dataType"`
	IsNullable   bool    `json:"isNullable"`
	DefaultValue *string `json:"defaultValue,omitempty"`
	Description  *string `json:"description,omitempty"`
}

// TableSchema represents the complete schema of a database table
type TableSchema struct {
	TableName   string       `json:"tableName"`
	Schema      string       `json:"schema"`
	Columns     []ColumnInfo `json:"columns"`
	Indexes     []string     `json:"indexes,omitempty"`
	Constraints []string     `json:"constraints,omitempty"`
}

// DatabaseQuery represents a parameterized database query
type DatabaseQuery struct {
	SQL        string        `json:"sql"`
	Parameters []interface{} `json:"parameters,omitempty"`
	Timeout    *int          `json:"timeout,omitempty"` // seconds
}

// QueryOptions represents options for query execution
type QueryOptions struct {
	MaxRows         *int `json:"maxRows,omitempty"`
	Timeout         *int `json:"timeout,omitempty"`         // seconds
	IncludeMetadata bool `json:"includeMetadata,omitempty"`
}

// DatabaseInfo represents general database information
type DatabaseInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Size    string `json:"size"`
}

// DatabaseStats represents database performance statistics
type DatabaseStats struct {
	Connections    int                    `json:"connections"`
	QueriesPerSec  float64                `json:"queriesPerSec"`
	DatabaseSize   string                 `json:"databaseSize"`
	CacheHitRatio  float64                `json:"cacheHitRatio"`
	LastUpdated    time.Time              `json:"lastUpdated"`
	TableSizes     map[string]string      `json:"tableSizes,omitempty"`
	IndexSizes     map[string]string      `json:"indexSizes,omitempty"`
	AdditionalInfo map[string]interface{} `json:"additionalInfo,omitempty"`
}

// ConnectionHealth represents the health status of a database connection
type ConnectionHealth struct {
	IsConnected   bool          `json:"isConnected"`
	ResponseTime  time.Duration `json:"responseTime"`
	LastChecked   time.Time     `json:"lastChecked"`
	ErrorMessage  string        `json:"errorMessage,omitempty"`
	DatabaseInfo  *DatabaseInfo `json:"databaseInfo,omitempty"`
}

// PoolStats represents connection pool statistics and health metrics
type PoolStats struct {
	// Pool Configuration
	MaxConnections    int32 `json:"maxConnections"`
	MinConnections    int32 `json:"minConnections"`

	// Current Usage
	AcquiredConnections   int32 `json:"acquiredConnections"`
	IdleConnections      int32 `json:"idleConnections"`
	TotalConnections     int32 `json:"totalConnections"`

	// Health Indicators
	IsHealthy            bool    `json:"isHealthy"`
	UtilizationPercent   float64 `json:"utilizationPercent"`

	// Timing Information
	LastHealthCheck      time.Time `json:"lastHealthCheck"`
	AverageResponseTime  time.Duration `json:"averageResponseTime,omitempty"`

	// Status Information
	Status               string `json:"status"`
	Warnings             []string `json:"warnings,omitempty"`
}

// PoolStatus represents the overall health and status of the connection pool
type PoolStatus struct {
	// Basic Health
	IsHealthy       bool      `json:"isHealthy"`
	Status          string    `json:"status"` // "healthy", "warning", "critical"
	LastChecked     time.Time `json:"lastChecked"`

	// Pool Statistics
	Stats           PoolStats `json:"stats"`

	// Performance Metrics
	ResponseTime    time.Duration `json:"responseTime"`
	ConnectivityOk  bool         `json:"connectivityOk"`

	// Issues and Warnings
	Issues          []string `json:"issues,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`
}