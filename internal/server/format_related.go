package server

import (
	"fmt"
	"strings"

	"github.com/stolostron/search-mcp-server/internal/findrelated"
)

// FormatRelatedResult formats a FindRelatedResult as a text string for MCP responses.
func FormatRelatedResult(result *findrelated.FindRelatedResult) string {
	var out strings.Builder

	fmt.Fprintf(&out, "# Related Resources\n\n")
	fmt.Fprintf(&out, "**Seeds:** %d | **Max hops:** %d | **Total related:** %d | **Time:** %dms\n\n",
		len(result.Metadata.SeedUIDs),
		result.Metadata.MaxHops,
		result.Metadata.TotalRelated,
		result.Metadata.ExecutionTime,
	)

	if len(result.Groups) == 0 {
		out.WriteString("No related resources found.\n")
		return out.String()
	}

	for _, group := range result.Groups {
		fmt.Fprintf(&out, "## %s (%d)\n\n", group.Kind, group.Count)

		if len(group.Items) == 0 {
			continue
		}

		out.WriteString("| Name | Namespace | Cluster |\n")
		out.WriteString("|------|-----------|----------|\n")

		for _, item := range group.Items {
			ns := "-"
			if item.Namespace != nil {
				ns = *item.Namespace
			}
			fmt.Fprintf(&out, "| %s | %s | %s |\n", escapeRelatedMarkdown(item.Name), escapeRelatedMarkdown(ns), escapeRelatedMarkdown(item.Cluster))
		}
		out.WriteString("\n")
	}

	return out.String()
}

func escapeRelatedMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"|", "\\|",
		"*", "\\*",
		"_", "\\_",
		"`", "\\`",
		"#", "\\#",
		"[", "\\[",
		"]", "\\]",
		"\n", " ",
		"\r", " ",
		"\t", " ",
	)
	escaped := replacer.Replace(text)
	for strings.Contains(escaped, "  ") {
		escaped = strings.ReplaceAll(escaped, "  ", " ")
	}
	return strings.TrimSpace(escaped)
}
