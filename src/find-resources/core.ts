// Core filtering logic for enhanced find_resources

import { DatabaseQueries } from '../database/queries.js';
import {
  FindResourcesArgs,
  FindResourcesResult,
  ResourceResult,
  TimeFilter
} from './types.js';
import {
  parseLabelSelector,
  labelSelectorsToSQL,
  validateLabelSelector
} from '../utils/label-selectors.js';
import {
  parseTimeFilters,
  timeFiltersToSQL,
  validateDuration,
  calculateAge
} from '../utils/time-filters.js';
import {
  findMatchingClusters,
  buildTextSearchConditions,
  buildStatusConditions,
  buildNamespaceConditions,
  buildClusterConditions,
  buildKindConditions,
  buildNameConditions
} from '../utils/cross-resource.js';

export class FindResourcesCore {
  constructor(private dbQueries: DatabaseQueries) {}

  /**
   * Main entry point for enhanced find_resources functionality
   */
  async findResources(args: FindResourcesArgs): Promise<FindResourcesResult> {
    const startTime = Date.now();

    try {
      // Validate input arguments
      await this.validateArgs(args);

      // Handle cross-resource filtering (cluster selector)
      let targetClusters: string[] = [];
      if (args.clusterSelector) {
        targetClusters = await findMatchingClusters(args.clusterSelector, this.dbQueries);
        if (targetClusters.length === 0) {
          // No clusters match the selector, return empty result
          return this.createEmptyResult(args, startTime);
        }
      }

      // Build SQL query based on output mode
      const query = await this.buildQuery(args, targetClusters);

      // Execute query
      const result = await this.dbQueries.executeQuery(query.sql, query.params);

      // Process results based on output mode
      const processedData = await this.processResults(result, args);

      return {
        mode: args.outputMode || 'list',
        data: processedData,
        metadata: {
          totalCount: result.rowCount || 0,
          executionTime: Date.now() - startTime,
          query: query.sql,
          filters: args
        }
      };
    } catch (error) {
      console.error('Error in findResources:', error);
      throw error;
    }
  }

  /**
   * Validate input arguments
   */
  private async validateArgs(args: FindResourcesArgs): Promise<void> {
    // convert "*" to empty string for non-pattern fields
    if (args.labelSelector === '*') args.labelSelector = '';
    if (args.clusterSelector === '*') args.clusterSelector = '';
    if (args.status === '*') args.status = '';
    if (args.textSearch === '*') args.textSearch = '';
    if (args.ageNewerThan === '*') args.ageNewerThan = '';
    if (args.ageOlderThan === '*') args.ageOlderThan = '';
    
    // also handle wildcards in name/namespace/cluster
    if (args.name === '*') args.name = '';
    if (args.namespace === '*') args.namespace = '';
    if (args.cluster === '*') args.cluster = '';

    // Validate label selector
    if (args.labelSelector) {
      const validation = validateLabelSelector(args.labelSelector);
      if (!validation.valid) {
        throw new Error(`Invalid labelSelector: ${validation.error}`);
      }
    }

    // Validate cluster selector
    if (args.clusterSelector) {
      const validation = validateLabelSelector(args.clusterSelector);
      if (!validation.valid) {
        throw new Error(`Invalid clusterSelector: ${validation.error}`);
      }
    }

    // Validate time durations
    if (args.ageNewerThan) {
      const validation = validateDuration(args.ageNewerThan);
      if (!validation.valid) {
        throw new Error(`Invalid ageNewerThan: ${validation.error}`);
      }
    }

    if (args.ageOlderThan) {
      const validation = validateDuration(args.ageOlderThan);
      if (!validation.valid) {
        throw new Error(`Invalid ageOlderThan: ${validation.error}`);
      }
    }

    // Validate output mode
    if (args.outputMode && !['list', 'count', 'summary', 'health'].includes(args.outputMode)) {
      throw new Error(`Invalid outputMode: ${args.outputMode}. Must be one of: list, count, summary, health`);
    }

    // Validate limit
    if (args.limit && (args.limit < 1 || args.limit > 1000)) {
      throw new Error(`Invalid limit: ${args.limit}. Must be between 1 and 1000`);
    }
  }

