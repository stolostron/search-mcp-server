// Package database provides database query functionality with read-only validation
package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/stolostron/search-mcp-server/pkg/types"
)

// SecurityValidationResult represents the result of SQL security validation
type SecurityValidationResult struct {
	IsValid bool
	Error   string
}

// DatabaseQueries provides database query functionality with security validation
type DatabaseQueries struct {
	db *DatabaseConnection

	// List of allowed SQL keywords for read-only operations
	allowedKeywords []string

	// List of forbidden SQL statement starters (mutating operations)
	forbiddenStatements []string

	// List of forbidden SQL commands that could appear anywhere
	forbiddenCommands []string
}

// NewDatabaseQueries creates a new DatabaseQueries instance
func NewDatabaseQueries(db *DatabaseConnection) *DatabaseQueries {
	dq := &DatabaseQueries{
		db: db,
		allowedKeywords: []string{
			"SELECT", "WITH", "FROM", "WHERE", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "OUTER",
			"GROUP", "BY", "HAVING", "ORDER", "LIMIT", "OFFSET", "UNION", "INTERSECT", "EXCEPT",
			"AS", "AND", "OR", "NOT", "IN", "EXISTS", "BETWEEN", "LIKE", "ILIKE", "SIMILAR",
			"CASE", "WHEN", "THEN", "ELSE", "END", "CAST", "EXTRACT", "COUNT", "SUM", "AVG",
			"MIN", "MAX", "DISTINCT", "ALL", "ANY", "SOME", "TRUE", "FALSE", "NULL", "IS",
		},
		forbiddenStatements: []string{
			"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "TRUNCATE", "REPLACE",
			"MERGE", "UPSERT", "COPY", "BULK", "GRANT", "REVOKE", "COMMIT", "ROLLBACK",
			"BEGIN", "START", "SAVEPOINT", "RELEASE", "SET", "RESET",
			"SHOW", "EXPLAIN", "ANALYZE", "VACUUM", "REINDEX", "LOCK", "UNLOCK",
		},
		forbiddenCommands: []string{
			"CLUSTER INDEX", "CLUSTER TABLE", "DROP TABLE", "DROP INDEX", "CREATE TABLE",
			"CREATE INDEX", "ALTER TABLE", "ALTER INDEX", "TRUNCATE TABLE",
		},
	}


	return dq
}

// validateQuery validates SQL query for read-only compliance
// NOTE: SQL injection protection comes from parameterized queries, NOT pattern detection
func (dq *DatabaseQueries) validateQuery(sql string) SecurityValidationResult {
	// Normalize the SQL query
	normalizedSQL := strings.TrimSpace(strings.ToUpper(sql))

	// Check if query is empty
	if normalizedSQL == "" {
		return SecurityValidationResult{
			IsValid: false,
			Error:   "Empty query not allowed",
		}
	}

	// Check if query starts with forbidden statement types
	for _, statement := range dq.forbiddenStatements {
		if strings.HasPrefix(normalizedSQL, statement+" ") || normalizedSQL == statement {
			return SecurityValidationResult{
				IsValid: false,
				Error:   fmt.Sprintf("Mutating operation '%s' is not allowed. This server is read-only.", statement),
			}
		}
	}

	// Check for forbidden multi-word commands anywhere in the query
	for _, command := range dq.forbiddenCommands {
		if strings.Contains(normalizedSQL, command) {
			return SecurityValidationResult{
				IsValid: false,
				Error:   fmt.Sprintf("Operation '%s' is not allowed. This server is read-only.", command),
			}
		}
	}


	// Ensure query starts with SELECT or WITH (for CTEs)
	if !strings.HasPrefix(normalizedSQL, "SELECT") && !strings.HasPrefix(normalizedSQL, "WITH") {
		return SecurityValidationResult{
			IsValid: false,
			Error:   "Only SELECT queries and CTEs (WITH) are allowed.",
		}
	}

	// Additional validation: Check for multiple statements
	statements := strings.Split(sql, ";")
	nonEmptyStatements := 0
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) != "" {
			nonEmptyStatements++
		}
	}
	if nonEmptyStatements > 1 {
		return SecurityValidationResult{
			IsValid: false,
			Error:   "Multiple SQL statements are not allowed. Please execute one query at a time.",
		}
	}

	return SecurityValidationResult{IsValid: true}
}

