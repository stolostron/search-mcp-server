package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCentralizedToolDefinitions(t *testing.T) {
	defs := GetCentralizedToolDefinitions()

	assert.Len(t, defs, 2, "expected 2 tool definitions")

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		assert.NotEmpty(t, d.Name, "tool name must not be empty")
		assert.NotEmpty(t, d.Description, "tool description must not be empty")
		assert.NotNil(t, d.Options, "tool options must not be nil")
		assert.NotNil(t, d.JSONSchema, "tool JSON schema must not be nil")

		props, ok := d.JSONSchema["properties"].(map[string]interface{})
		assert.True(t, ok, "JSON schema must have properties map")
		assert.NotEmpty(t, props, "JSON schema properties must not be empty")
	}

	assert.True(t, names["find_resources"], "must include find_resources")
	assert.True(t, names["find_related_resources"], "must include find_related_resources")
}

func TestGetCentralizedToolDefinitions_FindRelatedRequired(t *testing.T) {
	defs := GetCentralizedToolDefinitions()

	var relatedDef ToolDefinition
	for _, d := range defs {
		if d.Name == "find_related_resources" {
			relatedDef = d
			break
		}
	}

	required, ok := relatedDef.JSONSchema["required"].([]string)
	assert.True(t, ok, "find_related_resources must have required fields")
	assert.Contains(t, required, "uids", "uids must be required")
}

func TestGetCentralizedToolDefinitions_FindRelatedSchema(t *testing.T) {
	defs := GetCentralizedToolDefinitions()

	var relatedDef ToolDefinition
	for _, d := range defs {
		if d.Name == "find_related_resources" {
			relatedDef = d
			break
		}
	}

	props := relatedDef.JSONSchema["properties"].(map[string]interface{})
	expectedFields := []string{"uids", "relatedKinds", "maxHops", "outputMode", "limit"}
	for _, field := range expectedFields {
		_, exists := props[field]
		assert.True(t, exists, "find_related_resources schema must include %s", field)
	}

	maxHops := props["maxHops"].(map[string]interface{})
	assert.Equal(t, "integer", maxHops["type"])
	assert.Equal(t, 0, maxHops["minimum"])
	assert.Equal(t, 10, maxHops["maximum"])

	limit := props["limit"].(map[string]interface{})
	assert.Equal(t, "integer", limit["type"])
	assert.Equal(t, 1, limit["minimum"])
	assert.Equal(t, 1000, limit["maximum"])
}

func TestGetCentralizedToolDefinitions_FindResourcesSchema(t *testing.T) {
	defs := GetCentralizedToolDefinitions()

	var findDef ToolDefinition
	for _, d := range defs {
		if d.Name == "find_resources" {
			findDef = d
			break
		}
	}

	props := findDef.JSONSchema["properties"].(map[string]interface{})
	expectedFields := []string{"kind", "name", "namespace", "cluster", "labelSelector",
		"status", "textSearch", "outputMode", "limit", "sortBy", "sortOrder"}
	for _, field := range expectedFields {
		_, exists := props[field]
		assert.True(t, exists, "find_resources schema must include %s", field)
	}
}

func TestGetMCPTools(t *testing.T) {
	tools := GetMCPTools()

	assert.Len(t, tools, 2, "expected 2 MCP tools")

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
		assert.NotEmpty(t, tool.Name)
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema.Properties)
	}

	assert.True(t, names["find_resources"])
	assert.True(t, names["find_related_resources"])
}

func TestGetMCPTools_FindRelatedHasIntegerTypes(t *testing.T) {
	tools := GetMCPTools()

	var relatedTool interface{}
	for _, tool := range tools {
		if tool.Name == "find_related_resources" {
			relatedTool = tool
			break
		}
	}
	assert.NotNil(t, relatedTool, "find_related_resources tool must exist")
}
