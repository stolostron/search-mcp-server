package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stolostron/search-mcp-server/internal/findresources"
)

// Transport represents the abstraction for different MCP transports
type Transport interface {
	// Start starts the transport with the given server instance
	Start(ctx context.Context, server *PostgresMCPServer) error

	// Stop gracefully stops the transport
	Stop(ctx context.Context) error

	// SupportsStreaming returns whether this transport supports streaming responses
	SupportsStreaming() bool

	// GetName returns the transport name for logging and identification
	GetName() string

	// GetStatus returns the current status of the transport
	GetStatus() map[string]interface{}

	// GetMetrics returns transport-specific metrics
	GetMetrics() map[string]interface{}
}

// TransportManager handles multiple transport lifecycle and coordination
type TransportManager struct {
	config      *ServerConfig
	server      *PostgresMCPServer // Store server instance for transports
	transports  []Transport
	running     map[string]bool
	errors      map[string]error
	mutex       sync.RWMutex
	stopSignals map[string]context.CancelFunc
}

// NewTransportManager creates a new transport manager
func NewTransportManager(config *ServerConfig) *TransportManager {
	return &TransportManager{
		config:      config,
		transports:  make([]Transport, 0),
		running:     make(map[string]bool),
		errors:      make(map[string]error),
		stopSignals: make(map[string]context.CancelFunc),
	}
}

// RegisterTransports registers transports based on configuration
func (tm *TransportManager) RegisterTransports(server *PostgresMCPServer) error {
	// Store server instance for use in StartAll
	tm.server = server

	switch tm.config.TransportMode {
	case "stdio":
		return tm.registerSTDIOTransport(server)
	case "http":
		return tm.registerHTTPTransport(server)
	case "auto":
		return tm.autoRegisterTransports(server)
	default:
		return fmt.Errorf("unknown transport mode: %s (supported: stdio, http, auto)", tm.config.TransportMode)
	}
}

// registerSTDIOTransport registers only the STDIO transport
func (tm *TransportManager) registerSTDIOTransport(server *PostgresMCPServer) error {
	stdioTransport := NewSTDIOTransport()
	tm.transports = append(tm.transports, stdioTransport)

	// STDIO doesn't support streaming
	server.SetStreamingMode(false)

	log.Printf("Registered STDIO transport")
	return nil
}

// registerHTTPTransport registers the MCP-compliant HTTP transport
func (tm *TransportManager) registerHTTPTransport(server *PostgresMCPServer) error {
	// Register MCP-compliant HTTP transport
	httpTransport := NewHTTPTransport(tm.config)
	tm.transports = append(tm.transports, httpTransport)

	// HTTP supports streaming
	server.SetStreamingMode(true)

	log.Printf("Registered MCP-compliant HTTP transport")
	return nil
}



// autoRegisterTransports automatically detects and registers appropriate transports
func (tm *TransportManager) autoRegisterTransports(server *PostgresMCPServer) error {
	// Check environment to decide transport mode
	if os.Getenv("MCP_HTTP_MODE") != "" || tm.config.HTTPPort != "" {
		// HTTP mode requested
		return tm.registerHTTPTransport(server)
	} else if isRunningInTerminal() {
		// Terminal/CLI mode - use STDIO
		return tm.registerSTDIOTransport(server)
	} else {
		// Default to HTTP for maximum compatibility
		return tm.registerHTTPTransport(server)
	}
}

// StartAll starts all registered transports
func (tm *TransportManager) StartAll(ctx context.Context) error {
	if len(tm.transports) == 0 {
		return fmt.Errorf("no transports registered")
	}

	// For single transport, start synchronously
	if len(tm.transports) == 1 {
		transport := tm.transports[0]
		log.Printf("Starting single transport: %s", transport.GetName())

		tm.mutex.Lock()
		tm.running[transport.GetName()] = true
		tm.mutex.Unlock()

		err := transport.Start(ctx, tm.server)

		tm.mutex.Lock()
		tm.running[transport.GetName()] = false
		if err != nil {
			tm.errors[transport.GetName()] = err
		}
		tm.mutex.Unlock()

		return err
	}

	// For multiple transports, start them concurrently
	return tm.startConcurrent(ctx)
}

// startConcurrent starts multiple transports concurrently
func (tm *TransportManager) startConcurrent(ctx context.Context) error {
	log.Printf("Starting %d transports concurrently", len(tm.transports))

	var wg sync.WaitGroup
	errorCh := make(chan error, len(tm.transports))

	for _, transport := range tm.transports {
		wg.Add(1)
		go func(t Transport) {
			defer wg.Done()

			// Create cancellable context for this transport
			transportCtx, cancel := context.WithCancel(ctx)

			tm.mutex.Lock()
			tm.running[t.GetName()] = true
			tm.stopSignals[t.GetName()] = cancel
			tm.mutex.Unlock()

			log.Printf("Starting transport: %s", t.GetName())
			err := t.Start(transportCtx, tm.server)

			tm.mutex.Lock()
			tm.running[t.GetName()] = false
			if err != nil {
				tm.errors[t.GetName()] = err
				log.Printf("Transport %s failed: %v", t.GetName(), err)
			} else {
				log.Printf("Transport %s stopped gracefully", t.GetName())
			}
			tm.mutex.Unlock()

			if err != nil {
				errorCh <- fmt.Errorf("transport %s failed: %w", t.GetName(), err)
			}
		}(transport)
	}

	// Wait a moment to see if any transport fails immediately
	go func() {
		wg.Wait()
		close(errorCh)
	}()

	// Monitor for errors throughout lifecycle
	errMonitor := make(chan error, 1)
	go func() {
		for err := range errorCh {
			if err != nil {
				select {
				case errMonitor <- err:
				default:
					log.Printf("Additional transport error: %v", err)
				}
			}
		}
	}()

	// Check for immediate failures
	select {
	case err := <-errMonitor:
		return err
	case <-time.After(2 * time.Second):
		log.Printf("All transports started successfully")
	}

	// Continue monitoring for failures during operation
	go func() {
		select {
		case err := <-errMonitor:
			if err != nil {
				log.Printf("Transport failed after startup: %v", err)
				// Trigger shutdown on transport failure
				// Note: We don't cancel the context here as that would affect all transports
				// Instead, we let the caller handle shutdown via context cancellation
			}
		case <-ctx.Done():
		}
	}()

	// Wait for context cancellation or all transports to complete
	<-ctx.Done()

	// Stop all transports
	return tm.StopAll(context.Background())
}