// ExecuteQuery executes a SQL query with security validation and optional parameters
func (dq *DatabaseQueries) ExecuteQuery(ctx context.Context, sql string, parameters []interface{}, options *types.QueryOptions) (*types.QueryResult, error) {
	// Validate query for security and read-only compliance
	validation := dq.validateQuery(sql)
	if !validation.IsValid {
		return nil, fmt.Errorf("security validation failed: %s", validation.Error)
	}

	// Apply timeout if specified in options
	queryCtx := ctx
	var cancel context.CancelFunc
	if options != nil && options.Timeout != nil {
		queryCtx, cancel = context.WithTimeout(ctx, time.Duration(*options.Timeout)*time.Second)
		defer cancel()
	}

	result, err := dq.db.Query(queryCtx, sql, parameters...)
	if err != nil {
		// Check for timeout error and provide a clear message
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("query timeout exceeded (%d seconds)", *options.Timeout)
		}
		log.Printf("Query execution failed: %v", err)
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Apply row limit if specified
	if options != nil && options.MaxRows != nil && len(result.Rows) > *options.MaxRows {
		result.Rows = result.Rows[:*options.MaxRows]
		// Update row count to reflect the limit
		limitedCount := *options.MaxRows
		result.RowCount = &limitedCount
	}

	return result, nil
}

// ListTables returns a list of tables in public and search schemas
func (dq *DatabaseQueries) ListTables(ctx context.Context) ([]types.TableInfo, error) {
	sql := `
		SELECT
			schemaname as schema,
			tablename as table_name
		FROM pg_tables
		WHERE schemaname IN ('public', 'search')
		ORDER BY schemaname, tablename
	`

	result, err := dq.ExecuteQuery(ctx, sql, nil, nil)
	if err != nil {
		return nil, err
	}

	var tables []types.TableInfo
	for _, row := range result.Rows {
		if len(row) >= 2 {
			schema, schemaOk := row[0].(string)
			tableName, tableOk := row[1].(string)
			if schemaOk && tableOk {
				tables = append(tables, types.TableInfo{
					Schema:    schema,
					TableName: tableName,
				})
			}
		}
	}

	return tables, nil
}

