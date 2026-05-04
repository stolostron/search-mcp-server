// Package utils provides utilities for cross-resource filtering and queries
package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

// CrossResourceFilter represents a filter condition for cross-resource queries
type CrossResourceFilter struct {
	Field     string      `json:"field"`
	Operator  string      `json:"operator"`
	Values    []string    `json:"values"`
	Wildcard  bool        `json:"wildcard"`
}

// FilterOperator represents supported filter operators
type FilterOperator string

const (
	FilterOperatorEqual    FilterOperator = "="
	FilterOperatorNotEqual FilterOperator = "!="
	FilterOperatorIn       FilterOperator = "in"
	FilterOperatorNotIn    FilterOperator = "notin"
	FilterOperatorLike     FilterOperator = "like"
	FilterOperatorILike    FilterOperator = "ilike"
)

// BuildClusterConditions builds SQL conditions for filtering by cluster name(s)
// Supports single cluster or multiple clusters using IN clause
func BuildClusterConditions(clusters []string, dataColumn string, builder *utils.SQLBuilder) error {
	if len(clusters) == 0 {
		return nil
	}

	// Filter out empty cluster names
	validClusters := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if trimmed := strings.TrimSpace(cluster); trimmed != "" {
			validClusters = append(validClusters, trimmed)
		}
	}

	if len(validClusters) == 0 {
		return nil
	}

	if len(validClusters) == 1 {
		// Single cluster - use equality
		builder.AddCondition("cluster = %s", validClusters[0])
	} else {
		// Multiple clusters - use IN clause
		values := make([]interface{}, len(validClusters))
		for i, cluster := range validClusters {
			values[i] = cluster
		}
		builder.AddIN("cluster", values)
	}

	return nil
}

// BuildKindConditions builds SQL conditions for filtering by resource kind(s)
// Queries the data->>'kind' JSON field for single or multiple kinds
func BuildKindConditions(kinds []string, dataColumn string, builder *utils.SQLBuilder) error {
	if len(kinds) == 0 {
		return nil
	}

	// Filter out empty kind names
	validKinds := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if trimmed := strings.TrimSpace(kind); trimmed != "" {
			validKinds = append(validKinds, trimmed)
		}
	}

	if len(validKinds) == 0 {
		return nil
	}

	kindPath := fmt.Sprintf("%s->>'kind'", dataColumn)

	if len(validKinds) == 1 {
		// Single kind - use equality
		builder.AddCondition(kindPath+" = %s", validKinds[0])
	} else {
		// Multiple kinds - use IN clause
		values := make([]interface{}, len(validKinds))
		for i, kind := range validKinds {
			values[i] = kind
		}
		builder.AddIN(kindPath, values)
	}

	return nil
}

// BuildNameConditions builds SQL conditions for filtering by resource name
// Supports exact matching and wildcard patterns (* and ?)
func BuildNameConditions(names []string, dataColumn string, builder *utils.SQLBuilder) error {
	if len(names) == 0 {
		return nil
	}

	namePath := fmt.Sprintf("%s->>'name'", dataColumn)
	var orConditions []string
	var orParams []interface{}

	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}

		if hasWildcards(trimmed) {
			// Convert wildcards to SQL LIKE pattern
			likePattern := convertWildcardToLike(trimmed)
			orConditions = append(orConditions, namePath+" LIKE %s")
			orParams = append(orParams, likePattern)
		} else {
			// Exact match
			orConditions = append(orConditions, namePath+" = %s")
			orParams = append(orParams, trimmed)
		}
	}

	if len(orConditions) == 0 {
		return nil
	}

	if len(orConditions) == 1 {
		builder.AddCondition(orConditions[0], orParams[0])
	} else {
		builder.AddOR(orConditions, orParams...)
	}

	return nil
}

// BuildNamespaceConditions builds SQL conditions for filtering by namespace
// Supports exact matching and wildcard patterns (* and ?)
// Handles mix of exact and wildcard patterns in a single query
func BuildNamespaceConditions(namespaces []string, dataColumn string, builder *utils.SQLBuilder) error {
	if len(namespaces) == 0 {
		return nil
	}

	namespacePath := fmt.Sprintf("%s->>'namespace'", dataColumn)
	var orConditions []string
	var orParams []interface{}

	for _, namespace := range namespaces {
		trimmed := strings.TrimSpace(namespace)
		if trimmed == "" {
			continue
		}

		if hasWildcards(trimmed) {
			// Convert wildcards to SQL LIKE pattern
			likePattern := convertWildcardToLike(trimmed)
			orConditions = append(orConditions, namespacePath+" LIKE %s")
			orParams = append(orParams, likePattern)
		} else {
			// Exact match
			orConditions = append(orConditions, namespacePath+" = %s")
			orParams = append(orParams, trimmed)
		}
	}

	if len(orConditions) == 0 {
		return nil
	}

	if len(orConditions) == 1 {
		builder.AddCondition(orConditions[0], orParams[0])
	} else {
		builder.AddOR(orConditions, orParams...)
	}

	return nil
}

