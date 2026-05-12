package auth

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/dynamic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	clusterviewv1alpha1 "github.com/stolostron/cluster-lifecycle-api/clusterview/v1alpha1"
	"github.com/stolostron/search-mcp-server/pkg/database"
)

// PermissionRule represents a resolved permission rule - matches cluster-lifecycle-api format
type PermissionRule struct {
	authorizationv1.ResourceRule           // Contains Verbs, APIGroups, Resources, ResourceNames
	Clusters   []string `json:"clusters"`    // List of cluster names where this rule applies
	Namespaces []string `json:"namespaces"`  // List of namespaces where this rule applies
}

// RBACResolver handles permission resolution for users
type RBACResolver struct {
	config            *AuthConfig
	resourceDiscovery *ResourceDiscovery
}

// NewRBACResolver creates a new RBAC resolver
func NewRBACResolver(config *AuthConfig, db *database.DatabaseConnection) *RBACResolver {
	return &RBACResolver{
		config:            config,
		resourceDiscovery: GetSharedResourceDiscovery(config, db), // Use shared instance
	}
}

// ResolveUserPermissions resolves user's actual Kubernetes RBAC permissions using dual API approach
func (r *RBACResolver) ResolveUserPermissions(ctx context.Context, userToken string) (*QueryFilters, error) {
	log.Printf("[RBAC-DEBUG] Resolving permissions via dual API approach")

	var permissionSources []PermissionSource

	// 1. Get managed cluster permissions (UserPermission API) - existing logic
	managedSource, err := r.resolveUserPermissionAPI(ctx, userToken)
	if err != nil {
		log.Printf("[RBAC-SECURITY] UserPermission API failed: %v", err)
		// Don't fail completely - hub permissions might still work
	} else if len(managedSource.ClusterScopedKinds) > 0 || len(managedSource.NamespacedKinds) > 0 {
		permissionSources = append(permissionSources, *managedSource)
		log.Printf("[RBAC-DEBUG] UserPermission API returned %d cluster-scoped kinds, %d namespaced mappings", len(managedSource.ClusterScopedKinds), len(managedSource.NamespacedKinds))
	} else {
		log.Printf("[RBAC-DEBUG] UserPermission API returned 0 permissions - not adding source")
	}

	// 2. Get hub cluster permissions (Hub Kubernetes API) - NEW
	var hubClusterName string
	hubSource, resolvedHub, err := r.resolveHubKubernetesAPI(ctx, userToken)
	if err != nil {
		log.Printf("[RBAC-SECURITY] Hub Kubernetes API failed: %v", err)
		// Don't fail completely - managed permissions might still work
		hubClusterName = "local-cluster" // Fallback only when API fails
	} else if len(hubSource.ClusterScopedKinds) > 0 || len(hubSource.NamespacedKinds) > 0 {
		permissionSources = append(permissionSources, *hubSource)
		log.Printf("[RBAC-DEBUG] Hub Kubernetes API returned %d cluster-scoped kinds, %d namespaced mappings", len(hubSource.ClusterScopedKinds), len(hubSource.NamespacedKinds))

		hubClusterName = resolvedHub
	} else {
		log.Printf("[RBAC-DEBUG] Hub Kubernetes API returned 0 permissions - not adding source")
		hubClusterName = resolvedHub
	}

	if len(permissionSources) == 0 {
		return nil, fmt.Errorf("user has no permissions across any source - denying access for security")
	}


	filters := &QueryFilters{
		PermissionSources: permissionSources,
		HubClusterName:    hubClusterName,
	}

	log.Printf("[RBAC-DEBUG] Combined permissions: %d valid sources with permissions", len(permissionSources))
	return filters, nil
}

// createUserKubernetesConfig creates a Kubernetes config using the user's token
func (r *RBACResolver) createUserKubernetesConfig(userToken string) (*rest.Config, error) {
	return CreateDiscoveryConfig(r.config.KubernetesURL, userToken, r.config.AuthTimeout, r.config.SkipTLS), nil
}

// getUserPermissionCRsDirectly queries UserPermission CRs directly using dynamic client
// This bypasses the buggy GetSelfPermissionRules() API that has consolidation issues
func (r *RBACResolver) getUserPermissionCRsDirectly(ctx context.Context, userConfig *rest.Config) (*clusterviewv1alpha1.UserPermissionList, error) {
	// Create dynamic client with user's authentication
	dynamicClient, err := dynamic.NewForConfig(userConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Query UserPermission CRs (same as search-v2-api approach)
	gvr := schema.GroupVersionResource{
		Group:    "clusterview.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "userpermissions",
	}

	unstructuredList, err := dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list UserPermission CRs: %w", err)
	}

	// Convert unstructured.UnstructuredList to UserPermissionList (same as search-v2-api)
	var userPermissionList clusterviewv1alpha1.UserPermissionList
	for _, item := range unstructuredList.Items {
		var up clusterviewv1alpha1.UserPermission
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &up)
		if err != nil {
			return nil, fmt.Errorf("failed to convert Unstructured to UserPermission: %w", err)
		}
		userPermissionList.Items = append(userPermissionList.Items, up)
	}

	return &userPermissionList, nil
}