// DescribeTable returns the schema details of a specified table
func (dq *DatabaseQueries) DescribeTable(ctx context.Context, tableName, schema string) (*types.TableSchema, error) {
	if schema == "" {
		schema = "public"
	}

	// Get column information
	columnSQL := `
		SELECT
			column_name,
			data_type,
			is_nullable,
			column_default,
			col_description((table_schema||'.'||table_name)::regclass, ordinal_position) as description
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	columnResult, err := dq.ExecuteQuery(ctx, columnSQL, []interface{}{schema, tableName}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get column information: %w", err)
	}

	var columns []types.ColumnInfo
	for _, row := range columnResult.Rows {
		if len(row) >= 5 {
			columnName, columnOk := row[0].(string)
			dataType, typeOk := row[1].(string)
			isNullableStr, nullableOk := row[2].(string)

			if columnOk && typeOk && nullableOk {
				column := types.ColumnInfo{
					ColumnName: columnName,
					DataType:   dataType,
					IsNullable: isNullableStr == "YES",
				}

				// Handle nullable fields
				if row[3] != nil {
					if defaultVal, ok := row[3].(string); ok {
						column.DefaultValue = &defaultVal
					}
				}
				if row[4] != nil {
					if desc, ok := row[4].(string); ok {
						column.Description = &desc
					}
				}

				columns = append(columns, column)
			}
		}
	}

	// Get index information
	indexSQL := `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
	`

	indexResult, err := dq.ExecuteQuery(ctx, indexSQL, []interface{}{schema, tableName}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get index information: %w", err)
	}

	var indexes []string
	for _, row := range indexResult.Rows {
		if len(row) >= 1 {
			if indexName, ok := row[0].(string); ok {
				indexes = append(indexes, indexName)
			}
		}
	}

	// Get constraint information
	constraintSQL := `
		SELECT
			tc.constraint_name,
			tc.constraint_type,
			kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
		WHERE tc.table_schema = $1 AND tc.table_name = $2
	`

	constraintResult, err := dq.ExecuteQuery(ctx, constraintSQL, []interface{}{schema, tableName}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get constraint information: %w", err)
	}

	var constraints []string
	for _, row := range constraintResult.Rows {
		if len(row) >= 3 {
			constraintName, nameOk := row[0].(string)
			constraintType, typeOk := row[1].(string)
			columnName, columnOk := row[2].(string)
			if nameOk && typeOk && columnOk {
				constraint := fmt.Sprintf("%s %s (%s)", constraintType, constraintName, columnName)
				constraints = append(constraints, constraint)
			}
		}
	}

	return &types.TableSchema{
		TableName:   tableName,
		Schema:      schema,
		Columns:     columns,
		Indexes:     indexes,
		Constraints: constraints,
	}, nil
}

// GetTableData returns sample data from a specified table
func (dq *DatabaseQueries) GetTableData(ctx context.Context, tableName, schema string, limit int) (*types.QueryResult, error) {
	if schema == "" {
		schema = "public"
	}

	sql := fmt.Sprintf("SELECT * FROM %s.%s LIMIT $1", schema, tableName)
	return dq.ExecuteQuery(ctx, sql, []interface{}{limit}, nil)
}

// GetTableRowCount returns the number of rows in a specified table
func (dq *DatabaseQueries) GetTableRowCount(ctx context.Context, tableName, schema string) (int, error) {
	if schema == "" {
		schema = "public"
	}

	sql := fmt.Sprintf("SELECT COUNT(*) as count FROM %s.%s", schema, tableName)
	result, err := dq.ExecuteQuery(ctx, sql, nil, nil)
	if err != nil {
		return 0, err
	}

	if len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
		if count, ok := result.Rows[0][0].(int64); ok {
			return int(count), nil
		}
		// Try string conversion as fallback
		if countStr, ok := result.Rows[0][0].(string); ok {
			if count, err := strconv.Atoi(countStr); err == nil {
				return count, nil
			}
		}
	}

	return 0, fmt.Errorf("failed to get row count")
}

// GetTableSize returns the disk size of a specified table
func (dq *DatabaseQueries) GetTableSize(ctx context.Context, tableName, schema string) (string, error) {
	if schema == "" {
		schema = "public"
	}

	sql := `SELECT pg_size_pretty(pg_total_relation_size($1::regclass)) as size`
	fullTableName := fmt.Sprintf("%s.%s", schema, tableName)

	result, err := dq.ExecuteQuery(ctx, sql, []interface{}{fullTableName}, nil)
	if err != nil {
		return "", err
	}

	if len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
		if size, ok := result.Rows[0][0].(string); ok {
			return size, nil
		}
	}

	return "", fmt.Errorf("failed to get table size")
}

// SearchTables searches for tables by name pattern
func (dq *DatabaseQueries) SearchTables(ctx context.Context, searchTerm string) ([]types.TableInfo, error) {
	sql := `
		SELECT
			schemaname as schema,
			tablename as table_name
		FROM pg_tables
		WHERE tablename ILIKE $1 OR schemaname ILIKE $1
		ORDER BY schemaname, tablename
	`

	searchPattern := fmt.Sprintf("%%%s%%", searchTerm)
	result, err := dq.ExecuteQuery(ctx, sql, []interface{}{searchPattern}, nil)
	if err != nil {
		return nil, err
	}

	var tables []types.TableInfo
	for _, row := range result.Rows {
		if len(row) >= 2 {
			schema, schemaOk := row[0].(string)
			tableName, tableOk := row[1].(string)
			if schemaOk && tableOk {
				tables = append(tables, types.TableInfo{
					Schema:    schema,
					TableName: tableName,
				})
			}
		}
	}

	return tables, nil
}

// DatabaseStatsResult represents the result of database statistics query
type DatabaseStatsResult struct {
	TableCount           int    `json:"tableCount"`
	TotalRows           int    `json:"totalRows"`
	DatabaseSize        string `json:"databaseSize"`
	SearchSchemaSize    string `json:"searchSchemaSize"`
	ResourcesTableSize  string `json:"resourcesTableSize"`
	EdgesTableSize      string `json:"edgesTableSize"`
	ActiveConnections   int    `json:"activeConnections"`
}

// GetDatabaseStats returns comprehensive database statistics
func (dq *DatabaseQueries) GetDatabaseStats(ctx context.Context) (*DatabaseStatsResult, error) {
	// Get table count - include both public and search schemas
	tableCountSQL := `
		SELECT COUNT(*) as count
		FROM pg_tables
		WHERE schemaname IN ('public', 'search')
	`
	tableCountResult, err := dq.ExecuteQuery(ctx, tableCountSQL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get table count: %w", err)
	}

	tableCount := 0
	if len(tableCountResult.Rows) > 0 && len(tableCountResult.Rows[0]) > 0 {
		if count, ok := tableCountResult.Rows[0][0].(int64); ok {
			tableCount = int(count)
		}
	}

	// Get total rows from ACM search tables specifically
	totalRows := 0
	totalRowsSQL := `
		SELECT
			(SELECT COUNT(*) FROM search.resources) +
			(SELECT COUNT(*) FROM search.edges) as total_rows
	`
	if totalRowsResult, err := dq.ExecuteQuery(ctx, totalRowsSQL, nil, nil); err == nil {
		if len(totalRowsResult.Rows) > 0 && len(totalRowsResult.Rows[0]) > 0 {
			if count, ok := totalRowsResult.Rows[0][0].(int64); ok {
				totalRows = int(count)
			}
		}
	} else {
		// Fallback to pg_stat_user_tables if search tables don't exist
		fallbackSQL := `
			SELECT COALESCE(SUM(n_live_tup), 0) as total_rows
			FROM pg_stat_user_tables
			WHERE schemaname IN ('public', 'search')
		`
		if fallbackResult, err := dq.ExecuteQuery(ctx, fallbackSQL, nil, nil); err == nil {
			if len(fallbackResult.Rows) > 0 && len(fallbackResult.Rows[0]) > 0 {
				if count, ok := fallbackResult.Rows[0][0].(int64); ok {
					totalRows = int(count)
				}
			}
		}
	}

	// Get database size
	sizeSQL := `SELECT pg_size_pretty(pg_database_size(current_database())) as size`
	sizeResult, err := dq.ExecuteQuery(ctx, sizeSQL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get database size: %w", err)
	}

	databaseSize := "Unknown"
	if len(sizeResult.Rows) > 0 && len(sizeResult.Rows[0]) > 0 {
		if size, ok := sizeResult.Rows[0][0].(string); ok {
			databaseSize = size
		}
	}

	// Get search schema specific sizes
	searchSchemaSize := "N/A"
	resourcesTableSize := "N/A"
	edgesTableSize := "N/A"

	// Get total size of all tables in search schema
	searchSchemaSizeSQL := `
		SELECT pg_size_pretty(
			COALESCE(SUM(pg_total_relation_size(schemaname||'.'||tablename)), 0)
		) as size
		FROM pg_tables
		WHERE schemaname = 'search'
	`
	if searchSchemaResult, err := dq.ExecuteQuery(ctx, searchSchemaSizeSQL, nil, nil); err == nil {
		if len(searchSchemaResult.Rows) > 0 && len(searchSchemaResult.Rows[0]) > 0 {
			if size, ok := searchSchemaResult.Rows[0][0].(string); ok {
				searchSchemaSize = size
			}
		}
	}

	// Get individual table sizes
	resourcesSizeSQL := `SELECT pg_size_pretty(pg_total_relation_size('search.resources')) as size`
	if resourcesSizeResult, err := dq.ExecuteQuery(ctx, resourcesSizeSQL, nil, nil); err == nil {
		if len(resourcesSizeResult.Rows) > 0 && len(resourcesSizeResult.Rows[0]) > 0 {
			if size, ok := resourcesSizeResult.Rows[0][0].(string); ok {
				resourcesTableSize = size
			}
		}
	}

	edgesSizeSQL := `SELECT pg_size_pretty(pg_total_relation_size('search.edges')) as size`
	if edgesSizeResult, err := dq.ExecuteQuery(ctx, edgesSizeSQL, nil, nil); err == nil {
		if len(edgesSizeResult.Rows) > 0 && len(edgesSizeResult.Rows[0]) > 0 {
			if size, ok := edgesSizeResult.Rows[0][0].(string); ok {
				edgesTableSize = size
			}
		}
	}

	// Get active connections
	connectionsSQL := `
		SELECT COUNT(*) as count
		FROM pg_stat_activity
		WHERE state = 'active'
	`
	connectionsResult, err := dq.ExecuteQuery(ctx, connectionsSQL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection count: %w", err)
	}

	activeConnections := 0
	if len(connectionsResult.Rows) > 0 && len(connectionsResult.Rows[0]) > 0 {
		if count, ok := connectionsResult.Rows[0][0].(int64); ok {
			activeConnections = int(count)
		}
	}

	return &DatabaseStatsResult{
		TableCount:          tableCount,
		TotalRows:          totalRows,
		DatabaseSize:       databaseSize,
		SearchSchemaSize:   searchSchemaSize,
		ResourcesTableSize: resourcesTableSize,
		EdgesTableSize:     edgesTableSize,
		ActiveConnections:  activeConnections,
	}, nil
}