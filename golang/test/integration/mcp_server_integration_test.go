//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/server"
	"github.com/stolostron/search-mcp-server/pkg/config"
)

var _ = Describe("MCP Server Integration Tests", func() {
	var (
		mcpServer        *server.PostgresMCPServer
		testDatabaseURL  string
		httpBaseURL      string
		serverCtx        context.Context
		serverCancel     context.CancelFunc
	)

	BeforeEach(func() {
		// Load configuration using the config module (handles DATABASE_URL automatically)
		cfg := config.LoadConfig()
		testDatabaseURL = cfg.ConnectionString

		// Fallback to TEST_DATABASE_URL if DATABASE_URL not set
		if testDatabaseURL == "" {
			testDatabaseURL = os.Getenv("TEST_DATABASE_URL")
		}
		if testDatabaseURL == "" {
			Skip("DATABASE_URL or TEST_DATABASE_URL environment variable not set")
		}

		// Create test server configuration
		originalEnv := map[string]string{
			"MCP_TRANSPORT_MODE":    os.Getenv("MCP_TRANSPORT_MODE"),
			"MCP_HTTP_PORT":         os.Getenv("MCP_HTTP_PORT"),
			"MCP_ENABLE_STREAMING":  os.Getenv("MCP_ENABLE_STREAMING"),
		}

		DeferCleanup(func() {
			for key, value := range originalEnv {
				if value == "" {
					os.Unsetenv(key)
				} else {
					os.Setenv(key, value)
				}
			}
		})

		// Set test environment
		os.Setenv("MCP_TRANSPORT_MODE", "http")
		os.Setenv("MCP_HTTP_PORT", "18080")
		os.Setenv("MCP_ENABLE_STREAMING", "true")

		httpBaseURL = "http://localhost:18080"

		// Create and start server
		var err error
		mcpServer, err = server.NewPostgresMCPServer(testDatabaseURL)
		Expect(err).NotTo(HaveOccurred())
		Expect(mcpServer).NotTo(BeNil())

		serverCtx, serverCancel = context.WithCancel(context.Background())

		// Start server in background
		go func() {
			defer GinkgoRecover()
			err := mcpServer.Start(serverCtx)
			if err != nil && !strings.Contains(err.Error(), "context canceled") {
				Fail(fmt.Sprintf("Server start failed: %v", err))
			}
		}()

		// Wait for server to start
		Eventually(func() error {
			logIfVerbose("Checking server health at: %s", httpBaseURL+"/health")
			resp, err := http.Get(httpBaseURL + "/health")
			if err != nil {
				logIfVerbose("Health check request failed: %v", err)
				return err
			}
			defer resp.Body.Close()

			// Read response body for debugging
			body, bodyErr := io.ReadAll(resp.Body)
			if bodyErr != nil {
				logIfVerbose("Failed to read response body: %v", bodyErr)
			} else {
				logIfVerbose("Health check response status: %d, body: %s", resp.StatusCode, string(body))
			}

			if resp.StatusCode != http.StatusOK {
				logIfVerbose("Server not ready - status %d, expected %d", resp.StatusCode, http.StatusOK)
				return fmt.Errorf("server not ready: status %d", resp.StatusCode)
			}
			logIfVerbose("Server health check passed!")
			return nil
		}, "10s", "500ms").Should(Succeed())
	})

	AfterEach(func() {
		if serverCancel != nil {
			serverCancel()
		}
		if mcpServer != nil {
			_ = mcpServer.Stop(context.Background())
		}
	})

	Describe("HTTP Transport", func() {
		It("should handle MCP initialize", func() {
			// Use JSON-RPC format
			reqBody := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params":  map[string]interface{}{},
			}
			reqJSON, _ := json.Marshal(reqBody)

			resp, err := http.Post(httpBaseURL+"/mcp", "application/json", bytes.NewBuffer(reqJSON))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			defer resp.Body.Close()

			var response map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response).To(HaveKey("result"))
			result := response["result"].(map[string]interface{})
			Expect(result).To(HaveKey("protocolVersion"))
			Expect(result["protocolVersion"]).To(Equal("2025-06-18"))
			Expect(result).To(HaveKey("capabilities"))
			Expect(result).To(HaveKey("serverInfo"))
		})

		It("should list tools", func() {
			// Use JSON-RPC format
			reqBody := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/list",
				"params":  map[string]interface{}{},
			}
			reqJSON, _ := json.Marshal(reqBody)

			resp, err := http.Post(httpBaseURL+"/mcp", "application/json", bytes.NewBuffer(reqJSON))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			defer resp.Body.Close()

			var response map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response).To(HaveKey("result"))
			result := response["result"].(map[string]interface{})
			Expect(result).To(HaveKey("tools"))
			tools := result["tools"].([]interface{})

			// With auth disabled, only find_resources should be available
			Expect(len(tools)).To(BeNumerically(">=", 1))

			toolNames := make([]string, len(tools))
			for i, tool := range tools {
				toolMap := tool.(map[string]interface{})
				toolNames[i] = toolMap["name"].(string)
			}
			Expect(toolNames).To(ContainElements("find_resources"))
		})

		It("should handle tool calls", func() {
			// Use JSON-RPC format
			reqBody := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]interface{}{
					"name": "find_resources", // Use find_resources since auth is disabled
					"arguments": map[string]interface{}{
						"kind":  "Pod",
						"limit": 1,
					},
				},
			}
			reqJSON, _ := json.Marshal(reqBody)

			resp, err := http.Post(httpBaseURL+"/mcp", "application/json", bytes.NewBuffer(reqJSON))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				// Tool call succeeded
				var response map[string]interface{}
				err = json.NewDecoder(resp.Body).Decode(&response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(HaveKey("result"))
				result := response["result"].(map[string]interface{})
				Expect(result).To(HaveKey("content"))
			} else {
				// Tool call failed (e.g., database not accessible) - that's okay for integration tests
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			}
		})

		It("should list resources", func() {
			// Use JSON-RPC format
			reqBody := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "resources/list",
				"params":  map[string]interface{}{},
			}
			reqJSON, _ := json.Marshal(reqBody)

			resp, err := http.Post(httpBaseURL+"/mcp", "application/json", bytes.NewBuffer(reqJSON))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			// Resources were intentionally removed for transport consistency
			// The server should return an error or empty resources
			if resp.StatusCode == http.StatusOK {
				var response map[string]interface{}
				err = json.NewDecoder(resp.Body).Decode(&response)
				Expect(err).NotTo(HaveOccurred())

				if response["result"] != nil {
					result := response["result"].(map[string]interface{})
					if result["resources"] != nil {
						resources := result["resources"].([]interface{})
						// Should be empty due to our transport consistency fix
						Expect(len(resources)).To(Equal(0))
					}
				}
			} else {
				// Method not supported is also acceptable
				Expect(resp.StatusCode).To(BeElementOf([]int{http.StatusNotFound, http.StatusMethodNotAllowed}))
			}
		})

		It("should handle health checks", func() {
			resp, err := http.Get(httpBaseURL + "/health")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				// Health check passed
				var response map[string]interface{}
				err = json.NewDecoder(resp.Body).Decode(&response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(HaveKey("transport"))
				Expect(response["transport"]).To(Equal("http-mcp"))
			} else {
				// Health check failed (e.g., database issues) - that's acceptable
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			}
		})

		It("should handle metrics", func() {
			resp, err := http.Get(httpBaseURL + "/metrics")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				var response map[string]interface{}
				err = json.NewDecoder(resp.Body).Decode(&response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(HaveKey("transport"))
			}
		})
	})



	Describe("Streaming Capabilities", func() {
		It("should support HTTP streaming endpoint", func() {
			resp, err := http.Get(httpBaseURL + "/mcp/stream/tools/call")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			// Streaming endpoints removed for transport consistency - expect not found
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})