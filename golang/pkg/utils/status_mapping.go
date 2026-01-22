// Package utils provides status mapping utilities for Kubernetes resource health evaluation
package utils

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

// StatusCategory represents the different categories of status mappings
type StatusCategory string

const (
	StatusCategorySimple         StatusCategory = "simple"         // Direct status field
	StatusCategoryCustom        StatusCategory = "custom"         // Custom field name
	StatusCategoryComplex       StatusCategory = "complex"        // Requires evaluation logic
	StatusCategoryMultiCondition StatusCategory = "multi-condition" // Multiple boolean fields
	StatusCategoryNested        StatusCategory = "nested"         // Nested JSON path
	StatusCategoryNone          StatusCategory = "none"           // No status concept
)

// HealthStatus represents the possible health outcomes
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// StatusMapping defines how to extract status information from a resource type
type StatusMapping struct {
	Kind       string         `json:"kind"`
	Category   StatusCategory `json:"category"`

	// For simple and custom categories
	Field       *string  `json:"field,omitempty"`
	ValidValues []string `json:"validValues,omitempty"`

	// For custom category value transformation
	ValueMapping map[string]string `json:"valueMapping,omitempty"`

	// For complex category evaluation
	HealthLogic func(data map[string]interface{}) HealthStatus `json:"-"`

	// For multi-condition category
	ConditionFields []string `json:"conditionFields,omitempty"`

	// For nested category
	JSONPath *string `json:"jsonPath,omitempty"`
}

// BuildStatusResult represents the result of building status conditions
type BuildStatusResult struct {
	Conditions     []string      `json:"conditions"`
	Params         []interface{} `json:"params"`
	NextParamIndex int           `json:"nextParamIndex"`
}

// Default mapping for unknown resource types
var defaultStatusMapping = &StatusMapping{
	Kind:     "Unknown",
	Category: StatusCategoryNone,
}

