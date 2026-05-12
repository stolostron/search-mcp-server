package findresources

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/internal/utils"
	"github.com/stolostron/search-mcp-server/pkg/database"
	"github.com/stolostron/search-mcp-server/pkg/types"
	pkgutils "github.com/stolostron/search-mcp-server/pkg/utils"
)

// FindResourcesCore implements the main find_resources logic
type FindResourcesCore struct {
	dbQueries *database.DatabaseQueries
}

// NewFindResourcesCore creates a new instance of FindResourcesCore
func NewFindResourcesCore(dbQueries *database.DatabaseQueries) *FindResourcesCore {
	return &FindResourcesCore{
		dbQueries: dbQueries,
	}
}

// FindResources is the main entry point for the find_resources tool
func (f *FindResourcesCore) FindResources(ctx context.Context, args FindResourcesArgs, userCtx *auth.UserContext) (*FindResourcesResult, error) {
	startTime := time.Now()

	// Step 1: Validate arguments
	if err := f.validateArgs(args); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Step 2: Normalize arguments with defaults
	normalizedArgs := f.normalizeArgs(args)

	// Step 3: Find matching clusters if clusterSelector is specified
	var targetClusters []string
	var err error
	if normalizedArgs.ClusterSelector != "" {
		targetClusters, err = f.findMatchingClusters(ctx, normalizedArgs.ClusterSelector)
		if err != nil {
			return nil, fmt.Errorf("cluster selector failed: %w", err)
		}
		// If clusterSelector returns no matches, return empty result
		if len(targetClusters) == 0 {
			return f.createEmptyResult(normalizedArgs), nil
		}
	}

	// Step 4: Build authorized SQL query with user permissions
	query, err := f.buildAuthorizedQuery(normalizedArgs, targetClusters, userCtx)
	if err != nil {
		return nil, fmt.Errorf("authorized query building failed: %w", err)
	}

	// Step 5: Execute query
	queryResult, err := f.dbQueries.ExecuteQuery(ctx, query.SQL, query.Params, &types.QueryOptions{
		Timeout: &[]int{30}[0], // 30 second timeout
	})
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Step 6: Process results based on output mode
	processedData, err := f.processResults(queryResult, normalizedArgs)
	if err != nil {
		return nil, fmt.Errorf("result processing failed: %w", err)
	}

	// Step 7: Create final result
	executionTime := time.Since(startTime).Milliseconds()

	// Handle RowCount which might be a pointer
	totalCount := 0
	if queryResult.RowCount != nil {
		totalCount = *queryResult.RowCount
	}

	result := &FindResourcesResult{
		Mode: normalizedArgs.OutputMode,
		Data: processedData,
		Metadata: Metadata{
			TotalCount:    totalCount,
			ExecutionTime: executionTime,
			Query:         query.SQL,
			Filters:       normalizedArgs,
		},
	}

	return result, nil
}

// validateArgs validates the input arguments
func (f *FindResourcesCore) validateArgs(args FindResourcesArgs) error {
	// Validate label selector if provided
	if args.LabelSelector != "" {
		if err := pkgutils.ValidateLabelSelector(args.LabelSelector); err != nil {
			return fmt.Errorf("invalid labelSelector: %w", err)
		}
	}

	// Validate cluster selector if provided
	if args.ClusterSelector != "" {
		if err := pkgutils.ValidateLabelSelector(args.ClusterSelector); err != nil {
			return fmt.Errorf("invalid clusterSelector: %w", err)
		}
	}

	// Validate time filters
	if args.AgeNewerThan != "" {
		if err := pkgutils.ValidateDuration(args.AgeNewerThan); err != nil {
			return fmt.Errorf("invalid ageNewerThan: %w", err)
		}
	}
	if args.AgeOlderThan != "" {
		if err := pkgutils.ValidateDuration(args.AgeOlderThan); err != nil {
			return fmt.Errorf("invalid ageOlderThan: %w", err)
		}
	}

	// Validate output mode
	if args.OutputMode != "" {
		validModes := []string{OutputModeList, OutputModeCount, OutputModeSummary, OutputModeHealth}
		valid := false
		for _, mode := range validModes {
			if args.OutputMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid outputMode: %s, must be one of: %v", args.OutputMode, validModes)
		}
	}

	// Validate sort order
	if args.SortOrder != "" {
		if args.SortOrder != SortOrderAsc && args.SortOrder != SortOrderDesc {
			return fmt.Errorf("invalid sortOrder: %s, must be 'asc' or 'desc'", args.SortOrder)
		}
	}

	// Validate limit
	if args.Limit != 0 {
		if args.Limit < 1 || args.Limit > MaxLimit {
			return fmt.Errorf("invalid limit: %d, must be between 1 and %d", args.Limit, MaxLimit)
		}
	}

	return nil
}

// normalizeArgs sets default values and normalizes the arguments
func (f *FindResourcesCore) normalizeArgs(args FindResourcesArgs) FindResourcesArgs {
	normalized := args

	// Set defaults
	if normalized.OutputMode == "" {
		normalized.OutputMode = DefaultOutputMode
	}
	if normalized.Limit == 0 {
		normalized.Limit = DefaultLimit
	}
	if normalized.SortOrder == "" {
		normalized.SortOrder = DefaultSortOrder
	}
	if normalized.GroupBy == "" && normalized.OutputMode == OutputModeCount {
		normalized.GroupBy = "status"
	}

	return normalized
}

