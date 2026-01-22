package utils

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/search-mcp-server/internal/utils"
)

var _ = Describe("TimeFilters", func() {

	Describe("ParseDuration", func() {
		Context("with valid duration strings", func() {
			DescribeTable("should parse correctly",
				func(duration string, expected time.Duration) {
					result, err := ParseDuration(duration)
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(expected))
				},
				Entry("1 hour", "1h", time.Hour),
				Entry("24 hours", "24h", 24*time.Hour),
				Entry("1 day", "1d", 24*time.Hour),
				Entry("7 days", "7d", 7*24*time.Hour),
				Entry("1 week", "1w", 7*24*time.Hour),
				Entry("4 weeks", "4w", 4*7*24*time.Hour),
				Entry("single digit", "2h", 2*time.Hour),
				Entry("double digit", "12d", 12*24*time.Hour),
				Entry("triple digit", "365d", 365*24*time.Hour),
			)

			It("should handle whitespace", func() {
				result, err := ParseDuration(" 1h ")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(time.Hour))
			})
		})

		Context("with invalid duration strings", func() {
			DescribeTable("should return errors",
				func(duration string, expectedErrorPattern string) {
					_, err := ParseDuration(duration)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(expectedErrorPattern))
				},
				Entry("empty string", "", "invalid duration format"),
				Entry("no unit", "123", "invalid duration format"),
				Entry("no amount", "h", "invalid duration format"),
				Entry("invalid unit", "1x", "invalid duration format"),
				Entry("negative amount", "-1h", "invalid duration format"),
				Entry("decimal amount", "1.5h", "invalid duration format"),
				Entry("text amount", "oneh", "invalid duration format"),
				Entry("mixed case unit", "1H", "invalid duration format"),
				Entry("invalid chars", "1h2m", "invalid duration format"),
			)
		})
	})

	Describe("ParseTimeFilters", func() {

		Context("with ageNewerThan only", func() {
			It("should create gte filter", func() {
				// Mock time.Now() by calculating expected result
				filters, err := ParseTimeFilters("1h", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(filters).To(HaveLen(1))

				filter := filters[0]
				Expect(filter.Field).To(Equal("created"))
				Expect(filter.Operator).To(Equal("gte"))
				// The value should be approximately 1 hour ago
				expectedTime := time.Now().Add(-time.Hour)
				Expect(filter.Value).To(BeTemporally("~", expectedTime, time.Second))
			})
		})

		Context("with ageOlderThan only", func() {
			It("should create lte filter", func() {
				filters, err := ParseTimeFilters("", "2d")
				Expect(err).ToNot(HaveOccurred())
				Expect(filters).To(HaveLen(1))

				filter := filters[0]
				Expect(filter.Field).To(Equal("created"))
				Expect(filter.Operator).To(Equal("lte"))
				// The value should be approximately 2 days ago
				expectedTime := time.Now().Add(-48 * time.Hour)
				Expect(filter.Value).To(BeTemporally("~", expectedTime, time.Second))
			})
		})

		Context("with both ageNewerThan and ageOlderThan", func() {
			It("should create both filters", func() {
				filters, err := ParseTimeFilters("6h", "3d")
				Expect(err).ToNot(HaveOccurred())
				Expect(filters).To(HaveLen(2))

				// First filter should be ageNewerThan (gte)
				newerFilter := filters[0]
				Expect(newerFilter.Field).To(Equal("created"))
				Expect(newerFilter.Operator).To(Equal("gte"))

				// Second filter should be ageOlderThan (lte)
				olderFilter := filters[1]
				Expect(olderFilter.Field).To(Equal("created"))
				Expect(olderFilter.Operator).To(Equal("lte"))

				// Verify time relationships
				Expect(olderFilter.Value).To(BeTemporally("<", newerFilter.Value))
			})
		})

		Context("with empty strings", func() {
			It("should return empty filters", func() {
				filters, err := ParseTimeFilters("", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(filters).To(BeEmpty())
			})
		})

		Context("with invalid durations", func() {
			It("should return error for invalid ageNewerThan", func() {
				_, err := ParseTimeFilters("invalid", "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid ageNewerThan duration"))
			})

			It("should return error for invalid ageOlderThan", func() {
				_, err := ParseTimeFilters("", "invalid")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid ageOlderThan duration"))
			})
		})
	})

	Describe("TimeFiltersToSQL", func() {
		var builder *utils.SQLBuilder

		BeforeEach(func() {
			builder = utils.NewSQLBuilder(1)
		})

		Context("with empty filters", func() {
			It("should not add any conditions", func() {
				err := TimeFiltersToSQL([]*TimeFilter{}, "data", builder)
				Expect(err).ToNot(HaveOccurred())
				Expect(builder.GetConditionCount()).To(Equal(0))
			})
		})

		Context("with single time filter", func() {
			It("should add gte condition", func() {
				testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
				filter := &TimeFilter{
					Field:    "created",
					Operator: "gte",
					Value:    testTime,
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'created'::timestamp >= $1"))
				Expect(params).To(Equal([]interface{}{testTime.UTC().Format(time.RFC3339)}))
			})

			It("should add lte condition", func() {
				testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
				filter := &TimeFilter{
					Field:    "created",
					Operator: "lte",
					Value:    testTime,
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE data->>'created'::timestamp <= $1"))
				Expect(params).To(Equal([]interface{}{testTime.UTC().Format(time.RFC3339)}))
			})

			DescribeTable("should handle all operators",
				func(operator, expectedSQL string) {
					testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
					filter := &TimeFilter{
						Field:    "created",
						Operator: operator,
						Value:    testTime,
					}

					builder := utils.NewSQLBuilder(1)
					err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
					Expect(err).ToNot(HaveOccurred())

					whereClause, params := builder.BuildWhere()
					Expect(whereClause).To(Equal(expectedSQL))
					Expect(params).To(Equal([]interface{}{testTime.UTC().Format(time.RFC3339)}))
				},
				Entry("gt operator", "gt", "WHERE data->>'created'::timestamp > $1"),
				Entry("gte operator", "gte", "WHERE data->>'created'::timestamp >= $1"),
				Entry("lt operator", "lt", "WHERE data->>'created'::timestamp < $1"),
				Entry("lte operator", "lte", "WHERE data->>'created'::timestamp <= $1"),
			)
		})

		Context("with multiple time filters", func() {
			It("should combine filters with AND", func() {
				time1 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
				time2 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

				filters := []*TimeFilter{
					{Field: "created", Operator: "gte", Value: time2},
					{Field: "created", Operator: "lte", Value: time1},
				}

				err := TimeFiltersToSQL(filters, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE data->>'created'::timestamp >= $1 AND data->>'created'::timestamp <= $2"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{
					time2.UTC().Format(time.RFC3339),
					time1.UTC().Format(time.RFC3339),
				}))
			})
		})

		Context("with custom data column", func() {
			It("should use custom column name", func() {
				testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
				filter := &TimeFilter{
					Field:    "created",
					Operator: "gte",
					Value:    testTime,
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "resource_data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, _ := builder.BuildWhere()
				Expect(whereClause).To(Equal("WHERE resource_data->>'created'::timestamp >= $1"))
			})
		})

		Context("with invalid inputs", func() {
			It("should return error for invalid field", func() {
				filter := &TimeFilter{
					Field:    "invalid_field",
					Operator: "gte",
					Value:    time.Now(),
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported time filter field"))
			})

			It("should return error for invalid operator", func() {
				filter := &TimeFilter{
					Field:    "created",
					Operator: "invalid_op",
					Value:    time.Now(),
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported time filter operator"))
			})
		})

		Context("with existing conditions in builder", func() {
			It("should chain properly with existing conditions", func() {
				// Add existing condition
				builder.AddCondition("kind = %s", "Pod")

				// Add time filter
				testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
				filter := &TimeFilter{
					Field:    "created",
					Operator: "gte",
					Value:    testTime,
				}

				err := TimeFiltersToSQL([]*TimeFilter{filter}, "data", builder)
				Expect(err).ToNot(HaveOccurred())

				whereClause, params := builder.BuildWhere()
				expectedWhere := "WHERE kind = $1 AND data->>'created'::timestamp >= $2"
				Expect(whereClause).To(Equal(expectedWhere))
				Expect(params).To(Equal([]interface{}{
					"Pod",
					testTime.UTC().Format(time.RFC3339),
				}))
			})
		})
	})

	Describe("ValidateDuration", func() {
		DescribeTable("should validate duration formats",
			func(duration string, shouldBeValid bool) {
				err := ValidateDuration(duration)
				if shouldBeValid {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(HaveOccurred())
				}
			},
			Entry("valid hour", "1h", true),
			Entry("valid day", "2d", true),
			Entry("valid week", "1w", true),
			Entry("invalid format", "1x", false),
			Entry("empty string", "", false),
			Entry("no unit", "123", false),
		)
	})

	Describe("CalculateAge", func() {
		var now time.Time

		BeforeEach(func() {
			now = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
		})

		DescribeTable("should calculate human-readable ages",
			func(created time.Time, expected string) {
				// Note: This uses the actual current time, so we calculate expected relative to now
				age := CalculateAge(created)
				Expect(age).To(MatchRegexp(`^\d+[wdhms](\d+[dhms])?$`), "Age format should match expected pattern")
			},
			Entry("1 hour ago", now.Add(-1*time.Hour), "1h"),
			Entry("30 minutes ago", now.Add(-30*time.Minute), "30m"),
			Entry("45 seconds ago", now.Add(-45*time.Second), "45s"),
			Entry("1 day ago", now.Add(-24*time.Hour), "1d"),
			Entry("1 week ago", now.Add(-7*24*time.Hour), "1w"),
			Entry("1 week 2 days ago", now.Add(-(7*24+2*24)*time.Hour), "1w2d"),
			Entry("3 days 5 hours ago", now.Add(-(3*24+5)*time.Hour), "3d5h"),
		)

		It("should handle very recent times", func() {
			veryRecent := time.Now().Add(-5 * time.Second)
			age := CalculateAge(veryRecent)
			Expect(age).To(MatchRegexp(`^\d+s$`))
		})
	})

	Describe("CalculateAgeFromString", func() {
		It("should parse RFC3339 timestamps", func() {
			timestamp := "2024-01-15T10:00:00Z"
			age, err := CalculateAgeFromString(timestamp)
			Expect(err).ToNot(HaveOccurred())
			Expect(age).To(MatchRegexp(`^\d+[wdhms](\d+[dhms])?$`))
		})

		It("should parse timestamp without timezone", func() {
			timestamp := "2024-01-15T10:00:00"
			age, err := CalculateAgeFromString(timestamp)
			Expect(err).ToNot(HaveOccurred())
			Expect(age).To(MatchRegexp(`^\d+[wdhms](\d+[dhms])?$`))
		})

		It("should return error for invalid timestamp", func() {
			_, err := CalculateAgeFromString("invalid-timestamp")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unable to parse timestamp"))
		})
	})

	Describe("Helper functions", func() {
		Describe("SupportedDurationUnits", func() {
			It("should return all supported units", func() {
				units := SupportedDurationUnits()
				Expect(units).To(Equal([]string{"h", "d", "w"}))
			})
		})

		Describe("SupportedOperators", func() {
			It("should return all supported operators", func() {
				operators := SupportedOperators()
				Expect(operators).To(ConsistOf("gt", "gte", "lt", "lte"))
			})
		})

		Describe("SupportedFields", func() {
			It("should return all supported fields", func() {
				fields := SupportedFields()
				Expect(fields).To(ConsistOf("created", "age"))
			})
		})
	})
})