package sanitize

import "regexp"

// injectionPatterns is the compiled set of regex patterns used to detect prompt
// injection attempts in resource metadata. Patterns are case-insensitive.
//
// Categories covered:
//  1. Direct role/instruction overrides
//  2. Persona / role-play injection
//  3. LLM special token / delimiter attacks
//  4. Tool invocation injection
var injectionPatterns = []*regexp.Regexp{
	// --- Direct role/instruction overrides ---
	// "ignore previous instructions", "ignore all previous instructions"
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?previous\s+instructions?`),
	// "disregard all prior instructions / context"
	regexp.MustCompile(`(?i)disregard\s+(all\s+)?prior\s+`),
	// "forget your instructions", "forget all instructions"
	regexp.MustCompile(`(?i)forget\s+(your|all)\s+instructions?`),
	// "new instructions:", "updated instructions:"
	regexp.MustCompile(`(?i)\b(new|updated)\s+instructions?\s*:`),
	// "system prompt:"
	regexp.MustCompile(`(?i)\bsystem\s+prompt\s*:`),
	// "override instructions"
	regexp.MustCompile(`(?i)\boverride\s+instructions?\b`),

	// --- Persona / role-play injection ---
	// "you are now", "you are a"
	regexp.MustCompile(`(?i)\byou\s+are\s+now\b`),
	regexp.MustCompile(`(?i)\byou\s+are\s+a\b`),
	// "act as", "act like"
	regexp.MustCompile(`(?i)\bact\s+as\b`),
	regexp.MustCompile(`(?i)\bact\s+like\b`),
	// "pretend you are", "pretend to be"
	regexp.MustCompile(`(?i)\bpretend\s+(you\s+are|to\s+be)\b`),
	// "roleplay as"
	regexp.MustCompile(`(?i)\broleplay\s+as\b`),
	// "from now on"
	regexp.MustCompile(`(?i)\bfrom\s+now\s+on\b`),

	// --- LLM special tokens / delimiter attacks ---
	// Common instruction delimiters injected into data
	regexp.MustCompile(`(?i)\[SYSTEM\]`),
	regexp.MustCompile(`(?i)\[INST\]`),
	regexp.MustCompile(`(?i)\[/INST\]`),
	regexp.MustCompile(`(?i)\[USER\]`),
	regexp.MustCompile(`(?i)\[ASSISTANT\]`),
	// Llama/Mistral special tokens
	regexp.MustCompile(`<\|im_start\|>`),
	regexp.MustCompile(`<\|im_end\|>`),
	regexp.MustCompile(`<\|system\|>`),
	regexp.MustCompile(`<\|user\|>`),
	regexp.MustCompile(`<\|assistant\|>`),
	// Common prompt section markers
	regexp.MustCompile(`(?i)###\s*(system|instruction|human|assistant)\b`),
	// XML-style instruction tags
	regexp.MustCompile(`(?i)<(system|instruction|prompt)\s*>`),

	// --- Tool invocation injection ---
	// "call tool find_resources", "invoke the find_resources tool"
	regexp.MustCompile(`(?i)\bcall\s+tool\s+\w+`),
	regexp.MustCompile(`(?i)\binvoke\s+(the\s+)?\w+\s+tool\b`),
	// Explicit tool names
	regexp.MustCompile(`(?i)\buse\s+the\s+find_resources\s+tool\b`),
}

// InjectionDetected returns true if s matches any known injection pattern.
func InjectionDetected(s string) bool {
	for _, p := range injectionPatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}