// resolveUserPermissionAPI handles managed cluster permissions using direct namespace→resource mapping
func (r *RBACResolver) resolveUserPermissionAPI(ctx context.Context, userToken string) (*PermissionSource, error) {
	userConfig, err := r.createUserKubernetesConfig(userToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create user K8s config: %w", err)
	}

	log.Printf("[RBAC-DEBUG] Querying UserPermission CRs directly (avoiding buggy API consolidation)")

	// Query UserPermission CRs directly like search-v2-api does
	// This avoids the buggy GetSelfPermissionRules() API that loses cluster-namespace relationships
	userPermissions, err := r.getUserPermissionCRsDirectly(ctx, userConfig)
	if err != nil {
		return nil, fmt.Errorf("UserPermission CR query failed: %w", err)
	}

	log.Printf("[RBAC-DEBUG] Retrieved %d UserPermission CRs", len(userPermissions.Items))

	// DEBUG: Log the complete raw UserPermission CRs before any processing (debug level only)
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel == "debug" {
		log.Printf("[USERPERMISSION-RAW] ==================== RAW CR RESPONSE ====================")
		for i, up := range userPermissions.Items {
			log.Printf("[USERPERMISSION-RAW] UserPermission %d: %s", i, up.Name)
			for j, binding := range up.Status.Bindings {
				log.Printf("[USERPERMISSION-RAW]   Binding %d:", j)
				log.Printf("[USERPERMISSION-RAW]     Cluster: %s", binding.Cluster)
				log.Printf("[USERPERMISSION-RAW]     Namespaces: %v", binding.Namespaces)
				log.Printf("[USERPERMISSION-RAW]     Scope: %s", binding.Scope)
			}
			for k, rule := range up.Status.ClusterRoleDefinition.Rules {
				log.Printf("[USERPERMISSION-RAW]   Rule %d:", k)
				log.Printf("[USERPERMISSION-RAW]     Resources: %v", rule.Resources)
				log.Printf("[USERPERMISSION-RAW]     Verbs: %v", rule.Verbs)
				log.Printf("[USERPERMISSION-RAW]     APIGroups: %v", rule.APIGroups)
			}
		}
		log.Printf("[USERPERMISSION-RAW] ==================== END RAW CR RESPONSE ====================")
	}

	// Convert to direct mapping structure (NO Cartesian products)
	// Process UserPermission CRs correctly to preserve cluster-namespace relationships
	source := &PermissionSource{
		Source:              "userpermission-cr",
		ClusterScopedKinds:  make(map[string][]string), // cluster → allowed cluster-scoped Kinds
		NamespacedKinds:     make(map[string][]string), // Direct "cluster/namespace" → allowed Kinds mapping
		ManagedClusters:     make(map[string]struct{}),
	}

	// Process each UserPermission CR individually to build explicit namespace→resource mappings
	for i, userPerm := range userPermissions.Items {
		log.Printf("[RBAC-DEBUG] Processing UserPermission CR %d: %s with %d bindings",
			i, userPerm.Name, len(userPerm.Status.Bindings))

		// Process each binding (cluster + namespace combination) separately
		for j, binding := range userPerm.Status.Bindings {
			log.Printf("[RBAC-DEBUG] UserPerm %d, Binding %d: cluster=%s, namespaces=%v, scope=%s",
				i, j, binding.Cluster, binding.Namespaces, binding.Scope)

			// Track managed clusters
			source.ManagedClusters[binding.Cluster] = struct{}{}

			// Process each rule in the ClusterRoleDefinition
			for k, rule := range userPerm.Status.ClusterRoleDefinition.Rules {
				log.Printf("[RBAC-DEBUG] UserPerm %d, Binding %d, Rule %d: processing %d resources",
					i, j, k, len(rule.Resources))

				// Only process rules that allow "list" verb (for search operations)
				if !slices.Contains(rule.Verbs, "list") && !slices.Contains(rule.Verbs, "*") {
					log.Printf("[RBAC-DEBUG] UserPerm %d, Rule %d: Skipping, no 'list' verb", i, k)
					continue
				}

				// Map API resource names to Kubernetes Kinds using discovery
				var allowedKinds []string
				for _, resource := range rule.Resources {
					if resource == "*" {
						allowedKinds = append(allowedKinds, "*")
					} else {
						kind := r.mapResourceToKindWithToken(ctx, "", resource, userToken)
						if kind != "" {
							allowedKinds = append(allowedKinds, kind)
							log.Printf("[RBAC-DEBUG] UserPerm %d, Rule %d: Mapped resource '%s' → kind '%s'", i, k, resource, kind)
						} else {
							log.Printf("[RBAC-DEBUG] UserPerm %d, Rule %d: Failed to map resource '%s' to kind", i, k, resource)
						}
					}
				}

				// Skip rule if no valid kinds found
				if len(allowedKinds) == 0 {
					log.Printf("[RBAC-DEBUG] UserPerm %d, Rule %d: Skipping, no valid kinds", i, k)
					continue
				}

				// Handle cluster-scoped vs namespace-scoped resources
				// CRITICAL: Use binding.Scope and binding.Namespaces (not rule-level logic)
				if binding.Scope == "cluster" || (len(binding.Namespaces) == 1 && binding.Namespaces[0] == "*") {
					// Cluster-scoped permissions for this specific cluster
					cluster := binding.Cluster

					// Initialize cluster if not exists
					if _, exists := source.ClusterScopedKinds[cluster]; !exists {
						source.ClusterScopedKinds[cluster] = []string{}
					}

					// Add allowed kinds to this specific cluster ONLY
					for _, kind := range allowedKinds {
						if !slices.Contains(source.ClusterScopedKinds[cluster], kind) {
							source.ClusterScopedKinds[cluster] = append(source.ClusterScopedKinds[cluster], kind)
							log.Printf("[RBAC-DEBUG] UserPerm %d, Binding %d: Added cluster-scoped kind '%s' for cluster '%s'",
								i, j, kind, cluster)
						}
					}
				} else {
					// Namespace-scoped permissions - use binding.Namespaces (preserves cluster-namespace relationship)
					for _, namespace := range binding.Namespaces {
						// Create cluster-aware namespace key to prevent cross-cluster namespace conflicts
						namespaceKey := fmt.Sprintf("%s/%s", binding.Cluster, namespace)

						// Initialize namespace if not exists
						if _, exists := source.NamespacedKinds[namespaceKey]; !exists {
							source.NamespacedKinds[namespaceKey] = []string{}
						}

						// Add allowed kinds to this specific cluster/namespace combination
						for _, kind := range allowedKinds {
							if !slices.Contains(source.NamespacedKinds[namespaceKey], kind) {
								source.NamespacedKinds[namespaceKey] = append(source.NamespacedKinds[namespaceKey], kind)
								log.Printf("[RBAC-DEBUG] UserPerm %d, Binding %d: Added namespace '%s' → kind '%s'",
									i, j, namespaceKey, kind)
							}
						}
					}
				}
			}
		}
	}

	// Count total cluster-scoped kinds across all clusters
	totalClusterScopedKinds := 0
	for _, kinds := range source.ClusterScopedKinds {
		totalClusterScopedKinds += len(kinds)
	}
	log.Printf("[RBAC-DEBUG] UserPermission CR source: %d cluster-scoped kinds across %d clusters, %d namespace mappings, %d managed clusters",
		totalClusterScopedKinds, len(source.ClusterScopedKinds), len(source.NamespacedKinds), len(source.ManagedClusters))

	// Additional debug: show the actual mappings to verify cluster-kind relationships
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel == "debug" {
		log.Printf("[RBAC-DEBUG] Cluster-scoped kinds by cluster: %v", source.ClusterScopedKinds)
		for nsKey, kinds := range source.NamespacedKinds {
			log.Printf("[RBAC-DEBUG] Namespace '%s' → kinds: %v", nsKey, kinds)
		}
		var clusters []string
		for cluster := range source.ManagedClusters {
			clusters = append(clusters, cluster)
		}
		log.Printf("[RBAC-DEBUG] Managed clusters: %v", clusters)
	}

	return source, nil
}

