package server

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ServerConfig holds configuration for the MCP server with multi-transport support
type ServerConfig struct {
	// Database Configuration
	DatabaseURL string `env:"DATABASE_URL"`

	// Transport Configuration
	TransportMode string `env:"MCP_TRANSPORT_MODE" default:"auto"` // auto, stdio, http
	HTTPPort      string `env:"MCP_HTTP_PORT" default:"8080"`
	HTTPHost      string `env:"MCP_HTTP_HOST" default:"0.0.0.0"`

	// Streaming Configuration
	EnableStreaming  bool `env:"MCP_ENABLE_STREAMING" default:"true"`
	StreamBufferSize int  `env:"MCP_STREAM_BUFFER" default:"100"`    // Resources per chunk
	MaxResponseSize  int  `env:"MCP_MAX_RESPONSE_SIZE" default:"1000"` // Max resources before streaming

	// Security Configuration
	EnableCORS     bool     `env:"MCP_ENABLE_CORS" default:"true"`
	AllowedOrigins []string `env:"MCP_ALLOWED_ORIGINS" default:"*"`

	// Authentication Configuration (auto-enabled in Kubernetes environments)
	EnableAuth        bool          `env:"MCP_ENABLE_AUTH"`                    // Smart default: auto-detect K8s
	AuthTimeout       time.Duration `env:"MCP_AUTH_TIMEOUT" default:"5s"`     // Timeout for K8s API calls
	AuthCacheEnabled  bool          `env:"MCP_AUTH_CACHE" default:"true"`     // Cache validated tokens
	AuthCacheTTL      time.Duration `env:"MCP_AUTH_CACHE_TTL" default:"5m"`   // Token cache TTL

	// Discovery Configuration (for resource-to-kind mappings)
	DiscoveryTTL    time.Duration `env:"MCP_DISCOVERY_TTL" default:"5m"`      // Discovery cache TTL
	DiscoverySource string        `env:"MCP_DISCOVERY_SOURCE" default:"database"` // "database" or "kubernetes"

	// Local testing overrides (for non-K8s environments)
	KubernetesURL     string `env:"MCP_K8S_URL"`          // Manual cluster URL
	ServiceAccountToken string `env:"MCP_SA_TOKEN"`        // Direct token value
	TokenPath         string `env:"MCP_SA_TOKEN_PATH"`    // Custom token file
	KubeconfigPath    string `env:"MCP_KUBECONFIG"`       // Use kubeconfig
	SkipTLSVerify     bool   `env:"MCP_K8S_SKIP_TLS"`     // Skip TLS (testing only)

	// Performance Configuration
	EnableCompression bool          `env:"MCP_ENABLE_COMPRESSION" default:"true"`
	RequestTimeout    time.Duration `env:"MCP_REQUEST_TIMEOUT" default:"30s"`
	StreamTimeout     time.Duration `env:"MCP_STREAM_TIMEOUT" default:"300s"`

	// Logging Configuration
	LogLevel         string `env:"LOG_LEVEL" default:"info"`
	EnableMetrics    bool   `env:"MCP_ENABLE_METRICS" default:"true"`
	EnableHealthCheck bool   `env:"MCP_ENABLE_HEALTH" default:"true"`

	// Application Metadata (from Helm Chart)
	AppName        string `env:"APP_NAME" default:"acm-mcp-server"`
	AppDisplayName string `env:"APP_DISPLAY_NAME" default:"MCP Server for Red Hat ACM"`
	AppDescription string `env:"APP_DESCRIPTION" default:"MCP server for ACM search functionality"`
	AppVersion     string `env:"APP_VERSION" default:"0.1.0"`
}

