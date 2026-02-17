// Time-based filtering utilities

import { TimeFilter } from '../find-resources/types.js';

/**
 * Parse time duration strings like "1h", "2d", "1w" into milliseconds
 */
export function parseDuration(duration: string): number {
  const match = duration.match(/^(\d+)([hdw])$/);
  if (!match) {
    throw new Error(`Invalid duration format: ${duration}. Use format like "1h", "2d", "1w"`);
  }

  const [, amount, unit] = match;
  const num = parseInt(amount, 10);

  switch (unit) {
    case 'h':
      return num * 60 * 60 * 1000; // hours to milliseconds
    case 'd':
      return num * 24 * 60 * 60 * 1000; // days to milliseconds
    case 'w':
      return num * 7 * 24 * 60 * 60 * 1000; // weeks to milliseconds
    default:
      throw new Error(`Invalid time unit: ${unit}. Use h, d, or w`);
  }
}

/**
 * Convert age strings to Date objects for filtering
 */
export function parseTimeFilters(ageNewerThan?: string, ageOlderThan?: string): TimeFilter[] {
  const filters: TimeFilter[] = [];
  const now = new Date();

  if (ageNewerThan) {
    const duration = parseDuration(ageNewerThan);
    const threshold = new Date(now.getTime() - duration);
    filters.push({
      field: 'created',
      operator: 'gte',
      value: threshold
    });
  }

  if (ageOlderThan) {
    const duration = parseDuration(ageOlderThan);
    const threshold = new Date(now.getTime() - duration);
    filters.push({
      field: 'created',
      operator: 'lte',
      value: threshold
    });
  }

  return filters;
}

/**
 * Convert time filters to SQL WHERE conditions
 */
export function timeFiltersToSQL(
  filters: TimeFilter[],
  dataColumn: string = 'data',
  paramStartIndex: number = 1
): { conditions: string[], params: any[], nextParamIndex: number } {
  const conditions: string[] = [];
  const params: any[] = [];
  let paramIndex = paramStartIndex;

  for (const filter of filters) {
    const fieldPath = filter.field === 'created' ? `${dataColumn}->>'created'` : `${dataColumn}->>'created'`;

    // cast to timestamp for comparison
    switch (filter.operator) {
      case 'gt':
        conditions.push(`(${fieldPath})::timestamp > $${paramIndex}::timestamp`);
        break;
      case 'gte':
        conditions.push(`(${fieldPath})::timestamp >= $${paramIndex}::timestamp`);
        break;
      case 'lt':
        conditions.push(`(${fieldPath})::timestamp < $${paramIndex}::timestamp`);
        break;
      case 'lte':
        conditions.push(`(${fieldPath})::timestamp <= $${paramIndex}::timestamp`);
        break;
    }

    params.push(filter.value.toISOString());
    paramIndex++;
  }

  return { conditions, params, nextParamIndex: paramIndex };
}

/**
 * Validate time duration format
 */
export function validateDuration(duration: string): { valid: boolean, error?: string } {
  try {
    parseDuration(duration);
    return { valid: true };
  } catch (error) {
    return {
      valid: false,
      error: error instanceof Error ? error.message : 'Invalid duration format'
    };
  }
}

/**
 * Calculate human-readable age from created timestamp
 */
export function calculateAge(created: string): string {
  const createdDate = new Date(created);
  const now = new Date();
  const diffMs = now.getTime() - createdDate.getTime();

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const weeks = Math.floor(days / 7);

  if (weeks > 0) {
    return `${weeks}w${days % 7 > 0 ? `${days % 7}d` : ''}`;
  } else if (days > 0) {
    return `${days}d${hours % 24 > 0 ? `${hours % 24}h` : ''}`;
  } else if (hours > 0) {
    return `${hours}h${minutes % 60 > 0 ? `${minutes % 60}m` : ''}`;
  } else if (minutes > 0) {
    return `${minutes}m`;
  } else {
    return `${seconds}s`;
  }
}

/**
 * Examples of valid duration strings:
 * - "1h" (1 hour)
 * - "24h" (24 hours)
 * - "2d" (2 days)
 * - "1w" (1 week)
 * - "4w" (4 weeks)
 */