// STATUS_MAPPINGS contains the comprehensive mapping of all supported resource types
var statusMappings = []*StatusMapping{
	// SIMPLE CATEGORY - Direct status field (9 resources)
	{
		Kind:     "Pod",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Running", "Pending", "Succeeded", "Failed", "Unknown",
			"CrashLoopBackOff", "ImagePullBackOff", "Error", "Completed",
			"ContainerCreating", "Terminating",
		},
	},
	{
		Kind:     "Node",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Ready", "NotReady", "Unknown", "SchedulingDisabled",
		},
	},
	{
		Kind:     "PersistentVolume",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Available", "Bound", "Released", "Failed",
		},
	},
	{
		Kind:     "PersistentVolumeClaim",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Pending", "Bound", "Lost",
		},
	},
	{
		Kind:     "Job",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Complete", "Failed", "Running", "Suspended",
		},
	},
	{
		Kind:     "CronJob",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Active", "Suspended",
		},
	},
	{
		Kind:     "Build",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"New", "Pending", "Running", "Complete", "Failed", "Error", "Cancelled",
		},
	},
	{
		Kind:     "BuildConfig",
		Category: StatusCategorySimple,
		Field:    stringPtr("status"),
		ValidValues: []string{
			"Complete", "Failed", "Running", "Pending",
		},
	},

	// CUSTOM CATEGORY - Different field names (5 resources)
	{
		Kind:     "Policy",
		Category: StatusCategoryCustom,
		Field:    stringPtr("compliant"),
		ValidValues: []string{
			"Compliant", "NonCompliant", "Pending", "Unknown",
		},
	},
	{
		Kind:     "ManagedCluster",
		Category: StatusCategoryCustom,
		Field:    stringPtr("available"),
		ValidValues: []string{
			"True", "False", "Unknown",
		},
		ValueMapping: map[string]string{
			"True":    string(HealthStatusHealthy),
			"False":   string(HealthStatusUnhealthy),
			"Unknown": string(HealthStatusUnknown),
		},
	},
	{
		Kind:     "Certificate",
		Category: StatusCategoryCustom,
		Field:    stringPtr("ready"),
		ValidValues: []string{
			"True", "False",
		},
		ValueMapping: map[string]string{
			"True":  string(HealthStatusHealthy),
			"False": string(HealthStatusUnhealthy),
		},
	},
	{
		Kind:     "CertificateRequest",
		Category: StatusCategoryCustom,
		Field:    stringPtr("ready"),
		ValidValues: []string{
			"True", "False",
		},
	},
	{
		Kind:     "Ingress",
		Category: StatusCategoryCustom,
		Field:    stringPtr("ready"),
		ValidValues: []string{
			"True", "False", "Unknown",
		},
	},

	// COMPLEX CATEGORY - Evaluation logic required (6 resources)
	{
		Kind:        "Deployment",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateDeploymentHealth,
	},
	{
		Kind:        "ReplicaSet",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateReplicaSetHealth,
	},
	{
		Kind:        "StatefulSet",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateStatefulSetHealth,
	},
	{
		Kind:        "DaemonSet",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateDaemonSetHealth,
	},
	{
		Kind:        "ClusterOperator",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateClusterOperatorHealth,
	},
	{
		Kind:        "DeploymentConfig",
		Category:    StatusCategoryComplex,
		HealthLogic: evaluateDeploymentConfigHealth,
	},

	// MULTI-CONDITION CATEGORY - Multiple independent fields (3 resources)
	{
		Kind:     "ClusterOperator",
		Category: StatusCategoryMultiCondition,
		ConditionFields: []string{
			"available", "degraded", "progressing", "upgradeable",
		},
	},
	{
		Kind:     "ManagedCluster",
		Category: StatusCategoryMultiCondition,
		ConditionFields: []string{
			"available", "joined", "hubAccepted", "managed",
		},
	},
	{
		Kind:     "Certificate",
		Category: StatusCategoryMultiCondition,
		ConditionFields: []string{
			"ready", "issuing", "renewing",
		},
	},

	// NESTED CATEGORY - JSON path navigation (3 resources)
	{
		Kind:     "Application",
		Category: StatusCategoryNested,
		JSONPath: stringPtr("status.health.status"),
		ValidValues: []string{
			"Healthy", "Progressing", "Degraded", "Suspended", "Missing", "Unknown",
		},
	},
	{
		Kind:     "ApplicationSet",
		Category: StatusCategoryNested,
		JSONPath: stringPtr("status.health.status"),
		ValidValues: []string{
			"Healthy", "Progressing", "Degraded",
		},
	},
	{
		Kind:     "Route",
		Category: StatusCategoryNested,
		JSONPath: stringPtr("status.ingress.0.conditions.0.status"),
		ValidValues: []string{
			"True", "False",
		},
	},

	// NONE CATEGORY - No status concept (15 resources)
	{Kind: "Secret", Category: StatusCategoryNone},
	{Kind: "ConfigMap", Category: StatusCategoryNone},
	{Kind: "Service", Category: StatusCategoryNone},
	{Kind: "Namespace", Category: StatusCategoryNone},
	{Kind: "ServiceAccount", Category: StatusCategoryNone},
	{Kind: "Role", Category: StatusCategoryNone},
	{Kind: "RoleBinding", Category: StatusCategoryNone},
	{Kind: "ClusterRole", Category: StatusCategoryNone},
	{Kind: "ClusterRoleBinding", Category: StatusCategoryNone},
	{Kind: "NetworkPolicy", Category: StatusCategoryNone},
	{Kind: "LimitRange", Category: StatusCategoryNone},
	{Kind: "ResourceQuota", Category: StatusCategoryNone},
	{Kind: "PodSecurityPolicy", Category: StatusCategoryNone},
	{Kind: "StorageClass", Category: StatusCategoryNone},
	{Kind: "PriorityClass", Category: StatusCategoryNone},
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