  /**
   * Build SQL query based on arguments
   */
  private async buildQuery(args: FindResourcesArgs, targetClusters: string[]): Promise<{ sql: string, params: any[] }> {
    const conditions: string[] = [];
    const params: any[] = [];
    let paramIndex = 1;

    // Base query
    let sql = 'SELECT * FROM search.resources';

    // Always add WHERE clause
    conditions.push('1=1'); // Placeholder to make adding AND conditions easier

    // Kind filter
    if (args.kind) {
      const kindFilter = buildKindConditions(args.kind, 'data', paramIndex);
      conditions.push(...kindFilter.conditions);
      params.push(...kindFilter.params);
      paramIndex = kindFilter.nextParamIndex;
    }

    // Name filter
    if (args.name) {
      const nameFilter = buildNameConditions(args.name, 'data', paramIndex);
      conditions.push(...nameFilter.conditions);
      params.push(...nameFilter.params);
      paramIndex = nameFilter.nextParamIndex;
    }

    // Namespace filter
    if (args.namespace) {
      const namespaceFilter = buildNamespaceConditions(args.namespace, 'data', paramIndex);
      conditions.push(...namespaceFilter.conditions);
      params.push(...namespaceFilter.params);
      paramIndex = namespaceFilter.nextParamIndex;
    }

    // Cluster filter (either explicit or from cluster selector)
    let clusterFilter: string[] = [];
    if (args.cluster) {
      const explicitClusters = Array.isArray(args.cluster) ? args.cluster : [args.cluster];
      clusterFilter = explicitClusters;
    }
    if (targetClusters.length > 0) {
      clusterFilter = clusterFilter.length > 0
        ? clusterFilter.filter(c => targetClusters.includes(c)) // Intersection
        : targetClusters;
    }
    if (clusterFilter.length > 0) {
      const clusterConditions = buildClusterConditions(clusterFilter, paramIndex);
      conditions.push(...clusterConditions.conditions);
      params.push(...clusterConditions.params);
      paramIndex = clusterConditions.nextParamIndex;
    }

    // Label selector filter
    if (args.labelSelector) {
      const selectors = parseLabelSelector(args.labelSelector);
      const labelFilter = labelSelectorsToSQL(selectors, 'data');
      conditions.push(...labelFilter.conditions);
      params.push(...labelFilter.params);
      paramIndex += labelFilter.params.length;
    }

    // Status filter
    if (args.status) {
      const statusFilter = buildStatusConditions(args.status, 'data', paramIndex);
      conditions.push(...statusFilter.conditions);
      params.push(...statusFilter.params);
      paramIndex = statusFilter.nextParamIndex;
    }

    // Text search filter
    if (args.textSearch) {
      const textFilter = buildTextSearchConditions(args.textSearch, 'data', paramIndex);
      if (textFilter.conditions.length > 0) {
        conditions.push(...textFilter.conditions);
        params.push(...textFilter.params);
        paramIndex = textFilter.nextParamIndex;
      }
    }

    // Time-based filters
    if (args.ageNewerThan || args.ageOlderThan) {
      const timeFilters = parseTimeFilters(args.ageNewerThan, args.ageOlderThan);
      const timeSQL = timeFiltersToSQL(timeFilters, 'data', paramIndex);
      if (timeSQL.conditions.length > 0) {
        conditions.push(...timeSQL.conditions);
        params.push(...timeSQL.params);
        paramIndex = timeSQL.nextParamIndex;
      }
    }

    // Combine all conditions
    sql += ` WHERE ${conditions.join(' AND ')}`;

    // only log when debugging is enabled
    if (process.env.DEBUG === 'true') {
      console.log('[DEBUG] Generated SQL:', sql);
      console.log('[DEBUG] Parameters:', params);
    }

    // Handle different output modes
    if (args.outputMode === 'count' || args.outputMode === 'summary' || args.outputMode === 'health') {
      // For aggregation modes, we'll process the results differently
      // but still need the base query for now
    } else {
      // List mode - add sorting and limiting
      if (args.sortBy) {
        const sortColumn = this.getSortColumn(args.sortBy);
        const sortOrder = args.sortOrder || 'asc';
        sql += ` ORDER BY ${sortColumn} ${sortOrder.toUpperCase()}`;
      } else {
        sql += ` ORDER BY data->>'name'`;
      }

      // Add limit
      const limit = args.limit || 50;
      sql += ` LIMIT ${limit}`;
    }

    return { sql, params };
  }