// StopAll stops all running transports
func (tm *TransportManager) StopAll(ctx context.Context) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	var errors []error

	// Cancel all transport contexts
	for name, cancel := range tm.stopSignals {
		log.Printf("Stopping transport: %s", name)
		cancel()
	}

	// Stop each transport explicitly
	for _, transport := range tm.transports {
		if err := transport.Stop(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop %s: %w", transport.GetName(), err))
		}
	}

	// Clear running state
	for name := range tm.running {
		tm.running[name] = false
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping transports: %v", errors)
	}

	return nil
}

// GetStatus returns the status of all transports
func (tm *TransportManager) GetStatus() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	status := make(map[string]interface{})

	for _, transport := range tm.transports {
		name := transport.GetName()
		transportStatus := transport.GetStatus()

		// Add runtime information
		transportStatus["running"] = tm.running[name]
		if err, hasError := tm.errors[name]; hasError {
			transportStatus["error"] = err.Error()
		}

		status[name] = transportStatus
	}

	return status
}

// GetMetrics returns metrics for all transports
func (tm *TransportManager) GetMetrics() map[string]interface{} {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	metrics := make(map[string]interface{})

	for _, transport := range tm.transports {
		metrics[transport.GetName()] = transport.GetMetrics()
	}

	return metrics
}

// GetTransportNames returns the names of all registered transports
func (tm *TransportManager) GetTransportNames() []string {
	names := make([]string, len(tm.transports))
	for i, transport := range tm.transports {
		names[i] = transport.GetName()
	}
	return names
}

// Helper functions

// isRunningInTerminal checks if the process is running in a terminal
func isRunningInTerminal() bool {
	// Check if stdin/stdout are connected to a terminal
	fileInfo, _ := os.Stdin.Stat()
	return fileInfo.Mode()&os.ModeCharDevice != 0
}

// Shared argument parsing functions

// ParseFindResourcesArgs extracts and parses find_resources arguments from MCP request
// This shared function eliminates duplication between HTTP and STDIO transports
func ParseFindResourcesArgs(request mcp.CallToolRequest) (findresources.FindResourcesArgs, error) {
	// Parse all available arguments from centralized definition
	kind := request.GetString("kind", "")
	name := request.GetString("name", "")
	namespace := request.GetString("namespace", "")
	cluster := request.GetString("cluster", "")
	labelSelector := request.GetString("labelSelector", "")
	clusterSelector := request.GetString("clusterSelector", "")
	status := request.GetString("status", "")
	textSearch := request.GetString("textSearch", "")
	ageNewerThan := request.GetString("ageNewerThan", "")
	ageOlderThan := request.GetString("ageOlderThan", "")
	outputMode := request.GetString("outputMode", "list")
	groupBy := request.GetString("groupBy", "")
	countOnly := request.GetBool("countOnly", false)
	limit := request.GetInt("limit", 50)
	sortBy := request.GetString("sortBy", "name")
	sortOrder := request.GetString("sortOrder", "asc")
	stream := request.GetBool("stream", false)

	args := findresources.FindResourcesArgs{
		Limit:     limit,
		CountOnly: countOnly,
	}

	// Basic filters
	if kind != "" {
		args.Kind = kind
	}
	if name != "" {
		args.Name = name
	}
	if namespace != "" {
		args.Namespace = namespace
	}
	if cluster != "" {
		args.Cluster = cluster
	}

	// Advanced filters
	if labelSelector != "" {
		args.LabelSelector = labelSelector
	}
	if clusterSelector != "" {
		args.ClusterSelector = clusterSelector
	}
	if status != "" {
		args.Status = status
	}
	if textSearch != "" {
		args.TextSearch = textSearch
	}

	// Time filters
	if ageNewerThan != "" {
		args.AgeNewerThan = ageNewerThan
	}
	if ageOlderThan != "" {
		args.AgeOlderThan = ageOlderThan
	}

	// Output control
	switch outputMode {
	case "list":
		args.OutputMode = findresources.OutputModeList
	case "count":
		args.OutputMode = findresources.OutputModeCount
	case "summary":
		args.OutputMode = findresources.OutputModeSummary
	case "health":
		args.OutputMode = findresources.OutputModeHealth
	default:
		return args, fmt.Errorf("invalid output mode: %s", outputMode)
	}

	if groupBy != "" {
		args.GroupBy = groupBy
	}

	// Sorting
	if sortBy != "" {
		args.SortBy = sortBy
	}
	if sortOrder != "" {
		args.SortOrder = sortOrder
	}

	// Note: stream parameter is handled at transport level
	// Different transports handle streaming differently
	if stream {
		log.Printf("Streaming requested - handling at transport level")
	}

	return args, nil
}