// GetStatusMapping returns the status mapping for a given resource kind
func GetStatusMapping(kind string) *StatusMapping {
	for _, mapping := range statusMappings {
		if mapping.Kind == kind {
			return mapping
		}
	}
	return defaultStatusMapping
}

// HasStatusConcept returns true if the resource kind has a meaningful status
func HasStatusConcept(kind string) bool {
	mapping := GetStatusMapping(kind)
	return mapping.Category != StatusCategoryNone
}

// GetSupportedResourceKinds returns all resource kinds with status mappings
func GetSupportedResourceKinds() []string {
	kinds := make([]string, 0, len(statusMappings))
	for _, mapping := range statusMappings {
		if mapping.Category != StatusCategoryNone {
			kinds = append(kinds, mapping.Kind)
		}
	}
	return kinds
}

// GetStatusCategories returns all supported status categories
func GetStatusCategories() []StatusCategory {
	return []StatusCategory{
		StatusCategorySimple,
		StatusCategoryCustom,
		StatusCategoryComplex,
		StatusCategoryMultiCondition,
		StatusCategoryNested,
		StatusCategoryNone,
	}
}

// GetHealthStatuses returns all possible health status outcomes
func GetHealthStatuses() []HealthStatus {
	return []HealthStatus{
		HealthStatusHealthy,
		HealthStatusUnhealthy,
		HealthStatusDegraded,
		HealthStatusUnknown,
	}
}

// COMPLEX HEALTH EVALUATION FUNCTIONS

// evaluateDeploymentHealth evaluates Deployment health based on replica counts
func evaluateDeploymentHealth(data map[string]interface{}) HealthStatus {
	ready := getIntFromData(data, "ready")
	desired := getIntFromData(data, "desired")
	available := getIntFromData(data, "available")

	if desired == 0 {
		return HealthStatusUnknown // Scaled to zero
	}
	if ready >= desired && available >= desired {
		return HealthStatusHealthy
	}
	if ready == 0 {
		return HealthStatusUnhealthy
	}
	if ready < desired {
		return HealthStatusDegraded
	}
	return HealthStatusUnknown
}

// evaluateReplicaSetHealth evaluates ReplicaSet health
func evaluateReplicaSetHealth(data map[string]interface{}) HealthStatus {
	ready := getIntFromData(data, "ready")
	replicas := getIntFromData(data, "replicas")

	if replicas == 0 {
		return HealthStatusUnknown
	}
	if ready >= replicas {
		return HealthStatusHealthy
	}
	if ready == 0 {
		return HealthStatusUnhealthy
	}
	return HealthStatusDegraded
}

// evaluateStatefulSetHealth evaluates StatefulSet health (identical to ReplicaSet)
func evaluateStatefulSetHealth(data map[string]interface{}) HealthStatus {
	return evaluateReplicaSetHealth(data) // Same logic
}

// evaluateDaemonSetHealth evaluates DaemonSet health
func evaluateDaemonSetHealth(data map[string]interface{}) HealthStatus {
	numberReady := getIntFromData(data, "numberReady")
	desiredNumberScheduled := getIntFromData(data, "desiredNumberScheduled")
	numberMisscheduled := getIntFromData(data, "numberMisscheduled")

	if desiredNumberScheduled == 0 {
		return HealthStatusUnknown
	}
	if numberReady >= desiredNumberScheduled && numberMisscheduled == 0 {
		return HealthStatusHealthy
	}
	if numberReady == 0 {
		return HealthStatusUnhealthy
	}
	return HealthStatusDegraded
}