// findMatchingClusters finds clusters matching the cluster selector
func (f *FindResourcesCore) findMatchingClusters(ctx context.Context, clusterSelector string) ([]string, error) {
	return pkgutils.FindMatchingClusters(ctx, clusterSelector, f.dbQueries)
}

// createEmptyResult creates an empty result for the given arguments
func (f *FindResourcesCore) createEmptyResult(args FindResourcesArgs) *FindResourcesResult {
	var data interface{}

	switch args.OutputMode {
	case OutputModeList:
		data = []ResourceResult{}
	case OutputModeCount:
		data = []CountResult{}
	case OutputModeSummary:
		data = SummaryResult{
			TotalResources:       0,
			TotalClusters:        0,
			ResourcesByCluster:   []CountResult{},
			ResourcesByKind:      []CountResult{},
			ResourcesByNamespace: []CountResult{},
		}
	case OutputModeHealth:
		data = HealthResult{
			Total:     0,
			Healthy:   0,
			Unhealthy: 0,
			Unknown:   0,
			Details:   []CountResult{},
			TopIssues: []string{},
		}
	}

	return &FindResourcesResult{
		Mode: args.OutputMode,
		Data: data,
		Metadata: Metadata{
			TotalCount:    0,
			ExecutionTime: 0,
			Query:         "",
			Filters:       args,
		},
	}
}

// QueryPlan represents a complete SQL query with parameters
type QueryPlan struct {
	SQL    string
	Params []interface{}
}

// buildQuery constructs the SQL query based on the arguments
func (f *FindResourcesCore) buildQuery(args FindResourcesArgs, targetClusters []string) (*QueryPlan, error) {
	// Initialize SQL builder for WHERE conditions
	sqlBuilder := utils.NewSQLBuilder(1) // Start with parameter index 1

	// Base SELECT clause (without FROM - that's added later)
	var selectClause string
	if args.OutputMode == OutputModeList {
		selectClause = "SELECT uid, cluster, data"
	} else {
		// For aggregation modes, we still need all data for processing
		selectClause = "SELECT uid, cluster, data"
	}

	// Build WHERE conditions using utility modules

	// 1. Kind filter
	if args.Kind != nil {
		err := f.buildKindConditions(args.Kind, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("kind filter failed: %w", err)
		}
	}

	// 2. Name filter
	if args.Name != "" {
		err := f.buildNameConditions(args.Name, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("name filter failed: %w", err)
		}
	}

	// 3. Namespace filter
	if args.Namespace != nil {
		err := f.buildNamespaceConditions(args.Namespace, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("namespace filter failed: %w", err)
		}
	}

	// 4. Cluster filter (combine explicit clusters with targetClusters from clusterSelector)
	clusterList := f.combineClusterFilters(args.Cluster, targetClusters)
	if len(clusterList) > 0 {
		err := f.buildClusterConditions(clusterList, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("cluster filter failed: %w", err)
		}
	}

	// 5. Label selector filter
	if args.LabelSelector != "" {
		err := f.buildLabelConditions(args.LabelSelector, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("label selector filter failed: %w", err)
		}
	}

	// 6. Status filter
	if args.Status != nil {
		err := f.buildStatusConditions(args.Status, args.Kind, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("status filter failed: %w", err)
		}
	}

	// 7. Compliance filter (for Policy resources)
	if args.Compliance != nil {
		err := f.buildComplianceConditions(args.Compliance, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("compliance filter failed: %w", err)
		}
	}

	// 8. Text search filter
	if args.TextSearch != "" {
		err := f.buildTextSearchConditions(args.TextSearch, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("text search filter failed: %w", err)
		}
	}

	// 9. Time filters
	if args.AgeNewerThan != "" || args.AgeOlderThan != "" {
		err := f.buildTimeConditions(args.AgeNewerThan, args.AgeOlderThan, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("time filter failed: %w", err)
		}
	}

	// Build final SQL query
	whereClause, params := sqlBuilder.BuildConditions()

	// Construct the complete SQL query
	var sqlQuery strings.Builder
	sqlQuery.WriteString(selectClause)
	sqlQuery.WriteString(" FROM search.resources")

	if whereClause != "" {
		sqlQuery.WriteString(" WHERE ")
		sqlQuery.WriteString(whereClause)
	}

	// Add ORDER BY clause for list mode
	if args.OutputMode == OutputModeList {
		orderBy := f.buildOrderByClause(args.SortBy, args.SortOrder)
		sqlQuery.WriteString(" ORDER BY ")
		sqlQuery.WriteString(orderBy)

		// Add LIMIT clause
		sqlQuery.WriteString(fmt.Sprintf(" LIMIT %d", args.Limit))
	}

	return &QueryPlan{
		SQL:    sqlQuery.String(),
		Params: params,
	}, nil
}

