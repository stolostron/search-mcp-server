import { DatabaseConnection } from './connection.js';
import { QueryResult, TableInfo, TableSchema, ColumnInfo, QueryOptions } from '../types/index.js';

interface SecurityValidationResult {
  isValid: boolean;
  error?: string;
}

export class DatabaseQueries {
  private db: DatabaseConnection;

  // List of allowed SQL keywords for read-only operations
  private readonly ALLOWED_KEYWORDS = [
    'SELECT', 'WITH', 'FROM', 'WHERE', 'JOIN', 'INNER', 'LEFT', 'RIGHT', 'FULL', 'OUTER',
    'GROUP', 'BY', 'HAVING', 'ORDER', 'LIMIT', 'OFFSET', 'UNION', 'INTERSECT', 'EXCEPT',
    'AS', 'AND', 'OR', 'NOT', 'IN', 'EXISTS', 'BETWEEN', 'LIKE', 'ILIKE', 'SIMILAR',
    'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'CAST', 'EXTRACT', 'COUNT', 'SUM', 'AVG',
    'MIN', 'MAX', 'DISTINCT', 'ALL', 'ANY', 'SOME', 'TRUE', 'FALSE', 'NULL', 'IS'
  ];

  // List of forbidden SQL statement starters (mutating operations)
  private readonly FORBIDDEN_STATEMENTS = [
    'INSERT', 'UPDATE', 'DELETE', 'DROP', 'CREATE', 'ALTER', 'TRUNCATE', 'REPLACE',
    'MERGE', 'UPSERT', 'COPY', 'BULK', 'GRANT', 'REVOKE', 'COMMIT', 'ROLLBACK',
    'BEGIN', 'START', 'SAVEPOINT', 'RELEASE', 'SET', 'RESET',
    'SHOW', 'EXPLAIN', 'ANALYZE', 'VACUUM', 'REINDEX', 'LOCK', 'UNLOCK'
  ];

  // List of forbidden SQL commands that could appear anywhere
  private readonly FORBIDDEN_COMMANDS = [
    'CLUSTER INDEX', 'CLUSTER TABLE', 'DROP TABLE', 'DROP INDEX', 'CREATE TABLE',
    'CREATE INDEX', 'ALTER TABLE', 'ALTER INDEX', 'TRUNCATE TABLE'
  ];

  constructor(db: DatabaseConnection) {
    this.db = db;
  }

