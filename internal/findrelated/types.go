package findrelated

// FindRelatedArgs represents the input arguments for find_related_resources tool
type FindRelatedArgs struct {
	UIDs         []string `json:"uids"`
	RelatedKinds []string `json:"relatedKinds,omitempty"`
	MaxHops      int      `json:"maxHops,omitempty"`
	OutputMode   string   `json:"outputMode,omitempty"` // "list" or "count"
	Limit        int      `json:"limit,omitempty"`
}

// FindRelatedResult represents the response from find_related_resources
type FindRelatedResult struct {
	Groups   []RelatedKindGroup `json:"groups"`
	Metadata RelatedMetadata    `json:"metadata"`
}

// RelatedKindGroup represents related resources of a single kind
type RelatedKindGroup struct {
	Kind  string         `json:"kind"`
	Count int            `json:"count"`
	Items []RelatedItem  `json:"items,omitempty"`
}

// RelatedItem represents a single related resource
type RelatedItem struct {
	UID       string                 `json:"uid"`
	Name      string                 `json:"name"`
	Namespace *string                `json:"namespace,omitempty"`
	Kind      string                 `json:"kind"`
	Cluster   string                 `json:"cluster"`
	Data      map[string]interface{} `json:"data"`
}

// RelatedMetadata provides query execution information
type RelatedMetadata struct {
	SeedUIDs      []string `json:"seedUids"`
	MaxHops       int      `json:"maxHops"`
	TotalRelated  int      `json:"totalRelated"`
	ExecutionTime int64    `json:"executionTime"` // milliseconds
}

// Defaults
const (
	DefaultMaxHops       = 1
	ApplicationMaxHops   = 3
	DefaultRelatedLimit  = 200
	MaxRelatedLimit      = 1000
	OutputModeList       = "list"
	OutputModeCount      = "count"
)
