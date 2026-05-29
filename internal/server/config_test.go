package server

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoadServerConfig(t *testing.T) {
	// Save current env vars and restore after test
	originalEnv := map[string]string{
		"MCP_TRANSPORT_MODE":     os.Getenv("MCP_TRANSPORT_MODE"),
		"MCP_HTTP_PORT":          os.Getenv("MCP_HTTP_PORT"),
		"MCP_ENABLE_STREAMING":   os.Getenv("MCP_ENABLE_STREAMING"),
		"MCP_STREAM_BUFFER":      os.Getenv("MCP_STREAM_BUFFER"),
		"MCP_MAX_RESPONSE_SIZE":  os.Getenv("MCP_MAX_RESPONSE_SIZE"),
		"MCP_REQUEST_TIMEOUT":    os.Getenv("MCP_REQUEST_TIMEOUT"),
		"MCP_STREAM_TIMEOUT":     os.Getenv("MCP_STREAM_TIMEOUT"),
	}

	defer func() {
		for key, value := range originalEnv {
			if value == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, value)
			}
		}
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		expected ServerConfig
	}{
		{
			name: "default config",
			envVars: map[string]string{},
			expected: ServerConfig{
				TransportMode:     "auto",
				HTTPHost:         "0.0.0.0",
				HTTPPort:         "8080",
				DatabaseURL:      "postgresql://postgres:password@localhost:5432/search?sslmode=disable",
				EnableStreaming:  true,
				StreamBufferSize: 100,
				MaxResponseSize:  1000,
				RequestTimeout:   30 * time.Second,
				StreamTimeout:    300 * time.Second,
				EnableCORS:       true,
				AllowedOrigins:   []string{"*"},
			},
		},
		{
			name: "custom config",
			envVars: map[string]string{
				"MCP_TRANSPORT_MODE":    "http",
				"MCP_HTTP_PORT":         "9000",
				"MCP_ENABLE_STREAMING":  "false",
				"MCP_STREAM_BUFFER":     "200",
				"MCP_MAX_RESPONSE_SIZE": "2000",
				"MCP_REQUEST_TIMEOUT":   "60s",
				"MCP_STREAM_TIMEOUT":    "600s",
			},
			expected: ServerConfig{
				TransportMode:     "http",
				HTTPHost:         "0.0.0.0",
				HTTPPort:         "9000",
				DatabaseURL:      "postgresql://postgres:password@localhost:5432/search?sslmode=disable",
				EnableStreaming:  false,
				StreamBufferSize: 200,
				MaxResponseSize:  2000,
				RequestTimeout:   60 * time.Second,
				StreamTimeout:    600 * time.Second,
				EnableCORS:       true,
				AllowedOrigins:   []string{"*"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			for key := range originalEnv {
				_ = os.Unsetenv(key)
			}

			// Set test env vars
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value)
			}

			config := LoadServerConfig()

			assert.Equal(t, tt.expected.TransportMode, config.TransportMode)
			assert.Equal(t, tt.expected.HTTPHost, config.HTTPHost)
			assert.Equal(t, tt.expected.HTTPPort, config.HTTPPort)
			assert.Equal(t, tt.expected.EnableStreaming, config.EnableStreaming)
			assert.Equal(t, tt.expected.StreamBufferSize, config.StreamBufferSize)
			assert.Equal(t, tt.expected.MaxResponseSize, config.MaxResponseSize)
			assert.Equal(t, tt.expected.RequestTimeout, config.RequestTimeout)
			assert.Equal(t, tt.expected.StreamTimeout, config.StreamTimeout)
		})
	}
}

func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServerConfig{
				TransportMode:     "auto",
				DatabaseURL:      "postgresql://user:pass@host:5432/db",
				StreamBufferSize: 100,
				MaxResponseSize:  1000,
			},
			wantErr: false,
		},
		{
			name: "invalid transport mode",
			config: ServerConfig{
				TransportMode: "invalid",
				DatabaseURL:   "postgresql://user:pass@host:5432/db",
			},
			wantErr: true,
		},
		{
			name: "empty database URL",
			config: ServerConfig{
				TransportMode: "auto",
				DatabaseURL:   "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServerConfig_GetHTTPAddr(t *testing.T) {
	config := &ServerConfig{
		HTTPHost: "127.0.0.1",
		HTTPPort: "8080",
	}
	assert.Equal(t, "127.0.0.1:8080", config.GetHTTPAddr())
}


func TestServerConfig_String(t *testing.T) {
	config := &ServerConfig{
		TransportMode: "http",
		HTTPHost:     "0.0.0.0",
		HTTPPort:     "8080",
		DatabaseURL:  "postgresql://user:pass@host:5432/db",
		EnableStreaming: true,
	}

	result := config.String()
	assert.Contains(t, result, "TransportMode:http")
	assert.Contains(t, result, "HTTPAddr:0.0.0.0:8080")
	assert.Contains(t, result, "DatabaseURL:[REDACTED]@host:5432/db")
	assert.Contains(t, result, "Streaming:true")
}