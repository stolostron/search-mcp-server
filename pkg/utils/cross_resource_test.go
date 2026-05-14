package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

var _ = Describe("CrossResource", func() {

	Describe("BuildClusterConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty clusters", func() {
			It("should not add any conditions", func() {
				err := BuildClusterConditions([]string{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})

			It("should ignore empty strings", func() {
				err := BuildClusterConditions([]string{"", "  ", "\t"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with single cluster", func() {
			It("should generate equality condition", func() {
				err := BuildClusterConditions([]string{"cluster1"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE cluster = $1"))
				Expect(params).To(Equal([]interface{}{"cluster1"}))
			})

			It("should handle whitespace", func() {
				err := BuildClusterConditions([]string{" cluster1 "}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE cluster = $1"))
				Expect(params).To(Equal([]interface{}{"cluster1"}))
			})
		})

		Context("with multiple clusters", func() {
			It("should generate IN condition", func() {
				err := BuildClusterConditions([]string{"cluster1", "cluster2", "cluster3"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE cluster IN ($1, $2, $3)"))
				Expect(params).To(Equal([]interface{}{"cluster1", "cluster2", "cluster3"}))
			})

			It("should filter out empty strings", func() {
				err := BuildClusterConditions([]string{"cluster1", "", "cluster2"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE cluster IN ($1, $2)"))
				Expect(params).To(Equal([]interface{}{"cluster1", "cluster2"}))
			})
		})
	})

	Describe("BuildKindConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty kinds", func() {
			It("should not add any conditions", func() {
				err := BuildKindConditions([]string{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with single kind", func() {
			It("should generate equality condition for JSON field", func() {
				err := BuildKindConditions([]string{"Pod"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'kind' = $1"))
				Expect(params).To(Equal([]interface{}{"Pod"}))
			})
		})

		Context("with multiple kinds", func() {
			It("should generate IN condition for JSON field", func() {
				err := BuildKindConditions([]string{"Pod", "Service", "Deployment"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'kind' IN ($1, $2, $3)"))
				Expect(params).To(Equal([]interface{}{"Pod", "Service", "Deployment"}))
			})
		})

		Context("with custom data column", func() {
			It("should use custom column name", func() {
				err := BuildKindConditions([]string{"Node"}, "resource_data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, _ := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE resource_data->>'kind' = $1"))
			})
		})
	})

	Describe("BuildNameConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty names", func() {
			It("should not add any conditions", func() {
				err := BuildNameConditions([]string{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with exact name matching", func() {
			It("should generate equality condition for single name", func() {
				err := BuildNameConditions([]string{"my-pod"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'name' = $1"))
				Expect(params).To(Equal([]interface{}{"my-pod"}))
			})

			It("should generate OR condition for multiple names", func() {
				err := BuildNameConditions([]string{"pod1", "pod2"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE (data->>'name' = $1 OR data->>'name' = $2)"))
				Expect(params).To(Equal([]interface{}{"pod1", "pod2"}))
			})
		})

		Context("with wildcard patterns", func() {
			It("should convert asterisk to LIKE pattern", func() {
				err := BuildNameConditions([]string{"my-pod-*"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'name' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"my-pod-%"}))
			})

			It("should convert question mark to LIKE pattern", func() {
				err := BuildNameConditions([]string{"pod-?"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'name' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"pod-_"}))
			})

			It("should handle mixed wildcard patterns", func() {
				err := BuildNameConditions([]string{"app-*-?-test"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'name' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"app-%-_-test"}))
			})
		})

		Context("with mixed exact and wildcard patterns", func() {
			It("should generate proper OR condition", func() {
				err := BuildNameConditions([]string{"exact-name", "wildcard-*", "another-exact"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expected := "WHERE (data->>'name' = $1 OR data->>'name' LIKE $2 OR data->>'name' = $3)"
				Expect(whereClause).To(Equal(expected))
				Expect(params).To(Equal([]interface{}{"exact-name", "wildcard-%", "another-exact"}))
			})
		})
	})

	Describe("BuildNamespaceConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty namespaces", func() {
			It("should not add any conditions", func() {
				err := BuildNamespaceConditions([]string{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with exact namespace matching", func() {
			It("should generate equality condition for single namespace", func() {
				err := BuildNamespaceConditions([]string{"default"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'namespace' = $1"))
				Expect(params).To(Equal([]interface{}{"default"}))
			})

			It("should generate OR condition for multiple namespaces", func() {
				err := BuildNamespaceConditions([]string{"default", "kube-system"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE (data->>'namespace' = $1 OR data->>'namespace' = $2)"))
				Expect(params).To(Equal([]interface{}{"default", "kube-system"}))
			})
		})

		Context("with wildcard patterns", func() {
			It("should handle kube-* pattern", func() {
				err := BuildNamespaceConditions([]string{"kube-*"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'namespace' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"kube-%"}))
			})

			It("should handle openshift-* pattern", func() {
				err := BuildNamespaceConditions([]string{"openshift-*"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'namespace' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"openshift-%"}))
			})

			It("should handle single character wildcard", func() {
				err := BuildNamespaceConditions([]string{"app-ns-?"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'namespace' LIKE $1"))
				Expect(params).To(Equal([]interface{}{"app-ns-_"}))
			})
		})

		Context("with mixed exact and wildcard patterns", func() {
			It("should generate proper OR condition", func() {
				err := BuildNamespaceConditions([]string{"default", "kube-*", "openshift-monitoring"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expected := "WHERE (data->>'namespace' = $1 OR data->>'namespace' LIKE $2 OR data->>'namespace' = $3)"
				Expect(whereClause).To(Equal(expected))
				Expect(params).To(Equal([]interface{}{"default", "kube-%", "openshift-monitoring"}))
			})
		})
	})

	Describe("BuildTextSearchConditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty search terms", func() {
			It("should not add any conditions", func() {
				err := BuildTextSearchConditions([]string{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})

			It("should ignore empty strings", func() {
				err := BuildTextSearchConditions([]string{"", "  "}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with single search term", func() {
			It("should search across name, namespace, and full text", func() {
				err := BuildTextSearchConditions([]string{"nginx"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expected := "WHERE (data->>'name' ILIKE $1 OR data->>'namespace' ILIKE $2 OR data::text ILIKE $3)"
				Expect(whereClause).To(Equal(expected))
				Expect(params).To(Equal([]interface{}{"%nginx%", "%nginx%", "%nginx%"}))
			})
		})

		Context("with multiple search terms", func() {
			It("should add separate OR conditions for each term", func() {
				err := BuildTextSearchConditions([]string{"nginx", "pod"}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expected := "WHERE (data->>'name' ILIKE $1 OR data->>'namespace' ILIKE $2 OR data::text ILIKE $3) AND (data->>'name' ILIKE $4 OR data->>'namespace' ILIKE $5 OR data::text ILIKE $6)"
				Expect(whereClause).To(Equal(expected))
				Expect(params).To(Equal([]interface{}{"%nginx%", "%nginx%", "%nginx%", "%pod%", "%pod%", "%pod%"}))
			})
		})

		Context("with custom data column", func() {
			It("should use custom column name", func() {
				err := BuildTextSearchConditions([]string{"test"}, "resource_data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expected := "WHERE (resource_data->>'name' ILIKE $1 OR resource_data->>'namespace' ILIKE $2 OR resource_data::text ILIKE $3)"
				Expect(whereClause).To(Equal(expected))
				Expect(params).To(Equal([]interface{}{"%test%", "%test%", "%test%"}))
			})
		})
	})

	Describe("hasWildcards", func() {
		DescribeTable("should detect wildcard characters",
			func(input string, expected bool) {
				result := hasWildcards(input)
				Expect(result).To(Equal(expected))
			},
			Entry("no wildcards", "simple-name", false),
			Entry("asterisk wildcard", "name-*", true),
			Entry("question mark wildcard", "name-?", true),
			Entry("both wildcards", "na*me-?", true),
			Entry("empty string", "", false),
			Entry("only asterisk", "*", true),
			Entry("only question mark", "?", true),
		)
	})

	Describe("convertWildcardToLike", func() {
		DescribeTable("should convert wildcard patterns to SQL LIKE patterns",
			func(input string, expected string) {
				result := convertWildcardToLike(input)
				Expect(result).To(Equal(expected))
			},
			Entry("asterisk to percent", "name-*", "name-%"),
			Entry("question mark to underscore", "name-?", "name-_"),
			Entry("mixed wildcards", "na*me-?-test", "na%me-_-test"),
			Entry("no wildcards", "exact-name", "exact-name"),
			Entry("multiple asterisks", "**", "%%"),
			Entry("multiple question marks", "??", "__"),
			Entry("complex pattern", "*-app-?-*", "%-app-_-%"),
		)
	})

	Describe("GetSupportedFilterOperators", func() {
		It("should return all supported operators", func() {
			operators := GetSupportedFilterOperators()
			Expect(operators).To(ConsistOf(
				FilterOperatorEqual,
				FilterOperatorNotEqual,
				FilterOperatorIn,
				FilterOperatorNotIn,
				FilterOperatorLike,
				FilterOperatorILike,
			))
		})
	})

	Describe("GetSupportedFilterFields", func() {
		It("should return all supported fields", func() {
			fields := GetSupportedFilterFields()
			Expect(fields).To(ConsistOf(
				"cluster",
				"kind",
				"name",
				"namespace",
				"textSearch",
			))
		})
	})

	Describe("Integration with existing conditions", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		It("should chain properly with existing conditions", func() {
			// Add existing condition
			builder.AddCondition("kind = %s", "Pod")

			// Add cluster condition
			err := BuildClusterConditions([]string{"cluster1"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			// Add namespace condition
			err = BuildNamespaceConditions([]string{"default"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			expectedWhere := "WHERE kind = $1 AND cluster = $2 AND data->>'namespace' = $3"
			Expect(whereClause).To(Equal(expectedWhere))
			Expect(params).To(Equal([]interface{}{"Pod", "cluster1", "default"}))
		})

		It("should handle complex combinations", func() {
			// Add multiple filter types
			err := BuildKindConditions([]string{"Pod", "Service"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = BuildNameConditions([]string{"nginx-*"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = BuildNamespaceConditions([]string{"default", "kube-*"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(ContainSubstring("data->>'kind' IN"))
			Expect(whereClause).To(ContainSubstring("data->>'name' LIKE"))
			Expect(whereClause).To(ContainSubstring("data->>'namespace' = $"))
			Expect(whereClause).To(ContainSubstring("data->>'namespace' LIKE"))
			Expect(len(params)).To(Equal(5)) // 2 kinds + 1 name pattern + 2 namespace patterns
		})
	})
})