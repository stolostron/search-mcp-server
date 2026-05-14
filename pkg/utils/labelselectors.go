// Package utils provides utilities for Kubernetes label selector parsing and SQL generation
package utils

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

// LabelSelector represents a Kubernetes label selector requirement
type LabelSelector struct {
	Key      string          `json:"key"`
	Operator LabelOperator   `json:"operator"`
	Values   []string        `json:"values"`
}

// LabelOperator represents supported label selector operators
type LabelOperator string

const (
	LabelOperatorEqual     LabelOperator = "="        // key=value
	LabelOperatorNotEqual  LabelOperator = "!="       // key!=value
	LabelOperatorIn        LabelOperator = "in"       // key in (value1,value2)
	LabelOperatorNotIn     LabelOperator = "notin"    // key notin (value1,value2)
	LabelOperatorExists    LabelOperator = "exists"   // key
	LabelOperatorNotExists LabelOperator = "notexists" // !key
)

var (
	// Kubernetes label key validation regex
	// Must start with letter, end with alphanumeric, can contain hyphens, underscores, dots
	labelKeyRegex = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9\-_.]*[a-zA-Z0-9])?$`)

	// Regular expressions for parsing different selector types
	// Note: Using character class to capture potentially invalid keys for validation
	inOperatorRegex     = regexp.MustCompile(`^([a-zA-Z0-9\-_.@]+)\s+(notin|in)\s*\(([^)]+)\)$`)
	notEqualRegex       = regexp.MustCompile(`^([a-zA-Z0-9\-_.@]+)\s*!=\s*([^=]+)$`)
	equalRegex          = regexp.MustCompile(`^([a-zA-Z0-9\-_.@]+)\s*=\s*([^=]+)$`)
	notExistsRegex      = regexp.MustCompile(`^!([a-zA-Z0-9\-_.@]+)$`)
	existsRegex         = regexp.MustCompile(`^([a-zA-Z0-9\-_.@]+)$`)
)

// ParseLabelSelector parses a Kubernetes label selector string into structured format
// Supports: key=value, key!=value, key in (value1,value2), key notin (value1,value2), key, !key
// Example: "app=nginx,env!=test,tier in (frontend,backend)"
func ParseLabelSelector(selector string) ([]*LabelSelector, error) {
	if selector == "" || strings.TrimSpace(selector) == "" {
		return []*LabelSelector{}, nil
	}

	var selectors []*LabelSelector

	// Split by comma, but handle parentheses for 'in' and 'notin' operators
	parts, err := splitLabelSelector(selector)
	if err != nil {
		return nil, err
	}

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		parsed, err := parseSingleSelector(trimmed)
		if err != nil {
			return nil, fmt.Errorf("error parsing selector '%s': %w", trimmed, err)
		}

		if parsed != nil {
			selectors = append(selectors, parsed)
		}
	}

	return selectors, nil
}

// splitLabelSelector splits a label selector string by commas, handling parentheses correctly
// This prevents splitting on commas inside 'in' and 'notin' operator parentheses
func splitLabelSelector(selector string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inParens := false

	for i, char := range selector {
		switch char {
		case '(':
			inParens = true
			current.WriteRune(char)
		case ')':
			if !inParens {
				return nil, fmt.Errorf("unexpected closing parenthesis at position %d", i)
			}
			inParens = false
			current.WriteRune(char)
		case ',':
			if !inParens {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
			current.WriteRune(char)
		default:
			current.WriteRune(char)
		}
	}

	if inParens {
		return nil, fmt.Errorf("unclosed parenthesis in selector")
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts, nil
}

// parseSingleSelector parses a single label selector requirement
func parseSingleSelector(selector string) (*LabelSelector, error) {
	// Handle 'in' and 'notin' operators first (they have parentheses)
	if matches := inOperatorRegex.FindStringSubmatch(selector); matches != nil {
		key := matches[1]
		operator := matches[2]
		valuesStr := matches[3]

		// Validate key format immediately
		if !IsValidLabelKey(key) {
			return nil, fmt.Errorf("invalid label key: %s", key)
		}

		values := make([]string, 0)
		for _, v := range strings.Split(valuesStr, ",") {
			trimmed := strings.TrimSpace(v)
			if trimmed != "" {
				values = append(values, trimmed)
			}
		}

		var op LabelOperator
		switch operator {
		case "in":
			op = LabelOperatorIn
		case "notin":
			op = LabelOperatorNotIn
		default:
			return nil, fmt.Errorf("unknown operator: %s", operator)
		}

		return &LabelSelector{
			Key:      key,
			Operator: op,
			Values:   values,
		}, nil
	}

	// Handle != operator
	if matches := notEqualRegex.FindStringSubmatch(selector); matches != nil {
		key := matches[1]
		value := strings.TrimSpace(matches[2])

		// Validate key format immediately
		if !IsValidLabelKey(key) {
			return nil, fmt.Errorf("invalid label key: %s", key)
		}

		return &LabelSelector{
			Key:      key,
			Operator: LabelOperatorNotEqual,
			Values:   []string{value},
		}, nil
	}

	// Handle = operator (explicit)
	if matches := equalRegex.FindStringSubmatch(selector); matches != nil {
		key := matches[1]
		value := strings.TrimSpace(matches[2])

		// Validate key format immediately
		if !IsValidLabelKey(key) {
			return nil, fmt.Errorf("invalid label key: %s", key)
		}

		return &LabelSelector{
			Key:      key,
			Operator: LabelOperatorEqual,
			Values:   []string{value},
		}, nil
	}

	// Handle existence checks (!key)
	if matches := notExistsRegex.FindStringSubmatch(selector); matches != nil {
		key := matches[1]

		// Validate key format immediately
		if !IsValidLabelKey(key) {
			return nil, fmt.Errorf("invalid label key: %s", key)
		}

		return &LabelSelector{
			Key:      key,
			Operator: LabelOperatorNotExists,
			Values:   []string{},
		}, nil
	}

	// Handle existence checks (key)
	if matches := existsRegex.FindStringSubmatch(selector); matches != nil {
		key := matches[1]

		// Validate key format immediately
		if !IsValidLabelKey(key) {
			return nil, fmt.Errorf("invalid label key: %s", key)
		}

		return &LabelSelector{
			Key:      key,
			Operator: LabelOperatorExists,
			Values:   []string{},
		}, nil
	}

	return nil, fmt.Errorf("unable to parse label selector: %s", selector)
}

// LabelSelectorsToSQL converts label selectors to SQL WHERE conditions using SQLBuilder
// dataColumn: the JSON column name (usually "data")
// Kubernetes labels are stored in the JSON data under the "label" key
func LabelSelectorsToSQL(selectors []*LabelSelector, dataColumn string, builder *utils.SQLBuilder) error {
	if len(selectors) == 0 {
		return nil
	}

	for _, selector := range selectors {
		labelPath := fmt.Sprintf("%s->'label'->>'%s'", dataColumn, selector.Key)

		switch selector.Operator {
		case LabelOperatorEqual:
			if len(selector.Values) != 1 {
				return fmt.Errorf("equal operator requires exactly one value")
			}
			builder.AddCondition(labelPath+" = %s", selector.Values[0])

		case LabelOperatorNotEqual:
			if len(selector.Values) != 1 {
				return fmt.Errorf("not equal operator requires exactly one value")
			}
			// Include NULL check to match Kubernetes semantics: labels that don't exist are considered "not equal"
			condition := fmt.Sprintf("(%s != %s OR %s IS NULL)", labelPath, "%s", labelPath)
			builder.AddCondition(condition, selector.Values[0])

		case LabelOperatorIn:
			if len(selector.Values) == 0 {
				return fmt.Errorf("in operator requires at least one value")
			}
			// Convert string slice to interface slice for SQLBuilder
			values := make([]interface{}, len(selector.Values))
			for i, v := range selector.Values {
				values[i] = v
			}
			builder.AddIN(labelPath, values)

		case LabelOperatorNotIn:
			if len(selector.Values) == 0 {
				return fmt.Errorf("notin operator requires at least one value")
			}
			// Build NOT IN condition with NULL check
			placeholders := make([]string, len(selector.Values))
			params := make([]interface{}, len(selector.Values))
			for i, value := range selector.Values {
				placeholders[i] = "%s"
				params[i] = value
			}
			inClause := fmt.Sprintf("(%s)", strings.Join(placeholders, ", "))
			condition := fmt.Sprintf("(%s NOT IN %s OR %s IS NULL)", labelPath, inClause, labelPath)
			builder.AddCondition(condition, params...)

		case LabelOperatorExists:
			builder.AddCondition(labelPath + " IS NOT NULL")

		case LabelOperatorNotExists:
			builder.AddCondition(labelPath + " IS NULL")

		default:
			return fmt.Errorf("unsupported label operator: %s", selector.Operator)
		}
	}

	return nil
}

// ValidateLabelSelector validates the syntax of a label selector string
func ValidateLabelSelector(selector string) error {
	parsed, err := ParseLabelSelector(selector)
	if err != nil {
		return err
	}

	// Check for empty selectors with non-empty input
	if len(parsed) == 0 && strings.TrimSpace(selector) != "" {
		return fmt.Errorf("invalid label selector syntax")
	}

	// Validate each selector
	for _, sel := range parsed {
		// Check key format (must be valid Kubernetes label key)
		if !labelKeyRegex.MatchString(sel.Key) {
			return fmt.Errorf("invalid label key: %s", sel.Key)
		}

		// Check values for = and != operators
		if (sel.Operator == LabelOperatorEqual || sel.Operator == LabelOperatorNotEqual) && len(sel.Values) != 1 {
			return fmt.Errorf("operator %s requires exactly one value", sel.Operator)
		}

		// Check values for in/notin operators
		if (sel.Operator == LabelOperatorIn || sel.Operator == LabelOperatorNotIn) && len(sel.Values) == 0 {
			return fmt.Errorf("operator %s requires at least one value", sel.Operator)
		}

		// Check existence operators don't have values
		if (sel.Operator == LabelOperatorExists || sel.Operator == LabelOperatorNotExists) && len(sel.Values) > 0 {
			return fmt.Errorf("operator %s should not have values", sel.Operator)
		}
	}

	return nil
}

// GetSupportedOperators returns the list of supported label operators
func GetSupportedOperators() []LabelOperator {
	return []LabelOperator{
		LabelOperatorEqual,
		LabelOperatorNotEqual,
		LabelOperatorIn,
		LabelOperatorNotIn,
		LabelOperatorExists,
		LabelOperatorNotExists,
	}
}

// IsValidLabelKey checks if a string is a valid Kubernetes label key
func IsValidLabelKey(key string) bool {
	return labelKeyRegex.MatchString(key)
}