// buildAuthorizedQuery builds a SQL query with authorization filters applied first
func (f *FindResourcesCore) buildAuthorizedQuery(args FindResourcesArgs, targetClusters []string, userCtx *auth.UserContext) (*QueryPlan, error) {
	// Initialize SQL builder for WHERE conditions
	sqlBuilder := utils.NewSQLBuilder(1) // Start with parameter index 1

	// STEP 1: Apply authorization filters FIRST (before user-requested filters)
	if userCtx != nil {
		// When auth is enabled (HTTP with auth), QueryFilters must exist
		if userCtx.QueryFilters == nil {
			// SECURITY: Auth was enabled but permission resolution failed
			// Deny access rather than falling back to unrestricted access
			return &QueryPlan{
				SQL:    "SELECT uid, cluster, data FROM search.resources WHERE FALSE", // No results
				Params: []interface{}{},
			}, nil
		}

		// Apply granular RBAC authorization filters
		if err := f.applyAuthorizationFilters(userCtx.QueryFilters, args.Kind, sqlBuilder); err != nil {
			return nil, fmt.Errorf("authorization filter failed: %w", err)
		}
	} else {
		// No user context - operate without authorization restrictions
		// SECURITY NOTE: This occurs when auth is disabled or no user context provided
		// Common scenarios: STDIO transport, auth disabled, or direct pod access
		log.Printf("[AUTHZ-INFO] No user context provided - operating without RBAC filtering")
	}

	// Base SELECT clause (without FROM - that's added later)
	var selectClause string
	if args.OutputMode == OutputModeList {
		selectClause = "SELECT uid, cluster, data"
	} else {
		// For aggregation modes, we still need all data for processing
		selectClause = "SELECT uid, cluster, data"
	}

	// STEP 2: Apply user-requested filters (using existing logic from buildQuery)

	// 1. Kind filter
	if args.Kind != nil {
		err := f.buildKindConditions(args.Kind, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("kind filter failed: %w", err)
		}
	}

	// 2. Name filter
	if args.Name != "" {
		err := f.buildNameConditions(args.Name, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("name filter failed: %w", err)
		}
	}

	// 3. Namespace filter
	if args.Namespace != nil {
		err := f.buildNamespaceConditions(args.Namespace, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("namespace filter failed: %w", err)
		}
	}

	// 4. Cluster filter (combine explicit clusters with targetClusters from clusterSelector)
	clusterList := f.combineClusterFilters(args.Cluster, targetClusters)
	if len(clusterList) > 0 {
		err := f.buildClusterConditions(clusterList, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("cluster filter failed: %w", err)
		}
	}

	// 5. Label selector filter
	if args.LabelSelector != "" {
		err := f.buildLabelConditions(args.LabelSelector, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("label selector filter failed: %w", err)
		}
	}

	// 6. Status filter
	if args.Status != nil {
		err := f.buildStatusConditions(args.Status, args.Kind, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("status filter failed: %w", err)
		}
	}

	// 7. Compliance filter (for Policy resources)
	if args.Compliance != nil {
		err := f.buildComplianceConditions(args.Compliance, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("compliance filter failed: %w", err)
		}
	}

	// 8. Text search filter
	if args.TextSearch != "" {
		err := f.buildTextSearchConditions(args.TextSearch, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("text search filter failed: %w", err)
		}
	}

	// 9. Time filters
	if args.AgeNewerThan != "" || args.AgeOlderThan != "" {
		err := f.buildTimeConditions(args.AgeNewerThan, args.AgeOlderThan, sqlBuilder)
		if err != nil {
			return nil, fmt.Errorf("time filter failed: %w", err)
		}
	}

	// Build complete SQL query
	var sqlQuery strings.Builder
	sqlQuery.WriteString(selectClause)
	sqlQuery.WriteString(" FROM search.resources")

	// Add WHERE clause if there are conditions
	whereClause, params := sqlBuilder.BuildConditions()
	if whereClause != "" {
		sqlQuery.WriteString(" WHERE ")
		sqlQuery.WriteString(whereClause)
	}

	// Add ORDER BY clause for list mode
	if args.OutputMode == OutputModeList {
		orderBy := f.buildOrderByClause(args.SortBy, args.SortOrder)
		sqlQuery.WriteString(" ORDER BY ")
		sqlQuery.WriteString(orderBy)

		// Add LIMIT clause
		sqlQuery.WriteString(fmt.Sprintf(" LIMIT %d", args.Limit))
	}

	return &QueryPlan{
		SQL:    sqlQuery.String(),
		Params: params,
	}, nil
}

// applyAuthorizationFilters applies user authorization filters using direct mapping to prevent Cartesian products
func (f *FindResourcesCore) applyAuthorizationFilters(filters *auth.QueryFilters, kindFilter interface{}, builder *utils.SQLBuilder) error {
	if len(filters.PermissionSources) == 0 {
		// No permissions means no access
		builder.AddCondition("1 = 0") // Always false condition
		return nil
	}

	// Build OR conditions for each permission source (mirrors search-v2-api approach)
	var sourceConditions []string
	var allParams []interface{}

	for i, source := range filters.PermissionSources {
		log.Printf("[RBAC-DEBUG] Building SQL for permission source %d: %s", i, source.Source)

		// Generate permissions using direct namespace→resource mapping (NO Cartesian products)
		var sourcePermissions []string
		var sourceParams []interface{}

		// Handle cluster-scoped resources
		if len(source.ClusterScopedKinds) > 0 {
			clusterCondition, clusterParams := f.buildClusterScopedConditions(source, kindFilter, filters.HubClusterName)
			if clusterCondition != "" {
				sourcePermissions = append(sourcePermissions, clusterCondition)
				sourceParams = append(sourceParams, clusterParams...)
			}
		}

		// Handle namespaced resources with explicit namespace→resource pairing
		if len(source.NamespacedKinds) > 0 {
			namespaceConditions, namespaceParams := f.buildNamespacedConditions(source, kindFilter, filters.HubClusterName)
			if len(namespaceConditions) > 0 {
				sourcePermissions = append(sourcePermissions, namespaceConditions...)
				sourceParams = append(sourceParams, namespaceParams...)
			}
		}

		// Combine all permissions for this source with OR logic
		if len(sourcePermissions) > 0 {
			sourceCondition := "(" + strings.Join(sourcePermissions, " OR ") + ")"
			sourceConditions = append(sourceConditions, sourceCondition)
			allParams = append(allParams, sourceParams...)
			log.Printf("[RBAC-DEBUG] Source %d SQL: %s", i, sourceCondition)
		}
	}

	// Combine all permission sources with OR
	if len(sourceConditions) > 0 {
		finalCondition := "(" + strings.Join(sourceConditions, " OR ") + ")"
		builder.AddCondition(finalCondition, allParams...)
		log.Printf("[RBAC-DEBUG] Final combined SQL: %s", finalCondition)
	} else {
		// No valid conditions - deny access
		builder.AddCondition("1 = 0")
	}

	return nil
}

