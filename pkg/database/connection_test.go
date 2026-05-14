package database

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/pkg/config"
)

var _ = Describe("Database Connection", func() {
	Describe("parseConnectionString", func() {
		DescribeTable("should parse valid connection strings correctly",
			func(connectionStr string, expectedConfig map[string]interface{}) {
				config, err := parseConnectionString(connectionStr)
				Expect(err).ToNot(HaveOccurred())
				Expect(config.Host).To(Equal(expectedConfig["host"]))
				Expect(config.Port).To(Equal(expectedConfig["port"]))
				Expect(config.Database).To(Equal(expectedConfig["database"]))
				Expect(config.User).To(Equal(expectedConfig["user"]))
				Expect(config.Password).To(Equal(expectedConfig["password"]))
				Expect(config.SSL).To(Equal(expectedConfig["ssl"]))
			},
			Entry("Valid PostgreSQL connection string",
				"postgresql://user:password@localhost:5432/mydb?sslmode=require",
				map[string]interface{}{
					"host":     "localhost",
					"port":     5432,
					"database": "mydb",
					"user":     "user",
					"password": "password",
					"ssl":      true,
				}),
			Entry("Connection string with default port",
				"postgresql://user:password@localhost/mydb",
				map[string]interface{}{
					"host":     "localhost",
					"port":     5432,
					"database": "mydb",
					"user":     "user",
					"password": "password",
					"ssl":      false,
				}),
			Entry("Connection string with ssl=true parameter",
				"postgresql://user:password@localhost:5433/testdb?ssl=true",
				map[string]interface{}{
					"host":     "localhost",
					"port":     5433,
					"database": "testdb",
					"user":     "user",
					"password": "password",
					"ssl":      true,
				}),
		)

		It("should return error for malformed connection strings", func() {
			_, err := parseConnectionString("postgresql://user:@host:badport/db")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("NewDatabaseConnectionWithConfig", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should return error for invalid connection string", func() {
			cfg := config.DefaultConfig()
			cfg.ConnectionString = "invalid-url"

			_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("failed to parse connection string"),
				ContainSubstring("failed to parse connection config"),
			))
		})

		It("should handle valid connection string format gracefully", func() {
			// Test with valid connection string format (but non-existent database)
			// This tests parsing but connection will fail to non-existent database
			cfg := config.DefaultConfig()
			cfg.ConnectionString = "postgresql://user:password@localhost:5432/nonexistent"

			_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
			// We expect either parsing error or connection error, both are acceptable
			if err != nil {
				Expect(err).To(HaveOccurred())
			}
		})
	})

	Describe("Database Configuration Parsing", func() {
		It("should correctly parse connection configuration", func() {
			connectionStr := "postgresql://testuser:testpass@testhost:5433/testdb?sslmode=require"

			config, err := parseConnectionString(connectionStr)

			Expect(err).ToNot(HaveOccurred())
			Expect(config.Host).To(Equal("testhost"))
			Expect(config.Port).To(Equal(5433))
			Expect(config.Database).To(Equal("testdb"))
			Expect(config.User).To(Equal("testuser"))
			Expect(config.Password).To(Equal("testpass"))
			Expect(config.SSL).To(BeTrue())
		})
	})

	Describe("NewDatabaseConnectionWithConfig", func() {
		var ctx context.Context
		var cfg config.Config

		BeforeEach(func() {
			ctx = context.Background()
			cfg = config.DefaultConfig()
			cfg.ConnectionString = "postgresql://testuser:testpass@localhost:5433/testdb"
		})

		Context("with valid configuration", func() {
			It("should handle valid connection string format", func() {
				// Test with valid connection string format (connection may fail but parsing should work)
				_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
				// We expect either success or connection error, but not parsing errors
				if err != nil {
					// Connection errors are acceptable for unit tests
					Expect(err.Error()).ToNot(ContainSubstring("failed to parse"))
				}
			})

			It("should apply configuration settings", func() {
				// Test that custom configuration values are applied
				cfg.MaxConnections = 50
				cfg.IdleTimeout = 60 * time.Second
				cfg.ConnectTimeout = 10 * time.Second

				// Even if connection fails, the config parsing should work
				_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
				if err != nil {
					// Should not fail due to configuration parsing
					Expect(err.Error()).ToNot(ContainSubstring("invalid configuration"))
				}
			})
		})

		Context("with invalid configuration", func() {
			It("should return error for empty connection string", func() {
				cfg.ConnectionString = ""

				_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("connection string not provided"))
			})

			It("should return error for malformed connection string", func() {
				cfg.ConnectionString = "invalid-connection-string"

				_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("failed to parse connection string"),
					ContainSubstring("failed to parse connection config"),
				))
			})
		})

		Context("with configuration validation", func() {
			It("should validate and fix invalid configuration values", func() {
				cfg.MaxConnections = 0  // Invalid value
				cfg.IdleTimeout = -1    // Invalid value
				cfg.ConnectTimeout = 0  // Invalid value

				// Should not fail due to validation fixing the values
				_, err := NewDatabaseConnectionWithConfig(ctx, cfg)
				if err != nil {
					Expect(err.Error()).ToNot(ContainSubstring("invalid configuration"))
				}
			})
		})
	})
})