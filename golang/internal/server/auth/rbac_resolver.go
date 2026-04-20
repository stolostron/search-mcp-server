package auth

import (
	"context"
	"fmt"
	"log"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/stolostron/cluster-lifecycle-api/helpers/userpermission"
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
	} else if len(managedSource.ClusterScopedKinds) > 0 || len(managedSource.NamespacedResources) > 0 {
		// SECURITY FIX: Only append sources with actual permissions
		permissionSources = append(permissionSources, *managedSource)
		log.Printf("[RBAC-DEBUG] UserPermission API returned %d cluster-scoped kinds, %d namespaced mappings", len(managedSource.ClusterScopedKinds), len(managedSource.NamespacedResources))
	} else {
		log.Printf("[RBAC-DEBUG] UserPermission API returned 0 permissions - not adding source")
	}

	// 2. Get hub cluster permissions (Hub Kubernetes API) - NEW
	hubSource, err := r.resolveHubKubernetesAPI(ctx, userToken)
	if err != nil {
		log.Printf("[RBAC-SECURITY] Hub Kubernetes API failed: %v", err)
		// Don't fail completely - managed permissions might still work
	} else if len(hubSource.ClusterScopedKinds) > 0 || len(hubSource.NamespacedResources) > 0 {
		// SECURITY FIX: Only append sources with actual permissions
		permissionSources = append(permissionSources, *hubSource)
		log.Printf("[RBAC-DEBUG] Hub Kubernetes API returned %d cluster-scoped kinds, %d namespaced mappings", len(hubSource.ClusterScopedKinds), len(hubSource.NamespacedResources))
	} else {
		log.Printf("[RBAC-DEBUG] Hub Kubernetes API returned 0 permissions - not adding source")
	}

	// 3. SECURITY FIX: Must have at least one source WITH actual permissions
	if len(permissionSources) == 0 {
		return nil, fmt.Errorf("user has no permissions across any source - denying access for security")
	}

	filters := &QueryFilters{
		PermissionSources: permissionSources,
	}

	log.Printf("[RBAC-DEBUG] Combined permissions: %d valid sources with permissions", len(permissionSources))
	return filters, nil
}

// createUserKubernetesConfig creates a Kubernetes config using the user's token
func (r *RBACResolver) createUserKubernetesConfig(userToken string) (*rest.Config, error) {
	return CreateDiscoveryConfig(r.config.KubernetesURL, userToken, r.config.AuthTimeout, r.config.SkipTLS), nil
}

// resolvePermissions discovers user's actual permissions using UserPermission API
func (r *RBACResolver) resolvePermissions(ctx context.Context, clientset *kubernetes.Clientset, userToken string) ([]PermissionRule, error) {
	// Create user config for UserPermission API call
	userConfig, err := r.createUserKubernetesConfig(userToken)
	if err != nil {
		log.Printf("[RBAC-SECURITY] Failed to create user config for UserPermission API: %v", err)
		log.Printf("[RBAC-SECURITY] FAIL-SECURE: Denying access due to configuration failure")
		return nil, fmt.Errorf("user config creation failed, access denied for security: %w", err)
	}

	// Log the exact API call parameters
	log.Printf("[RBAC-DEBUG] ==================== USER PERMISSION API CALL ====================")
	log.Printf("[RBAC-DEBUG] Calling userpermission.GetSelfPermissionRules with:")
	log.Printf("[RBAC-DEBUG]   Host: %s", userConfig.Host)
	log.Printf("[RBAC-DEBUG]   BearerToken: %.20s... (first 20 chars)", userConfig.BearerToken)
	log.Printf("[RBAC-DEBUG]   Timeout: %v", userConfig.Timeout)
	if r.config.KubernetesURL != "" {
		log.Printf("[RBAC-DEBUG]   TLS Insecure: %t (testing mode)", userConfig.TLSClientConfig.Insecure)
	} else {
		log.Printf("[RBAC-DEBUG]   TLS Secure: true (production mode)")
	}
	log.Printf("[RBAC-DEBUG]   Interested Verbs: [get, list] (read-only access)")
	log.Printf("[RBAC-DEBUG] ================================================================")

	// CORRECT API: Use UserPermission API with read-only verb filter
	rawPermissions, err := userpermission.GetSelfPermissionRules(ctx, userConfig, "get", "list")
	if err != nil {
		log.Printf("[RBAC-SECURITY] Failed to get UserPermission rules: %v", err)
		log.Printf("[RBAC-SECURITY] FAIL-SECURE: Denying access due to UserPermission API failure")
		return nil, fmt.Errorf("UserPermission API failed, access denied for security: %w", err)
	}

	// Log the raw API response
	log.Printf("[RBAC-DEBUG] ==================== USER PERMISSION API RESPONSE ====================")
	log.Printf("[RBAC-DEBUG] Raw UserPermission API returned %d permission rules", len(rawPermissions))

	for i, rule := range rawPermissions {
		log.Printf("[RBAC-DEBUG] UserPermission Rule[%d]:", i)
		log.Printf("[RBAC-DEBUG]   Verbs: %v", rule.Verbs)
		log.Printf("[RBAC-DEBUG]   APIGroups: %v", rule.APIGroups)
		log.Printf("[RBAC-DEBUG]   Resources: %v", rule.Resources)
		log.Printf("[RBAC-DEBUG]   ResourceNames: %v", rule.ResourceNames)
		log.Printf("[RBAC-DEBUG]   Clusters: %v", rule.Clusters)
		log.Printf("[RBAC-DEBUG]   Namespaces: %v", rule.Namespaces)
		log.Printf("[RBAC-DEBUG]   ----")
	}
	log.Printf("[RBAC-DEBUG] ================================================================")

	// Convert from userpermission.PermissionRule to our PermissionRule format
	var permissions []PermissionRule
	for i, rule := range rawPermissions {
		log.Printf("[RBAC-DEBUG] Converting UserPermission rule %d to PermissionRule format", i)

		permissions = append(permissions, PermissionRule{
			ResourceRule: rule.ResourceRule, // This contains Verbs, APIGroups, Resources, ResourceNames
			Clusters:     rule.Clusters,
			Namespaces:   rule.Namespaces,
		})

		log.Printf("[RBAC-DEBUG] ✅ Converted rule %d: verbs=%v, groups=%v, resources=%v, clusters=%v, namespaces=%v",
			i, rule.Verbs, rule.APIGroups, rule.Resources, rule.Clusters, rule.Namespaces)
	}

	log.Printf("[RBAC-DEBUG] Total permissions discovered via UserPermission API: %d rules", len(permissions))
	return permissions, nil
}