// LoadServerConfig loads configuration from environment variables with defaults
func LoadServerConfig() *ServerConfig {
	config := &ServerConfig{}

	// Database URL (required)
	config.DatabaseURL = getEnvOrDefault("DATABASE_URL", "")

	// Transport Configuration
	config.TransportMode = getEnvOrDefault("MCP_TRANSPORT_MODE", "auto")
	config.HTTPPort = getEnvOrDefault("MCP_HTTP_PORT", "8080")
	config.HTTPHost = getEnvOrDefault("MCP_HTTP_HOST", "0.0.0.0")

	// Streaming Configuration
	config.EnableStreaming = getEnvBoolOrDefault("MCP_ENABLE_STREAMING", true)
	config.StreamBufferSize = getEnvIntOrDefault("MCP_STREAM_BUFFER", 100)
	config.MaxResponseSize = getEnvIntOrDefault("MCP_MAX_RESPONSE_SIZE", 1000)

	// Security Configuration
	config.EnableCORS = getEnvBoolOrDefault("MCP_ENABLE_CORS", true)
	originsStr := getEnvOrDefault("MCP_ALLOWED_ORIGINS", "*")
	if originsStr == "*" {
		config.AllowedOrigins = []string{"*"}
	} else {
		config.AllowedOrigins = strings.Split(originsStr, ",")
	}

	// Authentication Configuration (smart defaults)
	config.EnableAuth = shouldEnableAuth()
	config.AuthTimeout = getEnvDurationOrDefault("MCP_AUTH_TIMEOUT", 5*time.Second)
	config.AuthCacheEnabled = getEnvBoolOrDefault("MCP_AUTH_CACHE", true)
	config.AuthCacheTTL = getEnvDurationOrDefault("MCP_AUTH_CACHE_TTL", 5*time.Minute)

	// Discovery Configuration
	config.DiscoveryTTL = getEnvDurationOrDefault("MCP_DISCOVERY_TTL", 5*time.Minute)
	config.DiscoverySource = getEnvOrDefault("MCP_DISCOVERY_SOURCE", "database")

	// Local testing overrides
	config.KubernetesURL = getEnvOrDefault("MCP_K8S_URL", "")
	config.ServiceAccountToken = getEnvOrDefault("MCP_SA_TOKEN", "")
	config.TokenPath = getEnvOrDefault("MCP_SA_TOKEN_PATH", "")
	config.KubeconfigPath = getEnvOrDefault("MCP_KUBECONFIG", "")
	config.SkipTLSVerify = getEnvBoolOrDefault("MCP_K8S_SKIP_TLS", false)

	// Performance Configuration
	config.EnableCompression = getEnvBoolOrDefault("MCP_ENABLE_COMPRESSION", true)
	config.RequestTimeout = getEnvDurationOrDefault("MCP_REQUEST_TIMEOUT", 30*time.Second)
	config.StreamTimeout = getEnvDurationOrDefault("MCP_STREAM_TIMEOUT", 300*time.Second)

	// Logging Configuration
	config.LogLevel = getEnvOrDefault("LOG_LEVEL", "info")
	config.EnableMetrics = getEnvBoolOrDefault("MCP_ENABLE_METRICS", true)
	config.EnableHealthCheck = getEnvBoolOrDefault("MCP_ENABLE_HEALTH", true)

	// Application Metadata Configuration
	config.AppName = getEnvOrDefault("APP_NAME", "acm-mcp-server")
	config.AppDisplayName = getEnvOrDefault("APP_DISPLAY_NAME", "MCP Server for Red Hat ACM")
	config.AppDescription = getEnvOrDefault("APP_DESCRIPTION", "MCP server for ACM search functionality")
	config.AppVersion = getEnvOrDefault("APP_VERSION", "0.1.0")

	return config
}

// Validate validates the server configuration
func (c *ServerConfig) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	validTransportModes := []string{"auto", "stdio", "http"}
	if !slices.Contains(validTransportModes, c.TransportMode) {
		return fmt.Errorf("invalid transport mode: %s, valid options: %v", c.TransportMode, validTransportModes)
	}

	if c.StreamBufferSize < 1 || c.StreamBufferSize > 10000 {
		return fmt.Errorf("stream buffer size must be between 1 and 10000, got: %d", c.StreamBufferSize)
	}

	if c.MaxResponseSize < 1 || c.MaxResponseSize > 50000 {
		return fmt.Errorf("max response size must be between 1 and 50000, got: %d", c.MaxResponseSize)
	}

	return nil
}

// GetHTTPAddr returns the HTTP server address
func (c *ServerConfig) GetHTTPAddr() string {
	return c.HTTPHost + ":" + c.HTTPPort
}


// String returns a string representation of the config (with secrets redacted)
func (c *ServerConfig) String() string {
	redactedURL := c.DatabaseURL
	if redactedURL != "" && strings.Contains(redactedURL, "@") {
		parts := strings.Split(redactedURL, "@")
		if len(parts) == 2 {
			redactedURL = "[REDACTED]@" + parts[1]
		}
	}

	return fmt.Sprintf("ServerConfig{TransportMode:%s, HTTPAddr:%s, DatabaseURL:%s, Streaming:%t, Auth:%t}",
		c.TransportMode, c.GetHTTPAddr(), redactedURL, c.EnableStreaming, c.EnableAuth)
}

// Helper functions for environment variable parsing

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}

func getEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}


// shouldEnableAuth determines if authentication should be enabled based on environment
func shouldEnableAuth() bool {
	// Explicit setting always wins
	if envValue := os.Getenv("MCP_ENABLE_AUTH"); envValue != "" {
		enabled, _ := strconv.ParseBool(envValue)
		return enabled
	}

	// Smart default: enable auth if running in Kubernetes
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}