  /**
   * Validates SQL query for security and read-only compliance
   */
  private validateQuery(sql: string): SecurityValidationResult {
    // Normalize the SQL query
    const normalizedSql = sql.trim().toUpperCase();

    // Check if query is empty
    if (!normalizedSql) {
      return { isValid: false, error: 'Empty query not allowed' };
    }

    // Check if query starts with forbidden statement types
    for (const statement of this.FORBIDDEN_STATEMENTS) {
      if (normalizedSql.startsWith(statement + ' ') || normalizedSql === statement) {
        return {
          isValid: false,
          error: `Mutating operation '${statement}' is not allowed. This server is read-only.`
        };
      }
    }

    // Check for forbidden multi-word commands anywhere in the query
    for (const command of this.FORBIDDEN_COMMANDS) {
      if (normalizedSql.includes(command)) {
        return {
          isValid: false,
          error: `Operation '${command}' is not allowed. This server is read-only.`
        };
      }
    }

    // Check for SQL injection patterns
    const sqlInjectionPatterns = [
      /;[\s]*(--)/, // SQL comments after semicolon
      /;[\s]*(\/\*)/, // Block comments after semicolon
      /[\s]+(OR|AND)[\s]+1[\s]*=[\s]*1/i, // Classic OR 1=1 injection
      /[\s]+(OR|AND)[\s]+\w+[\s]*=[\s]*\w+/i, // Field=field injection
      /UNION[\s]+ALL[\s]+SELECT/i, // UNION injection
      /[\s]+UNION[\s]+SELECT/i, // UNION injection
      /'[\s]*(OR|AND)[\s]*'/i, // Quote-based injection
      /--[\s]*$/, // SQL comments at end
      /\/\*.*\*\//s, // Block comments
      /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/, // Control characters
      /exec[\s]*\(/i, // EXEC function calls
      /sp_/i, // Stored procedure calls
      /xp_/i, // Extended stored procedures
    ];

    for (const pattern of sqlInjectionPatterns) {
      if (pattern.test(sql)) {
        return {
          isValid: false,
          error: 'Query contains potentially unsafe patterns and has been blocked for security.'
        };
      }
    }

    // Ensure query starts with SELECT or WITH (for CTEs)
    if (!normalizedSql.startsWith('SELECT') && !normalizedSql.startsWith('WITH')) {
      return {
        isValid: false,
        error: 'Only SELECT queries and CTEs (WITH) are allowed.'
      };
    }

    // Additional validation: Check for multiple statements
    const statements = sql.split(';').filter(s => s.trim().length > 0);
    if (statements.length > 1) {
      return {
        isValid: false,
        error: 'Multiple SQL statements are not allowed. Please execute one query at a time.'
      };
    }

    return { isValid: true };
  }

  async executeQuery(sql: string, parameters?: any[], options?: QueryOptions): Promise<QueryResult> {
    try {
      // Validate query for security and read-only compliance
      const validation = this.validateQuery(sql);
      if (!validation.isValid) {
        throw new Error(`Security validation failed: ${validation.error}`);
      }

      // only log when debugging is enabled
      if (process.env.DEBUG === 'true') {
        console.log('[QUERY DEBUG] SQL:', sql);
        console.log('[QUERY DEBUG] Parameters:', parameters);
      }

      const result = await this.db.query(sql, parameters);
      
      // log result structure for debugging
      if (process.env.DEBUG === 'true') {
        console.log('[QUERY DEBUG] Query result structure:', {
          hasRows: !!result.rows,
          rowsType: typeof result.rows,
          rowsIsArray: Array.isArray(result.rows),
          rowsLength: result.rows?.length,
          fields: result.fields?.length,
          rowCount: result.rowCount
        });
      }
      
      // Convert row objects to arrays if needed
      const rowsAsArrays = result.rows.map(row => {
        if (Array.isArray(row)) {
          return row;
        } else {
          // Convert object to array based on field order
          return result.fields.map(field => row[field.name]);
        }
      });
      
      const queryResult: QueryResult = {
        columns: result.fields.map(field => field.name),
        rows: rowsAsArrays,
        rowCount: result.rowCount || 0,
        executionTime: (result as any).executionTime
      };

      // Apply row limit if specified
      if (options?.maxRows && queryResult.rows.length > options.maxRows) {
        queryResult.rows = queryResult.rows.slice(0, options.maxRows);
      }

      return queryResult;
    } catch (error) {
      console.error('Query execution failed:', error);
      throw new Error(`Query failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  }

  async listTables(): Promise<TableInfo[]> {
    const sql = `
      SELECT 
        schemaname as schema,
        tablename as table_name
      FROM pg_tables 
      WHERE schemaname IN ('public', 'search')
      ORDER BY schemaname, tablename
    `;

    const result = await this.executeQuery(sql);
    
    return result.rows.map(row => ({
      tableName: row[1], // tablename
      schema: row[0],    // schemaname
      rowCount: undefined // We'll get this separately if needed
    }));
  }

  async describeTable(tableName: string, schema: string = 'public'): Promise<TableSchema> {
    // Get column information
    const columnSql = `
      SELECT 
        column_name,
        data_type,
        is_nullable,
        column_default,
        col_description((table_schema||'.'||table_name)::regclass, ordinal_position) as description
      FROM information_schema.columns 
      WHERE table_schema = $1 AND table_name = $2
      ORDER BY ordinal_position
    `;

    const columnResult = await this.executeQuery(columnSql, [schema, tableName]);
    
    const columns: ColumnInfo[] = columnResult.rows.map(row => ({
      columnName: row[0],
      dataType: row[1],
      isNullable: row[2] === 'YES',
      defaultValue: row[3] || undefined,
      description: row[4] || undefined
    }));

    // Get index information
    const indexSql = `
      SELECT indexname 
      FROM pg_indexes 
      WHERE schemaname = $1 AND tablename = $2
    `;

    const indexResult = await this.executeQuery(indexSql, [schema, tableName]);
    const indexes = indexResult.rows.map(row => row[0]);

    // Get constraint information
    const constraintSql = `
      SELECT 
        tc.constraint_name,
        tc.constraint_type,
        kcu.column_name
      FROM information_schema.table_constraints tc
      JOIN information_schema.key_column_usage kcu 
        ON tc.constraint_name = kcu.constraint_name
      WHERE tc.table_schema = $1 AND tc.table_name = $2
    `;

    const constraintResult = await this.executeQuery(constraintSql, [schema, tableName]);
    const constraints = constraintResult.rows.map(row => 
      `${row[1]} ${row[0]} (${row[2]})`
    );

    return {
      tableName,
      schema,
      columns,
      indexes,
      constraints
    };
  }

  async getTableData(tableName: string, schema: string = 'public', limit: number = 10): Promise<QueryResult> {
    const sql = `SELECT * FROM ${schema}.${tableName} LIMIT $1`;
    return await this.executeQuery(sql, [limit]);
  }

  async getTableRowCount(tableName: string, schema: string = 'public'): Promise<number> {
    const sql = `SELECT COUNT(*) as count FROM ${schema}.${tableName}`;
    const result = await this.executeQuery(sql);
    return parseInt(result.rows[0][0]);
  }

  async getTableSize(tableName: string, schema: string = 'public'): Promise<string> {
    const sql = `
      SELECT pg_size_pretty(pg_total_relation_size($1::regclass)) as size
    `;
    const result = await this.executeQuery(sql, [`${schema}.${tableName}`]);
    return result.rows[0][0];
  }

  async searchTables(searchTerm: string): Promise<TableInfo[]> {
    const sql = `
      SELECT 
        schemaname as schema,
        tablename as table_name
      FROM pg_tables 
      WHERE tablename ILIKE $1 OR schemaname ILIKE $1
      ORDER BY schemaname, tablename
    `;
    
    const result = await this.executeQuery(sql, [`%${searchTerm}%`]);
    
    return result.rows.map(row => ({
      tableName: row[1],
      schema: row[0]
    }));
  }

  async getDatabaseStats(): Promise<{
    tableCount: number;
    totalRows: number;
    databaseSize: string;
    searchSchemaSize: string;
    resourcesTableSize: string;
    edgesTableSize: string;
    activeConnections: number;
  }> {
    // Get table count - include both public and search schemas
    const tableCountSql = `
      SELECT COUNT(*) as count
      FROM pg_tables
      WHERE schemaname IN ('public', 'search')
    `;
    const tableCountResult = await this.executeQuery(tableCountSql);
    const tableCount = parseInt(tableCountResult.rows[0][0]);

    // Get total rows from ACM search tables specifically
    let totalRows = 0;
    try {
      const totalRowsSql = `
        SELECT
          (SELECT COUNT(*) FROM search.resources) +
          (SELECT COUNT(*) FROM search.edges) as total_rows
      `;
      const totalRowsResult = await this.executeQuery(totalRowsSql);
      totalRows = parseInt(totalRowsResult.rows[0][0]) || 0;
    } catch (error) {
      // Fallback to pg_stat_user_tables if search tables don't exist
      try {
        const fallbackSql = `
          SELECT COALESCE(SUM(n_live_tup), 0) as total_rows
          FROM pg_stat_user_tables
          WHERE schemaname IN ('public', 'search')
        `;
        const fallbackResult = await this.executeQuery(fallbackSql);
        totalRows = parseInt(fallbackResult.rows[0][0]) || 0;
      } catch (fallbackError) {
        totalRows = 0;
      }
    }

    // Get database size
    const sizeSql = `SELECT pg_size_pretty(pg_database_size(current_database())) as size`;
    const sizeResult = await this.executeQuery(sizeSql);
    const databaseSize = sizeResult.rows[0][0];

    // Get search schema specific sizes
    let searchSchemaSize = 'N/A';
    let resourcesTableSize = 'N/A';
    let edgesTableSize = 'N/A';

    try {
      // Get total size of all tables in search schema
      const searchSchemaSql = `
        SELECT pg_size_pretty(
          COALESCE(SUM(pg_total_relation_size(schemaname||'.'||tablename)), 0)
        ) as size
        FROM pg_tables
        WHERE schemaname = 'search'
      `;
      const searchSchemaResult = await this.executeQuery(searchSchemaSql);
      searchSchemaSize = searchSchemaResult.rows[0][0];

      // Get individual table sizes
      const resourcesSizeSql = `
        SELECT pg_size_pretty(pg_total_relation_size('search.resources')) as size
      `;
      const resourcesSizeResult = await this.executeQuery(resourcesSizeSql);
      resourcesTableSize = resourcesSizeResult.rows[0][0];

      const edgesSizeSql = `
        SELECT pg_size_pretty(pg_total_relation_size('search.edges')) as size
      `;
      const edgesSizeResult = await this.executeQuery(edgesSizeSql);
      edgesTableSize = edgesSizeResult.rows[0][0];
    } catch (error) {
      // If search tables don't exist, sizes will remain 'N/A'
      console.error('Error getting search schema sizes:', error);
    }

    // Get active connections
    const connectionsSql = `
      SELECT COUNT(*) as count
      FROM pg_stat_activity
      WHERE state = 'active'
    `;
    const connectionsResult = await this.executeQuery(connectionsSql);
    const activeConnections = parseInt(connectionsResult.rows[0][0]);

    return {
      tableCount,
      totalRows,
      databaseSize,
      searchSchemaSize,
      resourcesTableSize,
      edgesTableSize,
      activeConnections
    };
  }
} 