// resolveUserPermissionAPI handles managed cluster permissions using direct namespace→resource mapping
func (r *RBACResolver) resolveUserPermissionAPI(ctx context.Context, userToken string) (*PermissionSource, error) {
	userConfig, err := r.createUserKubernetesConfig(userToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create user K8s config: %w", err)
	}

	log.Printf("[RBAC-DEBUG] ==================== USER PERMISSION API CALL ====================")
	log.Printf("[RBAC-DEBUG] Calling userpermission.GetSelfPermissionRules with:")
	log.Printf("[RBAC-DEBUG]   Host: %s", userConfig.Host)
	log.Printf("[RBAC-DEBUG]   BearerToken: %.20s... (first 20 chars)", userConfig.BearerToken)
	log.Printf("[RBAC-DEBUG]   Timeout: %v", userConfig.Timeout)

	rawPermissions, err := userpermission.GetSelfPermissionRules(ctx, userConfig, "get", "list")
	if err != nil {
		return nil, fmt.Errorf("UserPermission API failed: %w", err)
	}

	log.Printf("[RBAC-DEBUG] Raw UserPermission API returned %d permission rules", len(rawPermissions))

	// Convert to direct mapping structure (NO Cartesian products)
	source := &PermissionSource{
		Source:              "userpermission",
		ClusterScopedKinds:  []string{},
		NamespacedResources: make(map[string][]string), // Direct namespace→resource mapping
		ManagedClusters:     make(map[string]struct{}),
	}

	// Process each permission rule individually to build explicit namespace→resource mappings
	for i, perm := range rawPermissions {
		log.Printf("[RBAC-DEBUG] UserPermission rule %d: clusters=%v, namespaces=%v, resources=%v",
			i, perm.Clusters, perm.Namespaces, perm.Resources)

		// Only process rules that allow "list" verb (for search operations)
		if !contains(perm.Verbs, "list") && !contains(perm.Verbs, "*") {
			log.Printf("[RBAC-DEBUG] Rule %d: Skipping, no 'list' verb", i)
			continue
		}

		// Map API resource names to Kubernetes Kinds using discovery
		var allowedKinds []string
		for _, resource := range perm.Resources {
			if resource == "*" {
				allowedKinds = append(allowedKinds, "*")
			} else {
				kind := r.mapResourceToKindWithToken("", resource, userToken)
				if kind != "" {
					allowedKinds = append(allowedKinds, kind)
					log.Printf("[RBAC-DEBUG] Rule %d: Mapped resource '%s' → kind '%s'", i, resource, kind)
				} else {
					log.Printf("[RBAC-DEBUG] Rule %d: Failed to map resource '%s' to kind", i, resource)
				}
			}
		}

		// Skip rule if no valid kinds found
		if len(allowedKinds) == 0 {
			log.Printf("[RBAC-DEBUG] Rule %d: Skipping, no valid kinds", i)
			continue
		}

		// Track managed clusters
		for _, cluster := range perm.Clusters {
			source.ManagedClusters[cluster] = struct{}{}
		}

		// Handle cluster-scoped vs namespace-scoped resources
		if len(perm.Namespaces) == 1 && perm.Namespaces[0] == "*" {
			// Cluster-scoped permissions
			for _, kind := range allowedKinds {
				if !contains(source.ClusterScopedKinds, kind) {
					source.ClusterScopedKinds = append(source.ClusterScopedKinds, kind)
					log.Printf("[RBAC-DEBUG] Rule %d: Added cluster-scoped kind '%s'", i, kind)
				}
			}
		} else {
			// Namespace-scoped permissions - create explicit namespace→resource mapping
			for _, namespace := range perm.Namespaces {
				// Initialize namespace if not exists
				if _, exists := source.NamespacedResources[namespace]; !exists {
					source.NamespacedResources[namespace] = []string{}
				}

				// Add allowed kinds to this specific namespace (prevents cross-multiplication)
				for _, kind := range allowedKinds {
					if !contains(source.NamespacedResources[namespace], kind) {
						source.NamespacedResources[namespace] = append(source.NamespacedResources[namespace], kind)
						log.Printf("[RBAC-DEBUG] Rule %d: Added namespace '%s' → kind '%s'", i, namespace, kind)
					}
				}
			}
		}
	}

	log.Printf("[RBAC-DEBUG] UserPermission source: %d cluster-scoped kinds, %d namespace mappings, %d managed clusters",
		len(source.ClusterScopedKinds), len(source.NamespacedResources), len(source.ManagedClusters))

	// Debug: Print namespace mappings
	for namespace, kinds := range source.NamespacedResources {
		log.Printf("[RBAC-DEBUG] UserPermission: Namespace '%s' → kinds %v", namespace, kinds)
	}

	return source, nil
}

