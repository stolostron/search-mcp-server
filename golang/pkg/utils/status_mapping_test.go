package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

var _ = Describe("StatusMapping", func() {

	Describe("GetStatusMapping", func() {
		It("should return correct mapping for known resource types", func() {
			// Test simple category
			mapping := GetStatusMapping("Pod")
			Expect(mapping.Kind).To(Equal("Pod"))
			Expect(mapping.Category).To(Equal(StatusCategorySimple))
			Expect(mapping.Field).ToNot(BeNil())
			Expect(*mapping.Field).To(Equal("status"))
			Expect(mapping.ValidValues).To(ContainElement("Running"))

			// Test custom category
			mapping = GetStatusMapping("Policy")
			Expect(mapping.Kind).To(Equal("Policy"))
			Expect(mapping.Category).To(Equal(StatusCategoryCustom))
			Expect(mapping.Field).ToNot(BeNil())
			Expect(*mapping.Field).To(Equal("compliant"))

			// Test complex category
			mapping = GetStatusMapping("Deployment")
			Expect(mapping.Kind).To(Equal("Deployment"))
			Expect(mapping.Category).To(Equal(StatusCategoryComplex))
			Expect(mapping.HealthLogic).ToNot(BeNil())
		})

		It("should return default mapping for unknown resource types", func() {
			mapping := GetStatusMapping("UnknownResource")
			Expect(mapping.Kind).To(Equal("Unknown"))
			Expect(mapping.Category).To(Equal(StatusCategoryNone))
		})

		It("should handle multi-condition resources", func() {
			mapping := GetStatusMapping("ClusterOperator")
			Expect(mapping.Kind).To(Equal("ClusterOperator"))
			// ClusterOperator appears in both complex and multi-condition - should get first match
			Expect(mapping.Category).To(Equal(StatusCategoryComplex))
		})

		It("should handle nested resources", func() {
			mapping := GetStatusMapping("Application")
			Expect(mapping.Kind).To(Equal("Application"))
			Expect(mapping.Category).To(Equal(StatusCategoryNested))
			Expect(mapping.JSONPath).ToNot(BeNil())
			Expect(*mapping.JSONPath).To(Equal("status.health.status"))
		})
	})

	Describe("HasStatusConcept", func() {
		DescribeTable("should correctly identify resources with/without status",
			func(kind string, expected bool) {
				result := HasStatusConcept(kind)
				Expect(result).To(Equal(expected))
			},
			Entry("Pod has status", "Pod", true),
			Entry("Deployment has status", "Deployment", true),
			Entry("Secret has no status", "Secret", false),
			Entry("ConfigMap has no status", "ConfigMap", false),
			Entry("Unknown resource has no status", "UnknownResource", false),
		)
	})

	Describe("Complex Health Evaluation", func() {
		Describe("evaluateDeploymentHealth", func() {
			It("should return healthy when ready >= desired", func() {
				data := map[string]interface{}{
					"ready":     "3",
					"desired":   "3",
					"available": "3",
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusHealthy))
			})

			It("should return unhealthy when ready is 0", func() {
				data := map[string]interface{}{
					"ready":     "0",
					"desired":   "3",
					"available": "0",
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusUnhealthy))
			})

			It("should return degraded when ready < desired", func() {
				data := map[string]interface{}{
					"ready":     "1",
					"desired":   "3",
					"available": "1",
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusDegraded))
			})

			It("should return unknown when desired is 0", func() {
				data := map[string]interface{}{
					"ready":     "0",
					"desired":   "0",
					"available": "0",
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusUnknown))
			})

			It("should handle numeric types", func() {
				data := map[string]interface{}{
					"ready":     3,
					"desired":   3,
					"available": 3,
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusHealthy))
			})

			It("should handle float types", func() {
				data := map[string]interface{}{
					"ready":     3.0,
					"desired":   3.0,
					"available": 3.0,
				}
				result := evaluateDeploymentHealth(data)
				Expect(result).To(Equal(HealthStatusHealthy))
			})
		})

		Describe("evaluateClusterOperatorHealth", func() {
			It("should return healthy when available=True and degraded=False", func() {
				data := map[string]interface{}{
					"available":   "True",
					"degraded":    "False",
					"progressing": "False",
				}
				result := evaluateClusterOperatorHealth(data)
				Expect(result).To(Equal(HealthStatusHealthy))
			})

			It("should return degraded when available=True and progressing=True", func() {
				data := map[string]interface{}{
					"available":   "True",
					"degraded":    "False",
					"progressing": "True",
				}
				result := evaluateClusterOperatorHealth(data)
				Expect(result).To(Equal(HealthStatusDegraded))
			})

			It("should return unhealthy when available=False", func() {
				data := map[string]interface{}{
					"available":   "False",
					"degraded":    "False",
					"progressing": "False",
				}
				result := evaluateClusterOperatorHealth(data)
				Expect(result).To(Equal(HealthStatusUnhealthy))
			})

			It("should return unhealthy when degraded=True", func() {
				data := map[string]interface{}{
					"available":   "True",
					"degraded":    "True",
					"progressing": "False",
				}
				result := evaluateClusterOperatorHealth(data)
				Expect(result).To(Equal(HealthStatusUnhealthy))
			})
		})

		Describe("EvaluateComplexStatus", func() {
			It("should work for complex category resources", func() {
				data := map[string]interface{}{
					"ready":   "2",
					"desired": "2",
				}
				result := EvaluateComplexStatus("DeploymentConfig", data)
				Expect(result).To(Equal(HealthStatusHealthy))
			})

			It("should return unknown for non-complex resources", func() {
				data := map[string]interface{}{"status": "Running"}
				result := EvaluateComplexStatus("Pod", data)
				Expect(result).To(Equal(HealthStatusUnknown))
			})

			It("should return unknown for unknown resource types", func() {
				data := map[string]interface{}{}
				result := EvaluateComplexStatus("UnknownResource", data)
				Expect(result).To(Equal(HealthStatusUnknown))
			})
		})
	})

	Describe("normalizeStatusInput", func() {
		It("should handle string input", func() {
			result := normalizeStatusInput("Running")
			Expect(result).To(Equal([]string{"Running"}))
		})

		It("should handle comma-separated string", func() {
			result := normalizeStatusInput("Running,Pending,Failed")
			Expect(result).To(Equal([]string{"Running", "Pending", "Failed"}))
		})

		It("should handle string slice", func() {
			result := normalizeStatusInput([]string{"Running", "Pending"})
			Expect(result).To(Equal([]string{"Running", "Pending"}))
		})

		It("should trim whitespace", func() {
			result := normalizeStatusInput("Running, Pending , Failed")
			Expect(result).To(Equal([]string{"Running", "Pending", "Failed"}))
		})

		It("should filter empty strings", func() {
			result := normalizeStatusInput([]string{"Running", "", "Pending", "  "})
			Expect(result).To(Equal([]string{"Running", "Pending"}))
		})

		It("should handle empty input", func() {
			result := normalizeStatusInput("")
			Expect(result).To(BeEmpty())

			result = normalizeStatusInput([]string{})
			Expect(result).To(BeEmpty())

			result = normalizeStatusInput(nil)
			Expect(result).To(BeEmpty())
		})
	})

	Describe("convertJSONPathToSQL", func() {
		It("should convert simple paths", func() {
			result, err := convertJSONPathToSQL("status", "data")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("data->>'status'"))
		})

		It("should convert nested paths", func() {
			result, err := convertJSONPathToSQL("status.health.status", "data")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("data->'status'->'health'->>'status'"))
		})

		It("should handle array indices", func() {
			result, err := convertJSONPathToSQL("status.ingress.0.conditions.0.status", "data")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("data->'status'->'ingress'->0->'conditions'->0->>'status'"))
		})

		It("should handle empty path", func() {
			_, err := convertJSONPathToSQL("", "data")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty JSON path"))
		})

		It("should handle empty path part", func() {
			_, err := convertJSONPathToSQL("status..health", "data")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty path part"))
		})
	})

	Describe("SQL Condition Building", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Describe("buildSimpleStatusConditions", func() {
			It("should handle single status value", func() {
				err := buildSimpleStatusConditions("Running", "status", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'status' = $1"))
				Expect(params).To(Equal([]interface{}{"Running"}))
			})

			It("should handle multiple status values", func() {
				err := buildSimpleStatusConditions([]string{"Running", "Pending"}, "status", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'status' IN ($1, $2)"))
				Expect(params).To(Equal([]interface{}{"Running", "Pending"}))
			})

			It("should handle custom field", func() {
				err := buildSimpleStatusConditions("Compliant", "compliant", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'compliant' = $1"))
				Expect(params).To(Equal([]interface{}{"Compliant"}))
			})

			It("should handle empty status", func() {
				err := buildSimpleStatusConditions([]string{}, "status", "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Describe("buildNestedStatusConditions", func() {
			It("should handle nested JSON path", func() {
				err := buildNestedStatusConditions("Healthy", "status.health.status", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'status'->'health'->>'status' = $1"))
				Expect(params).To(Equal([]interface{}{"Healthy"}))
			})

			It("should handle array indices in path", func() {
				err := buildNestedStatusConditions("True", "status.ingress.0.conditions.0.status", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'status'->'ingress'->0->'conditions'->0->>'status' = $1"))
				Expect(params).To(Equal([]interface{}{"True"}))
			})

			It("should handle multiple values", func() {
				err := buildNestedStatusConditions([]string{"Healthy", "Progressing"}, "status.health.status", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'status'->'health'->>'status' IN ($1, $2)"))
				Expect(params).To(Equal([]interface{}{"Healthy", "Progressing"}))
			})
		})

		Describe("buildMultiConditionStatusConditions", func() {
			It("should handle single status across multiple fields", func() {
				fields := []string{"available", "degraded", "progressing"}
				err := buildMultiConditionStatusConditions("True", fields, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE (data->>'available' = $1 OR data->>'degraded' = $2 OR data->>'progressing' = $3)"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{"True", "True", "True"}))
			})

			It("should handle multiple status values across multiple fields", func() {
				fields := []string{"available", "degraded"}
				err := buildMultiConditionStatusConditions([]string{"True", "False"}, fields, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE (data->>'available' IN ($1, $2) OR data->>'degraded' IN ($3, $4))"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{"True", "False", "True", "False"}))
			})
		})

		Describe("buildTextSearchStatusFallback", func() {
			It("should handle single status value", func() {
				err := buildTextSearchStatusFallback("Running", "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data::text ILIKE $1"))
				Expect(params).To(Equal([]interface{}{`%"Running"%`}))
			})

			It("should handle multiple status values", func() {
				err := buildTextSearchStatusFallback([]string{"Running", "Failed"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE (data::text ILIKE $1 OR data::text ILIKE $2)"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{`%"Running"%`, `%"Failed"%`}))
			})
		})
	})

	Describe("BuildKindAwareStatusConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		It("should handle simple category resources", func() {
			err := BuildKindAwareStatusConditions("Pod", "Running", "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'status' = $1"))
			Expect(params).To(Equal([]interface{}{"Running"}))
		})

		It("should handle custom category resources", func() {
			err := BuildKindAwareStatusConditions("Policy", "Compliant", "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'compliant' = $1"))
			Expect(params).To(Equal([]interface{}{"Compliant"}))
		})

		It("should handle complex category resources", func() {
			err := BuildKindAwareStatusConditions("Deployment", "healthy", "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE 1=1")) // Placeholder condition
			Expect(params).To(BeEmpty())
		})

		It("should handle none category resources", func() {
			err := BuildKindAwareStatusConditions("Secret", "Active", "data", builder)
			Expect(err).ToNot(HaveOccurred())
			Expect(builder.GetConditionCount()).To(Equal(0)) // No conditions added
		})

		It("should handle nested category resources", func() {
			err := BuildKindAwareStatusConditions("Application", "Healthy", "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->'status'->'health'->>'status' = $1"))
			Expect(params).To(Equal([]interface{}{"Healthy"}))
		})

		It("should handle unknown resource types", func() {
			err := BuildKindAwareStatusConditions("UnknownResource", "Active", "data", builder)
			Expect(err).ToNot(HaveOccurred())
			Expect(builder.GetConditionCount()).To(Equal(0)) // Falls back to 'none'
		})

		It("should handle array of kinds with single kind", func() {
			err := BuildKindAwareStatusConditions([]string{"Pod"}, "Running", "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'status' = $1"))
			Expect(params).To(Equal([]interface{}{"Running"}))
		})
	})

	Describe("BuildStatusConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		It("should use kind-aware filtering for known types", func() {
			err := BuildStatusConditions("Running", "data", builder, "Pod")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'status' = $1"))
			Expect(params).To(Equal([]interface{}{"Running"}))
		})

		It("should fall back to text search for unknown types", func() {
			err := BuildStatusConditions("Active", "data", builder, "CustomResource")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data::text ILIKE $1"))
			Expect(params).To(Equal([]interface{}{`%"Active"%`}))
		})

		It("should fall back to text search when no kind provided", func() {
			err := BuildStatusConditions("Running", "data", builder, nil)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data::text ILIKE $1"))
			Expect(params).To(Equal([]interface{}{`%"Running"%`}))
		})
	})

	Describe("PostFilterByComplexStatus", func() {
		It("should filter complex resources correctly", func() {
			// Mock results data structure
			results := []map[string]interface{}{
				{
					"uid":     "deployment-1",
					"cluster": "cluster1",
					"data": map[string]interface{}{
						"kind":      "Deployment",
						"ready":     "3",
						"desired":   "3",
						"available": "3",
					},
				},
				{
					"uid":     "deployment-2",
					"cluster": "cluster1",
					"data": map[string]interface{}{
						"kind":      "Deployment",
						"ready":     "0",
						"desired":   "3",
						"available": "0",
					},
				},
				{
					"uid":     "pod-1",
					"cluster": "cluster1",
					"data": map[string]interface{}{
						"kind":   "Pod",
						"status": "Running",
					},
				},
			}

			// Filter for healthy resources
			filtered := PostFilterByComplexStatus(results, "healthy")

			// Should include healthy deployment and the pod (non-complex)
			Expect(filtered).To(HaveLen(2))

			// Check that the healthy deployment is included
			healthyDeployment := filtered[0]
			Expect(healthyDeployment["uid"]).To(Equal("deployment-1"))

			// Check that the pod is included (non-complex, included as-is)
			pod := filtered[1]
			Expect(pod["uid"]).To(Equal("pod-1"))
		})

		It("should return all results when no filter provided", func() {
			results := []map[string]interface{}{
				{"data": map[string]interface{}{"kind": "Pod"}},
			}

			filtered := PostFilterByComplexStatus(results, nil)
			Expect(filtered).To(Equal(results))
		})

		It("should handle malformed data gracefully", func() {
			results := []map[string]interface{}{
				{"invalid": "data"},
				{"data": "not-a-map"},
				{"data": map[string]interface{}{"no-kind": "value"}},
			}

			filtered := PostFilterByComplexStatus(results, "healthy")
			Expect(filtered).To(BeEmpty())
		})
	})

	Describe("Helper Functions", func() {
		Describe("getIntFromData", func() {
			It("should extract integer from string", func() {
				data := map[string]interface{}{"count": "42"}
				result := getIntFromData(data, "count")
				Expect(result).To(Equal(42))
			})

			It("should extract integer from int", func() {
				data := map[string]interface{}{"count": 42}
				result := getIntFromData(data, "count")
				Expect(result).To(Equal(42))
			})

			It("should extract integer from float", func() {
				data := map[string]interface{}{"count": 42.7}
				result := getIntFromData(data, "count")
				Expect(result).To(Equal(42))
			})

			It("should return 0 for missing key", func() {
				data := map[string]interface{}{}
				result := getIntFromData(data, "count")
				Expect(result).To(Equal(0))
			})

			It("should return 0 for invalid value", func() {
				data := map[string]interface{}{"count": "invalid"}
				result := getIntFromData(data, "count")
				Expect(result).To(Equal(0))
			})
		})

		Describe("getStringFromData", func() {
			It("should extract string from string", func() {
				data := map[string]interface{}{"status": "Ready"}
				result := getStringFromData(data, "status")
				Expect(result).To(Equal("Ready"))
			})

			It("should convert int to string", func() {
				data := map[string]interface{}{"count": 42}
				result := getStringFromData(data, "count")
				Expect(result).To(Equal("42"))
			})

			It("should convert float to string", func() {
				data := map[string]interface{}{"value": 3.14}
				result := getStringFromData(data, "value")
				Expect(result).To(Equal("3.14"))
			})

			It("should convert bool to string", func() {
				data := map[string]interface{}{"available": true}
				result := getStringFromData(data, "available")
				Expect(result).To(Equal("True"))

				data = map[string]interface{}{"available": false}
				result = getStringFromData(data, "available")
				Expect(result).To(Equal("False"))
			})

			It("should return empty string for missing key", func() {
				data := map[string]interface{}{}
				result := getStringFromData(data, "status")
				Expect(result).To(Equal(""))
			})
		})

		Describe("getFirstKind", func() {
			It("should extract string kind", func() {
				result := getFirstKind("Pod")
				Expect(result).To(Equal("Pod"))
			})

			It("should extract first kind from slice", func() {
				result := getFirstKind([]string{"Pod", "Service"})
				Expect(result).To(Equal("Pod"))
			})

			It("should return empty for empty slice", func() {
				result := getFirstKind([]string{})
				Expect(result).To(Equal(""))
			})

			It("should return empty for nil", func() {
				result := getFirstKind(nil)
				Expect(result).To(Equal(""))
			})
		})
	})

	Describe("Utility Functions", func() {
		Describe("GetSupportedResourceKinds", func() {
			It("should return all non-none resource kinds", func() {
				kinds := GetSupportedResourceKinds()

				// Should include known types
				Expect(kinds).To(ContainElement("Pod"))
				Expect(kinds).To(ContainElement("Deployment"))
				Expect(kinds).To(ContainElement("Policy"))

				// Should not include none-category types
				Expect(kinds).ToNot(ContainElement("Secret"))
				Expect(kinds).ToNot(ContainElement("ConfigMap"))

				// Should have expected count (41+ mappings minus none category)
				Expect(len(kinds)).To(BeNumerically(">=", 25)) // At least 25+ with status
			})
		})

		Describe("GetStatusCategories", func() {
			It("should return all status categories", func() {
				categories := GetStatusCategories()
				Expect(categories).To(ConsistOf(
					StatusCategorySimple,
					StatusCategoryCustom,
					StatusCategoryComplex,
					StatusCategoryMultiCondition,
					StatusCategoryNested,
					StatusCategoryNone,
				))
			})
		})

		Describe("GetHealthStatuses", func() {
			It("should return all health statuses", func() {
				statuses := GetHealthStatuses()
				Expect(statuses).To(ConsistOf(
					HealthStatusHealthy,
					HealthStatusUnhealthy,
					HealthStatusDegraded,
					HealthStatusUnknown,
				))
			})
		})
	})
})