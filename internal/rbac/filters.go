package rbac

import (
	"fmt"
	"log"
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

			if namespace == "*" && cluster == "" {
				log.Printf("[RBAC-SECURITY] Skipping wildcard namespace rule with empty cluster")
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

// buildClusterPerms builds an OR-combined SQL condition for a set of permissions
// on a given cluster and optional namespace. Uses %s placeholders.
func buildClusterPerms(colPrefix, cluster, namespace string, perms []auth.ResourcePermission) (string, []interface{}) {
	var conditions []string
	var allParams []interface{}

	for _, perm := range perms {
		var parts []string
		var params []interface{}

		parts = append(parts, fmt.Sprintf("%scluster = %%s", colPrefix))
		params = append(params, cluster)

		if namespace != "" && namespace != "*" {
			parts = append(parts, fmt.Sprintf("%sdata->>'namespace' = %%s", colPrefix))
			params = append(params, namespace)
		}

		if perm.Kind == "*" && perm.APIGroup == "*" {
			// Full wildcard — cluster (+ namespace) only.
		} else if perm.Kind == "*" {
			agCond, agParams := apiGroupCondition(colPrefix, perm.APIGroup)
			parts = append(parts, agCond)
			params = append(params, agParams...)
		} else {
			parts = append(parts, fmt.Sprintf("%sdata->>'kind' = %%s", colPrefix))
			params = append(params, perm.Kind)

			if perm.APIGroup != "*" {
				agCond, agParams := apiGroupCondition(colPrefix, perm.APIGroup)
				parts = append(parts, agCond)
				params = append(params, agParams...)
			}
		}

		allParams = append(allParams, params...)
		conditions = append(conditions, "("+strings.Join(parts, " AND ")+")")
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return strings.Join(conditions, " OR "), allParams
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
