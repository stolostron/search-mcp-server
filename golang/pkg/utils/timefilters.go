// Package utils provides utilities for time-based filtering and duration parsing
package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

// TimeFilter represents a time-based filter condition
type TimeFilter struct {
	Field    string    `json:"field"`    // "created" or "age"
	Operator string    `json:"operator"` // "gt", "lt", "gte", "lte"
	Value    time.Time `json:"value"`
}

// TimeOperator represents supported time comparison operators
type TimeOperator string

const (
	OperatorGT  TimeOperator = "gt"  // Greater than
	OperatorGTE TimeOperator = "gte" // Greater than or equal
	OperatorLT  TimeOperator = "lt"  // Less than
	OperatorLTE TimeOperator = "lte" // Less than or equal
)

// TimeField represents supported time fields
type TimeField string

const (
	FieldCreated TimeField = "created"
	FieldAge     TimeField = "age"
)

var (
	// durationRegex matches patterns like "1h", "2d", "1w"
	durationRegex = regexp.MustCompile(`^(\d+)([hdw])$`)
)

// ParseDuration parses time duration strings like "1h", "2d", "1w" into time.Duration
// Supported units: h (hours), d (days), w (weeks)
func ParseDuration(duration string) (time.Duration, error) {
	matches := durationRegex.FindStringSubmatch(strings.TrimSpace(duration))
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s. Use format like \"1h\", \"2d\", \"1w\"", duration)
	}

	amount, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid number in duration: %s", matches[1])
	}

	unit := matches[2]

	switch unit {
	case "h":
		return time.Duration(amount) * time.Hour, nil
	case "d":
		return time.Duration(amount) * 24 * time.Hour, nil
	case "w":
		return time.Duration(amount) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid time unit: %s. Use h, d, or w", unit)
	}
}

// ParseTimeFilters converts age filter strings into TimeFilter objects
// ageNewerThan: resources created after now - duration (e.g., "1d" = last day)
// ageOlderThan: resources created before now - duration (e.g., "1w" = older than week)
func ParseTimeFilters(ageNewerThan, ageOlderThan string) ([]*TimeFilter, error) {
	var filters []*TimeFilter
	now := time.Now()

	if ageNewerThan != "" {
		duration, err := ParseDuration(ageNewerThan)
		if err != nil {
			return nil, fmt.Errorf("invalid ageNewerThan duration: %w", err)
		}

		threshold := now.Add(-duration)
		filters = append(filters, &TimeFilter{
			Field:    string(FieldCreated),
			Operator: string(OperatorGTE),
			Value:    threshold,
		})
	}

	if ageOlderThan != "" {
		duration, err := ParseDuration(ageOlderThan)
		if err != nil {
			return nil, fmt.Errorf("invalid ageOlderThan duration: %w", err)
		}

		threshold := now.Add(-duration)
		filters = append(filters, &TimeFilter{
			Field:    string(FieldCreated),
			Operator: string(OperatorLTE),
			Value:    threshold,
		})
	}

	return filters, nil
}

// TimeFiltersToSQL converts time filters to SQL WHERE conditions using SQLBuilder
// dataColumn: the JSON column name (usually "data")
func TimeFiltersToSQL(filters []*TimeFilter, dataColumn string, builder *utils.SQLBuilder) error {
	if len(filters) == 0 {
		return nil
	}

	for _, filter := range filters {
		// Build field path - currently only 'created' field is supported
		var fieldPath string
		switch filter.Field {
		case string(FieldCreated), string(FieldAge):
			fieldPath = fmt.Sprintf("%s->>'created'", dataColumn)
		default:
			return fmt.Errorf("unsupported time filter field: %s", filter.Field)
		}

		// Build SQL condition based on operator
		var condition string
		switch filter.Operator {
		case string(OperatorGT):
			condition = fmt.Sprintf("%s::timestamp > %%s", fieldPath)
		case string(OperatorGTE):
			condition = fmt.Sprintf("%s::timestamp >= %%s", fieldPath)
		case string(OperatorLT):
			condition = fmt.Sprintf("%s::timestamp < %%s", fieldPath)
		case string(OperatorLTE):
			condition = fmt.Sprintf("%s::timestamp <= %%s", fieldPath)
		default:
			return fmt.Errorf("unsupported time filter operator: %s", filter.Operator)
		}

		// Add condition to builder with ISO timestamp
		builder.AddCondition(condition, filter.Value.UTC().Format(time.RFC3339))
	}

	return nil
}

// ValidateDuration validates the format of a duration string
func ValidateDuration(duration string) error {
	_, err := ParseDuration(duration)
	return err
}

// CalculateAge calculates human-readable age from a creation timestamp
// Returns format like "6w2d", "3d5h", "45m", "23s"
func CalculateAge(created time.Time) string {
	now := time.Now()
	diff := now.Sub(created)

	seconds := int(diff.Seconds())
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24
	weeks := days / 7

	// Format in descending order of significance
	if weeks > 0 {
		remainingDays := days % 7
		if remainingDays > 0 {
			return fmt.Sprintf("%dw%dd", weeks, remainingDays)
		}
		return fmt.Sprintf("%dw", weeks)
	} else if days > 0 {
		remainingHours := hours % 24
		if remainingHours > 0 {
			return fmt.Sprintf("%dd%dh", days, remainingHours)
		}
		return fmt.Sprintf("%dd", days)
	} else if hours > 0 {
		remainingMinutes := minutes % 60
		if remainingMinutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, remainingMinutes)
		}
		return fmt.Sprintf("%dh", hours)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// CalculateAgeFromString is a convenience function that parses an ISO timestamp string
// and calculates the human-readable age
func CalculateAgeFromString(createdStr string) (string, error) {
	created, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		// Try parsing without timezone (fallback for non-standard formats)
		created, err = time.Parse("2006-01-02T15:04:05", createdStr)
		if err != nil {
			return "", fmt.Errorf("unable to parse timestamp: %s", createdStr)
		}
	}

	return CalculateAge(created), nil
}

// SupportedDurationUnits returns the list of supported duration units
func SupportedDurationUnits() []string {
	return []string{"h", "d", "w"}
}

// SupportedOperators returns the list of supported time operators
func SupportedOperators() []string {
	return []string{
		string(OperatorGT),
		string(OperatorGTE),
		string(OperatorLT),
		string(OperatorLTE),
	}
}

// SupportedFields returns the list of supported time fields
func SupportedFields() []string {
	return []string{
		string(FieldCreated),
		string(FieldAge),
	}
}