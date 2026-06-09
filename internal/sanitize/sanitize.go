package sanitize

import (
	"fmt"
	"log"
)

// RedactionMarker is the string used to replace values containing injection patterns.
const RedactionMarker = "[REDACTED: potential prompt injection detected]"

// Sanitizer applies prompt injection sanitization to resource metadata.
// Detected injection patterns are always redacted. It is safe to use from
// multiple goroutines.
type Sanitizer struct {
	cfg Config
}

// New returns a Sanitizer with the given configuration.
// It makes a defensive copy of cfg.FieldPolicy so that the caller cannot
// affect the Sanitizer's behavior by mutating the map after construction.
func New(cfg Config) *Sanitizer {
	fp := make(map[string]FieldPolicy, len(cfg.FieldPolicy))
	for k, v := range cfg.FieldPolicy {
		fp[k] = v
	}
	cfg.FieldPolicy = fp
	return &Sanitizer{cfg: cfg}
}

// SanitizeString checks a single string value for injection patterns.
// If a pattern is detected the RedactionMarker is returned and detected is true.
// Otherwise the original value and false are returned.
func (s *Sanitizer) SanitizeString(field, value string) (string, bool) {
	if !InjectionDetected(value) {
		return value, false
	}
	log.Printf("[sanitize] prompt injection pattern detected in field %q", field)
	return RedactionMarker, true
}

// SanitizeResourceDataMap sanitizes all fields in a resource dataMap according
// to the configured FieldPolicy for each key. Fields with PolicySkip are copied
// unchanged. Fields with PolicyRedactFully have their entire value replaced.
// All other fields are recursively sanitized with sanitizeValue.
//
// The input map is never mutated; a new map is always returned.
func (s *Sanitizer) SanitizeResourceDataMap(dataMap map[string]interface{}) map[string]interface{} {
	if dataMap == nil {
		return nil
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
			log.Printf("[sanitize] fully redacting field %q", k)
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