// resolveHubKubernetesAPI handles hub cluster permissions using direct namespace→resource mapping
func (r *RBACResolver) resolveHubKubernetesAPI(ctx context.Context, userToken string) (*PermissionSource, string, error) {
	// Use the new HubRBACClient with shared resource discovery
	hubClient, err := NewHubRBACClient(r.config, r.resourceDiscovery)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create hub RBAC client: %w", err)
	}

	hubPermissions, err := hubClient.GetHubClusterPermissions(ctx, userToken)
	if err != nil {
		return nil, "", fmt.Errorf("hub Kubernetes API failed: %w", err)
	}

	// Convert to direct mapping structure (NO Cartesian products)
	source := &PermissionSource{
		Source:              "hub-kubernetes",
		ClusterScopedKinds:  make(map[string][]string), // cluster → allowed cluster-scoped Kinds
		NamespacedKinds:     make(map[string][]string), // Direct namespace→resource mapping
		ManagedClusters:     map[string]struct{}{hubPermissions.HubClusterName: {}}, // Hub cluster only (dynamically detected)
	}

	// 1. Add cluster-scoped resources (hub cluster only)
	if len(hubPermissions.ClusterScopedResources) > 0 {
		hubCluster := hubPermissions.HubClusterName

		// Initialize hub cluster if not exists
		if _, exists := source.ClusterScopedKinds[hubCluster]; !exists {
			source.ClusterScopedKinds[hubCluster] = []string{}
		}

		for _, resource := range hubPermissions.ClusterScopedResources {
			if !slices.Contains(source.ClusterScopedKinds[hubCluster], resource.Kind) {
				source.ClusterScopedKinds[hubCluster] = append(source.ClusterScopedKinds[hubCluster], resource.Kind)
				log.Printf("[HUB-RBAC-DEBUG] Added hub cluster-scoped kind: %s for cluster: %s", resource.Kind, hubCluster)
			}
		}
	}

	// 2. Add namespaced resources with explicit namespace→resource mapping
	for namespace, resources := range hubPermissions.NamespacedKinds {
		// Initialize namespace if not exists
		if _, exists := source.NamespacedKinds[namespace]; !exists {
			source.NamespacedKinds[namespace] = []string{}
		}

		// Add each resource kind to this specific namespace (prevents cross-multiplication)
		for _, resource := range resources {
			if !slices.Contains(source.NamespacedKinds[namespace], resource.Kind) {
				source.NamespacedKinds[namespace] = append(source.NamespacedKinds[namespace], resource.Kind)
				log.Printf("[HUB-RBAC-DEBUG] Added hub namespace '%s' → kind '%s'", namespace, resource.Kind)
			}
		}
	}

	// Count total cluster-scoped kinds across all clusters
	totalClusterScopedKinds := 0
	for _, kinds := range source.ClusterScopedKinds {
		totalClusterScopedKinds += len(kinds)
	}
	log.Printf("[HUB-RBAC-DEBUG] Hub permissions: %d cluster-scoped kinds across %d clusters, %d namespace mappings",
		totalClusterScopedKinds, len(source.ClusterScopedKinds), len(source.NamespacedKinds))

	return source, hubPermissions.HubClusterName, nil
}