// evaluateClusterOperatorHealth evaluates ClusterOperator health
func evaluateClusterOperatorHealth(data map[string]interface{}) HealthStatus {
	available := getStringFromData(data, "available")
	degraded := getStringFromData(data, "degraded")
	progressing := getStringFromData(data, "progressing")

	if available == "True" && degraded == "False" {
		if progressing == "True" {
			return HealthStatusDegraded
		}
		return HealthStatusHealthy
	}
	if available == "False" || degraded == "True" {
		return HealthStatusUnhealthy
	}
	return HealthStatusUnknown
}

// evaluateDeploymentConfigHealth evaluates DeploymentConfig health
func evaluateDeploymentConfigHealth(data map[string]interface{}) HealthStatus {
	ready := getIntFromData(data, "ready")
	desired := getIntFromData(data, "desired")

	if desired == 0 {
		return HealthStatusUnknown
	}
	if ready >= desired {
		return HealthStatusHealthy
	}
	if ready == 0 {
		return HealthStatusUnhealthy
	}
	return HealthStatusDegraded
}

// HELPER FUNCTIONS

// getIntFromData safely extracts an integer value from data map
func getIntFromData(data map[string]interface{}, key string) int {
	if value, ok := data[key]; ok {
		if intVal, ok := value.(int); ok {
			return intVal
		}
		if floatVal, ok := value.(float64); ok {
			return int(floatVal)
		}
		if strVal, ok := value.(string); ok {
			if intVal, err := strconv.Atoi(strVal); err == nil {
				return intVal
			}
		}
	}
	return 0
}

// getStringFromData safely extracts a string value from data map
func getStringFromData(data map[string]interface{}, key string) string {
	if value, ok := data[key]; ok {
		if strVal, ok := value.(string); ok {
			return strVal
		}
		if intVal, ok := value.(int); ok {
			return strconv.Itoa(intVal)
		}
		if floatVal, ok := value.(float64); ok {
			return strconv.FormatFloat(floatVal, 'f', -1, 64)
		}
		if boolVal, ok := value.(bool); ok {
			if boolVal {
				return "True"
			}
			return "False"
		}
	}
	return ""
}

// EvaluateComplexStatus evaluates health status for complex resource types
func EvaluateComplexStatus(kind string, data map[string]interface{}) HealthStatus {
	mapping := GetStatusMapping(kind)

	// Only works for complex category
	if mapping.Category != StatusCategoryComplex || mapping.HealthLogic == nil {
		return HealthStatusUnknown
	}

	// Apply the health evaluation logic
	return mapping.HealthLogic(data)
}

// BuildKindAwareStatusConditions is the main router for building status-aware SQL conditions
func BuildKindAwareStatusConditions(kind interface{}, status interface{}, dataColumn string, builder *utils.SQLBuilder) error {
	log.Printf("[STATUS] Building kind-aware conditions for kind: %v, status: %v", kind, status)

	// Handle multiple kinds
	if kindSlice, ok := kind.([]string); ok && len(kindSlice) > 1 {
		return buildMultiKindStatusConditions(kindSlice, status, dataColumn, builder)
	}

	// Extract single kind
	var singleKind string
	if kindSlice, ok := kind.([]string); ok && len(kindSlice) == 1 {
		singleKind = kindSlice[0]
	} else if kindStr, ok := kind.(string); ok {
		singleKind = kindStr
	} else {
		// No kind specified - use fallback
		log.Printf("[STATUS] No kind specified, using fallback")
		return buildTextSearchStatusFallback(status, dataColumn, builder)
	}

	// Get status mapping for the kind
	mapping := GetStatusMapping(singleKind)

	// Handle different categories
	switch mapping.Category {
	case StatusCategoryNone:
		log.Printf("[STATUS] Resource kind '%s' has no status concept - ignoring status filter", singleKind)
		return nil // No conditions added

	case StatusCategorySimple:
		field := "status"
		if mapping.Field != nil {
			field = *mapping.Field
		}
		return buildSimpleStatusConditions(status, field, dataColumn, builder)

	case StatusCategoryCustom:
		if mapping.Field == nil {
			return fmt.Errorf("invalid mapping for %s: custom category requires field", singleKind)
		}
		return buildSimpleStatusConditions(status, *mapping.Field, dataColumn, builder)

	case StatusCategoryComplex:
		log.Printf("[STATUS] Status filtering for %s requires post-query processing", singleKind)
		// Add a placeholder condition that matches all
		builder.AddCondition("1=1")
		return nil

	case StatusCategoryMultiCondition:
		if mapping.ConditionFields == nil {
			return fmt.Errorf("invalid mapping for %s: multi-condition category requires conditionFields", singleKind)
		}
		return buildMultiConditionStatusConditions(status, mapping.ConditionFields, dataColumn, builder)

	case StatusCategoryNested:
		if mapping.JSONPath == nil {
			return fmt.Errorf("invalid mapping for %s: nested category requires jsonPath", singleKind)
		}
		return buildNestedStatusConditions(status, *mapping.JSONPath, dataColumn, builder)

	default:
		// Fallback to simple status
		log.Printf("[STATUS] Unknown category for %s, falling back to simple status", singleKind)
		return buildSimpleStatusConditions(status, "status", dataColumn, builder)
	}
}

