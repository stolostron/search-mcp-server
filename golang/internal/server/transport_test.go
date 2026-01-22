package server

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTransport is a mock implementation of Transport interface
type MockTransport struct {
	mock.Mock
}

func (m *MockTransport) Start(ctx context.Context, server *PostgresMCPServer) error {
	args := m.Called(ctx, server)
	return args.Error(0)
}

func (m *MockTransport) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTransport) SupportsStreaming() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockTransport) GetName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTransport) GetStatus() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockTransport) GetMetrics() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func TestNewTransportManager(t *testing.T) {
	config := &ServerConfig{
		TransportMode: "auto",
		HTTPPort:     "8080",
	}

	tm := NewTransportManager(config)

	assert.NotNil(t, tm)
	assert.Equal(t, config, tm.config)
	assert.Empty(t, tm.transports)
	assert.NotNil(t, tm.running)
	assert.NotNil(t, tm.errors)
	assert.NotNil(t, tm.stopSignals)
}

func TestTransportManager_RegisterTransports(t *testing.T) {
	tests := []struct {
		name          string
		transportMode string
		expectError   bool
	}{
		{
			name:          "stdio mode",
			transportMode: "stdio",
			expectError:   false,
		},
		{
			name:          "http mode",
			transportMode: "http",
			expectError:   false,
		},
		{
			name:          "sse mode (deprecated)",
			transportMode: "sse",
			expectError:   true,
		},
		{
			name:          "auto mode",
			transportMode: "auto",
			expectError:   false,
		},
		{
			name:          "invalid mode",
			transportMode: "invalid",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ServerConfig{
				TransportMode: tt.transportMode,
				HTTPPort:     "8080",
			}

			tm := NewTransportManager(config)
			server := &PostgresMCPServer{}

			err := tm.RegisterTransports(server)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.transportMode != "invalid" {
					assert.NotEmpty(t, tm.transports)
				}
			}
		})
	}
}

func TestTransportManager_GetTransportNames(t *testing.T) {
	config := &ServerConfig{
		TransportMode: "stdio",
	}

	tm := NewTransportManager(config)
	server := &PostgresMCPServer{}

	// Register transports
	err := tm.RegisterTransports(server)
	assert.NoError(t, err)

	names := tm.GetTransportNames()
	assert.Contains(t, names, "stdio")
}

func TestTransportManager_GetStatus(t *testing.T) {
	mockTransport := &MockTransport{}
	mockTransport.On("GetName").Return("mock")
	mockTransport.On("GetStatus").Return(map[string]interface{}{
		"name":    "mock",
		"running": true,
	})

	config := &ServerConfig{}
	tm := NewTransportManager(config)
	tm.transports = []Transport{mockTransport}

	status := tm.GetStatus()

	assert.NotNil(t, status)
	assert.Contains(t, status, "mock")
	mockTransport.AssertExpectations(t)
}

func TestTransportManager_GetMetrics(t *testing.T) {
	mockTransport := &MockTransport{}
	mockTransport.On("GetName").Return("mock")
	mockTransport.On("GetMetrics").Return(map[string]interface{}{
		"requests_total": 10,
		"transport":      "mock",
	})

	config := &ServerConfig{}
	tm := NewTransportManager(config)
	tm.transports = []Transport{mockTransport}

	metrics := tm.GetMetrics()

	assert.NotNil(t, metrics)
	assert.Contains(t, metrics, "mock")
	mockTransport.AssertExpectations(t)
}

func TestIsRunningInTerminal(t *testing.T) {
	// This test will vary based on test environment
	// Just check that it returns a boolean without error
	result := isRunningInTerminal()
	assert.IsType(t, true, result)
}

func TestTransportManager_AutoRegisterTransports(t *testing.T) {
	// Save original env
	originalHTTPMode := os.Getenv("MCP_HTTP_MODE")
	defer func() {
		if originalHTTPMode == "" {
			_ = os.Unsetenv("MCP_HTTP_MODE")
		} else {
			_ = os.Setenv("MCP_HTTP_MODE", originalHTTPMode)
		}
	}()

	tests := []struct {
		name         string
		httpModeEnv  string
		expectedType string
	}{
		{
			name:         "http mode env set",
			httpModeEnv:  "1",
			expectedType: "http", // Should register HTTP transport
		},
		{
			name:         "no http mode env",
			httpModeEnv:  "",
			expectedType: "auto", // Should auto-detect
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.httpModeEnv == "" {
				_ = os.Unsetenv("MCP_HTTP_MODE")
			} else {
				_ = os.Setenv("MCP_HTTP_MODE", tt.httpModeEnv)
			}

			config := &ServerConfig{
				TransportMode: "auto",
				HTTPPort:     "8080",
			}

			tm := NewTransportManager(config)
			server := &PostgresMCPServer{}

			err := tm.autoRegisterTransports(server)
			assert.NoError(t, err)

			// Verify transports were registered
			assert.NotEmpty(t, tm.transports)
		})
	}
}