// buildClusterScopedConditions builds conditions for cluster-scoped resources
func (f *FindResourcesCore) buildClusterScopedConditions(source auth.PermissionSource, kindFilter interface{}, hubClusterName string) (string, []interface{}) {
	var allConditions []string
	var allParams []interface{}

	// Process each cluster's cluster-scoped permissions separately (prevents Cartesian products)
	for cluster, allowedKinds := range source.ClusterScopedKinds {
		if len(allowedKinds) == 0 {
			continue
		}

		var authorizedKinds []string

		// Apply kind filtering if specified
		if kindFilter != nil {
			requestedKinds := f.convertKindFilter(kindFilter)
			if len(requestedKinds) > 0 {
				// SECURITY: Check permissions for ALL requested kinds in this cluster
				for _, requestedKind := range requestedKinds {
					isAuthorized := false
					for _, allowedKind := range allowedKinds {
						if allowedKind == "*" || strings.EqualFold(allowedKind, requestedKind) {
							isAuthorized = true
							break
						}
					}
					if isAuthorized {
						authorizedKinds = append(authorizedKinds, requestedKind)
					}
				}
			}
		} else {
			// No kind filter - use all allowed kinds for this cluster
			authorizedKinds = allowedKinds
		}

		if len(authorizedKinds) == 0 {
			continue // No authorized kinds for this cluster
		}

		// Build resource conditions for this specific cluster
		var resourceConditions []string
		var resourceParams []interface{}

		if f.containsWildcard(authorizedKinds) {
			resourceConditions = append(resourceConditions, "1 = 1") // Allow all cluster-scoped resources
		} else {
			placeholders := make([]string, len(authorizedKinds))
			for i := range authorizedKinds {
				placeholders[i] = "%s"
			}
			resourceConditions = append(resourceConditions, fmt.Sprintf("data->>'kind' IN (%s)", strings.Join(placeholders, ",")))
			for _, kind := range authorizedKinds {
				resourceParams = append(resourceParams, kind)
			}
		}

		if len(resourceConditions) > 0 {
			// Create explicit (cluster = 'specific-cluster' AND kind IN ('allowed', 'kinds'))
			condition := fmt.Sprintf("(cluster = %s AND (%s))", "%s", strings.Join(resourceConditions, " OR "))
			allConditions = append(allConditions, condition)
			allParams = append(allParams, cluster)
			allParams = append(allParams, resourceParams...)
		}
	}

	if len(allConditions) > 0 {
		return "(" + strings.Join(allConditions, " OR ") + ")", allParams
	}

	return "", nil
}

