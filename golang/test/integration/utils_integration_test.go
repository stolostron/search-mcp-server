//go:build integration

package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/utils"
	"github.com/stolostron/search-mcp-server/pkg/config"
	"github.com/stolostron/search-mcp-server/pkg/database"
	utilsPkg "github.com/stolostron/search-mcp-server/pkg/utils"
)

var testTime = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

var _ = Describe("Utils Database Integration Tests", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		dbConn *database.DatabaseConnection
		db     *database.DatabaseQueries
	)

	BeforeEach(func() {
		// Use standard config system - reads DATABASE_URL environment variable
		cfg := config.LoadConfig()
		if cfg.ConnectionString == "" {
			// Fallback to test default if DATABASE_URL not set
			cfg.ConnectionString = defaultTestDatabaseURL
		}

		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)

		var err error
		dbConn, err = database.NewDatabaseConnectionWithConfig(ctx, cfg)
		Expect(err).ToNot(HaveOccurred(), "Failed to create database connection")

		db = database.NewDatabaseQueries(dbConn)
		setupUtilsTestData(dbConn, ctx)
	})

	AfterEach(func() {
		if dbConn != nil {
			dbConn.Close()
		}
		cancel()
	})

	Describe("Phase 3A + Phase 3B SQL Execution", func() {

		It("should execute namespace and kind filtering", func() {
			// Test Phase 3A cross-resource filtering
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildNamespaceConditions([]string{"kube-system"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildKindConditions([]string{"Pod"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf("SELECT data->>'name' as name, data->>'namespace' as namespace, data->>'kind' as kind FROM test_resources %s", whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute namespace + kind query")

			// Should find the pod in kube-system
			Expect(result.Rows).To(HaveLen(1))
			Expect(result.Rows[0][0]).To(Equal("coredns-pod"))
			Expect(result.Rows[0][1]).To(Equal("kube-system"))
			Expect(result.Rows[0][2]).To(Equal("Pod"))
		})

		It("should execute multi-namespace filtering with wildcards", func() {
			// Test wildcard namespace filtering
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildNamespaceConditions([]string{"kube-*", "default"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf("SELECT data->>'name' as name, data->>'namespace' as namespace FROM test_resources %s ORDER BY data->>'name'", whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute wildcard namespace query")

			// Should find resources in kube-system and default namespaces
			Expect(len(result.Rows)).To(BeNumerically(">=", 2))

			// Verify namespaces match patterns
			for _, row := range result.Rows {
				namespace := row[1].(string)
				Expect([]string{"kube-system", "default"}).To(ContainElement(namespace))
			}
		})

		It("should execute multi-kind filtering", func() {
			// Test multiple kinds
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildKindConditions([]string{"Pod", "Deployment"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf("SELECT data->>'name' as name, data->>'kind' as kind FROM test_resources %s ORDER BY data->>'kind', data->>'name'", whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute multi-kind query")

			// Should find pods and deployment
			Expect(len(result.Rows)).To(Equal(3)) // 2 pods + 1 deployment

			// Verify kinds
			for _, row := range result.Rows {
				kind := row[1].(string)
				Expect([]string{"Pod", "Deployment"}).To(ContainElement(kind))
			}
		})

		It("should execute status filtering for simple resource types", func() {
			// Test Phase 3B status mapping for Pod (simple type)
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildKindConditions([]string{"Pod"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildStatusConditions("Running", "data", builder, "Pod")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf("SELECT data->>'name' as name, data->>'status' as status FROM test_resources %s", whereClause)

			// Debug: check what SQL was generated
			fmt.Printf("Pod status SQL: %s\n", sql)
			fmt.Printf("Pod status params: %v\n", params)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute status query")

			fmt.Printf("Pod status result: %v\n", result.Rows)

			// Should find the running pod
			Expect(result.Rows).To(HaveLen(1))
			Expect(result.Rows[0][0]).To(Equal("coredns-pod"))
			Expect(result.Rows[0][1]).To(Equal("Running"))
		})

		It("should handle complex resource types correctly", func() {
			// Test status filtering for complex types (validates SQL generation)
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildKindConditions([]string{"Deployment"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildStatusConditions("healthy", "data", builder, "Deployment")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()

			// For complex types, should use placeholder (1=1) for status
			Expect(whereClause).To(ContainSubstring("data->>'kind' = $"))
			Expect(whereClause).To(ContainSubstring("1=1")) // Placeholder for complex status
			Expect(params).To(ContainElement("Deployment"))

			// Test that we can find the deployment by kind only (complex status handled elsewhere)
			sql := fmt.Sprintf("SELECT data->>'name' as name FROM test_resources WHERE data->>'kind' = $1")
			result, err := db.ExecuteQuery(ctx, sql, []interface{}{"Deployment"}, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute kind-only query")

			Expect(result.Rows).To(HaveLen(1))
			Expect(result.Rows[0][0]).To(Equal("nginx-deployment"))
		})

		It("should execute text search fallback for unknown resource types", func() {
			// Test text search fallback
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildStatusConditions("Active", "data", builder, "UnknownCustomResource")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf("SELECT data->>'name' as name FROM test_resources %s", whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute text search query")

			// Should find the custom resource containing "Active"
			Expect(result.Rows).To(HaveLen(1))
			Expect(result.Rows[0][0]).To(Equal("custom-resource"))
		})

		It("should combine multiple filters in complex queries", func() {
			// Test combining namespace, kind, and status filters
			builder := utils.NewSQLBuilder(1)

			err := utilsPkg.BuildNamespaceConditions([]string{"default", "kube-system"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildKindConditions([]string{"Pod", "Deployment"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()
			sql := fmt.Sprintf(`
				SELECT data->>'name' as name,
				       data->>'kind' as kind,
				       data->>'namespace' as namespace
				FROM test_resources %s
				ORDER BY data->>'kind', data->>'name'
			`, whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute complex combined query")

			// Should find pods and deployment in specified namespaces
			Expect(len(result.Rows)).To(Equal(3)) // coredns-pod, nginx-deployment, failed-pod

			// Verify each result matches constraints
			for _, row := range result.Rows {
				kind := row[1].(string)
				namespace := row[2].(string)

				Expect([]string{"Pod", "Deployment"}).To(ContainElement(kind))
				Expect([]string{"default", "kube-system"}).To(ContainElement(namespace))
			}
		})

		It("should handle parameter ordering correctly", func() {
			// Test that parameters are ordered correctly across multiple conditions
			builder := utils.NewSQLBuilder(1)

			// Add conditions in specific order
			err := utilsPkg.BuildKindConditions([]string{"Pod"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildNamespaceConditions([]string{"kube-system"}, "data", builder)
			Expect(err).ToNot(HaveOccurred())

			err = utilsPkg.BuildStatusConditions("Running", "data", builder, "Pod")
			Expect(err).ToNot(HaveOccurred())

			whereClause, params := builder.BuildWhere()

			// Verify parameter order matches SQL placeholders
			Expect(params).To(Equal([]interface{}{"Pod", "kube-system", "Running"}))
			Expect(whereClause).To(ContainSubstring("$1"))
			Expect(whereClause).To(ContainSubstring("$2"))
			Expect(whereClause).To(ContainSubstring("$3"))

			sql := fmt.Sprintf("SELECT data->>'name' as name FROM test_resources %s", whereClause)

			result, err := db.ExecuteQuery(ctx, sql, params, nil)
			Expect(err).ToNot(HaveOccurred(), "Failed to execute parameter order test query")

			Expect(result.Rows).To(HaveLen(1))
			Expect(result.Rows[0][0]).To(Equal("coredns-pod"))
		})
	})
})

// setupUtilsTestData creates test data for utils integration testing
func setupUtilsTestData(db *database.DatabaseConnection, ctx context.Context) {
	// Drop table if exists
	_, _ = db.Query(ctx, "DROP TABLE IF EXISTS test_resources")

	// Create test table matching ACM search.resources structure
	createSQL := `
		CREATE TABLE test_resources (
			uid VARCHAR(100) PRIMARY KEY,
			cluster VARCHAR(100),
			data JSONB NOT NULL
		)
	`
	_, err := db.Query(ctx, createSQL)
	Expect(err).ToNot(HaveOccurred(), "Failed to create test_resources table")

	// Insert test data (only the Kubernetes resource data goes in the data column)
	testResources := []struct {
		uid     string
		cluster string
		data    string
	}{
		{
			uid:     "pod-1-uid",
			cluster: "cluster1",
			data: `{
				"apiVersion": "v1",
				"kind": "Pod",
				"name": "coredns-pod",
				"namespace": "kube-system",
				"created": "2024-01-15T10:00:00Z",
				"label": {
					"app": "coredns"
				},
				"status": "Running"
			}`,
		},
		{
			uid:     "deployment-1-uid",
			cluster: "cluster1",
			data: `{
				"apiVersion": "apps/v1",
				"kind": "Deployment",
				"name": "nginx-deployment",
				"namespace": "default",
				"created": "2024-01-15T11:00:00Z",
				"label": {
					"app": "nginx"
				},
				"replicas": 3,
				"ready": 3,
				"available": 3,
				"desired": 3
			}`,
		},
		{
			uid:     "pod-2-uid",
			cluster: "cluster1",
			data: `{
				"apiVersion": "v1",
				"kind": "Pod",
				"name": "failed-pod",
				"namespace": "default",
				"created": "2024-01-15T09:00:00Z",
				"status": "Failed"
			}`,
		},
		{
			uid:     "custom-1-uid",
			cluster: "cluster1",
			data: `{
				"apiVersion": "custom.io/v1",
				"kind": "UnknownCustomResource",
				"name": "custom-resource",
				"namespace": "default",
				"created": "2024-01-15T12:00:00Z",
				"spec": {
					"state": "Active",
					"config": "enabled"
				}
			}`,
		},
	}

	for i, resource := range testResources {
		insertSQL := `INSERT INTO test_resources (uid, cluster, data) VALUES ($1, $2, $3::jsonb)`
		_, err := db.Query(ctx, insertSQL, resource.uid, resource.cluster, resource.data)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to insert test resource %d", i+1))
	}

	// Cleanup function
	DeferCleanup(func() {
		_, _ = db.Query(context.Background(), "DROP TABLE IF EXISTS test_resources")
	})
}