// CONDITION BUILDING FUNCTIONS

// normalizeStatusInput converts status input to string slice
func normalizeStatusInput(status interface{}) []string {
	switch v := status.(type) {
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
		for _, item := range v {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		return []string{}
	}
}

// buildSimpleStatusConditions builds SQL conditions for single-field lookups
func buildSimpleStatusConditions(status interface{}, field string, dataColumn string, builder *utils.SQLBuilder) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	fieldPath := fmt.Sprintf("%s->>'%s'", dataColumn, field)

	if len(statusArray) == 1 {
		// Single value: use equality
		builder.AddCondition(fieldPath+" = %s", statusArray[0])
	} else {
		// Multiple values: use IN clause
		values := make([]interface{}, len(statusArray))
		for i, v := range statusArray {
			values[i] = v
		}
		builder.AddIN(fieldPath, values)
	}

	return nil
}

// buildNestedStatusConditions builds SQL conditions for nested JSON path navigation
func buildNestedStatusConditions(status interface{}, jsonPath string, dataColumn string, builder *utils.SQLBuilder) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	// Convert JSON path to PostgreSQL operators
	sqlPath, err := convertJSONPathToSQL(jsonPath, dataColumn)
	if err != nil {
		return fmt.Errorf("error converting JSON path: %w", err)
	}

	if len(statusArray) == 1 {
		// Single value: use equality
		builder.AddCondition(sqlPath+" = %s", statusArray[0])
	} else {
		// Multiple values: use IN clause
		values := make([]interface{}, len(statusArray))
		for i, v := range statusArray {
			values[i] = v
		}
		builder.AddIN(sqlPath, values)
	}

	return nil
}

// convertJSONPathToSQL converts dot-notation JSON paths to PostgreSQL JSON operators
func convertJSONPathToSQL(jsonPath string, dataColumn string) (string, error) {
	if jsonPath == "" {
		return "", fmt.Errorf("empty JSON path")
	}

	pathParts := strings.Split(jsonPath, ".")
	if len(pathParts) == 0 {
		return "", fmt.Errorf("empty JSON path")
	}

	sqlPath := dataColumn
	lastIndex := len(pathParts) - 1

	for i, part := range pathParts {
		if part == "" {
			return "", fmt.Errorf("empty path part in JSON path: %s", jsonPath)
		}

		// Check if this is an array index (e.g., "0")
		if _, err := strconv.Atoi(part); err == nil {
			// Array index: use ->0 notation
			sqlPath += fmt.Sprintf("->%s", part)
		} else {
			// Object key: use ->'key' or ->>'key'
			if i == lastIndex {
				// Last element: use ->> to extract as text
				sqlPath += fmt.Sprintf("->>'%s'", part)
			} else {
				// Intermediate element: use -> to navigate JSON
				sqlPath += fmt.Sprintf("->'%s'", part)
			}
		}
	}

	return sqlPath, nil
}