// BuildTextSearchConditions builds SQL conditions for text search across multiple fields
// Searches in name, namespace, and full JSON text with case-insensitive ILIKE
func BuildTextSearchConditions(searchTexts []string, dataColumn string, builder *utils.SQLBuilder) error {
	if len(searchTexts) == 0 {
		return nil
	}

	namePath := fmt.Sprintf("%s->>'name'", dataColumn)
	namespacePath := fmt.Sprintf("%s->>'namespace'", dataColumn)
	fullTextPath := fmt.Sprintf("%s::text", dataColumn)

	for _, searchText := range searchTexts {
		trimmed := strings.TrimSpace(searchText)
		if trimmed == "" {
			continue
		}

		// Create ILIKE pattern with wildcards
		searchPattern := "%" + trimmed + "%"

		// Build OR condition for searching across multiple fields
		searchConditions := []string{
			namePath + " ILIKE %s",
			namespacePath + " ILIKE %s",
			fullTextPath + " ILIKE %s",
		}
		searchParams := []interface{}{searchPattern, searchPattern, searchPattern}

		builder.AddOR(searchConditions, searchParams...)
	}

	return nil
}

// hasWildcards checks if a string contains wildcard characters (* or ?)
func hasWildcards(str string) bool {
	return strings.ContainsAny(str, "*?")
}

// convertWildcardToLike converts shell-style wildcards to SQL LIKE patterns
// * becomes % (matches any sequence of characters)
// ? becomes _ (matches any single character)
func convertWildcardToLike(pattern string) string {
	// Replace * with % and ? with _
	result := strings.ReplaceAll(pattern, "*", "%")
	result = strings.ReplaceAll(result, "?", "_")
	return result
}

// GetSupportedFilterOperators returns the list of supported filter operators
func GetSupportedFilterOperators() []FilterOperator {
	return []FilterOperator{
		FilterOperatorEqual,
		FilterOperatorNotEqual,
		FilterOperatorIn,
		FilterOperatorNotIn,
		FilterOperatorLike,
		FilterOperatorILike,
	}
}

// GetSupportedFilterFields returns the list of fields that support filtering
func GetSupportedFilterFields() []string {
	return []string{
		"cluster",
		"kind",
		"name",
		"namespace",
		"textSearch",
	}
}

// FindMatchingClusters finds clusters matching the cluster selector
func FindMatchingClusters(ctx context.Context, clusterSelector string, dbQueries interface{}) ([]string, error) {
	// For now, implement a simple placeholder that parses the selector but doesn't execute queries
	// This would need to be integrated with actual database queries in a real implementation

	// Parse the label selector to validate it
	_, err := ParseLabelSelector(clusterSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid cluster selector: %w", err)
	}

	// TODO: Implement actual database query execution
	// This would involve:
	// 1. Building SQL to find ManagedCluster resources
	// 2. Adding label selector conditions using LabelSelectorsToSQL
	// 3. Executing the query and extracting cluster names
	//
	// For now, return empty list for testing/compilation
	return []string{}, nil
}

// EvaluateResourceHealth determines the health status of a resource
func EvaluateResourceHealth(kind string, data map[string]interface{}) string {
	// Check if the resource kind has a status concept
	if !HasStatusConcept(kind) {
		return "unknown"
	}

	// Get status mapping for the kind
	mapping := GetStatusMapping(kind)
	if mapping == nil {
		// Use fallback logic for unmapped kinds
		return evaluateGenericHealth(data)
	}

	// Use complex evaluation if available
	if mapping.Category == StatusCategoryComplex {
		healthStatus := EvaluateComplexStatus(kind, data)
		switch healthStatus {
		case HealthStatusHealthy:
			return "healthy"
		case HealthStatusUnhealthy:
			return "unhealthy"
		default:
			return "unknown"
		}
	}

	// For simple, custom, nested, and multi-condition statuses
	// extract the status value and evaluate
	status := extractStatusValue(data, mapping)
	return evaluateStatusHealth(status, kind)
}

