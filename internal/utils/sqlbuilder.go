// Package utils provides internal utility functions for the search MCP server
package utils

import (
	"fmt"
	"strings"
)

// SQLBuilder helps manage SQL condition building with automatic parameter index tracking
// This prevents parameter index mismatches that were a major risk factor in the TypeScript implementation
type SQLBuilder struct {
	conditions []string
	params     []interface{}
	paramIndex int
}

// NewSQLBuilder creates a new SQLBuilder starting with the specified parameter index
func NewSQLBuilder(startIndex int) *SQLBuilder {
	return &SQLBuilder{
		conditions: make([]string, 0),
		params:     make([]interface{}, 0),
		paramIndex: startIndex,
	}
}

// AddCondition adds a SQL condition with automatic parameter index management
// The condition string should use %s placeholders where parameters will be inserted
// Example: builder.AddCondition("data->>'name' = %s", "my-name")
func (sb *SQLBuilder) AddCondition(condition string, conditionParams ...interface{}) {
	if len(conditionParams) == 0 {
		// Simple condition without parameters
		sb.conditions = append(sb.conditions, condition)
		return
	}

	// Replace %s placeholders with actual parameter indices
	finalCondition := condition
	for i := 0; i < len(conditionParams); i++ {
		placeholder := fmt.Sprintf("$%d", sb.paramIndex)
		finalCondition = strings.Replace(finalCondition, "%s", placeholder, 1)
		sb.params = append(sb.params, conditionParams[i])
		sb.paramIndex++
	}

	sb.conditions = append(sb.conditions, finalCondition)
}

// AddConditionWithPlaceholders adds a condition where parameter indices are explicitly managed
// Use this when you need more control over parameter placement
// Example: builder.AddConditionWithPlaceholders("name IN ($1, $2)", "name1", "name2")
func (sb *SQLBuilder) AddConditionWithPlaceholders(condition string, conditionParams ...interface{}) {
	sb.conditions = append(sb.conditions, condition)
	sb.params = append(sb.params, conditionParams...)
	sb.paramIndex += len(conditionParams)
}

// AddOR adds multiple conditions joined with OR
// Example: builder.AddOR([]string{"status = %s", "health = %s"}, "Running", "healthy")
func (sb *SQLBuilder) AddOR(orConditions []string, conditionParams ...interface{}) {
	if len(orConditions) == 0 {
		return
	}

	if len(orConditions) == 1 {
		sb.AddCondition(orConditions[0], conditionParams...)
		return
	}

	// Build OR condition with proper parameter management
	orParts := make([]string, len(orConditions))
	paramIndex := 0

	for i, condition := range orConditions {
		// Count %s placeholders in this condition
		placeholderCount := strings.Count(condition, "%s")

		// Replace %s with actual parameter indices
		finalCondition := condition
		for j := 0; j < placeholderCount; j++ {
			placeholder := fmt.Sprintf("$%d", sb.paramIndex)
			finalCondition = strings.Replace(finalCondition, "%s", placeholder, 1)
			sb.paramIndex++
		}

		orParts[i] = finalCondition

		// Add corresponding parameters
		for j := 0; j < placeholderCount && paramIndex < len(conditionParams); j++ {
			sb.params = append(sb.params, conditionParams[paramIndex])
			paramIndex++
		}
	}

	orCondition := fmt.Sprintf("(%s)", strings.Join(orParts, " OR "))
	sb.conditions = append(sb.conditions, orCondition)
}

// AddIN adds an IN condition for multiple values
// Example: builder.AddIN("cluster", []string{"cluster1", "cluster2"})
func (sb *SQLBuilder) AddIN(column string, values []interface{}) {
	if len(values) == 0 {
		return
	}

	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", sb.paramIndex)
		sb.params = append(sb.params, values[i])
		sb.paramIndex++
	}

	condition := fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", "))
	sb.conditions = append(sb.conditions, condition)
}

// AddLIKE adds a LIKE condition for wildcard matching
// Example: builder.AddLIKE("data->>'namespace'", "kube-*")
func (sb *SQLBuilder) AddLIKE(column string, pattern string) {
	condition := fmt.Sprintf("%s LIKE %s", column, fmt.Sprintf("$%d", sb.paramIndex))
	sb.params = append(sb.params, pattern)
	sb.paramIndex++
	sb.conditions = append(sb.conditions, condition)
}

// AddILIKE adds an ILIKE condition for case-insensitive wildcard matching
// Example: builder.AddILIKE("data->>'name'", "*nginx*")
func (sb *SQLBuilder) AddILIKE(column string, pattern string) {
	condition := fmt.Sprintf("%s ILIKE %s", column, fmt.Sprintf("$%d", sb.paramIndex))
	sb.params = append(sb.params, pattern)
	sb.paramIndex++
	sb.conditions = append(sb.conditions, condition)
}

// BuildWhere returns the complete WHERE clause and parameters
// Returns empty string if no conditions were added
func (sb *SQLBuilder) BuildWhere() (string, []interface{}) {
	if len(sb.conditions) == 0 {
		return "", nil
	}

	whereClause := "WHERE " + strings.Join(sb.conditions, " AND ")
	return whereClause, sb.params
}

// BuildConditions returns just the conditions (without WHERE keyword) and parameters
func (sb *SQLBuilder) BuildConditions() (string, []interface{}) {
	if len(sb.conditions) == 0 {
		return "", nil
	}

	conditionsClause := strings.Join(sb.conditions, " AND ")
	return conditionsClause, sb.params
}

// GetNextParamIndex returns the next parameter index that would be used
// Useful for chaining multiple builders or manual parameter management
func (sb *SQLBuilder) GetNextParamIndex() int {
	return sb.paramIndex
}

// GetConditionCount returns the number of conditions added to this builder
func (sb *SQLBuilder) GetConditionCount() int {
	return len(sb.conditions)
}

// GetParamCount returns the number of parameters added to this builder
func (sb *SQLBuilder) GetParamCount() int {
	return len(sb.params)
}

// Reset clears all conditions and parameters, optionally setting a new starting index
func (sb *SQLBuilder) Reset(startIndex int) {
	sb.conditions = sb.conditions[:0]
	sb.params = sb.params[:0]
	sb.paramIndex = startIndex
}

// Clone creates a copy of this builder with the same conditions and parameters
// The clone starts with the next available parameter index
func (sb *SQLBuilder) Clone() *SQLBuilder {
	clone := &SQLBuilder{
		conditions: make([]string, len(sb.conditions)),
		params:     make([]interface{}, len(sb.params)),
		paramIndex: sb.paramIndex,
	}

	copy(clone.conditions, sb.conditions)
	copy(clone.params, sb.params)

	return clone
}