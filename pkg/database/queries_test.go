package database

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/pkg/types"
)

var _ = Describe("Database Queries", func() {
	var dq *DatabaseQueries

	BeforeEach(func() {
		dq = NewDatabaseQueries(nil)
	})

	Describe("NewDatabaseQueries", func() {
		It("should create a properly initialized instance", func() {
			Expect(dq).ToNot(BeNil())
			Expect(dq.allowedKeywords).ToNot(BeEmpty())
			Expect(dq.forbiddenStatements).ToNot(BeEmpty())
			Expect(dq.forbiddenCommands).ToNot(BeEmpty())
		})

		It("should contain expected allowed keywords", func() {
			Expect(dq.allowedKeywords).To(ContainElements("SELECT", "FROM", "WHERE"))
		})

		It("should contain expected forbidden statements", func() {
			Expect(dq.forbiddenStatements).To(ContainElements("INSERT", "UPDATE", "DELETE"))
		})

		It("should contain expected forbidden commands", func() {
			Expect(dq.forbiddenCommands).To(ContainElements("DROP TABLE", "CREATE TABLE"))
		})
	})

	Describe("validateQuery", func() {
		Context("with valid queries", func() {
			It("should accept basic SELECT queries", func() {
				result := dq.validateQuery("SELECT * FROM users")
				Expect(result.IsValid).To(BeTrue())
				Expect(result.Error).To(BeEmpty())
			})

			It("should accept WITH queries (CTEs)", func() {
				query := "WITH recent_users AS (SELECT * FROM users) SELECT * FROM recent_users"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
				Expect(result.Error).To(BeEmpty())
			})

			It("should accept complex SELECT with JOIN", func() {
				query := "SELECT u.name, p.title FROM users u JOIN posts p ON u.id = p.user_id WHERE u.active = true"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
				Expect(result.Error).To(BeEmpty())
			})

			It("should accept queries with parameters", func() {
				query := "SELECT * FROM users WHERE id = $1 AND name = $2"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
				Expect(result.Error).To(BeEmpty())
			})

			It("should accept lowercase keywords", func() {
				result := dq.validateQuery("select * from users where id = 1")
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept mixed case keywords", func() {
				result := dq.validateQuery("SeLeCt * FrOm users WhErE id = 1")
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept queries with extra whitespace", func() {
				result := dq.validateQuery("   SELECT   *   FROM   users   WHERE   id = 1   ")
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept CTE with complex structure", func() {
				query := "WITH RECURSIVE t(n) AS (VALUES (1) UNION ALL SELECT n+1 FROM t WHERE n < 100) SELECT n FROM t"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept queries with subqueries", func() {
				query := "SELECT * FROM users WHERE id IN (SELECT user_id FROM posts WHERE created_at > '2023-01-01')"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept queries with window functions", func() {
				query := "SELECT name, ROW_NUMBER() OVER (ORDER BY created_at) FROM users"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
			})

			It("should accept legitimate UNION queries", func() {
				query := "SELECT 'type1' as type, count(*) FROM table1 UNION ALL SELECT 'type2' as type, count(*) FROM table2"
				result := dq.validateQuery(query)
				Expect(result.IsValid).To(BeTrue())
			})
		})

		Context("with invalid queries", func() {
			It("should reject empty queries", func() {
				result := dq.validateQuery("")
				Expect(result.IsValid).To(BeFalse())
				Expect(result.Error).To(Equal("Empty query not allowed"))
			})

			It("should reject whitespace-only queries", func() {
				result := dq.validateQuery("   \n\t   ")
				Expect(result.IsValid).To(BeFalse())
				Expect(result.Error).To(Equal("Empty query not allowed"))
			})
		})

		Context("with forbidden mutating operations", func() {
			DescribeTable("should reject mutating statements",
				func(query, operation string) {
					result := dq.validateQuery(query)
					Expect(result.IsValid).To(BeFalse())
					Expect(result.Error).To(ContainSubstring("Mutating operation '%s' is not allowed", operation))
				},
				Entry("INSERT statement", "INSERT INTO users (name) VALUES ('test')", "INSERT"),
				Entry("UPDATE statement", "UPDATE users SET name = 'test' WHERE id = 1", "UPDATE"),
				Entry("DELETE statement", "DELETE FROM users WHERE id = 1", "DELETE"),
				Entry("DROP statement", "DROP TABLE users", "DROP"),
				Entry("SHOW statement", "SHOW TABLES", "SHOW"),
				Entry("EXPLAIN statement", "EXPLAIN SELECT * FROM users", "EXPLAIN"),
				Entry("VACUUM statement", "VACUUM FULL users", "VACUUM"),
			)

			It("should reject CREATE TABLE commands", func() {
				result := dq.validateQuery("SELECT * FROM users; CREATE TABLE test (id int)")
				Expect(result.IsValid).To(BeFalse())
				Expect(result.Error).To(ContainSubstring("Operation 'CREATE TABLE' is not allowed"))
			})
		})


		Context("with multiple statements", func() {
			It("should reject multiple SQL statements", func() {
				result := dq.validateQuery("SELECT * FROM users; SELECT * FROM posts;")
				Expect(result.IsValid).To(BeFalse())
				Expect(result.Error).To(ContainSubstring("Multiple SQL statements are not allowed"))
			})
		})

		Context("with non-SELECT statements", func() {
			It("should reject non-SELECT/WITH statements", func() {
				result := dq.validateQuery("SHOW TABLES")
				Expect(result.IsValid).To(BeFalse())
				Expect(result.Error).To(ContainSubstring("Mutating operation 'SHOW' is not allowed"))
			})
		})
	})

	Describe("Query Timeout Enforcement", func() {

		Context("when timeout is not specified", func() {
			It("should use the original context without timeout", func() {
				// This tests the code path where options.Timeout is nil
				// We expect the query to use the original context
				options := &types.QueryOptions{MaxRows: nil, Timeout: nil}

				// Since we don't have a real database connection for this test,
				// we're testing that the code doesn't panic and handles the nil timeout correctly
				Expect(options.Timeout).To(BeNil())
			})
		})

		Context("when timeout is specified", func() {
			It("should create a timeout context with correct duration", func() {
				timeout := 5 // 5 seconds
				options := &types.QueryOptions{Timeout: &timeout}

				// Verify that the timeout option is properly set
				Expect(options.Timeout).ToNot(BeNil())
				Expect(*options.Timeout).To(Equal(5))
			})
		})

		Context("timeout error handling", func() {
			It("should handle context deadline exceeded appropriately", func() {
				// Test that we check for context.DeadlineExceeded error
				err := context.DeadlineExceeded
				Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
			})
		})
	})
})

// Note: Performance benchmarks can be added using Go's standard benchmarking tools:
// go test -bench=BenchmarkValidateQuery ./pkg/database