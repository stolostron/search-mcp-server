package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQLBuilder", func() {
	var builder *SQLBuilder

	BeforeEach(func() {
		builder = NewSQLBuilder(1)
	})

	Describe("NewSQLBuilder", func() {
		It("should create a builder with the correct starting index", func() {
			builder := NewSQLBuilder(5)
			Expect(builder.GetNextParamIndex()).To(Equal(5))
			Expect(builder.GetConditionCount()).To(Equal(0))
			Expect(builder.GetParamCount()).To(Equal(0))
		})
	})

	Describe("AddCondition", func() {
		It("should add simple conditions without parameters", func() {
			builder.AddCondition("data->>'status' IS NOT NULL")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'status' IS NOT NULL"))
			Expect(params).To(BeEmpty())
			Expect(builder.GetNextParamIndex()).To(Equal(1))
		})

		It("should add conditions with single parameter", func() {
			builder.AddCondition("data->>'name' = %s", "nginx")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'name' = $1"))
			Expect(params).To(Equal([]interface{}{"nginx"}))
			Expect(builder.GetNextParamIndex()).To(Equal(2))
		})

		It("should add conditions with multiple parameters", func() {
			builder.AddCondition("data->>'name' = %s AND data->>'namespace' = %s", "nginx", "default")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'name' = $1 AND data->>'namespace' = $2"))
			Expect(params).To(Equal([]interface{}{"nginx", "default"}))
			Expect(builder.GetNextParamIndex()).To(Equal(3))
		})

		It("should handle multiple separate conditions", func() {
			builder.AddCondition("data->>'name' = %s", "nginx")
			builder.AddCondition("data->>'namespace' = %s", "default")
			builder.AddCondition("data->>'status' IS NOT NULL")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'name' = $1 AND data->>'namespace' = $2 AND data->>'status' IS NOT NULL"))
			Expect(params).To(Equal([]interface{}{"nginx", "default"}))
			Expect(builder.GetNextParamIndex()).To(Equal(3))
		})
	})

	Describe("AddConditionWithPlaceholders", func() {
		It("should add condition with explicit parameter management", func() {
			builder.AddConditionWithPlaceholders("name IN ($1, $2)", "name1", "name2")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE name IN ($1, $2)"))
			Expect(params).To(Equal([]interface{}{"name1", "name2"}))
			Expect(builder.GetNextParamIndex()).To(Equal(3))
		})
	})

	Describe("AddOR", func() {
		It("should handle empty OR conditions", func() {
			builder.AddOR([]string{})

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal(""))
			Expect(params).To(BeNil())
		})

		It("should handle single OR condition", func() {
			builder.AddOR([]string{"data->>'status' = %s"}, "Running")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'status' = $1"))
			Expect(params).To(Equal([]interface{}{"Running"}))
		})

		It("should handle multiple OR conditions", func() {
			builder.AddOR([]string{
				"data->>'status' = %s",
				"data->>'health' = %s",
			}, "Running", "healthy")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE (data->>'status' = $1 OR data->>'health' = $2)"))
			Expect(params).To(Equal([]interface{}{"Running", "healthy"}))
		})

		It("should handle OR conditions with multiple parameters each", func() {
			builder.AddOR([]string{
				"(data->>'name' = %s AND data->>'namespace' = %s)",
				"(data->>'label' = %s)",
			}, "nginx", "default", "app=web")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE ((data->>'name' = $1 AND data->>'namespace' = $2) OR (data->>'label' = $3))"))
			Expect(params).To(Equal([]interface{}{"nginx", "default", "app=web"}))
		})
	})

	Describe("AddIN", func() {
		It("should handle empty values", func() {
			builder.AddIN("cluster", []interface{}{})

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal(""))
			Expect(params).To(BeNil())
		})

		It("should handle single value", func() {
			builder.AddIN("cluster", []interface{}{"cluster1"})

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE cluster IN ($1)"))
			Expect(params).To(Equal([]interface{}{"cluster1"}))
		})

		It("should handle multiple values", func() {
			builder.AddIN("cluster", []interface{}{"cluster1", "cluster2", "cluster3"})

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE cluster IN ($1, $2, $3)"))
			Expect(params).To(Equal([]interface{}{"cluster1", "cluster2", "cluster3"}))
		})
	})

	Describe("AddLIKE", func() {
		It("should add LIKE condition", func() {
			builder.AddLIKE("data->>'namespace'", "kube-%")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'namespace' LIKE $1"))
			Expect(params).To(Equal([]interface{}{"kube-%"}))
		})
	})

	Describe("AddILIKE", func() {
		It("should add ILIKE condition", func() {
			builder.AddILIKE("data->>'name'", "%nginx%")

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal("WHERE data->>'name' ILIKE $1"))
			Expect(params).To(Equal([]interface{}{"%nginx%"}))
		})
	})

	Describe("Complex scenarios", func() {
		It("should handle mixed condition types", func() {
			builder.AddCondition("data->>'kind' = %s", "Pod")
			builder.AddIN("cluster", []interface{}{"cluster1", "cluster2"})
			builder.AddLIKE("data->>'namespace'", "kube-%")
			builder.AddOR([]string{
				"data->>'status' = %s",
				"data->>'phase' = %s",
			}, "Running", "Active")

			whereClause, params := builder.BuildWhere()
			expectedWhere := "WHERE data->>'kind' = $1 AND cluster IN ($2, $3) AND data->>'namespace' LIKE $4 AND (data->>'status' = $5 OR data->>'phase' = $6)"
			Expect(whereClause).To(Equal(expectedWhere))
			Expect(params).To(Equal([]interface{}{"Pod", "cluster1", "cluster2", "kube-%", "Running", "Active"}))
			Expect(builder.GetNextParamIndex()).To(Equal(7))
		})

		It("should handle chained builders with different starting indices", func() {
			// First builder starts at index 1
			builder1 := NewSQLBuilder(1)
			builder1.AddCondition("data->>'kind' = %s", "Pod")
			builder1.AddCondition("data->>'status' = %s", "Running")

			// Second builder starts where first left off
			builder2 := NewSQLBuilder(builder1.GetNextParamIndex())
			builder2.AddCondition("cluster = %s", "cluster1")
			builder2.AddIN("namespace", []interface{}{"default", "kube-system"})

			// Verify parameter indices don't conflict
			_, params1 := builder1.BuildWhere()
			_, params2 := builder2.BuildWhere()

			Expect(params1).To(Equal([]interface{}{"Pod", "Running"}))
			Expect(params2).To(Equal([]interface{}{"cluster1", "default", "kube-system"}))
			Expect(builder1.GetNextParamIndex()).To(Equal(3))
			Expect(builder2.GetNextParamIndex()).To(Equal(6))
		})
	})

	Describe("BuildConditions", func() {
		It("should return conditions without WHERE keyword", func() {
			builder.AddCondition("data->>'kind' = %s", "Pod")
			builder.AddCondition("data->>'status' = %s", "Running")

			conditions, params := builder.BuildConditions()
			Expect(conditions).To(Equal("data->>'kind' = $1 AND data->>'status' = $2"))
			Expect(params).To(Equal([]interface{}{"Pod", "Running"}))
		})
	})

	Describe("Reset", func() {
		It("should clear conditions and parameters", func() {
			builder.AddCondition("data->>'kind' = %s", "Pod")
			builder.AddIN("cluster", []interface{}{"cluster1", "cluster2"})

			Expect(builder.GetConditionCount()).To(Equal(2))
			Expect(builder.GetParamCount()).To(Equal(3))

			builder.Reset(10)

			Expect(builder.GetConditionCount()).To(Equal(0))
			Expect(builder.GetParamCount()).To(Equal(0))
			Expect(builder.GetNextParamIndex()).To(Equal(10))

			whereClause, params := builder.BuildWhere()
			Expect(whereClause).To(Equal(""))
			Expect(params).To(BeNil())
		})
	})

	Describe("Clone", func() {
		It("should create an independent copy", func() {
			builder.AddCondition("data->>'kind' = %s", "Pod")
			builder.AddIN("cluster", []interface{}{"cluster1"})

			clone := builder.Clone()

			// Verify clone has same content
			originalConditions, originalParams := builder.BuildConditions()
			cloneConditions, cloneParams := clone.BuildConditions()

			Expect(cloneConditions).To(Equal(originalConditions))
			Expect(cloneParams).To(Equal(originalParams))
			Expect(clone.GetNextParamIndex()).To(Equal(builder.GetNextParamIndex()))

			// Verify independence - changes to clone don't affect original
			clone.AddCondition("data->>'status' = %s", "Running")

			Expect(clone.GetConditionCount()).To(Equal(3))
			Expect(builder.GetConditionCount()).To(Equal(2))
		})
	})
})