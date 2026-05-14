package findresources

import (
	"time"
)

// FindResourcesArgs represents the input arguments for find_resources tool
type FindResourcesArgs struct {
	// Basic filters
	Kind      interface{} `json:"kind,omitempty"`      // string or []string
	Name      string      `json:"name,omitempty"`      // exact name or wildcard pattern
	Namespace interface{} `json:"namespace,omitempty"` // string or []string
	Cluster   interface{} `json:"cluster,omitempty"`   // string or []string

	// Advanced filters
	LabelSelector   string      `json:"labelSelector,omitempty"`   // K8s label selector: "app=nginx,env!=test"
	ClusterSelector string      `json:"clusterSelector,omitempty"` // Labels on clusters: "env=prod,cloud=AWS"
	Status          interface{} `json:"status,omitempty"`          // string or []string - Running, Failed, Pending, etc.
	Compliance      interface{} `json:"compliance,omitempty"`      // string or []string - Compliant, NonCompliant, UnknownCompliancy
	TextSearch      string      `json:"textSearch,omitempty"`      // Search across all text fields

	// Time-based filters
	AgeNewerThan string `json:"ageNewerThan,omitempty"` // "1h", "2d", "1w"
	AgeOlderThan string `json:"ageOlderThan,omitempty"` // "1h", "2d", "1w"

	// Output control
	OutputMode string `json:"outputMode,omitempty"` // "list", "count", "summary", "health" (default: "list")
	GroupBy    string `json:"groupBy,omitempty"`    // "status", "namespace", "cluster", "kind", "label:key"
	CountOnly  bool   `json:"countOnly,omitempty"`  // Return only numbers
	Limit      int    `json:"limit,omitempty"`      // Max results (default 50, max 1000)
	SortBy     string `json:"sortBy,omitempty"`     // "name", "created", "namespace"
	SortOrder  string `json:"sortOrder,omitempty"`  // "asc", "desc" (default: "asc")
}

// FindResourcesResult represents the response from find_resources tool
type FindResourcesResult struct {
	Mode     string      `json:"mode"`
	Data     interface{} `json:"data"` // ResourceResult[], CountResult[], SummaryResult, or HealthResult
	Metadata Metadata    `json:"metadata"`
}

// Metadata provides query execution information
type Metadata struct {
	TotalCount    int               `json:"totalCount"`
	ExecutionTime int64             `json:"executionTime"` // milliseconds
	Query         string            `json:"query"`
	Filters       FindResourcesArgs `json:"filters"`
}

// ResourceResult represents a single resource in list mode
type ResourceResult struct {
	Name      string                 `json:"name"`
	Namespace *string                `json:"namespace,omitempty"` // nil for cluster-scoped
	Kind      string                 `json:"kind"`
	Cluster   string                 `json:"cluster"`
	Age       string                 `json:"age"`      // human-readable age like "1w2d"
	Status    *string                `json:"status,omitempty"`
	Created   *time.Time             `json:"created,omitempty"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Data      map[string]interface{} `json:"data"` // JSON data stored in database
}

// CountResult represents a count entry for count mode
type CountResult struct {
	Label      string  `json:"label"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage,omitempty"`
}

// SummaryResult represents aggregated data for summary mode
type SummaryResult struct {
	TotalResources       int           `json:"totalResources"`
	TotalClusters        int           `json:"totalClusters"`
	ResourcesByCluster   []CountResult `json:"resourcesByCluster"`   // sorted desc by count
	ResourcesByKind      []CountResult `json:"resourcesByKind"`      // sorted desc by count
	ResourcesByNamespace []CountResult `json:"resourcesByNamespace"` // sorted desc by count
}

// HealthResult represents health analysis for health mode
type HealthResult struct {
	Total     int           `json:"total"`
	Healthy   int           `json:"healthy"`
	Unhealthy int           `json:"unhealthy"`
	Unknown   int           `json:"unknown"`
	Details   []CountResult `json:"details"`   // status breakdown
	TopIssues []string      `json:"topIssues"` // top 10 unhealthy issues
}

// QueryComponents represents the parts of a SQL query being built
type QueryComponents struct {
	Conditions []string      `json:"conditions"`
	Params     []interface{} `json:"params"`
	ParamIndex int           `json:"paramIndex"`
}

// FilterResult represents the result of applying a single filter
type FilterResult struct {
	Conditions    []string      `json:"conditions"`
	Params        []interface{} `json:"params"`
	NextParamIndex int           `json:"nextParamIndex"`
}

// OutputMode enumeration
const (
	OutputModeList    = "list"
	OutputModeCount   = "count"
	OutputModeSummary = "summary"
	OutputModeHealth  = "health"
)

// SortOrder enumeration
const (
	SortOrderAsc  = "asc"
	SortOrderDesc = "desc"
)

// Default values
const (
	DefaultOutputMode = OutputModeList
	DefaultLimit      = 50
	MaxLimit          = 1000
	DefaultSortOrder  = SortOrderAsc
)

// Health status categories
const (
	HealthStatusHealthy   = "healthy"
	HealthStatusUnhealthy = "unhealthy"
	HealthStatusUnknown   = "unknown"
)