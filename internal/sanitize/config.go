// Package sanitize provides prompt injection detection and sanitization for
// resource metadata returned by the search-mcp-server find_resources tool.
package sanitize

// FieldPolicy describes how a specific dataMap field should be sanitized.
type FieldPolicy int

const (
	// PolicySanitizeStrings recursively sanitizes string values in the field.
	PolicySanitizeStrings FieldPolicy = iota
	// PolicyRedactFully replaces the entire field value with the redaction marker,
	// regardless of content. Use for fields that should never appear in output.
	PolicyRedactFully
	// PolicySkip skips sanitization entirely. Use for fields whose values are
	// structurally constrained (e.g. DNS labels, RFC3339 timestamps, fixed vocabularies).
	PolicySkip
)

// Config holds sanitization settings.
type Config struct {
	FieldPolicy map[string]FieldPolicy
}

// DefaultConfig returns a Config with production defaults:
//   - DNS-constrained fields (name, namespace, kind, cluster) are skipped
//   - High-risk free-text fields (status, annotation, label) are sanitized
func DefaultConfig() Config {
	return Config{
		FieldPolicy: map[string]FieldPolicy{
			// DNS-constrained: [a-z0-9-.] only — structurally injection-safe
			"name":      PolicySkip,
			"namespace": PolicySkip,
			"kind":      PolicySkip,
			"cluster":   PolicySkip,
			// Timestamp — fixed RFC3339 format
			"created": PolicySkip,
			// High-risk free-text fields
			"status":     PolicySanitizeStrings,
			"annotation": PolicySanitizeStrings,
			"label":      PolicySanitizeStrings,
			// Internal metadata fields
			"_uid":            PolicySkip,
			"_hubClusterName": PolicySkip,
		},
	}
}