// buildNamespacedConditions builds explicit namespace→resource conditions (prevents Cartesian products)
func (f *FindResourcesCore) buildNamespacedConditions(source auth.PermissionSource, kindFilter interface{}, hubClusterName string) ([]string, []interface{}) {
	var conditions []string
	var allParams []interface{}

	// Iterate through direct namespace→resource mapping
	// NOTE: For userpermission-cr source, keys are in "cluster/namespace" format to preserve cluster-namespace relationships
	for namespaceKey, allowedKinds := range source.NamespacedKinds {
		var resourceConditions []string
		var resourceParams []interface{}

		// Apply kind filtering if specified
		if kindFilter != nil {
			requestedKinds := f.convertKindFilter(kindFilter)
			if len(requestedKinds) > 0 {
				// Check permissions for ALL requested kinds in this namespace
				var authorizedKinds []string

				for _, requestedKind := range requestedKinds {
					// Check if this specific requested kind is allowed in this namespace (case-insensitive)
					isAuthorized := false
					for _, allowedKind := range allowedKinds {
						if allowedKind == "*" || strings.EqualFold(allowedKind, requestedKind) {
							isAuthorized = true
							break
						}
					}

					if isAuthorized {
						authorizedKinds = append(authorizedKinds, requestedKind)
					}
				}

				// SECURITY: Only include kinds user has permissions for in this namespace
				if len(authorizedKinds) > 0 {
					if len(authorizedKinds) == 1 {
						// Single authorized kind
						resourceConditions = append(resourceConditions, "data->>'kind' = %s")
						resourceParams = append(resourceParams, authorizedKinds[0])
					} else {
						// Multiple authorized kinds
						placeholders := make([]string, len(authorizedKinds))
						for i := range authorizedKinds {
							placeholders[i] = "%s"
						}
						resourceConditions = append(resourceConditions, fmt.Sprintf("data->>'kind' IN (%s)", strings.Join(placeholders, ",")))
						for _, kind := range authorizedKinds {
							resourceParams = append(resourceParams, kind)
						}
					}
				}
			}
		} else {
			// No specific kind filter - return all allowed resources for this namespace
			if f.containsWildcard(allowedKinds) {
				resourceConditions = append(resourceConditions, "1 = 1") // Allow all resources in this namespace
			} else if len(allowedKinds) > 0 {
				placeholders := make([]string, len(allowedKinds))
				for i := range allowedKinds {
					placeholders[i] = "%s"
				}
				resourceConditions = append(resourceConditions, fmt.Sprintf("data->>'kind' IN (%s)", strings.Join(placeholders, ",")))
				for _, kind := range allowedKinds {
					resourceParams = append(resourceParams, kind)
				}
			}
		}

		// Build namespace+resource condition (explicit pairing prevents Cartesian products)
		if len(resourceConditions) > 0 {
			var namespaceCondition string
			var namespaceParams []interface{}

			// Parse cluster and namespace from the key based on source type
			var cluster, namespace string
			if source.Source == "userpermission-cr" {
				// New format: "cluster/namespace" to preserve cluster-namespace relationships
				parts := strings.SplitN(namespaceKey, "/", 2)
				if len(parts) == 2 {
					cluster, namespace = parts[0], parts[1]
				} else {
					// Fallback for unexpected format
					cluster, namespace = "", namespaceKey
				}
			} else {
				// Legacy format: just namespace (for hub-kubernetes source)
				namespace = namespaceKey
				cluster = hubClusterName // For hub resources
			}

			if namespace == "*" {
				// Wildcard namespace access
				if cluster != "" {
					// Wildcard namespace but specific cluster (applies to ALL sources)
					namespaceCondition = fmt.Sprintf("(cluster = %s AND (%s))", "%s", strings.Join(resourceConditions, " OR "))
					namespaceParams = append(namespaceParams, cluster)
					namespaceParams = append(namespaceParams, resourceParams...)
				} else {
					// Pure wildcard (should rarely happen)
					namespaceCondition = strings.Join(resourceConditions, " OR ")
					namespaceParams = resourceParams
				}
			} else {
				// Specific namespace access
				if source.Source == "hub-kubernetes" {
					// Hub cluster resources
					namespaceCondition = fmt.Sprintf("(cluster = %s AND data->>'namespace' = %s AND (%s))", "%s", "%s", strings.Join(resourceConditions, " OR "))
					namespaceParams = append(namespaceParams, hubClusterName, namespace)
					namespaceParams = append(namespaceParams, resourceParams...)
				} else if source.Source == "userpermission-cr" {
					// UserPermission CR resources with explicit cluster-namespace pairing
					namespaceCondition = fmt.Sprintf("(cluster = %s AND data->>'namespace' = %s AND (%s))", "%s", "%s", strings.Join(resourceConditions, " OR "))
					namespaceParams = append(namespaceParams, cluster, namespace)
					namespaceParams = append(namespaceParams, resourceParams...)
				} else {
					// Legacy UserPermission API resources (any managed cluster) - should not happen with our fix
					namespaceCondition = fmt.Sprintf("(data->>'namespace' = %s AND (%s))", "%s", strings.Join(resourceConditions, " OR "))
					namespaceParams = append(namespaceParams, namespace)
					namespaceParams = append(namespaceParams, resourceParams...)
				}
			}

			conditions = append(conditions, namespaceCondition)
			allParams = append(allParams, namespaceParams...)

			log.Printf("[RBAC-DEBUG] Namespace key '%s' (cluster: %s, namespace: %s): %d allowed kinds = %v",
				namespaceKey, cluster, namespace, len(allowedKinds), allowedKinds)
		}
	}

	return conditions, allParams
}

// convertKindFilter converts kind filter to slice for processing multiple kinds
// Now returns ALL requested kinds to prevent authorization bypasses
// Supports both arrays and comma-separated strings (e.g., "Pod,ConfigMap,Service")
func (f *FindResourcesCore) convertKindFilter(kindFilter interface{}) []string {
	if kindFilter == nil {
		return nil
	}

	switch v := kindFilter.(type) {
	case string:
		if v == "" {
			return nil
		}

		// Handle comma-separated kinds like "Pod,ConfigMap,Service"
		var kinds []string
		for _, kind := range strings.Split(v, ",") {
			trimmed := strings.TrimSpace(kind)
			if trimmed != "" {
				kinds = append(kinds, trimmed)
			}
		}
		return kinds

	case []string:
		// Return ALL kinds, not just the first one
		if len(v) > 0 {
			// Clean up any empty strings
			var kinds []string
			for _, kind := range v {
				trimmed := strings.TrimSpace(kind)
				if trimmed != "" {
					kinds = append(kinds, trimmed)
				}
			}
			return kinds
		}
		return nil
	}
	return nil
}

// containsVerb checks if a verb slice contains a specific verb
func (f *FindResourcesCore) containsVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// containsWildcard checks if a string slice contains "*" wildcard
func (f *FindResourcesCore) containsWildcard(slice []string) bool {
	for _, s := range slice {
		if s == "*" {
			return true
		}
	}
	return false
}

// Helper methods for building individual filter conditions will follow...