// resolveHubKubernetesAPI handles hub cluster permissions using direct namespace→resource mapping
func (r *RBACResolver) resolveHubKubernetesAPI(ctx context.Context, userToken string) (*PermissionSource, error) {
	// Use the new HubRBACClient with shared resource discovery
	hubClient, err := NewHubRBACClient(r.config, r.resourceDiscovery)
	if err != nil {
		return nil, fmt.Errorf("failed to create hub RBAC client: %w", err)
	}

	hubPermissions, err := hubClient.GetHubClusterPermissions(ctx, userToken)
	if err != nil {
		return nil, fmt.Errorf("hub Kubernetes API failed: %w", err)
	}

	// Convert to direct mapping structure (NO Cartesian products)
	source := &PermissionSource{
		Source:              "hub-kubernetes",
		ClusterScopedKinds:  []string{},
		NamespacedResources: make(map[string][]string), // Direct namespace→resource mapping
		ManagedClusters:     map[string]struct{}{"local-cluster": {}}, // Hub cluster only
	}

	// 1. Add cluster-scoped resources (hub cluster only)
	if len(hubPermissions.ClusterScopedResources) > 0 {
		for _, resource := range hubPermissions.ClusterScopedResources {
			if !contains(source.ClusterScopedKinds, resource.Kind) {
				source.ClusterScopedKinds = append(source.ClusterScopedKinds, resource.Kind)
				log.Printf("[HUB-RBAC-DEBUG] Added hub cluster-scoped kind: %s", resource.Kind)
			}
		}
	}

	// 2. Add namespaced resources with explicit namespace→resource mapping
	for namespace, resources := range hubPermissions.NamespacedResources {
		// Initialize namespace if not exists
		if _, exists := source.NamespacedResources[namespace]; !exists {
			source.NamespacedResources[namespace] = []string{}
		}

		// Add each resource kind to this specific namespace (prevents cross-multiplication)
		for _, resource := range resources {
			if !contains(source.NamespacedResources[namespace], resource.Kind) {
				source.NamespacedResources[namespace] = append(source.NamespacedResources[namespace], resource.Kind)
				log.Printf("[HUB-RBAC-DEBUG] Added hub namespace '%s' → kind '%s'", namespace, resource.Kind)
			}
		}
	}

	log.Printf("[HUB-RBAC-DEBUG] Hub permissions: %d cluster-scoped kinds, %d namespace mappings",
		len(source.ClusterScopedKinds), len(source.NamespacedResources))

	// Debug: Print namespace mappings
	for namespace, kinds := range source.NamespacedResources {
		log.Printf("[HUB-RBAC-DEBUG] Hub: Namespace '%s' → kinds %v", namespace, kinds)
	}

	return source, nil
}