// buildMultiConditionStatusConditions builds OR conditions across multiple status fields
func buildMultiConditionStatusConditions(status interface{}, conditionFields []string, dataColumn string, builder *utils.SQLBuilder) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	var orConditions []string
	var orParams []interface{}

	// For each condition field, create a condition
	for _, field := range conditionFields {
		fieldPath := fmt.Sprintf("%s->>'%s'", dataColumn, field)

		if len(statusArray) == 1 {
			// Single status value
			orConditions = append(orConditions, fieldPath+" = %s")
			orParams = append(orParams, statusArray[0])
		} else {
			// Multiple status values - build IN clause for this field
			placeholders := make([]string, len(statusArray))
			for i := range statusArray {
				placeholders[i] = "%s"
				orParams = append(orParams, statusArray[i])
			}
			orConditions = append(orConditions, fieldPath+" IN ("+strings.Join(placeholders, ", ")+")")
		}
	}

	if len(orConditions) > 0 {
		// Build final OR condition
		builder.AddOR(orConditions, orParams...)
	}

	return nil
}

// buildMultiKindStatusConditions handles queries filtering multiple resource kinds simultaneously
func buildMultiKindStatusConditions(kinds []string, status interface{}, dataColumn string, builder *utils.SQLBuilder) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	var orConditions []string
	var orParams []interface{}

	// For each kind
	for _, kind := range kinds {
		mapping := GetStatusMapping(kind)

		// Skip kinds without status concept
		if mapping.Category == StatusCategoryNone {
			log.Printf("[STATUS] Skipping kind '%s' - no status concept", kind)
			continue
		}

		// Build kind condition: data->>'kind' = $X
		kindCondition := fmt.Sprintf("%s->>'kind' = %%s", dataColumn)

		// Handle different categories
		switch mapping.Category {
		case StatusCategorySimple, StatusCategoryCustom:
			field := "status"
			if mapping.Field != nil {
				field = *mapping.Field
			}
			fieldPath := fmt.Sprintf("%s->>'%s'", dataColumn, field)

			if len(statusArray) == 1 {
				// Single status value
				statusCondition := fieldPath + " = %s"
				combinedCondition := fmt.Sprintf("(%s AND %s)", kindCondition, statusCondition)
				orConditions = append(orConditions, combinedCondition)
				orParams = append(orParams, kind, statusArray[0])
			} else {
				// Multiple status values
				placeholders := make([]string, len(statusArray))
				for i := range statusArray {
					placeholders[i] = "%s"
				}
				statusCondition := fieldPath + " IN (" + strings.Join(placeholders, ", ") + ")"
				combinedCondition := fmt.Sprintf("(%s AND %s)", kindCondition, statusCondition)
				orConditions = append(orConditions, combinedCondition)
				orParams = append(orParams, kind)
				for _, val := range statusArray {
					orParams = append(orParams, val)
				}
			}

		case StatusCategoryNested:
			if mapping.JSONPath != nil {
				sqlPath, err := convertJSONPathToSQL(*mapping.JSONPath, dataColumn)
				if err != nil {
					log.Printf("[STATUS] Error converting JSON path for %s: %v", kind, err)
					continue
				}

				if len(statusArray) == 1 {
					// Single status value
					statusCondition := sqlPath + " = %s"
					combinedCondition := fmt.Sprintf("(%s AND %s)", kindCondition, statusCondition)
					orConditions = append(orConditions, combinedCondition)
					orParams = append(orParams, kind, statusArray[0])
				} else {
					// Multiple status values
					placeholders := make([]string, len(statusArray))
					for i := range statusArray {
						placeholders[i] = "%s"
					}
					statusCondition := sqlPath + " IN (" + strings.Join(placeholders, ", ") + ")"
					combinedCondition := fmt.Sprintf("(%s AND %s)", kindCondition, statusCondition)
					orConditions = append(orConditions, combinedCondition)
					orParams = append(orParams, kind)
					for _, val := range statusArray {
						orParams = append(orParams, val)
					}
				}
			}

		default:
			// Complex/multi-condition kinds skip SQL filtering
			log.Printf("[STATUS] Skipping status filter for %s in multi-kind query (category: %s)", kind, mapping.Category)
		}
	}

	if len(orConditions) > 0 {
		builder.AddOR(orConditions, orParams...)
	}

	return nil
}

