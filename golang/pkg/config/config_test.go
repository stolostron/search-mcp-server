package config

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configuration", func() {
	var originalEnv map[string]string

	BeforeEach(func() {
		// Store original environment variables
		originalEnv = make(map[string]string)
		envVars := []string{
			"DATABASE_URL", "DB_MAX_CONNECTIONS", "DB_IDLE_TIMEOUT", "DB_CONNECT_TIMEOUT",
			"ENABLE_SQL_VALIDATION", "SQL_VALIDATION_LEVEL", "HEALTH_CHECK_INTERVAL",
			"LOG_LEVEL", "DEFAULT_QUERY_TIMEOUT", "MAX_QUERY_ROWS",
		}
		for _, key := range envVars {
			if value := os.Getenv(key); value != "" {
				originalEnv[key] = value
			}
			_ = os.Unsetenv(key)
		}
	})

	AfterEach(func() {
		// Restore original environment variables
		for key, value := range originalEnv {
			_ = os.Setenv(key, value)
		}
		// Clear any we set during tests
		envVars := []string{
			"DATABASE_URL", "DB_MAX_CONNECTIONS", "DB_IDLE_TIMEOUT", "DB_CONNECT_TIMEOUT",
			"ENABLE_SQL_VALIDATION", "SQL_VALIDATION_LEVEL", "HEALTH_CHECK_INTERVAL",
			"LOG_LEVEL", "DEFAULT_QUERY_TIMEOUT", "MAX_QUERY_ROWS",
		}
		for _, key := range envVars {
			if _, exists := originalEnv[key]; !exists {
				_ = os.Unsetenv(key)
			}
		}
	})

	Describe("DefaultConfig", func() {
		It("should return sensible defaults", func() {
			config := DefaultConfig()

			// Database defaults
			Expect(config.MaxConnections).To(Equal(int32(20)))
			Expect(config.IdleTimeout).To(Equal(30 * time.Second))
			Expect(config.ConnectTimeout).To(Equal(2 * time.Second))

			// Security defaults
			Expect(config.EnableSQLValidation).To(BeTrue())
			Expect(config.ValidationLevel).To(Equal("standard"))

			// Operational defaults
			Expect(config.HealthCheckInterval).To(Equal(30 * time.Second))
			Expect(config.LogLevel).To(Equal("info"))

			// Query defaults
			Expect(config.DefaultQueryTimeout).To(Equal(30 * time.Second))
			Expect(config.MaxQueryRows).To(Equal(1000))
		})

		It("should have empty connection string by default", func() {
			config := DefaultConfig()
			Expect(config.ConnectionString).To(BeEmpty())
		})
	})

	Describe("LoadConfig", func() {
		Context("with no environment variables", func() {
			It("should return default configuration", func() {
				config := LoadConfig()
				defaultConfig := DefaultConfig()

				Expect(config).To(Equal(defaultConfig))
			})
		})

		Context("with database environment variables", func() {
			BeforeEach(func() {
				_ = os.Setenv("DATABASE_URL", "postgresql://user:pass@host:5432/db")
				_ = os.Setenv("DB_MAX_CONNECTIONS", "50")
				_ = os.Setenv("DB_IDLE_TIMEOUT", "60")
				_ = os.Setenv("DB_CONNECT_TIMEOUT", "5")
			})

			It("should override database defaults", func() {
				config := LoadConfig()

				Expect(config.ConnectionString).To(Equal("postgresql://user:pass@host:5432/db"))
				Expect(config.MaxConnections).To(Equal(int32(50)))
				Expect(config.IdleTimeout).To(Equal(60 * time.Second))
				Expect(config.ConnectTimeout).To(Equal(5 * time.Second))
			})
		})

		Context("with security environment variables", func() {
			BeforeEach(func() {
				_ = os.Setenv("ENABLE_SQL_VALIDATION", "false")
				_ = os.Setenv("SQL_VALIDATION_LEVEL", "strict")
			})

			It("should override security defaults", func() {
				config := LoadConfig()

				Expect(config.EnableSQLValidation).To(BeFalse())
				Expect(config.ValidationLevel).To(Equal("strict"))
			})
		})

		Context("with operational environment variables", func() {
			BeforeEach(func() {
				_ = os.Setenv("HEALTH_CHECK_INTERVAL", "120")
				_ = os.Setenv("LOG_LEVEL", "debug")
			})

			It("should override operational defaults", func() {
				config := LoadConfig()

				Expect(config.HealthCheckInterval).To(Equal(120 * time.Second))
				Expect(config.LogLevel).To(Equal("debug"))
			})
		})

		Context("with query environment variables", func() {
			BeforeEach(func() {
				_ = os.Setenv("DEFAULT_QUERY_TIMEOUT", "60")
				_ = os.Setenv("MAX_QUERY_ROWS", "5000")
			})

			It("should override query defaults", func() {
				config := LoadConfig()

				Expect(config.DefaultQueryTimeout).To(Equal(60 * time.Second))
				Expect(config.MaxQueryRows).To(Equal(5000))
			})
		})

		Context("with invalid environment variables", func() {
			BeforeEach(func() {
				_ = os.Setenv("DB_MAX_CONNECTIONS", "invalid")
				_ = os.Setenv("DB_IDLE_TIMEOUT", "notanumber")
				_ = os.Setenv("ENABLE_SQL_VALIDATION", "maybe")
			})

			It("should fall back to defaults for invalid values", func() {
				config := LoadConfig()
				defaultConfig := DefaultConfig()

				Expect(config.MaxConnections).To(Equal(defaultConfig.MaxConnections))
				Expect(config.IdleTimeout).To(Equal(defaultConfig.IdleTimeout))
				Expect(config.EnableSQLValidation).To(Equal(defaultConfig.EnableSQLValidation))
			})
		})
	})

	Describe("Validate", func() {
		var config Config

		BeforeEach(func() {
			config = DefaultConfig()
		})

		It("should validate a valid configuration", func() {
			err := config.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should fix invalid MaxConnections", func() {
			config.MaxConnections = 0
			err := config.Validate()

			Expect(err).ToNot(HaveOccurred())
			Expect(config.MaxConnections).To(Equal(int32(1)))
		})

		It("should fix invalid IdleTimeout", func() {
			config.IdleTimeout = 0
			err := config.Validate()

			Expect(err).ToNot(HaveOccurred())
			Expect(config.IdleTimeout).To(Equal(30 * time.Second))
		})

		It("should fix invalid ConnectTimeout", func() {
			config.ConnectTimeout = 0
			err := config.Validate()

			Expect(err).ToNot(HaveOccurred())
			Expect(config.ConnectTimeout).To(Equal(2 * time.Second))
		})
	})

	Describe("Helper Functions", func() {
		Describe("getEnvAsString", func() {
			It("should return environment value when set", func() {
				_ = os.Setenv("TEST_STRING", "test_value")
				value := getEnvAsString("TEST_STRING", "default")
				Expect(value).To(Equal("test_value"))
			})

			It("should return default when environment value not set", func() {
				value := getEnvAsString("NONEXISTENT_VAR", "default")
				Expect(value).To(Equal("default"))
			})
		})

		Describe("getEnvAsInt", func() {
			It("should return environment value when valid integer", func() {
				_ = os.Setenv("TEST_INT", "42")
				value := getEnvAsInt("TEST_INT", 10)
				Expect(value).To(Equal(42))
			})

			It("should return default when environment value is invalid", func() {
				_ = os.Setenv("TEST_INT", "invalid")
				value := getEnvAsInt("TEST_INT", 10)
				Expect(value).To(Equal(10))
			})

			It("should return default when environment value not set", func() {
				value := getEnvAsInt("NONEXISTENT_INT", 10)
				Expect(value).To(Equal(10))
			})
		})

		Describe("getEnvAsInt32", func() {
			It("should return environment value when valid int32", func() {
				_ = os.Setenv("TEST_INT32", "42")
				value := getEnvAsInt32("TEST_INT32", 10)
				Expect(value).To(Equal(int32(42)))
			})

			It("should return default when environment value is invalid", func() {
				_ = os.Setenv("TEST_INT32", "invalid")
				value := getEnvAsInt32("TEST_INT32", 10)
				Expect(value).To(Equal(int32(10)))
			})
		})

		Describe("getEnvAsBool", func() {
			It("should return environment value when valid boolean", func() {
				_ = os.Setenv("TEST_BOOL", "true")
				value := getEnvAsBool("TEST_BOOL", false)
				Expect(value).To(BeTrue())

				_ = os.Setenv("TEST_BOOL", "false")
				value = getEnvAsBool("TEST_BOOL", true)
				Expect(value).To(BeFalse())
			})

			It("should return default when environment value is invalid", func() {
				_ = os.Setenv("TEST_BOOL", "invalid")
				value := getEnvAsBool("TEST_BOOL", true)
				Expect(value).To(BeTrue())
			})
		})

		Describe("getEnvAsDuration", func() {
			It("should return environment value when valid duration (seconds)", func() {
				_ = os.Setenv("TEST_DURATION", "60")
				value := getEnvAsDuration("TEST_DURATION", 10*time.Second)
				Expect(value).To(Equal(60 * time.Second))
			})

			It("should return default when environment value is invalid", func() {
				_ = os.Setenv("TEST_DURATION", "invalid")
				value := getEnvAsDuration("TEST_DURATION", 10*time.Second)
				Expect(value).To(Equal(10 * time.Second))
			})
		})
	})
})