// getRawUserPermissions uses SelfSubjectRulesReview to get actual user permissions
func (r *RBACResolver) getRawUserPermissions(ctx context.Context, clientset *kubernetes.Clientset) ([]authorizationv1.ResourceRule, error) {
	// Create SelfSubjectRulesReview request
	review := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{
			Namespace: "", // Empty namespace means cluster-wide permissions
		},
	}

	// Get permissions
	result, err := clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("SelfSubjectRulesReview failed: %w", err)
	}

	log.Printf("[RBAC-DEBUG] RAW PERMISSIONS RESPONSE:")
	log.Printf("[RBAC-DEBUG] =========================")
	log.Printf("[RBAC-DEBUG] Status.Incomplete: %t", result.Status.Incomplete)
	log.Printf("[RBAC-DEBUG] Status.EvaluationError: %s", result.Status.EvaluationError)
	log.Printf("[RBAC-DEBUG] ResourceRules count: %d", len(result.Status.ResourceRules))
	log.Printf("[RBAC-DEBUG] NonResourceRules count: %d", len(result.Status.NonResourceRules))

	// Log each resource rule in detail
	for i, rule := range result.Status.ResourceRules {
		log.Printf("[RBAC-DEBUG] ResourceRule[%d]:", i)
		log.Printf("[RBAC-DEBUG]   Verbs: %v", rule.Verbs)
		log.Printf("[RBAC-DEBUG]   APIGroups: %v", rule.APIGroups)
		log.Printf("[RBAC-DEBUG]   Resources: %v", rule.Resources)
		log.Printf("[RBAC-DEBUG]   ResourceNames: %v", rule.ResourceNames)
	}

	// Log non-resource rules if any
	for i, rule := range result.Status.NonResourceRules {
		log.Printf("[RBAC-DEBUG] NonResourceRule[%d]: Verbs=%v, URLs=%v", i, rule.Verbs, rule.NonResourceURLs)
	}

	log.Printf("[RBAC-DEBUG] =========================")

	return result.Status.ResourceRules, nil
}

// resolvePermissionsWithPredefinedList is DEPRECATED and INSECURE - kept for reference only
//
// SECURITY VULNERABILITY: This fallback grants wildcard access (clusters=["*"], namespaces=["*"])
// when any permission is found, creating a massive privilege escalation attack vector.
//
// DECISION: Removed from production use in favor of fail-secure behavior.
// If UserPermission API fails, we deny access entirely rather than risk privilege escalation.
//
// DO NOT USE THIS FUNCTION - it exists only for reference and potential secure reimplementation.
func (r *RBACResolver) resolvePermissionsWithPredefinedList(ctx context.Context, clientset *kubernetes.Clientset, userToken string) ([]PermissionRule, error) {
	var permissions []PermissionRule

	// Define resource types to check - focusing on common Kubernetes resources
	resourceChecks := []struct {
		apiGroup string
		resource string
		verbs    []string
	}{
		{"", "pods", []string{"get", "list"}},
		{"", "services", []string{"get", "list"}},
		{"", "configmaps", []string{"get", "list"}},
		{"", "secrets", []string{"get", "list"}},
		{"", "events", []string{"get", "list"}},
		{"apps", "deployments", []string{"get", "list"}},
		{"apps", "replicasets", []string{"get", "list"}},
		{"apps", "daemonsets", []string{"get", "list"}},
		{"apps", "statefulsets", []string{"get", "list"}},
		{"extensions", "ingresses", []string{"get", "list"}},
		{"networking.k8s.io", "ingresses", []string{"get", "list"}},
		{"route.openshift.io", "routes", []string{"get", "list"}},
	}

	// Check permissions for each resource type
	log.Printf("[RBAC-DEBUG] FALLBACK: Checking %d resource types with %d total permission combinations", len(resourceChecks),
		func() int {
			count := 0
			for _, check := range resourceChecks {
				count += len(check.verbs)
			}
			return count
		}())

	for _, check := range resourceChecks {
		for _, verb := range check.verbs {
			log.Printf("[RBAC-DEBUG] Checking permission: %s %s/%s", verb, check.apiGroup, check.resource)

			allowed, clusters, namespaces, err := r.checkResourcePermission(ctx, clientset, check.apiGroup, check.resource, verb)
			if err != nil {
				log.Printf("[RBAC-DEBUG] ❌ Permission check failed for %s/%s %s: %v", check.apiGroup, check.resource, verb, err)
				continue
			}

			if allowed {
				log.Printf("[RBAC-DEBUG] ✅ Permission GRANTED: %s %s/%s → clusters=%v, namespaces=%v",
					verb, check.apiGroup, check.resource, clusters, namespaces)
				permissions = append(permissions, PermissionRule{
					ResourceRule: authorizationv1.ResourceRule{
						Verbs:     []string{verb},
						APIGroups: []string{check.apiGroup},
						Resources: []string{check.resource},
					},
					Clusters:   clusters,
					Namespaces: namespaces,
				})
			} else {
				log.Printf("[RBAC-DEBUG] ❌ Permission DENIED: %s %s/%s", verb, check.apiGroup, check.resource)
			}
		}
	}

	return permissions, nil
}