// buildTextSearchStatusFallback builds fallback text search conditions for unmapped types
func buildTextSearchStatusFallback(status interface{}, dataColumn string, builder *utils.SQLBuilder) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	log.Printf("[STATUS] Using textSearch fallback for status values: %v", statusArray)

	if len(statusArray) == 1 {
		// Single value: JSON-aware search
		searchPattern := fmt.Sprintf(`%%"%s"%%`, statusArray[0])
		builder.AddCondition(dataColumn+"::text ILIKE %s", searchPattern)
	} else {
		// Multiple values: OR conditions
		var orConditions []string
		var orParams []interface{}

		for _, statusVal := range statusArray {
			searchPattern := fmt.Sprintf(`%%"%s"%%`, statusVal)
			orConditions = append(orConditions, dataColumn+"::text ILIKE %s")
			orParams = append(orParams, searchPattern)
		}

		builder.AddOR(orConditions, orParams...)
	}

	return nil
}

// PostFilterByComplexStatus performs post-query filtering for complex category resources
func PostFilterByComplexStatus(results []map[string]interface{}, statusFilter interface{}) []map[string]interface{} {
	statusArray := normalizeStatusInput(statusFilter)

	if len(statusArray) == 0 {
		return results
	}

	var filtered []map[string]interface{}

	for _, row := range results {
		// Assume row structure: map with "data" field containing resource JSON
		data, ok := row["data"].(map[string]interface{})
		if !ok {
			continue
		}

		kind, ok := data["kind"].(string)
		if !ok {
			continue
		}

		// Only apply to complex category resources
		mapping := GetStatusMapping(kind)
		if mapping.Category != StatusCategoryComplex {
			// Include non-complex resources as-is
			filtered = append(filtered, row)
			continue
		}

		// Evaluate complex status
		evaluatedStatus := EvaluateComplexStatus(kind, data)

		// Check if it matches the filter
		for _, filterStatus := range statusArray {
			if string(evaluatedStatus) == filterStatus {
				filtered = append(filtered, row)
				break
			}
		}
	}

	return filtered
}

// BuildStatusConditions is the main entry point for building status conditions
// It implements the hybrid approach: kind-aware filtering with fallback to text search
func BuildStatusConditions(status interface{}, dataColumn string, builder *utils.SQLBuilder, kind interface{}) error {
	statusArray := normalizeStatusInput(status)

	if len(statusArray) == 0 {
		return nil
	}

	// Try kind-aware status mapping first
	if kind != nil {
		mapping := GetStatusMapping(getFirstKind(kind))
		if mapping != nil && mapping.Category != StatusCategoryNone {
			log.Printf("[STATUS] Using kind-aware filtering for: %v", kind)
			return BuildKindAwareStatusConditions(kind, status, dataColumn, builder)
		}
	}

	// Fallback to text search for unmapped types
	log.Printf("[STATUS] Using textSearch fallback for kind: %v", kind)
	return buildTextSearchStatusFallback(status, dataColumn, builder)
}

// getFirstKind extracts the first kind from various input types
func getFirstKind(kind interface{}) string {
	switch v := kind.(type) {
	case string:
		return v
	case []string:
		if len(v) > 0 {
			return v[0]
		}
	}
	return ""
}