// combineClusterFilters combines explicit cluster filter with clusters from clusterSelector
func (f *FindResourcesCore) combineClusterFilters(explicitClusters interface{}, targetClusters []string) []string {
	var clusterList []string

	// Add explicit clusters
	if explicitClusters != nil {
		switch v := explicitClusters.(type) {
		case string:
			clusterList = append(clusterList, v)
		case []string:
			clusterList = append(clusterList, v...)
		case []interface{}:
			for _, cluster := range v {
				if str, ok := cluster.(string); ok {
					clusterList = append(clusterList, str)
				}
			}
		}
	}

	// If we have targetClusters from clusterSelector
	if len(targetClusters) > 0 {
		if len(clusterList) == 0 {
			// No explicit clusters, use targetClusters
			clusterList = targetClusters
		} else {
			// Intersect explicit clusters with targetClusters
			intersection := []string{}
			explicitMap := make(map[string]bool)
			for _, cluster := range clusterList {
				explicitMap[cluster] = true
			}
			for _, cluster := range targetClusters {
				if explicitMap[cluster] {
					intersection = append(intersection, cluster)
				}
			}
			clusterList = intersection
		}
	}

	return clusterList
}

// buildOrderByClause creates the ORDER BY clause
func (f *FindResourcesCore) buildOrderByClause(sortBy, sortOrder string) string {
	var orderField string
	switch sortBy {
	case "name":
		orderField = "data->>'name'"
	case "created":
		orderField = "data->>'created'"
	case "namespace":
		orderField = "data->>'namespace'"
	default:
		orderField = "data->>'name'" // default to name
	}

	return fmt.Sprintf("%s %s", orderField, strings.ToUpper(sortOrder))
}

// buildKindConditions creates WHERE conditions for kind filter
// COMMA SUPPORT: Now handles comma-separated kind strings like "ConfigMap,Pod"
func (f *FindResourcesCore) buildKindConditions(kind interface{}, builder *utils.SQLBuilder) error {
	var kinds []string
	switch v := kind.(type) {
	case string:
		// Handle comma-separated kinds like "ConfigMap,Pod"
		if v == "" {
			return nil // Empty string, no filter needed
		}
		for _, k := range strings.Split(v, ",") {
			trimmed := strings.TrimSpace(k)
			if trimmed != "" {
				kinds = append(kinds, trimmed)
			}
		}
	case []string:
		// Clean up any empty strings
		for _, k := range v {
			trimmed := strings.TrimSpace(k)
			if trimmed != "" {
				kinds = append(kinds, trimmed)
			}
		}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					kinds = append(kinds, trimmed)
				}
			}
		}
	default:
		return fmt.Errorf("invalid kind type: %T", kind)
	}

	if len(kinds) == 0 {
		return nil // No valid kinds found, no filter needed
	}

	return pkgutils.BuildKindConditions(kinds, "data", builder)
}

// buildNameConditions creates WHERE conditions for name filter
func (f *FindResourcesCore) buildNameConditions(name string, builder *utils.SQLBuilder) error {
	names := []string{name}
	return pkgutils.BuildNameConditions(names, "data", builder)
}

// buildNamespaceConditions creates WHERE conditions for namespace filter
func (f *FindResourcesCore) buildNamespaceConditions(namespace interface{}, builder *utils.SQLBuilder) error {
	var namespaces []string
	switch v := namespace.(type) {
	case string:
		namespaces = []string{v}
	case []string:
		namespaces = v
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				namespaces = append(namespaces, str)
			}
		}
	default:
		return fmt.Errorf("invalid namespace type: %T", namespace)
	}

	return pkgutils.BuildNamespaceConditions(namespaces, "data", builder)
}

// buildClusterConditions creates WHERE conditions for cluster filter
func (f *FindResourcesCore) buildClusterConditions(clusters []string, builder *utils.SQLBuilder) error {
	return pkgutils.BuildClusterConditions(clusters, "data", builder)
}

// buildLabelConditions creates WHERE conditions for label selector
func (f *FindResourcesCore) buildLabelConditions(labelSelector string, builder *utils.SQLBuilder) error {
	selectors, err := pkgutils.ParseLabelSelector(labelSelector)
	if err != nil {
		return err
	}

	return pkgutils.LabelSelectorsToSQL(selectors, "data", builder)
}

// buildStatusConditions creates WHERE conditions for status filter
func (f *FindResourcesCore) buildStatusConditions(status interface{}, kind interface{}, builder *utils.SQLBuilder) error {
	return pkgutils.BuildStatusConditions(status, "data", builder, kind)
}

// buildComplianceConditions creates WHERE conditions for policy compliance filter
func (f *FindResourcesCore) buildComplianceConditions(compliance interface{}, builder *utils.SQLBuilder) error {
	return pkgutils.BuildComplianceConditions(compliance, "data", builder)
}

// buildTextSearchConditions creates WHERE conditions for text search
func (f *FindResourcesCore) buildTextSearchConditions(textSearch string, builder *utils.SQLBuilder) error {
	searchTexts := []string{textSearch}
	return pkgutils.BuildTextSearchConditions(searchTexts, "data", builder)
}

// buildTimeConditions creates WHERE conditions for time filters
func (f *FindResourcesCore) buildTimeConditions(ageNewerThan, ageOlderThan string, builder *utils.SQLBuilder) error {
	filters, err := pkgutils.ParseTimeFilters(ageNewerThan, ageOlderThan)
	if err != nil {
		return err
	}

	return pkgutils.TimeFiltersToSQL(filters, "data", builder)
}

