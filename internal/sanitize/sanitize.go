package sanitize

import (
	"fmt"
	"log"
)

// RedactionMarker is the string used to replace redacted values in block mode.
const RedactionMarker = "[REDACTED: potential prompt injection detected]"

// Sanitizer applies prompt injection sanitization to resource metadata.
// It is safe to use from multiple goroutines.
type Sanitizer struct {
	cfg Config
}

// New returns a Sanitizer with the given configuration.
func New(cfg Config) *Sanitizer {
	return &Sanitizer{cfg: cfg}
}

// SanitizeString checks a single string value for injection patterns.
// It returns the (possibly redacted) string and whether a detection occurred.
//
//   - ModeAllow: always returns (value, false) — no-op.
//   - ModeWarn:  logs a warning, returns (value, true) — value is unchanged.
//   - ModeBlock: returns (RedactionMarker, true) — value is replaced.
func (s *Sanitizer) SanitizeString(field, value string) (string, bool) {
	if s.cfg.Mode == ModeAllow {
		return value, false
	}
	if !InjectionDetected(value) {
		return value, false
	}
	log.Printf("[sanitize] prompt injection pattern detected in field %q (mode=%s)", field, s.cfg.Mode)
	if s.cfg.Mode == ModeWarn {
		return value, true
	}
	// ModeBlock
	return RedactionMarker, true
}

// SanitizeResourceDataMap sanitizes all fields in a resource dataMap according
// to the configured FieldPolicy for each key. Fields with PolicySkip are copied
// unchanged. Fields with PolicyRedactFully have their entire value replaced.
// All other fields are recursively sanitized with sanitizeValue.
//
// The input map is never mutated; a new map is always returned.
func (s *Sanitizer) SanitizeResourceDataMap(dataMap map[string]interface{}) map[string]interface{} {
	if s.cfg.Mode == ModeAllow {
		return dataMap
	}
	result := make(map[string]interface{}, len(dataMap))
	for k, v := range dataMap {
		policy, ok := s.cfg.FieldPolicy[k]
		if !ok {
			policy = PolicySanitizeStrings
		}
		switch policy {
		case PolicySkip:
			result[k] = v
		case PolicyRedactFully:
			log.Printf("[sanitize] fully redacting field %q (mode=%s)", k, s.cfg.Mode)
			result[k] = RedactionMarker
		default: // PolicySanitizeStrings
			result[k] = s.sanitizeValue(k, v)
		}
	}
	return result
}

// sanitizeValue recursively sanitizes an arbitrary value.
// Strings are checked against injection patterns.
// Maps and slices are traversed recursively.
// All other types (numbers, bools, nil) are returned unchanged.
func (s *Sanitizer) sanitizeValue(field string, v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		sanitized, _ := s.SanitizeString(field, val)
		return sanitized
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, inner := range val {
			out[k] = s.sanitizeValue(fmt.Sprintf("%s.%s", field, k), inner)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = s.sanitizeValue(fmt.Sprintf("%s[%d]", field, i), item)
		}
		return out
	default:
		// Numbers, bools, nil — not injectable
		return v
	}
}