  /**
   * Get the appropriate column name for sorting
   */
  private getSortColumn(sortBy: string): string {
    switch (sortBy.toLowerCase()) {
      case 'name':
        return "data->>'name'";
      case 'namespace':
        return "data->>'namespace'";
      case 'cluster':
        return 'cluster';
      case 'created':
        return "data->>'created'";
      case 'kind':
        return "data->>'kind'";
      default:
        return "data->>'name'";
    }
  }

  /**
   * Process query results based on output mode
   */
  private async processResults(result: any, args: FindResourcesArgs): Promise<any> {
    if (!result.rows || result.rows.length === 0) {
      return args.outputMode === 'list' ? [] : this.getEmptyModeResult(args.outputMode || 'list');
    }

    switch (args.outputMode) {
      case 'count':
        return this.processCountMode(result, args);
      case 'summary':
        return this.processSummaryMode(result);
      case 'health':
        return this.processHealthMode(result);
      case 'list':
      default:
        return this.processListMode(result);
    }
  }

  /**
   * Process results for list mode
   */
  private processListMode(result: any): ResourceResult[] {
    return result.rows.map((row: any) => {
      // FIXED: Correct row structure is [uid, cluster, data]
      const uid = row[0];     // uid column
      const cluster = row[1]; // cluster column
      const data = row[2];    // JSON data column

      // SAFE EXTRACTION: Only extract the most universal, bulletproof fields
      return {
        name: data?.name || 'N/A',
        namespace: data?.namespace || null, // null for cluster-scoped resources
        kind: data?.kind || 'Unknown',
        age: data?.created ? calculateAge(data.created) : 'unknown',
        cluster: cluster || 'unknown',
        data: data // Full JSON data for detailed analysis
      };
    });
  }


  /**
   * Process results for count mode
   */
  private processCountMode(result: any, args: FindResourcesArgs): any {
    const groupBy = args.groupBy || 'status';
    const counts: Record<string, number> = {};

    for (const row of result.rows) {
      // FIXED: Correct row structure is [uid, cluster, data]
      const uid = row[0];     // uid column
      const cluster = row[1]; // cluster column
      const data = row[2];    // JSON data column

      let groupKey: string;
      if (groupBy.startsWith('label:')) {
        const labelKey = groupBy.substring(6);
        groupKey = data.label?.[labelKey] || 'unknown';
      } else {
        switch (groupBy) {
          case 'status':
            groupKey = data.status || 'unknown';
            break;
          case 'namespace':
            groupKey = data.namespace || 'cluster-scoped';
            break;
          case 'cluster':
            groupKey = cluster || 'unknown';
            break;
          case 'kind':
            groupKey = data.kind || 'unknown';
            break;
          default:
            groupKey = data.status || 'unknown';
        }
      }

      counts[groupKey] = (counts[groupKey] || 0) + 1;
    }

    const total = Object.values(counts).reduce((sum, count) => sum + count, 0);

    return Object.entries(counts)
      .map(([label, count]) => ({
        label,
        count,
        percentage: total > 0 ? Math.round((count / total) * 100) : 0
      }))
      .sort((a, b) => b.count - a.count);
  }

