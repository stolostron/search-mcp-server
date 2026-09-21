package rbac

import (
	"fmt"
	"strings"

	"github.com/stolostron/search-mcp-server/internal/server/auth"
)

// Options configures RBAC condition building.
type Options struct {
	// ColPrefix is prepended to column references (e.g. "r." for aliased tables, "" for bare).
	ColPrefix string
	// PermFilter, if set, narrows the permission list before SQL generation.
	// Used by findresources to restrict to the user's requested kind filter.
	PermFilter func([]auth.ResourcePermission) []auth.ResourcePermission
}

// BuildConditions builds the complete RBAC SQL condition for the given QueryFilters.
// The returned condition string uses %s placeholders for parameter substitution,
// suitable for passing to SQLBuilder.AddCondition().
// colPrefix is prepended to column references (e.g. "r." for aliased tables, "" for bare).
func BuildConditions(filters *auth.QueryFilters, colPrefix string) (string, []interface{}) {
	return BuildConditionsWithOptions(filters, Options{ColPrefix: colPrefix})
}

// BuildConditionsWithOptions is like BuildConditions but accepts full Options.
func BuildConditionsWithOptions(filters *auth.QueryFilters, opts Options) (string, []interface{}) {
	if filters == nil || len(filters.PermissionSources) == 0 {
		return "1 = 0", nil
	}

	filterPerms := func(perms []auth.ResourcePermission) []auth.ResourcePermission {
		if opts.PermFilter != nil {
			return opts.PermFilter(perms)
		}
		return perms
	}

	var sourceConditions []string
	var allParams []interface{}

	for _, source := range filters.PermissionSources {
		var perSourceConditions []string
		var perSourceParams []interface{}

		for cluster, perms := range source.ClusterScopedKinds {
			filtered := filterPerms(perms)
			if len(filtered) == 0 {
				continue
			}
			cond, params := buildClusterPerms(opts.ColPrefix, cluster, "", filtered)
			if cond != "" {
				perSourceConditions = append(perSourceConditions, cond)
				perSourceParams = append(perSourceParams, params...)
			}
		}

		for nsKey, perms := range source.NamespacedKinds {
			filtered := filterPerms(perms)
			if len(filtered) == 0 {
				continue
			}

			var cluster, namespace string
			if source.Source == "userpermission-cr" {
				parts := strings.SplitN(nsKey, "/", 2)
				if len(parts) == 2 {
					cluster, namespace = parts[0], parts[1]
				} else {
					cluster, namespace = "", nsKey
				}
			} else {
				namespace = nsKey
				cluster = filters.HubClusterName
			}

			if cluster == "" {
				continue
			}

			cond, params := buildClusterPerms(opts.ColPrefix, cluster, namespace, filtered)
			if cond != "" {
				perSourceConditions = append(perSourceConditions, cond)
				perSourceParams = append(perSourceParams, params...)
			}
		}

		if len(perSourceConditions) > 0 {
			sourceConditions = append(sourceConditions,
				"("+strings.Join(perSourceConditions, " OR ")+")")
			allParams = append(allParams, perSourceParams...)
		}
	}

	if len(sourceConditions) == 0 {
		return "1 = 0", nil
	}

	combined := "(" + strings.Join(sourceConditions, " OR ") + ")"
	return combined, allParams
}

// buildClusterPerms builds a SQL condition for a set of permissions on a given
// cluster and optional namespace. Permissions are grouped by apiGroup so that
// multiple kinds in the same group emit a single kind IN (...) clause. The
// cluster (and namespace) condition wraps the resource conditions once, matching
// the structure of the original buildAPIGroupKindConditions. Uses %s placeholders.
func buildClusterPerms(colPrefix, cluster, namespace string, perms []auth.ResourcePermission) (string, []interface{}) {
	resourceCond, resourceParams := apiGroupKindConditions(colPrefix, perms)
	if resourceCond == "" {
		return "", nil
	}

	var outerParts []string
	var outerParams []interface{}

	outerParts = append(outerParts, fmt.Sprintf("%scluster = %%s", colPrefix))
	outerParams = append(outerParams, cluster)

	if namespace != "" && namespace != "*" {
		outerParts = append(outerParts, fmt.Sprintf("%sdata->>'namespace' = %%s", colPrefix))
		outerParams = append(outerParams, namespace)
	}

	outerParts = append(outerParts, "("+resourceCond+")")
	outerParams = append(outerParams, resourceParams...)

	return "(" + strings.Join(outerParts, " AND ") + ")", outerParams
}