// processResults processes the query results based on output mode
func (f *FindResourcesCore) processResults(queryResult *types.QueryResult, args FindResourcesArgs) (interface{}, error) {
	switch args.OutputMode {
	case OutputModeList:
		return f.processListMode(queryResult, args)
	case OutputModeCount:
		return f.processCountMode(queryResult, args)
	case OutputModeSummary:
		return f.processSummaryMode(queryResult, args)
	case OutputModeHealth:
		return f.processHealthMode(queryResult, args)
	default:
		return nil, fmt.Errorf("unsupported output mode: %s", args.OutputMode)
	}
}

// processListMode processes results for list output mode
func (f *FindResourcesCore) processListMode(queryResult *types.QueryResult, args FindResourcesArgs) ([]ResourceResult, error) {
	results := make([]ResourceResult, 0, len(queryResult.Rows))

	for _, row := range queryResult.Rows {
		// Parse the row: uid, cluster, data
		if len(row) < 3 {
			continue
		}

		cluster, ok := row[1].(string)
		if !ok {
			continue
		}

		dataMap, ok := row[2].(map[string]interface{})
		if !ok {
			continue
		}

		// Extract standard fields
		resource := ResourceResult{
			Cluster: cluster,
			Data:    dataMap,
		}

		// Extract name (required field)
		if name, exists := dataMap["name"]; exists {
			if nameStr, ok := name.(string); ok {
				resource.Name = nameStr
			}
		}

		// Extract kind (required field)
		if kind, exists := dataMap["kind"]; exists {
			if kindStr, ok := kind.(string); ok {
				resource.Kind = kindStr
			}
		}

		// Extract namespace (optional for cluster-scoped resources)
		if namespace, exists := dataMap["namespace"]; exists && namespace != nil {
			if namespaceStr, ok := namespace.(string); ok && namespaceStr != "" {
				resource.Namespace = &namespaceStr
			}
		}

		// Extract status (optional)
		if status, exists := dataMap["status"]; exists && status != nil {
			if statusStr, ok := status.(string); ok && statusStr != "" {
				resource.Status = &statusStr
			}
		}

		// Extract created timestamp and calculate age
		if created, exists := dataMap["created"]; exists && created != nil {
			if createdStr, ok := created.(string); ok {
				if createdTime, err := time.Parse(time.RFC3339, createdStr); err == nil {
					resource.Created = &createdTime
					resource.Age = pkgutils.CalculateAge(createdTime)
				}
			}
		}

		// Extract labels
		if labels, exists := dataMap["label"]; exists && labels != nil {
			if labelsMap, ok := labels.(map[string]interface{}); ok {
				resource.Labels = make(map[string]string)
				for k, v := range labelsMap {
					if vStr, ok := v.(string); ok {
						resource.Labels[k] = vStr
					}
				}
			}
		}

		results = append(results, resource)
	}

	return results, nil
}

// processCountMode processes results for count output mode
func (f *FindResourcesCore) processCountMode(queryResult *types.QueryResult, args FindResourcesArgs) ([]CountResult, error) {
	// Group resources by the specified groupBy field
	groupCounts := make(map[string]int)
	total := 0

	for _, row := range queryResult.Rows {
		if len(row) < 3 {
			continue
		}

		dataMap, ok := row[2].(map[string]interface{})
		if !ok {
			continue
		}

		// Determine grouping key based on groupBy parameter
		groupKey := f.extractGroupKey(dataMap, row, args.GroupBy)
		groupCounts[groupKey]++
		total++
	}

	// Convert to CountResult slice
	results := make([]CountResult, 0, len(groupCounts))
	for label, count := range groupCounts {
		percentage := 0.0
		if total > 0 {
			percentage = float64(count) / float64(total) * 100
		}

		results = append(results, CountResult{
			Label:      label,
			Count:      count,
			Percentage: percentage,
		})
	}

	// Sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	return results, nil
}

// processSummaryMode processes results for summary output mode
func (f *FindResourcesCore) processSummaryMode(queryResult *types.QueryResult, args FindResourcesArgs) (SummaryResult, error) {
	clusterCounts := make(map[string]int)
	kindCounts := make(map[string]int)
	namespaceCounts := make(map[string]int)
	uniqueClusters := make(map[string]bool)

	for _, row := range queryResult.Rows {
		if len(row) < 3 {
			continue
		}

		cluster, ok := row[1].(string)
		if !ok {
			continue
		}

		dataMap, ok := row[2].(map[string]interface{})
		if !ok {
			continue
		}

		// Count by cluster
		clusterCounts[cluster]++
		uniqueClusters[cluster] = true

		// Count by kind
		if kind, exists := dataMap["kind"]; exists {
			if kindStr, ok := kind.(string); ok {
				kindCounts[kindStr]++
			}
		}

		// Count by namespace
		namespaceKey := "cluster-scoped"
		if namespace, exists := dataMap["namespace"]; exists && namespace != nil {
			if namespaceStr, ok := namespace.(string); ok && namespaceStr != "" {
				namespaceKey = namespaceStr
			}
		}
		namespaceCounts[namespaceKey]++
	}

	// Create summary result
	result := SummaryResult{
		TotalResources:       len(queryResult.Rows),
		TotalClusters:        len(uniqueClusters),
		ResourcesByCluster:   f.mapToCountResults(clusterCounts, 10),
		ResourcesByKind:      f.mapToCountResults(kindCounts, 10),
		ResourcesByNamespace: f.mapToCountResults(namespaceCounts, 10),
	}

	return result, nil
}

