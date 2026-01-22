package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

var _ = Describe("LabelSelectors", func() {

	Describe("ParseLabelSelector", func() {
		Context("with empty or invalid input", func() {
			DescribeTable("should handle empty inputs",
				func(selector string, expectedCount int) {
					result, err := ParseLabelSelector(selector)
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(HaveLen(expectedCount))
				},
				Entry("empty string", "", 0),
				Entry("whitespace only", "   ", 0),
				Entry("tabs and spaces", "\t  \n  ", 0),
			)
		})

		Context("with single selectors", func() {
			DescribeTable("should parse basic operators correctly",
				func(selector string, expectedKey string, expectedOp LabelOperator, expectedValues []string) {
					result, err := ParseLabelSelector(selector)
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(HaveLen(1))

					sel := result[0]
					Expect(sel.Key).To(Equal(expectedKey))
					Expect(sel.Operator).To(Equal(expectedOp))
					Expect(sel.Values).To(Equal(expectedValues))
				},
				// Equal operator
				Entry("simple equals", "app=nginx", "app", LabelOperatorEqual, []string{"nginx"}),
				Entry("equals with spaces", "app = nginx", "app", LabelOperatorEqual, []string{"nginx"}),
				Entry("equals with complex value", "version=1.2.3", "version", LabelOperatorEqual, []string{"1.2.3"}),

				// Not equal operator
				Entry("not equals", "app!=nginx", "app", LabelOperatorNotEqual, []string{"nginx"}),
				Entry("not equals with spaces", "app != nginx", "app", LabelOperatorNotEqual, []string{"nginx"}),

				// In operator
				Entry("in single value", "app in (nginx)", "app", LabelOperatorIn, []string{"nginx"}),
				Entry("in multiple values", "app in (nginx,apache)", "app", LabelOperatorIn, []string{"nginx", "apache"}),
				Entry("in with spaces", "app in (nginx, apache, tomcat)", "app", LabelOperatorIn, []string{"nginx", "apache", "tomcat"}),

				// Not in operator
				Entry("notin single value", "app notin (nginx)", "app", LabelOperatorNotIn, []string{"nginx"}),
				Entry("notin multiple values", "app notin (nginx,apache)", "app", LabelOperatorNotIn, []string{"nginx", "apache"}),

				// Existence operators
				Entry("exists", "app", "app", LabelOperatorExists, []string{}),
				Entry("not exists", "!app", "app", LabelOperatorNotExists, []string{}),
			)
		})

		Context("with multiple selectors", func() {
			It("should parse comma-separated selectors", func() {
				result, err := ParseLabelSelector("app=nginx,env=prod")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(2))

				Expect(result[0].Key).To(Equal("app"))
				Expect(result[0].Operator).To(Equal(LabelOperatorEqual))
				Expect(result[0].Values).To(Equal([]string{"nginx"}))

				Expect(result[1].Key).To(Equal("env"))
				Expect(result[1].Operator).To(Equal(LabelOperatorEqual))
				Expect(result[1].Values).To(Equal([]string{"prod"}))
			})

			It("should handle complex mixed selectors", func() {
				result, err := ParseLabelSelector("app=nginx,env in (prod,staging),tier!=frontend,version,!debug")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(5))

				Expect(result[0]).To(Equal(&LabelSelector{Key: "app", Operator: LabelOperatorEqual, Values: []string{"nginx"}}))
				Expect(result[1]).To(Equal(&LabelSelector{Key: "env", Operator: LabelOperatorIn, Values: []string{"prod", "staging"}}))
				Expect(result[2]).To(Equal(&LabelSelector{Key: "tier", Operator: LabelOperatorNotEqual, Values: []string{"frontend"}}))
				Expect(result[3]).To(Equal(&LabelSelector{Key: "version", Operator: LabelOperatorExists, Values: []string{}}))
				Expect(result[4]).To(Equal(&LabelSelector{Key: "debug", Operator: LabelOperatorNotExists, Values: []string{}}))
			})

			It("should handle whitespace around commas", func() {
				result, err := ParseLabelSelector("app=nginx , env=prod  ,  tier=frontend")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				for _, sel := range result {
					Expect(sel.Operator).To(Equal(LabelOperatorEqual))
				}
			})
		})

		Context("with parentheses in 'in' and 'notin' operators", func() {
			It("should not split on commas inside parentheses", func() {
				result, err := ParseLabelSelector("app in (nginx,apache),env=prod")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(2))

				Expect(result[0].Key).To(Equal("app"))
				Expect(result[0].Operator).To(Equal(LabelOperatorIn))
				Expect(result[0].Values).To(Equal([]string{"nginx", "apache"}))

				Expect(result[1].Key).To(Equal("env"))
				Expect(result[1].Operator).To(Equal(LabelOperatorEqual))
			})

			It("should handle nested commas correctly", func() {
				result, err := ParseLabelSelector("tier in (value1,value2,value3),app=nginx")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(2))

				Expect(result[0].Values).To(Equal([]string{"value1", "value2", "value3"}))
			})
		})

		Context("with invalid input", func() {
			DescribeTable("should return errors for malformed selectors",
				func(selector string, expectedErrorPattern string) {
					_, err := ParseLabelSelector(selector)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(expectedErrorPattern))
				},
				Entry("unclosed parenthesis", "app in (nginx", "unclosed parenthesis"),
				Entry("unexpected closing paren", "app) = nginx", "unexpected closing parenthesis"),
				Entry("invalid syntax", "app === nginx", "unable to parse label selector"),
				Entry("malformed in", "app in nginx", "unable to parse label selector"),
				Entry("empty key", "=nginx", "unable to parse label selector"),
			)
		})
	})

	Describe("LabelSelectorsToSQL", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty selectors", func() {
			It("should not add any conditions", func() {
				err := LabelSelectorsToSQL([]*LabelSelector{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with equal operator", func() {
			It("should generate correct SQL condition", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorEqual,
					Values:   []string{"nginx"},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'label'->>'app' = $1"))
				Expect(params).To(Equal([]interface{}{"nginx"}))
			})
		})

		Context("with not equal operator", func() {
			It("should include NULL check", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorNotEqual,
					Values:   []string{"nginx"},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE (data->'label'->>'app' != $1 OR data->'label'->>'app' IS NULL)"))
				Expect(params).To(Equal([]interface{}{"nginx"}))
			})
		})

		Context("with in operator", func() {
			It("should generate IN clause", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorIn,
					Values:   []string{"nginx", "apache"},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'label'->>'app' IN ($1, $2)"))
				Expect(params).To(Equal([]interface{}{"nginx", "apache"}))
			})
		})

		Context("with notin operator", func() {
			It("should generate NOT IN clause with NULL check", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorNotIn,
					Values:   []string{"nginx", "apache"},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE (data->'label'->>'app' NOT IN ($1, $2) OR data->'label'->>'app' IS NULL)"))
				Expect(params).To(Equal([]interface{}{"nginx", "apache"}))
			})
		})

		Context("with exists operator", func() {
			It("should generate IS NOT NULL condition", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorExists,
					Values:   []string{},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'label'->>'app' IS NOT NULL"))
				Expect(params).To(BeEmpty())
			})
		})

		Context("with notexists operator", func() {
			It("should generate IS NULL condition", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorNotExists,
					Values:   []string{},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->'label'->>'app' IS NULL"))
				Expect(params).To(BeEmpty())
			})
		})

		Context("with multiple selectors", func() {
			It("should combine with AND", func() {
				selectors := []*LabelSelector{
					{Key: "app", Operator: LabelOperatorEqual, Values: []string{"nginx"}},
					{Key: "env", Operator: LabelOperatorIn, Values: []string{"prod", "staging"}},
				}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE data->'label'->>'app' = $1 AND data->'label'->>'env' IN ($2, $3)"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{"nginx", "prod", "staging"}))
			})
		})

		Context("with custom data column", func() {
			It("should use custom column name", func() {
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorEqual,
					Values:   []string{"nginx"},
				}}

				err := LabelSelectorsToSQL(selectors, "resource_data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, _ := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE resource_data->'label'->>'app' = $1"))
			})
		})

		Context("with existing conditions in builder", func() {
			It("should chain properly with existing conditions", func() {
				// Add existing condition
				builder.AddCondition("kind = %s", "Pod")

				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorEqual,
					Values:   []string{"nginx"},
				}}

				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE kind = $1 AND data->'label'->>'app' = $2"))
				Expect(params).To(Equal([]interface{}{"Pod", "nginx"}))
			})
		})

		Context("with invalid operators", func() {
			DescribeTable("should return errors for invalid configurations",
				func(selector *LabelSelector, expectedErrorPattern string) {
					err := LabelSelectorsToSQL([]*LabelSelector{selector}, "data", builder)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(expectedErrorPattern))
				},
				Entry("equal with no values", &LabelSelector{Key: "app", Operator: LabelOperatorEqual, Values: []string{}}, "equal operator requires exactly one value"),
				Entry("equal with multiple values", &LabelSelector{Key: "app", Operator: LabelOperatorEqual, Values: []string{"a", "b"}}, "equal operator requires exactly one value"),
				Entry("in with no values", &LabelSelector{Key: "app", Operator: LabelOperatorIn, Values: []string{}}, "in operator requires at least one value"),
				Entry("notin with no values", &LabelSelector{Key: "app", Operator: LabelOperatorNotIn, Values: []string{}}, "notin operator requires at least one value"),
			)
		})
	})

	Describe("ValidateLabelSelector", func() {
		DescribeTable("should validate selector syntax",
			func(selector string, shouldBeValid bool, expectedErrorPattern string) {
				err := ValidateLabelSelector(selector)
				if shouldBeValid {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(HaveOccurred())
					if expectedErrorPattern != "" {
						Expect(err.Error()).To(ContainSubstring(expectedErrorPattern))
					}
				}
			},
			// Valid selectors
			Entry("simple equals", "app=nginx", true, ""),
			Entry("not equals", "app!=nginx", true, ""),
			Entry("in operator", "app in (nginx,apache)", true, ""),
			Entry("notin operator", "app notin (nginx,apache)", true, ""),
			Entry("exists", "app", true, ""),
			Entry("not exists", "!app", true, ""),
			Entry("complex valid", "app=nginx,env in (prod,staging),tier!=frontend", true, ""),
			Entry("empty string", "", true, ""),

			// Invalid selectors
			Entry("invalid key format - start with number", "1app=nginx", false, "invalid label key"),
			Entry("invalid key format - special chars", "app@=nginx", false, "invalid label key"),
			Entry("malformed syntax", "app === nginx", false, "unable to parse"),
			Entry("unclosed parenthesis", "app in (nginx", false, "unclosed parenthesis"),
		)

		Context("with operator-specific validations", func() {
			It("should reject equal operator with multiple values", func() {
				// This would never be produced by the parser, but test validation logic
				selectors := []*LabelSelector{{
					Key:      "app",
					Operator: LabelOperatorEqual,
					Values:   []string{"nginx", "apache"}, // Invalid: should be exactly one
				}}

				builder := utils.NewSQLBuilder(1)
				err := LabelSelectorsToSQL(selectors, "data", builder)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("exactly one value"))
			})
		})
	})

	Describe("IsValidLabelKey", func() {
		DescribeTable("should validate Kubernetes label key format",
			func(key string, expected bool) {
				result := IsValidLabelKey(key)
				Expect(result).To(Equal(expected))
			},
			// Valid keys
			Entry("simple alphanumeric", "app", true),
			Entry("with numbers", "app123", true),
			Entry("with hyphens", "app-name", true),
			Entry("with dots", "app.example.com", true),
			Entry("with underscores", "app_name", true),
			Entry("mixed valid chars", "my-app.v1_2", true),

			// Invalid keys
			Entry("starts with hyphen", "-app", false),
			Entry("ends with hyphen", "app-", false),
			Entry("starts with dot", ".app", false),
			Entry("ends with dot", "app.", false),
			Entry("starts with underscore", "_app", false),
			Entry("ends with underscore", "app_", false),
			Entry("special characters", "app@name", false),
			Entry("empty string", "", false),
			Entry("single char", "a", true), // Actually valid in Kubernetes
		)
	})

	Describe("GetSupportedOperators", func() {
		It("should return all supported operators", func() {
			operators := GetSupportedOperators()
			Expect(operators).To(ConsistOf(
				LabelOperatorEqual,
				LabelOperatorNotEqual,
				LabelOperatorIn,
				LabelOperatorNotIn,
				LabelOperatorExists,
				LabelOperatorNotExists,
			))
		})
	})

	Describe("Integration with real-world examples", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		It("should handle Kubernetes pod selector example", func() {
			selector := "app=nginx,tier!=cache,environment in (production,qa),!temporary"
			selectors, err := ParseLabelSelector(selector)
			Expect(err).ToNot(HaveOccurred())
			Expect(selectors).To(HaveLen(4))

			err = LabelSelectorsToSQL(selectors, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(ContainSubstring("data->'label'->>'app' = $1"))
			Expect(whereClause).To(ContainSubstring("data->'label'->>'tier' != $2"))
			Expect(whereClause).To(ContainSubstring("data->'label'->>'environment' IN ($3, $4)"))
			Expect(whereClause).To(ContainSubstring("data->'label'->>'temporary' IS NULL"))

			Expect(params).To(Equal([]interface{}{"nginx", "cache", "production", "qa"}))
		})
	})
})