// apiGroupKindConditions groups ResourcePermissions by apiGroup and generates
// SQL conditions pairing data->>'apigroup' with data->>'kind'. Full wildcard
// (apigroup=* AND kind=*) short-circuits to "1 = 1". Uses %s placeholders.
func apiGroupKindConditions(colPrefix string, perms []auth.ResourcePermission) (string, []interface{}) {
	for _, p := range perms {
		if p.Kind == "*" && p.APIGroup == "*" {
			return "1 = 1", nil
		}
	}

	type groupEntry struct {
		kinds    []string
		wildcard bool
	}
	groups := make(map[string]*groupEntry)
	var groupOrder []string

	for _, p := range perms {
		entry, exists := groups[p.APIGroup]
		if !exists {
			entry = &groupEntry{}
			groups[p.APIGroup] = entry
			groupOrder = append(groupOrder, p.APIGroup)
		}
		if p.Kind == "*" {
			entry.wildcard = true
		} else if !entry.wildcard {
			found := false
			for _, k := range entry.kinds {
				if k == p.Kind {
					found = true
					break
				}
			}
			if !found {
				entry.kinds = append(entry.kinds, p.Kind)
			}
		}
	}

	var conditions []string
	var params []interface{}

	for _, apiGroup := range groupOrder {
		entry := groups[apiGroup]

		var apiGroupCond string
		switch apiGroup {
		case "*":
			apiGroupCond = ""
		case "":
			apiGroupCond = fmt.Sprintf("(%sdata->>'apigroup' IS NULL OR %sdata->>'apigroup' = '')", colPrefix, colPrefix)
		default:
			apiGroupCond = fmt.Sprintf("%sdata->>'apigroup' = %%s", colPrefix)
		}

		var kindCond string
		if entry.wildcard {
			kindCond = ""
		} else if len(entry.kinds) == 1 {
			kindCond = fmt.Sprintf("%sdata->>'kind' = %%s", colPrefix)
		} else if len(entry.kinds) > 1 {
			placeholders := make([]string, len(entry.kinds))
			for i := range entry.kinds {
				placeholders[i] = "%s"
			}
			kindCond = fmt.Sprintf("%sdata->>'kind' IN (%s)", colPrefix, strings.Join(placeholders, ","))
		}

		var combined string
		if apiGroupCond == "" && kindCond == "" {
			combined = "1 = 1"
		} else if apiGroupCond == "" {
			combined = kindCond
		} else if kindCond == "" {
			combined = apiGroupCond
			if apiGroup != "" && apiGroup != "*" {
				params = append(params, apiGroup)
			}
		} else {
			combined = fmt.Sprintf("(%s AND %s)", apiGroupCond, kindCond)
			if apiGroup != "" && apiGroup != "*" {
				params = append(params, apiGroup)
			}
		}

		if !entry.wildcard {
			for _, k := range entry.kinds {
				params = append(params, k)
			}
		}

		if combined != "" {
			conditions = append(conditions, combined)
		}
	}

	if len(conditions) == 0 {
		return "", nil
	}
	if len(conditions) == 1 {
		return conditions[0], params
	}
	return strings.Join(conditions, " OR "), params
}

// apiGroupCondition returns the SQL fragment and params for matching an apiGroup.
// Empty apiGroup (core resources) uses IS NULL OR = '' to match NULL or empty values.
func apiGroupCondition(colPrefix, apiGroup string) (string, []interface{}) {
	col := fmt.Sprintf("%sdata->>'apigroup'", colPrefix)
	if apiGroup == "" {
		return fmt.Sprintf("(%s IS NULL OR %s = %%s)", col, col), []interface{}{""}
	}
	return fmt.Sprintf("%s = %%s", col), []interface{}{apiGroup}
}