// Helper function to extract status value based on mapping
func extractStatusValue(data map[string]interface{}, mapping *StatusMapping) string {
	switch mapping.Category {
	case StatusCategorySimple:
		field := "status"
		if mapping.Field != nil {
			field = *mapping.Field
		}
		return getStringFromData(data, field)

	case StatusCategoryCustom:
		if mapping.Field != nil {
			return getStringFromData(data, *mapping.Field)
		}
		return getStringFromData(data, "status")

	case StatusCategoryNested:
		if mapping.JSONPath != nil {
			// Navigate JSON path
			return extractJSONPathValue(data, *mapping.JSONPath)
		}
		return getStringFromData(data, "status")

	case StatusCategoryMultiCondition:
		// For multi-condition, check all condition fields
		if mapping.ConditionFields != nil {
			for _, field := range mapping.ConditionFields {
				if value := getStringFromData(data, field); value != "" {
					return value
				}
			}
		}
		return getStringFromData(data, "status")

	default:
		return getStringFromData(data, "status")
	}
}

// Helper function to extract value from JSON path
func extractJSONPathValue(data map[string]interface{}, jsonPath string) string {
	// Simple JSON path navigation (supports dot notation)
	parts := strings.Split(jsonPath, ".")
	current := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Handle array indexing [0]
		if strings.Contains(part, "[") && strings.Contains(part, "]") {
			// For now, just take the first element
			arrayField := strings.Split(part, "[")[0]
			if val, exists := current[arrayField]; exists {
				if arr, ok := val.([]interface{}); ok && len(arr) > 0 {
					if item, ok := arr[0].(map[string]interface{}); ok {
						current = item
					} else {
						return ""
					}
				} else {
					return ""
				}
			} else {
				return ""
			}
		} else {
			if val, exists := current[part]; exists {
				if nextLevel, ok := val.(map[string]interface{}); ok {
					current = nextLevel
				} else if str, ok := val.(string); ok {
					return str
				} else {
					return fmt.Sprintf("%v", val)
				}
			} else {
				return ""
			}
		}
	}

	return ""
}

// Helper function to evaluate status health
func evaluateStatusHealth(status, kind string) string {
	if status == "" {
		return "unknown"
	}

	status = strings.ToLower(status)

	// Healthy statuses
	switch status {
	case "running", "active", "ready", "available", "succeeded", "completed", "true":
		return "healthy"
	}

	// Unhealthy statuses
	switch status {
	case "failed", "error", "false", "crashloopbackoff", "imagepullbackoff", "evicted":
		return "unhealthy"
	}

	// Resource-specific evaluations
	switch kind {
	case "Pod":
		switch status {
		case "pending", "terminating", "containercreating":
			return "unknown"
		default:
			return "unhealthy"
		}
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		// For these, rely on the complex evaluation
		return "unknown"
	case "ClusterOperator":
		switch status {
		case "degraded", "progressing":
			return "unknown"
		default:
			return "unhealthy"
		}
	default:
		return "unknown"
	}
}

// Helper function for generic health evaluation
func evaluateGenericHealth(data map[string]interface{}) string {
	status := getStringFromData(data, "status")
	return evaluateStatusHealth(status, "")
}

// BuildComplianceConditions creates WHERE conditions for ACM governance policy compliance filtering
// Filters on the 'compliant' field: "Compliant", "NonCompliant", "UnknownCompliancy"
func BuildComplianceConditions(compliance interface{}, dataColumn string, builder *utils.SQLBuilder) error {
	complianceValues := normalizeComplianceInput(compliance)

	if len(complianceValues) == 0 {
		return nil
	}

	// Simple and focused: only filter on the compliant field
	// Examples: data->>'compliant' = 'Compliant', data->>'compliant' = 'NonCompliant'
	var conditions []string
	var params []interface{}

	for _, complianceValue := range complianceValues {
		normalizedValue := strings.TrimSpace(complianceValue)
		if normalizedValue == "" {
			continue
		}

		conditions = append(conditions, fmt.Sprintf("%s->>'compliant' = %%s", dataColumn))
		params = append(params, normalizedValue)
	}

	if len(conditions) > 0 {
		builder.AddOR(conditions, params...)
	}

	return nil
}

// normalizeComplianceInput converts compliance filter input to normalized string array
func normalizeComplianceInput(compliance interface{}) []string {
	switch v := compliance.(type) {
	case string:
		// Handle comma-separated values
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []string:
		// Filter out empty strings
		result := make([]string, 0, len(v))
		for _, str := range v {
			trimmed := strings.TrimSpace(str)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []interface{}:
		// Convert interface{} array to string array
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	default:
		return []string{}
	}
}