  /**
   * Process results for summary mode
   */
  private processSummaryMode(result: any): any {
    const clusters = new Set<string>();
    const kinds = new Set<string>();
    const namespaces = new Set<string>();
    const clusterCounts: Record<string, number> = {};
    const kindCounts: Record<string, number> = {};
    const namespaceCounts: Record<string, number> = {};

    for (const row of result.rows) {
      // FIXED: Correct row structure is [uid, cluster, data]
      const uid = row[0];     // uid column
      const cluster = row[1]; // cluster column
      const data = row[2];    // JSON data column

      clusters.add(cluster);
      kinds.add(data.kind);
      if (data.namespace) namespaces.add(data.namespace);

      clusterCounts[cluster] = (clusterCounts[cluster] || 0) + 1;
      kindCounts[data.kind] = (kindCounts[data.kind] || 0) + 1;
      if (data.namespace) {
        namespaceCounts[data.namespace] = (namespaceCounts[data.namespace] || 0) + 1;
      }
    }

    return {
      totalResources: result.rows.length,
      totalClusters: clusters.size,
      resourcesByCluster: Object.entries(clusterCounts)
        .map(([label, count]) => ({ label, count }))
        .sort((a, b) => b.count - a.count),
      resourcesByKind: Object.entries(kindCounts)
        .map(([label, count]) => ({ label, count }))
        .sort((a, b) => b.count - a.count),
      resourcesByNamespace: Object.entries(namespaceCounts)
        .map(([label, count]) => ({ label, count }))
        .sort((a, b) => b.count - a.count)
    };
  }

  /**
   * Process results for health mode
   */
  private processHealthMode(result: any): any {
    const statusCounts: Record<string, number> = {};
    const issues: string[] = [];

    for (const row of result.rows) {
      // FIXED: Correct row structure is [uid, cluster, data]
      const uid = row[0];     // uid column
      const cluster = row[1]; // cluster column
      const data = row[2];    // JSON data column

      const status = this.determineHealthStatus(data);
      statusCounts[status] = (statusCounts[status] || 0) + 1;

      // Collect issues
      if (status === 'unhealthy') {
        const issue = `${cluster}/${data.namespace || 'cluster-scoped'}/${data.name}: ${data.status || 'Unknown issue'}`;
        issues.push(issue);
      }
    }

    const total = Object.values(statusCounts).reduce((sum, count) => sum + count, 0);
    const healthy = statusCounts.healthy || 0;
    const unhealthy = statusCounts.unhealthy || 0;
    const unknown = statusCounts.unknown || 0;

    return {
      total,
      healthy,
      unhealthy,
      unknown,
      details: Object.entries(statusCounts).map(([status, count]) => ({
        status,
        count,
        percentage: total > 0 ? Math.round((count / total) * 100) : 0
      })),
      topIssues: issues.slice(0, 10) // Top 10 issues
    };
  }

  /**
   * Determine health status of a resource
   */
  private determineHealthStatus(data: any): string {
    switch (data.kind) {
      case 'Pod':
        if (['Running', 'Succeeded'].includes(data.status)) return 'healthy';
        if (['Failed', 'CrashLoopBackOff', 'Error'].includes(data.status)) return 'unhealthy';
        return 'unknown';

      case 'Deployment':
        if (data.ready && data.desired && data.ready >= data.desired) return 'healthy';
        if (data.ready === 0) return 'unhealthy';
        return 'unknown';

      case 'ClusterOperator':
        if (data.available === 'True' && data.degraded === 'False') return 'healthy';
        if (data.available === 'False' || data.degraded === 'True') return 'unhealthy';
        return 'unknown';

      default:
        if (data.status === 'Running' || data.status === 'Active') return 'healthy';
        if (data.status === 'Failed' || data.status === 'Error') return 'unhealthy';
        return 'unknown';
    }
  }

  /**
   * Create empty result for when no resources match
   */
  private createEmptyResult(args: FindResourcesArgs, startTime: number): FindResourcesResult {
    return {
      mode: args.outputMode || 'list',
      data: this.getEmptyModeResult(args.outputMode || 'list'),
      metadata: {
        totalCount: 0,
        executionTime: Date.now() - startTime,
        query: '',
        filters: args
      }
    };
  }

  /**
   * Get empty result based on mode
   */
  private getEmptyModeResult(mode: string): any {
    switch (mode) {
      case 'count':
        return [];
      case 'summary':
        return {
          totalResources: 0,
          totalClusters: 0,
          resourcesByCluster: [],
          resourcesByKind: [],
          resourcesByNamespace: []
        };
      case 'health':
        return {
          total: 0,
          healthy: 0,
          unhealthy: 0,
          unknown: 0,
          details: [],
          topIssues: []
        };
      case 'list':
      default:
        return [];
    }
  }
}