// processHealthMode processes results for health output mode
func (f *FindResourcesCore) processHealthMode(queryResult *types.QueryResult, args FindResourcesArgs) (HealthResult, error) {
	healthCounts := map[string]int{
		HealthStatusHealthy:   0,
		HealthStatusUnhealthy: 0,
		HealthStatusUnknown:   0,
	}
	statusCounts := make(map[string]int)
	total := 0
	topIssues := make(map[string]int)

	for _, row := range queryResult.Rows {
		if len(row) < 3 {
			continue
		}

		dataMap, ok := row[2].(map[string]interface{})
		if !ok {
			continue
		}

		// Determine health status
		healthStatus, actualStatus := f.determineHealthStatus(dataMap)
		healthCounts[healthStatus]++

		if actualStatus != "" {
			statusCounts[actualStatus]++

			// Track unhealthy issues for topIssues
			if healthStatus == HealthStatusUnhealthy {
				topIssues[actualStatus]++
			}
		}

		total++
	}

	// Create details array
	details := make([]CountResult, 0, len(statusCounts))
	for status, count := range statusCounts {
		percentage := 0.0
		if total > 0 {
			percentage = float64(count) / float64(total) * 100
		}
		details = append(details, CountResult{
			Label:      status,
			Count:      count,
			Percentage: percentage,
		})
	}

	// Sort details by count descending
	sort.Slice(details, func(i, j int) bool {
		return details[i].Count > details[j].Count
	})

	// Create top issues list (top 10 unhealthy)
	issuesList := make([]string, 0, len(topIssues))
	for issue, count := range topIssues {
		issuesList = append(issuesList, fmt.Sprintf("%s (%d)", issue, count))
	}
	sort.Slice(issuesList, func(i, j int) bool {
		// Sort by count (extract count from string for proper sorting)
		return strings.Contains(issuesList[i], ")") && strings.Contains(issuesList[j], ")")
	})
	if len(issuesList) > 10 {
		issuesList = issuesList[:10]
	}

	result := HealthResult{
		Total:     total,
		Healthy:   healthCounts[HealthStatusHealthy],
		Unhealthy: healthCounts[HealthStatusUnhealthy],
		Unknown:   healthCounts[HealthStatusUnknown],
		Details:   details,
		TopIssues: issuesList,
	}

	return result, nil
}

// Helper methods

// extractGroupKey extracts the grouping key based on groupBy parameter
func (f *FindResourcesCore) extractGroupKey(dataMap map[string]interface{}, row []interface{}, groupBy string) string {
	switch groupBy {
	case "status":
		if status, exists := dataMap["status"]; exists && status != nil {
			if statusStr, ok := status.(string); ok {
				return statusStr
			}
		}
		return "unknown"
	case "namespace":
		if namespace, exists := dataMap["namespace"]; exists && namespace != nil {
			if namespaceStr, ok := namespace.(string); ok && namespaceStr != "" {
				return namespaceStr
			}
		}
		return "cluster-scoped"
	case "cluster":
		if len(row) >= 2 {
			if cluster, ok := row[1].(string); ok {
				return cluster
			}
		}
		return "unknown"
	case "kind":
		if kind, exists := dataMap["kind"]; exists {
			if kindStr, ok := kind.(string); ok {
				return kindStr
			}
		}
		return "unknown"
	default:
		// Handle label grouping: "label:key"
		if strings.HasPrefix(groupBy, "label:") {
			labelKey := strings.TrimPrefix(groupBy, "label:")
			if labels, exists := dataMap["label"]; exists && labels != nil {
				if labelsMap, ok := labels.(map[string]interface{}); ok {
					if value, exists := labelsMap[labelKey]; exists {
						if valueStr, ok := value.(string); ok {
							return valueStr
						}
					}
				}
			}
			return "not-set"
		}
		return "unknown"
	}
}

// mapToCountResults converts a count map to sorted CountResult slice
func (f *FindResourcesCore) mapToCountResults(countMap map[string]int, limit int) []CountResult {
	results := make([]CountResult, 0, len(countMap))
	total := 0

	// Calculate total for percentages
	for _, count := range countMap {
		total += count
	}

	// Convert to CountResult slice
	for label, count := range countMap {
		percentage := 0.0
		if total > 0 {
			percentage = float64(count) / float64(total) * 100
		}

		results = append(results, CountResult{
			Label:      label,
			Count:      count,
			Percentage: percentage,
		})
	}

	// Sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

// determineHealthStatus determines the health status of a resource
func (f *FindResourcesCore) determineHealthStatus(dataMap map[string]interface{}) (string, string) {
	// Get kind and status
	var kind string
	if k, exists := dataMap["kind"]; exists {
		if kStr, ok := k.(string); ok {
			kind = kStr
		}
	}

	var status string
	if s, exists := dataMap["status"]; exists && s != nil {
		if sStr, ok := s.(string); ok {
			status = sStr
		}
	}

	// Use status mapping utility to determine health
	healthStatus := pkgutils.EvaluateResourceHealth(kind, dataMap)

	return healthStatus, status
}