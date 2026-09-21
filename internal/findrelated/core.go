package findrelated

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/stolostron/search-mcp-server/internal/rbac"
	"github.com/stolostron/search-mcp-server/internal/sanitize"
	"github.com/stolostron/search-mcp-server/internal/server/auth"
	"github.com/stolostron/search-mcp-server/internal/utils"
	"github.com/stolostron/search-mcp-server/pkg/database"
	"github.com/stolostron/search-mcp-server/pkg/types"
)

type FindRelatedCore struct {
	dbQueries *database.DatabaseQueries
	sanitizer *sanitize.Sanitizer
}

func NewFindRelatedCore(dbQueries *database.DatabaseQueries) *FindRelatedCore {
	return &FindRelatedCore{
		dbQueries: dbQueries,
		sanitizer: sanitize.New(sanitize.DefaultConfig()),
	}
}

func (f *FindRelatedCore) FindRelatedResources(ctx context.Context, args FindRelatedArgs, userCtx *auth.UserContext) (*FindRelatedResult, error) {
	startTime := time.Now()

	if err := f.validateArgs(args); err != nil {
		return nil, err
	}

	args = f.normalizeArgs(ctx, args)

	// Query 1: Edge traversal CTE + RBAC to find related UIDs and kinds.
	query, params := f.buildRelatedQuery(args, userCtx)

	log.Printf("[RELATED] Executing edge traversal query for %d seed UIDs, maxHops=%d", len(args.UIDs), args.MaxHops)

	timeout30 := 30
	queryResult, err := f.dbQueries.ExecuteQuery(ctx, query, params, &types.QueryOptions{Timeout: &timeout30})
	if err != nil {
		return nil, fmt.Errorf("edge traversal query failed: %w", err)
	}

	var relatedUIDs []uidInfo
	seedSet := make(map[string]bool, len(args.UIDs))
	for _, uid := range args.UIDs {
		seedSet[uid] = true
	}

	for _, row := range queryResult.Rows {
		if len(row) < 2 {
			continue
		}
		uid, ok := row[0].(string)
		if !ok {
			continue
		}
		kind, ok := row[1].(string)
		if !ok {
			continue
		}
		if seedSet[uid] {
			continue
		}
		relatedUIDs = append(relatedUIDs, uidInfo{uid: uid, kind: kind})
	}

	// Apply relatedKinds filter.
	if len(args.RelatedKinds) > 0 {
		kindSet := make(map[string]bool, len(args.RelatedKinds))
		for _, k := range args.RelatedKinds {
			kindSet[strings.ToLower(k)] = true
		}
		filtered := relatedUIDs[:0]
		for _, r := range relatedUIDs {
			if kindSet[strings.ToLower(r.kind)] {
				filtered = append(filtered, r)
			}
		}
		relatedUIDs = filtered
	}

	// Count-only mode: group and return counts.
	if args.OutputMode == OutputModeCount {
		groups := f.groupCounts(relatedUIDs)
		return &FindRelatedResult{
			Groups: groups,
			Metadata: RelatedMetadata{
				SeedUIDs:      args.UIDs,
				MaxHops:       args.MaxHops,
				TotalRelated:  len(relatedUIDs),
				ExecutionTime: time.Since(startTime).Milliseconds(),
			},
		}, nil
	}

	// Query 2: Fetch full resource data for the related UIDs.
	if len(relatedUIDs) == 0 {
		return &FindRelatedResult{
			Groups: []RelatedKindGroup{},
			Metadata: RelatedMetadata{
				SeedUIDs:      args.UIDs,
				MaxHops:       args.MaxHops,
				TotalRelated:  0,
				ExecutionTime: time.Since(startTime).Milliseconds(),
			},
		}, nil
	}

	fetchCount := len(relatedUIDs)
	if fetchCount > args.Limit {
		fetchCount = args.Limit
	}
	uidList := make([]interface{}, fetchCount)
	for i := 0; i < fetchCount; i++ {
		uidList[i] = relatedUIDs[i].uid
	}

	itemQuery, itemParams := f.buildItemQuery(uidList, args.Limit)
	itemResult, err := f.dbQueries.ExecuteQuery(ctx, itemQuery, itemParams, &types.QueryOptions{Timeout: &timeout30})
	if err != nil {
		return nil, fmt.Errorf("item fetch query failed: %w", err)
	}

	// Parse items and group by kind.
	groups := f.processItems(itemResult, relatedUIDs)

	returnedCount := 0
	for _, g := range groups {
		returnedCount += g.Count
	}

	return &FindRelatedResult{
		Groups: groups,
		Metadata: RelatedMetadata{
			SeedUIDs:      args.UIDs,
			MaxHops:       args.MaxHops,
			TotalRelated:  returnedCount,
			ExecutionTime: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

func (f *FindRelatedCore) validateArgs(args FindRelatedArgs) error {
	if len(args.UIDs) == 0 {
		return fmt.Errorf("uids is required and must not be empty")
	}
	if len(args.UIDs) > 1 {
		return fmt.Errorf("uids must contain exactly 1 seed UID")
	}
	if args.MaxHops < 0 {
		return fmt.Errorf("maxHops must be non-negative")
	}
	if args.MaxHops > 3 {
		return fmt.Errorf("maxHops must not exceed 3")
	}
	if args.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

func (f *FindRelatedCore) normalizeArgs(ctx context.Context, args FindRelatedArgs) FindRelatedArgs {
	if args.MaxHops == 0 {
		args.MaxHops = f.resolveDefaultHops(ctx, args.UIDs)
	}
	if args.Limit == 0 {
		args.Limit = DefaultRelatedLimit
	}
	if args.Limit > MaxRelatedLimit {
		args.Limit = MaxRelatedLimit
	}
	if args.OutputMode == "" {
		args.OutputMode = OutputModeList
	}
	return args
}

// resolveDefaultHops returns ApplicationMaxHops if any seed UID is an
// Application, otherwise DefaultMaxHops. This is the "smart default" from
// the design: explicit maxHops always wins, but when omitted, Application
// seeds get deeper traversal to follow Subscription chains.
func (f *FindRelatedCore) resolveDefaultHops(ctx context.Context, uids []string) int {
	if f.dbQueries == nil {
		return DefaultMaxHops
	}

	sb := utils.NewSQLBuilder(1)
	uidValues := make([]interface{}, len(uids))
	for i, uid := range uids {
		uidValues[i] = uid
	}
	sb.AddIN("uid", uidValues)
	where, params := sb.BuildWhere()

	query := fmt.Sprintf("SELECT COUNT(*) FROM search.resources %s AND data->>'kind' = 'Application'", where)
	timeout5 := 5
	result, err := f.dbQueries.ExecuteQuery(ctx, query, params, &types.QueryOptions{Timeout: &timeout5})
	if err != nil {
		log.Printf("[RELATED] Application auto-detect query failed, using default hops: %v", err)
		return DefaultMaxHops
	}

	if len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
		if count, ok := result.Rows[0][0].(int64); ok && count > 0 {
			log.Printf("[RELATED] Detected %d Application seed(s), using maxHops=%d", count, ApplicationMaxHops)
			return ApplicationMaxHops
		}
	}

	return DefaultMaxHops
}

// buildRelatedQuery constructs the recursive CTE for edge traversal with RBAC.
func (f *FindRelatedCore) buildRelatedQuery(args FindRelatedArgs, userCtx *auth.UserContext) (string, []interface{}) {
	sb := utils.NewSQLBuilder(1)

	// Build seed UID placeholders.
	seedPlaceholders := make([]string, len(args.UIDs))
	for i := range args.UIDs {
		seedPlaceholders[i] = fmt.Sprintf("$%d", sb.GetNextParamIndex()+i)
	}
	seedParams := make([]interface{}, len(args.UIDs))
	for i, uid := range args.UIDs {
		seedParams[i] = uid
	}
	seedIN := strings.Join(seedPlaceholders, ", ")

	// We'll manage parameters manually for the CTE portion since SQLBuilder
	// is designed for WHERE clauses, not CTEs.
	params := make([]interface{}, 0, len(args.UIDs)+10)
	params = append(params, seedParams...)
	nextIdx := len(args.UIDs) + 1

	var cte string
	if args.MaxHops <= 1 {
		// No recursion needed — just direct edges.
		cte = fmt.Sprintf(`
SELECT DISTINCT unnest(array[e.sourceid, e.destid]) AS uid,
       unnest(array[e.sourcekind, e.destkind]) AS kind
FROM search.edges e
WHERE (e.sourceid IN (%s) OR e.destid IN (%s))
  AND e.edgetype != 'interCluster'`, seedIN, seedIN)
	} else {
		maxLevelParam := fmt.Sprintf("$%d", nextIdx)
		params = append(params, args.MaxHops)
		nextIdx++

		cte = fmt.Sprintf(`
WITH RECURSIVE search_graph(level, sourceid, destid, sourcekind, destkind) AS (
  SELECT 1 AS level, e.sourceid, e.destid, e.sourcekind, e.destkind
  FROM search.edges e
  WHERE e.sourceid IN (%s) AND e.edgetype != 'interCluster'
  UNION ALL
  SELECT 1 AS level, e.sourceid, e.destid, e.sourcekind, e.destkind
  FROM search.edges e
  WHERE e.destid IN (%s) AND e.edgetype != 'interCluster'

  UNION

  SELECT sg.level + 1, e.sourceid, e.destid, e.sourcekind, e.destkind
  FROM search.edges e
  INNER JOIN search_graph sg
    ON (sg.destid IN (e.sourceid, e.destid) OR sg.sourceid IN (e.sourceid, e.destid))
  WHERE e.destkind NOT IN ('Node', 'Channel')
    AND e.sourcekind NOT IN ('Node', 'Channel')
    AND e.edgetype != 'interCluster'
    AND sg.level < %s
)
SELECT DISTINCT unnest(array[sourceid, destid]) AS uid,
       unnest(array[sourcekind, destkind]) AS kind
FROM search_graph`, seedIN, seedIN, maxLevelParam)
	}

	// Wrap CTE with INNER JOIN on search.resources for RBAC filtering.
	rbacBuilder := utils.NewSQLBuilder(nextIdx)

	if userCtx != nil {
		if userCtx.QueryFilters == nil {
			rbacBuilder.AddCondition("1 = 0")
		} else {
			cond, condParams := rbac.BuildConditions(userCtx.QueryFilters, "r.")
			rbacBuilder.AddCondition(cond, condParams...)
		}
	}

	rbacWhere, rbacParams := rbacBuilder.BuildConditions()
	params = append(params, rbacParams...)

	var query string
	if rbacWhere != "" {
		query = fmt.Sprintf(`
SELECT related.uid, related.kind FROM (%s) AS related
INNER JOIN search.resources r ON related.uid = r.uid
WHERE %s`, cte, rbacWhere)
	} else {
		query = fmt.Sprintf(`
SELECT related.uid, related.kind FROM (%s) AS related
INNER JOIN search.resources r ON related.uid = r.uid`, cte)
	}

	return query, params
}

func (f *FindRelatedCore) buildItemQuery(uids []interface{}, limit int) (string, []interface{}) {
	sb := utils.NewSQLBuilder(1)
	sb.AddIN("uid", uids)

	where, params := sb.BuildWhere()
	query := fmt.Sprintf("SELECT uid, cluster, data FROM search.resources %s LIMIT %d", where, limit)
	return query, params
}

func (f *FindRelatedCore) processItems(queryResult *types.QueryResult, relatedUIDs []uidInfo) []RelatedKindGroup {
	// Build UID→kind map from traversal results.
	uidKind := make(map[string]string, len(relatedUIDs))
	for _, r := range relatedUIDs {
		uidKind[r.uid] = r.kind
	}

	// Group items by kind.
	grouped := make(map[string][]RelatedItem)
	for _, row := range queryResult.Rows {
		if len(row) < 3 {
			continue
		}
		uid, ok := row[0].(string)
		if !ok {
			continue
		}
		cluster, ok := row[1].(string)
		if !ok {
			continue
		}
		dataMap, ok := row[2].(map[string]interface{})
		if !ok {
			continue
		}

		dataMap = f.sanitizer.SanitizeResourceDataMap(dataMap)

		kind := uidKind[uid]
		if kind == "" {
			if k, exists := dataMap["kind"]; exists {
				if ks, ok := k.(string); ok {
					kind = ks
				}
			}
		}

		item := RelatedItem{
			UID:     uid,
			Cluster: cluster,
			Kind:    kind,
			Data:    dataMap,
		}
		if name, exists := dataMap["name"]; exists {
			if ns, ok := name.(string); ok {
				item.Name = ns
			}
		}
		if ns, exists := dataMap["namespace"]; exists && ns != nil {
			if nsStr, ok := ns.(string); ok && nsStr != "" {
				item.Namespace = &nsStr
			}
		}

		grouped[kind] = append(grouped[kind], item)
	}

	groups := make([]RelatedKindGroup, 0, len(grouped))
	for kind, items := range grouped {
		groups = append(groups, RelatedKindGroup{
			Kind:  kind,
			Count: len(items),
			Items: items,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Kind < groups[j].Kind })
	return groups
}

type uidInfo struct {
	uid  string
	kind string
}

func (f *FindRelatedCore) groupCounts(related []uidInfo) []RelatedKindGroup {
	counts := make(map[string]int)
	for _, r := range related {
		counts[r.kind]++
	}
	groups := make([]RelatedKindGroup, 0, len(counts))
	for kind, count := range counts {
		groups = append(groups, RelatedKindGroup{
			Kind:  kind,
			Count: count,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Kind < groups[j].Kind })
	return groups
}