// checkResourcePermission checks if user has permission for a specific resource
func (r *RBACResolver) checkResourcePermission(ctx context.Context, clientset *kubernetes.Clientset, apiGroup, resource, verb string) (bool, []string, []string, error) {
	// Create SelfSubjectAccessReview request
	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     verb,
				Group:    apiGroup,
				Resource: resource,
			},
		},
	}

	// Check permission
	result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, nil, nil, fmt.Errorf("SelfSubjectAccessReview failed: %w", err)
	}

	// For now, assume cluster-wide access if permission is granted
	// TODO: In a real implementation, you would need to check specific clusters and namespaces
	// This would require additional logic to enumerate managed clusters and check permissions per cluster
	var clusters []string
	var namespaces []string

	if result.Status.Allowed {
		// For MVP, grant access to all clusters/namespaces if user has the base permission
		// In production, this should be more granular
		clusters = []string{"*"}
		namespaces = []string{"*"}
	}

	return result.Status.Allowed, clusters, namespaces, nil
}


// mapResourceToKind maps Kubernetes API resource names to their Kind names using discovery
func (r *RBACResolver) mapResourceToKindWithToken(apiGroup, resource, userToken string) string {
	// PHASE 6: Dynamic Resource Discovery Implementation
	// This replaces the previous hardcoded mapping with live Kubernetes Discovery API

	if r.resourceDiscovery == nil {
		log.Printf("[DISCOVERY-ERROR] Resource discovery not initialized, falling back to algorithmic mapping for %s", resource)
		// Emergency fallback - should not happen in normal operation
		return r.algorithmicFallback(resource)
	}

	// Use discovery to get the correct Kind, passing userToken for authentication
	ctx := context.Background() // Use background context for discovery calls
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
	case "hardcoded":
		log.Printf("[DISCOVERY-DEBUG] ⚠️  Hardcoded fallback: %s/%s → %s", apiGroup, resource, kind)
	case "algorithmic":
		log.Printf("[DISCOVERY-DEBUG] ❌ Algorithmic fallback: %s/%s → %s (discovery failed)", apiGroup, resource, kind)
	default:
		log.Printf("[DISCOVERY-ERROR] Unknown discovery source '%s' for %s/%s → %s", source, apiGroup, resource, kind)
	}

	// Log discovery error if present
	if result != nil && result.Error != nil {
		log.Printf("[DISCOVERY-DEBUG] Discovery error details: %v", result.Error)
	}
}

// algorithmicFallback provides emergency mapping when discovery is not available
func (r *RBACResolver) algorithmicFallback(resource string) string {
	if len(resource) == 0 {
		return ""
	}

	// Handle common plural-to-singular patterns (same algorithm as ResourceDiscovery)
	var singular string

	if strings.HasSuffix(resource, "ies") {
		// policies → policy, categories → category
		base := strings.TrimSuffix(resource, "ies")
		singular = base + "y"
	} else if strings.HasSuffix(resource, "s") && !strings.HasSuffix(resource, "ss") {
		// pods → pod, deployments → deployment
		// But not: services → service (keep ss intact)
		singular = strings.TrimSuffix(resource, "s")
	} else {
		// No plural pattern detected, use as-is
		singular = resource
	}

	// Capitalize first letter
	if len(singular) == 0 {
		return ""
	}

	fallback := strings.ToUpper(singular[:1]) + singular[1:]
	log.Printf("[DISCOVERY-ERROR] Emergency algorithmic fallback: %s → %s", resource, fallback)

	return fallback
}


// Helper method to integrate with AuthMiddleware
func (m *AuthMiddleware) resolveUserPermissions(ctx context.Context, userToken string) (*QueryFilters, error) {
	resolver := NewRBACResolver(m.config, m.db)
	return resolver.ResolveUserPermissions(ctx, userToken)
}

