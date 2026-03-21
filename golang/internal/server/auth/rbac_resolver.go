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
func NewRBACResolver(config *AuthConfig) *RBACResolver {
	return &RBACResolver{
		config: config,
		// resourceDiscovery will be initialized per-request with user token
	}
}

// ResolveUserPermissions resolves user's actual Kubernetes RBAC permissions
func (r *RBACResolver) ResolveUserPermissions(ctx context.Context, userToken string) (*QueryFilters, error) {
	log.Printf("[RBAC-DEBUG] Starting permission resolution for user token (first 20 chars): %.20s...", userToken)

	// Initialize resource discovery for this request with user's token
	r.resourceDiscovery = NewResourceDiscovery(r.config, userToken)
	log.Printf("[RBAC-DEBUG] Initialized resource discovery with user token")

	// Create Kubernetes client with user's token
	userConfig, err := r.createUserKubernetesConfig(userToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create user K8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(userConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	// Resolve permissions for common resource types
	permissions, err := r.resolvePermissions(ctx, clientset, userToken)
	if err != nil {
		log.Printf("[RBAC-SECURITY] Permission resolution failed for user token, denying access: %v", err)
		log.Printf("[RBAC-SECURITY] This is a security-first design choice - K8s API failures result in access denial")
		// SECURITY: Fail secure - if we can't determine permissions, deny access entirely
		return nil, fmt.Errorf("permission resolution failed, access denied for security: %w", err)
	}

	// DEBUG: Log detailed permissions discovered
	log.Printf("[RBAC-DEBUG] Raw permissions discovered: %d rules", len(permissions))
	for i, perm := range permissions {
		log.Printf("[RBAC-DEBUG] Permission[%d]: Verbs=%v, APIGroups=%v, Resources=%v, Clusters=%v, Namespaces=%v",
			i, perm.Verbs, perm.APIGroups, perm.Resources, perm.Clusters, perm.Namespaces)
	}

	// Convert permissions to query filters
	filters := r.convertPermissionsToFilters(permissions)

	log.Printf("[RBAC] Resolved %d permission rules, filters: clusters=%d, namespaces=%d, resources=%d",
		len(permissions),
		len(filters.AllowedClusters),
		len(filters.AllowedNamespaces),
		len(filters.AllowedResources))

	return filters, nil
}

// createUserKubernetesConfig creates a Kubernetes config using the user's token
func (r *RBACResolver) createUserKubernetesConfig(userToken string) (*rest.Config, error) {
	// Build Kubernetes API server URL
	var kubernetesURL string
	if r.config.KubernetesURL != "" {
		kubernetesURL = r.config.KubernetesURL
	} else {
		host := r.config.KubernetesHost
		port := r.config.KubernetesPort
		if host == "" || port == "" {
			return nil, fmt.Errorf("Kubernetes host/port not configured")
		}
		kubernetesURL = fmt.Sprintf("https://%s:%s", host, port)
	}

	// Create rest.Config with user's token
	config := &rest.Config{
		Host:        kubernetesURL,
		BearerToken: strings.TrimPrefix(userToken, "Bearer "),
		Timeout:     r.config.AuthTimeout,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: r.config.SkipTLS,
		},
	}

	return config, nil
}

// resolvePermissions discovers user's actual permissions using UserPermission API
func (r *RBACResolver) resolvePermissions(ctx context.Context, clientset *kubernetes.Clientset, userToken string) ([]PermissionRule, error) {
	// Create user config for UserPermission API call
	userConfig, err := r.createUserKubernetesConfig(userToken)
	if err != nil {
		log.Printf("[RBAC-DEBUG] Failed to create user config for UserPermission API: %v", err)
		log.Printf("[RBAC-DEBUG] Falling back to predefined resource checks...")
		return r.resolvePermissionsWithPredefinedList(ctx, clientset, userToken)
	}

	// Log the exact API call parameters
	log.Printf("[RBAC-DEBUG] ==================== USER PERMISSION API CALL ====================")
	log.Printf("[RBAC-DEBUG] Calling userpermission.GetSelfPermissionRules with:")
	log.Printf("[RBAC-DEBUG]   Host: %s", userConfig.Host)
	log.Printf("[RBAC-DEBUG]   BearerToken: %.20s... (first 20 chars)", userConfig.BearerToken)
	log.Printf("[RBAC-DEBUG]   Timeout: %v", userConfig.Timeout)
	log.Printf("[RBAC-DEBUG]   TLS Insecure: %t", userConfig.TLSClientConfig.Insecure)
	log.Printf("[RBAC-DEBUG]   Interested Verbs: [get, list] (read-only access)")
	log.Printf("[RBAC-DEBUG] ================================================================")

	// CORRECT API: Use UserPermission API with read-only verb filter
	rawPermissions, err := userpermission.GetSelfPermissionRules(ctx, userConfig, "get", "list")
	if err != nil {
		log.Printf("[RBAC-DEBUG] Failed to get UserPermission rules: %v", err)
		log.Printf("[RBAC-DEBUG] Falling back to predefined resource checks...")
		return r.resolvePermissionsWithPredefinedList(ctx, clientset, userToken)
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

// resolvePermissionsWithPredefinedList is the old approach as fallback
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

// convertPermissionsToFilters converts PermissionRules to database QueryFilters
func (r *RBACResolver) convertPermissionsToFilters(permissions []PermissionRule) *QueryFilters {
	log.Printf("[RBAC-DEBUG] Converting %d permission rules to query filters", len(permissions))

	filters := &QueryFilters{
		AllowedClusters:    []string{},
		AllowedNamespaces:  []string{},
		AllowedResources:   []string{},
		ResourceNamespaces: make(map[string][]string), // Track per-resource namespace permissions
	}

	clusterSet := make(map[string]bool)
	namespaceSet := make(map[string]bool)
	resourceSet := make(map[string]bool)
	resourceNamespaceMap := make(map[string]map[string]bool) // map[resourceKind]map[namespace]bool

	for i, perm := range permissions {
		log.Printf("[RBAC-DEBUG] Processing permission rule %d:", i)
		log.Printf("[RBAC-DEBUG]   Verbs: %v", perm.Verbs)
		log.Printf("[RBAC-DEBUG]   APIGroups: %v", perm.APIGroups)
		log.Printf("[RBAC-DEBUG]   Resources: %v", perm.Resources)
		log.Printf("[RBAC-DEBUG]   Clusters: %v", perm.Clusters)
		log.Printf("[RBAC-DEBUG]   Namespaces: %v", perm.Namespaces)

		// Collect clusters
		for _, cluster := range perm.Clusters {
			log.Printf("[RBAC-DEBUG]   Adding cluster: %s", cluster)
			clusterSet[cluster] = true
		}

		// Collect namespaces
		for _, namespace := range perm.Namespaces {
			log.Printf("[RBAC-DEBUG]   Adding namespace: %s", namespace)
			namespaceSet[namespace] = true
		}

		// Map API resources to Kubernetes kinds and track per-resource namespace permissions
		for i, resource := range perm.Resources {
			apiGroup := ""
			if i < len(perm.APIGroups) {
				apiGroup = perm.APIGroups[i]
			}
			kind := r.mapResourceToKind(apiGroup, resource)
			if kind != "" {
				log.Printf("[RBAC-DEBUG]   Mapped %s/%s → Kind: %s", apiGroup, resource, kind)
				resourceSet[kind] = true

				// Track namespace permissions for this specific resource type
				if resourceNamespaceMap[kind] == nil {
					resourceNamespaceMap[kind] = make(map[string]bool)
				}
				for _, namespace := range perm.Namespaces {
					resourceNamespaceMap[kind][namespace] = true
					log.Printf("[RBAC-DEBUG]   Resource %s can access namespace: %s", kind, namespace)
				}
			} else {
				log.Printf("[RBAC-DEBUG]   Failed to map %s/%s to Kind", apiGroup, resource)
			}
		}
	}

	// Convert sets to slices
	for cluster := range clusterSet {
		filters.AllowedClusters = append(filters.AllowedClusters, cluster)
	}
	for namespace := range namespaceSet {
		filters.AllowedNamespaces = append(filters.AllowedNamespaces, namespace)
	}
	for resource := range resourceSet {
		filters.AllowedResources = append(filters.AllowedResources, resource)
	}

	// Convert resource-specific namespace maps to slices
	for resourceKind, nsMap := range resourceNamespaceMap {
		var namespaces []string
		for namespace := range nsMap {
			namespaces = append(namespaces, namespace)
		}
		filters.ResourceNamespaces[resourceKind] = namespaces
		log.Printf("[RBAC-DEBUG]   Resource %s allowed namespaces: %v", resourceKind, namespaces)
	}

	// Debug: Log final converted filters
	log.Printf("[RBAC-DEBUG] Final query filters:")
	log.Printf("[RBAC-DEBUG]   AllowedClusters (%d): %v", len(filters.AllowedClusters), filters.AllowedClusters)
	log.Printf("[RBAC-DEBUG]   AllowedNamespaces (%d): %v", len(filters.AllowedNamespaces), filters.AllowedNamespaces)
	log.Printf("[RBAC-DEBUG]   AllowedResources (%d): %v", len(filters.AllowedResources), filters.AllowedResources)

	// If user has no specific permissions, deny access (empty filters)
	if len(filters.AllowedClusters) == 0 && len(filters.AllowedNamespaces) == 0 && len(filters.AllowedResources) == 0 {
		log.Printf("[RBAC-DEBUG] No permissions found - returning empty filters (access denied)")
		return &QueryFilters{
			AllowedClusters:   []string{}, // Empty = no access
			AllowedNamespaces: []string{},
			AllowedResources:  []string{},
		}
	}

	return filters
}

// mapResourceToKind maps Kubernetes API resource names to their Kind names using discovery
func (r *RBACResolver) mapResourceToKind(apiGroup, resource string) string {
	// PHASE 6: Dynamic Resource Discovery Implementation
	// This replaces the previous hardcoded mapping with live Kubernetes Discovery API

	if r.resourceDiscovery == nil {
		log.Printf("[DISCOVERY-ERROR] Resource discovery not initialized, falling back to algorithmic mapping for %s", resource)
		// Emergency fallback - should not happen in normal operation
		return r.algorithmicFallback(resource)
	}

	// Use discovery to get the correct Kind
	ctx := context.Background() // Use background context for discovery calls
	kind, discoveryResult := r.resourceDiscovery.GetResourceKind(ctx, apiGroup, resource)

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

// createFallbackFilters creates wildcard filters for graceful fallback
func (r *RBACResolver) createFallbackFilters() *QueryFilters {
	return &QueryFilters{
		AllowedClusters:   []string{"*"},
		AllowedNamespaces: []string{"*"},
		AllowedResources:  []string{"*"},
	}
}

// Helper method to integrate with AuthMiddleware
func (m *AuthMiddleware) resolveUserPermissions(ctx context.Context, userToken string) (*QueryFilters, error) {
	resolver := NewRBACResolver(m.config)
	return resolver.ResolveUserPermissions(ctx, userToken)
}