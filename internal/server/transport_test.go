package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

// TestHandleHealth_NilServer verifies that a nil mcpServer yields HTTP 500
// and {"status":"degraded"} with no internal details.
func TestHandleHealth_NilServer(t *testing.T) {
	transport := &HTTPTransport{
		config: &ServerConfig{HTTPHost: "0.0.0.0", HTTPPort: "8080"},
		// mcpServer intentionally left nil
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	transport.handleHealth(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var body map[string]string
	err := json.NewDecoder(rr.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, "degraded", body["status"])

	// Must not leak internal details
	assert.Len(t, body, 1, "response must contain only 'status'")
}

// TestHandleHealth_ContentType verifies the Content-Type header is set correctly
// for all health responses regardless of server state.
func TestHandleHealth_ContentType(t *testing.T) {
	transport := &HTTPTransport{
		config: &ServerConfig{HTTPHost: "0.0.0.0", HTTPPort: "8080"},
		// mcpServer nil — easiest path that avoids a live DB
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	transport.handleHealth(rr, req)

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

// TestHandleHealth_ResponseShape verifies the exact JSON shape of the response,
// ensuring no internal details (pool stats, config, transport info) are disclosed.
// Uses nil mcpServer to avoid needing a live database connection.
func TestHandleHealth_ResponseShape(t *testing.T) {
	transport := &HTTPTransport{
		config: &ServerConfig{HTTPHost: "0.0.0.0", HTTPPort: "8080"},
		// mcpServer nil — triggers the degraded path, sufficient to verify shape
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	transport.handleHealth(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))

	assert.Equal(t, "degraded", body["status"])
	assert.Len(t, body, 1, "response must contain only the 'status' key")

	// Explicit checks for fields that must NOT appear (SAR-08)
	for _, forbidden := range []string{
		"transport", "address", "mcp_compliant", "health",
		"database", "configuration", "transports",
		"stream_buffer_size", "max_response_size",
	} {
		assert.NotContains(t, body, forbidden,
			"field %q must not be disclosed to unauthenticated callers", forbidden)
	}
}

// stubHealthChecker is a test double for healthChecker that returns a fixed status.
type stubHealthChecker struct {
	status string
}

func (s *stubHealthChecker) Health(_ context.Context) map[string]interface{} {
	return map[string]interface{}{"status": s.status}
}

// TestHandleHealth_OKBranch verifies that a healthy server returns HTTP 200
// and {"status":"ok"} with no internal details.
func TestHandleHealth_OKBranch(t *testing.T) {
	transport := &HTTPTransport{
		config:         &ServerConfig{HTTPHost: "0.0.0.0", HTTPPort: "8080"},
		healthOverride: &stubHealthChecker{status: "healthy"},
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	transport.handleHealth(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))

	assert.Equal(t, "ok", body["status"])
	assert.Len(t, body, 1, "response must contain only 'status'")

	// Verify no internal details are present
	for _, forbidden := range []string{
		"transport", "address", "mcp_compliant", "health",
		"database", "configuration", "transports",
	} {
		assert.NotContains(t, body, forbidden)
	}
}

// TestHandleHealth_DegradedBranch verifies that an unhealthy server returns HTTP 500
// and {"status":"degraded"} with no internal details.
func TestHandleHealth_DegradedBranch(t *testing.T) {
	transport := &HTTPTransport{
		config:         &ServerConfig{HTTPHost: "0.0.0.0", HTTPPort: "8080"},
		healthOverride: &stubHealthChecker{status: "unhealthy"},
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	transport.handleHealth(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))

	assert.Equal(t, "degraded", body["status"])
	assert.Len(t, body, 1, "response must contain only 'status'")
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

			// Verify the expected transport type was registered
			if tt.expectedType == "http" {
				found := false
				for _, tr := range tm.transports {
					if tr.GetName() == "http-mcp" {
						found = true
						break
					}
				}
				assert.True(t, found, "expected http-mcp transport to be registered")
			}
		})
	}
}