// mapResourceToKind maps Kubernetes API resource names to their Kind names using discovery
func (r *RBACResolver) mapResourceToKindWithToken(ctx context.Context, apiGroup, resource, userToken string) string {
	// PHASE 6: Dynamic Resource Discovery Implementation
	// This replaces the previous hardcoded mapping with live Kubernetes Discovery API

	if r.resourceDiscovery == nil {
		log.Printf("[DISCOVERY-ERROR] Resource discovery not initialized for %s", resource)
		// No fallback - return empty string to fail gracefully
		return ""
	}

	// Use discovery to get the correct Kind, passing userToken for authentication
	// Use request context instead of background to respect cancellation
	kind, discoveryResult := r.resourceDiscovery.GetResourceKind(ctx, userToken, apiGroup, resource)

	// Log the discovery result for audit and debugging
	r.logDiscoveryResult(apiGroup, resource, kind, discoveryResult)

	return kind
}

// logDiscoveryResult provides detailed logging for discovery operations
func (r *RBACResolver) logDiscoveryResult(apiGroup, resource, kind string, result *DiscoveryResult) {
	source := "unknown"
	if result != nil {
		source = result.Source
	}

	switch source {
	case "discovery":
		log.Printf("[DISCOVERY-DEBUG] ✅ Live discovery success: %s/%s → %s", apiGroup, resource, kind)
		if result != nil && len(result.ResourceToKind) > 0 {
			log.Printf("[DISCOVERY-DEBUG] Discovery found %d total resource mappings", len(result.ResourceToKind))
		}
	case "cache":
		log.Printf("[DISCOVERY-DEBUG] ✅ Cache hit: %s/%s → %s", apiGroup, resource, kind)
	case "not_found":
		log.Printf("[DISCOVERY-DEBUG] ❌ Resource not found: %s/%s", apiGroup, resource)
	default:
		log.Printf("[DISCOVERY-ERROR] Unknown discovery source '%s' for %s/%s → %s", source, apiGroup, resource, kind)
	}

	// Log discovery error if present
	if result != nil && result.Error != nil {
		log.Printf("[DISCOVERY-DEBUG] Discovery error details: %v", result.Error)
	}
}



// Helper method to integrate with AuthMiddleware
func (m *AuthMiddleware) resolveUserPermissions(ctx context.Context, userToken string) (*QueryFilters, error) {
	resolver := NewRBACResolver(m.config, m.db)
	return resolver.ResolveUserPermissions(ctx, userToken)
}

