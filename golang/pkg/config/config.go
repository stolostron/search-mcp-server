// Package config provides centralized configuration management for the search MCP server
package config

import (
	"os"
	"strconv"
	"time"
)

// Config represents the application configuration
type Config struct {
	// Database Configuration
	ConnectionString string        `json:"connectionString,omitempty"` // Omitted from JSON for security
	MaxConnections   int32         `json:"maxConnections"`
	IdleTimeout      time.Duration `json:"idleTimeout"`
	ConnectTimeout   time.Duration `json:"connectTimeout"`

	// Security Configuration
	EnableSQLValidation bool   `json:"enableSQLValidation"`
	ValidationLevel     string `json:"validationLevel"`

	// Operational Configuration
	HealthCheckInterval time.Duration `json:"healthCheckInterval"`
	LogLevel           string        `json:"logLevel"`

	// Query Configuration
	DefaultQueryTimeout time.Duration `json:"defaultQueryTimeout"`
	MaxQueryRows       int           `json:"maxQueryRows"`
}

// DefaultConfig returns configuration with sensible defaults for development
func DefaultConfig() Config {
	return Config{
		// Database - matching current hardcoded values
		MaxConnections: 20,
		IdleTimeout:    30 * time.Second,
		ConnectTimeout: 2 * time.Second,

		// Security - current behavior
		EnableSQLValidation: true,
		ValidationLevel:     "standard",

		// Operational
		HealthCheckInterval: 30 * time.Second,
		LogLevel:           "info",

		// Query
		DefaultQueryTimeout: 30 * time.Second,
		MaxQueryRows:       1000,
	}
}

// LoadConfig loads configuration from environment variables with fallback to defaults
func LoadConfig() Config {
	config := DefaultConfig()

	// Database Configuration
	if connStr := os.Getenv("DATABASE_URL"); connStr != "" {
		config.ConnectionString = connStr
	}

	if maxConn := getEnvAsInt32("DB_MAX_CONNECTIONS", config.MaxConnections); maxConn > 0 {
		config.MaxConnections = maxConn
	}

	if idleTimeout := getEnvAsDuration("DB_IDLE_TIMEOUT", config.IdleTimeout); idleTimeout > 0 {
		config.IdleTimeout = idleTimeout
	}

	if connTimeout := getEnvAsDuration("DB_CONNECT_TIMEOUT", config.ConnectTimeout); connTimeout > 0 {
		config.ConnectTimeout = connTimeout
	}

	// Security Configuration
	config.EnableSQLValidation = getEnvAsBool("ENABLE_SQL_VALIDATION", config.EnableSQLValidation)

	if validationLevel := os.Getenv("SQL_VALIDATION_LEVEL"); validationLevel != "" {
		config.ValidationLevel = validationLevel
	}

	// Operational Configuration
	if healthInterval := getEnvAsDuration("HEALTH_CHECK_INTERVAL", config.HealthCheckInterval); healthInterval > 0 {
		config.HealthCheckInterval = healthInterval
	}

	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
	}

	// Query Configuration
	if queryTimeout := getEnvAsDuration("DEFAULT_QUERY_TIMEOUT", config.DefaultQueryTimeout); queryTimeout > 0 {
		config.DefaultQueryTimeout = queryTimeout
	}

	if maxRows := getEnvAsInt("MAX_QUERY_ROWS", config.MaxQueryRows); maxRows > 0 {
		config.MaxQueryRows = maxRows
	}

	return config
}

// LoadConfigWithArgs loads configuration from environment variables and command line arguments
// Command line arguments take precedence over environment variables
func LoadConfigWithArgs(args []string) Config {
	// Start with environment-based configuration
	config := LoadConfig()

	// Override with command line arguments if provided
	if len(args) >= 2 && args[1] != "" {
		config.ConnectionString = args[1]
	}

	return config
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// For now, minimal validation - can be expanded as needed
	if c.MaxConnections <= 0 {
		c.MaxConnections = 1 // Ensure at least one connection
	}

	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 30 * time.Second
	}

	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 2 * time.Second
	}

	return nil
}

// RedactedString returns a string representation with sensitive data redacted
func (c *Config) RedactedString() string {
	redacted := *c
	if redacted.ConnectionString != "" {
		redacted.ConnectionString = "[REDACTED]"
	}
	return ""
}

// Helper functions for environment variable parsing

// getEnvAsString gets environment variable as string with default fallback
func getEnvAsString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets environment variable as int with default fallback
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsInt32 gets environment variable as int32 with default fallback
func getEnvAsInt32(key string, defaultValue int32) int32 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 32); err == nil {
			return int32(intValue)
		}
	}
	return defaultValue
}

// getEnvAsBool gets environment variable as bool with default fallback
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvAsDuration gets environment variable as duration (in seconds) with default fallback
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultValue
}