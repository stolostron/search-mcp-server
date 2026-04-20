package auth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Resource represents a Kubernetes resource with permissions
type Resource struct {
	Group    string   `json:"group"`
	Resource string   `json:"resource"`
	Kind     string   `json:"kind"`
	Verbs    []string `json:"verbs"`
}

// HubPermissions represents permissions discovered from Hub Kubernetes API
type HubPermissions struct {
	ClusterScopedResources []Resource              `json:"cluster_scoped_resources"`
	NamespacedResources    map[string][]Resource   `json:"namespaced_resources"`
}

// HubPermissionsCache represents cached hub permissions with metadata
type HubPermissionsCache struct {
	Permissions *HubPermissions
	CachedAt    time.Time
	TTL         time.Duration
	UserUID     string
}

// HubRBACCache manages cached hub RBAC permissions (like search-v2-api)
type HubRBACCache struct {
	cache    map[string]*HubPermissionsCache // key: userUID
	mutex    sync.RWMutex
	defaultTTL time.Duration
}

// Global cache instance
var (
	hubRBACCache *HubRBACCache
	cacheOnce    sync.Once
)

// HubRBACClient handles Hub Kubernetes API RBAC calls
type HubRBACClient struct {
	kubernetesClient kubernetes.Interface
	config           *AuthConfig
	cache           *HubRBACCache
	resourceDiscovery *ResourceDiscovery // NEW: Access to shared resource discovery
}

// NewHubRBACClient creates a client for Hub Kubernetes API RBAC calls
func NewHubRBACClient(config *AuthConfig, resourceDiscovery *ResourceDiscovery) (*HubRBACClient, error) {
	restConfig := CreateHubRBACConfig(config.SkipTLS)

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create hub Kubernetes client: %w", err)
	}

	return &HubRBACClient{
		kubernetesClient: client,
		config:           config,
		cache:           getHubRBACCache(config),
		resourceDiscovery: resourceDiscovery,
	}, nil
}

// GetHubClusterPermissions mirrors search-v2-api's dual API approach with caching
func (h *HubRBACClient) GetHubClusterPermissions(ctx context.Context, userToken string) (*HubPermissions, error) {
	log.Printf("[HUB-RBAC-DEBUG] Starting hub cluster permission discovery")

	// Get user UID for caching (we need to extract this from the token)
	userUID, err := h.getUserUIDFromToken(ctx, userToken)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get user UID for caching: %v", err)
		// Continue without caching - fallback to direct discovery
	}

	// PHASE 2: Check cache first (if we have userUID)
	if userUID != "" {
		if cachedPerms, found := h.cache.getCachedPermissions(userUID); found {
			log.Printf("[HUB-RBAC-DEBUG] Using cached hub permissions for user UID: %s", userUID)
			return cachedPerms, nil
		}
		log.Printf("[HUB-RBAC-DEBUG] No valid cache for user UID: %s, performing discovery", userUID)
	}

	// Create impersonated client with user token
	userConfig := h.createUserConfig(userToken)
	userClient, err := kubernetes.NewForConfig(userConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create user-impersonated client: %w", err)
	}

	// PHASE 1: Check if user is cluster admin first (like search-v2-api)
	// Equivalent to: oc auth can-i '*' '*' -A --as=<user>
	if h.isClusterAdmin(ctx, userClient) {
		log.Printf("[HUB-RBAC-DEBUG] User is cluster admin - returning wildcard permissions immediately")
		permissions := h.createClusterAdminPermissions()

		// Cache cluster admin permissions (if we have userUID)
		if userUID != "" {
			h.cache.setCachedPermissions(userUID, permissions)
		}

		return permissions, nil
	}

	log.Printf("[HUB-RBAC-DEBUG] User is not cluster admin - performing detailed discovery")

	hubPerms := &HubPermissions{
		ClusterScopedResources: []Resource{},
		NamespacedResources:    make(map[string][]Resource),
	}

	// 1. Get cluster-scoped permissions (like search-v2-api)
	clusterResources, err := h.getClusterScopedResources(ctx, userClient)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get cluster-scoped permissions: %v", err)
	} else {
		hubPerms.ClusterScopedResources = clusterResources
	}

	// 2. Get namespaced permissions (like search-v2-api)
	namespacedResources, err := h.getNamespacedResources(ctx, userClient)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get namespaced permissions: %v", err)
	} else {
		hubPerms.NamespacedResources = namespacedResources
	}

	log.Printf("[HUB-RBAC-DEBUG] Hub permissions summary: %d cluster-scoped, %d namespaced",
		len(hubPerms.ClusterScopedResources), len(hubPerms.NamespacedResources))

	// PHASE 2: Cache the discovered permissions (if we have userUID)
	if userUID != "" {
		h.cache.setCachedPermissions(userUID, hubPerms)
	}

	return hubPerms, nil
}

// createUserConfig creates REST config with user's token
func (h *HubRBACClient) createUserConfig(userToken string) *rest.Config {
	return CreateDiscoveryConfig(h.config.KubernetesURL, userToken, h.config.AuthTimeout, h.config.SkipTLS)
}

// isClusterAdmin checks if user has cluster admin permissions (like search-v2-api)
// Equivalent to: oc auth can-i '*' '*' -A --as=<user>
func (h *HubRBACClient) isClusterAdmin(ctx context.Context, client kubernetes.Interface) bool {
	log.Printf("[HUB-RBAC-DEBUG] Checking if user has cluster admin permissions")

	accessCheck := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     "*",
				Group:    "*",
				Resource: "*",
			},
		},
	}

	result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(
		ctx, accessCheck, metav1.CreateOptions{})
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to check cluster admin permissions: %v", err)
		return false
	}

	isAdmin := result.Status.Allowed
	log.Printf("[HUB-RBAC-DEBUG] Cluster admin check result: %v", isAdmin)
	return isAdmin
}

// createClusterAdminPermissions returns wildcard permissions for cluster admins
func (h *HubRBACClient) createClusterAdminPermissions() *HubPermissions {
	log.Printf("[HUB-RBAC-DEBUG] Creating wildcard permissions for cluster admin")

	return &HubPermissions{
		ClusterScopedResources: []Resource{
			{
				Group:    "*",
				Resource: "*",
				Kind:     "*",
				Verbs:    []string{"*"},
			},
		},
		NamespacedResources: map[string][]Resource{
			"*": {
				{
					Group:    "*",
					Resource: "*",
					Kind:     "*",
					Verbs:    []string{"*"},
				},
			},
		},
	}
}

// getClusterScopedResources uses parallel SelfSubjectAccessReview like search-v2-api
func (h *HubRBACClient) getClusterScopedResources(ctx context.Context, client kubernetes.Interface) ([]Resource, error) {
	// DATABASE DISCOVERY: Direct copy of search-v2-api's proven approach (no hardcoded fallbacks)
	log.Printf("[HUB-RBAC-DEBUG] Using database-driven resource discovery for comprehensive coverage")

	clusterScopedResourceTypes, err := h.getClusterScopedResourcesFromDatabase(ctx)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Database discovery failed: %v", err)
		return []Resource{}, err
	}

	log.Printf("[HUB-RBAC-DEBUG] Starting parallel check of %d cluster-scoped resource types", len(clusterScopedResourceTypes))

	// PHASE 3: Parallelize SSAR API calls (like search-v2-api lines 272-286)
	var resources []Resource
	var mutex sync.Mutex
	var wg sync.WaitGroup

	for _, resourceType := range clusterScopedResourceTypes {
		wg.Add(1)
		go func(rt struct {
			APIGroup string
			Resource string
			Kind     string
		}) {
			defer wg.Done()

			// Create SelfSubjectAccessReview for this resource type
			accessCheck := &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Verb:     "list",
						Group:    rt.APIGroup,
						Resource: rt.Resource,
					},
				},
			}

			result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(
				ctx, accessCheck, metav1.CreateOptions{})
			if err != nil {
				log.Printf("[HUB-RBAC-DEBUG] Failed parallel SSAR for %s: %v", rt.Resource, err)
				return
			}

			if result.Status.Allowed {
				log.Printf("[HUB-RBAC-DEBUG] Hub cluster access granted: %s → %s", rt.Resource, rt.Kind)

				// Thread-safe append to results
				mutex.Lock()
				resources = append(resources, Resource{
					Group:    rt.APIGroup,
					Resource: rt.Resource,
					Kind:     rt.Kind,
					Verbs:    []string{"list"},
				})
				mutex.Unlock()
			} else {
				log.Printf("[HUB-RBAC-DEBUG] Hub cluster access denied: %s", rt.Resource)
			}
		}(resourceType)
	}

	// Wait for all parallel SSAR calls to complete
	wg.Wait()

	log.Printf("[HUB-RBAC-DEBUG] Parallel cluster-scoped discovery complete: %d resources accessible", len(resources))
	return resources, nil
}

// getNamespacedResources uses parallel SelfSubjectRulesReview like search-v2-api
func (h *HubRBACClient) getNamespacedResources(ctx context.Context, client kubernetes.Interface) (map[string][]Resource, error) {
	// Get list of accessible namespaces first
	namespaces, err := h.getAccessibleNamespaces(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get accessible namespaces: %w", err)
	}

	log.Printf("[HUB-RBAC-DEBUG] Starting parallel discovery of %d accessible namespaces", len(namespaces))

	// PHASE 3: Parallelize SSRR calls per namespace (like search-v2-api lines 449-459)
	resourceMap := make(map[string][]Resource)
	var mutex sync.Mutex
	var wg sync.WaitGroup

	for _, namespace := range namespaces {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()

			// Create SelfSubjectRulesReview for this namespace
			rulesCheck := &authorizationv1.SelfSubjectRulesReview{
				Spec: authorizationv1.SelfSubjectRulesReviewSpec{
					Namespace: ns,
				},
			}

			result, err := client.AuthorizationV1().SelfSubjectRulesReviews().Create(
				ctx, rulesCheck, metav1.CreateOptions{})
			if err != nil {
				log.Printf("[HUB-RBAC-DEBUG] Failed parallel SSRR for namespace %s: %v", ns, err)
				return
			}

			var namespaceResources []Resource
			for _, rule := range result.Status.ResourceRules {
				// Only include rules that allow "list" verb
				if !contains(rule.Verbs, "list") && !contains(rule.Verbs, "*") {
					continue
				}

				// BUG FIX: Skip specific-named permissions (only grant general namespace access)
				if len(rule.ResourceNames) > 0 {
					log.Printf("[HUB-RBAC-DEBUG] Skipping specific-named permission in namespace %s: %v resources=%v names=%v",
						ns, rule.APIGroups, rule.Resources, rule.ResourceNames)
					continue
				}

				for _, resource := range rule.Resources {
					// Map resource name to Kind using discovery
					kind := h.mapResourceToKind(resource, rule.APIGroups)
					if kind != "" {
						namespaceResources = append(namespaceResources, Resource{
							Group:    strings.Join(rule.APIGroups, ","),
							Resource: resource,
							Kind:     kind,
							Verbs:    rule.Verbs,
						})
					}
				}
			}

			if len(namespaceResources) > 0 {
				// Thread-safe update to resource map
				mutex.Lock()
				resourceMap[ns] = namespaceResources
				mutex.Unlock()

				// Extract just the Kinds for detailed logging
				var kinds []string
				for _, res := range namespaceResources {
					kinds = append(kinds, res.Kind)
				}
				log.Printf("[HUB-RBAC-DEBUG] Hub namespace %s: %d resource types accessible", ns, len(namespaceResources))
				log.Printf("[HUB-RBAC-DETAIL] Namespace %s resources: %v", ns, kinds)
			}
		}(namespace)
	}

	// Wait for all parallel SSRR calls to complete
	wg.Wait()

	log.Printf("[HUB-RBAC-DEBUG] Parallel namespace discovery complete: %d namespaces processed", len(resourceMap))
	return resourceMap, nil
}

// getAccessibleNamespaces discovers which namespaces the user can access
// Uses database-driven namespace discovery when user can't list all namespaces
func (h *HubRBACClient) getAccessibleNamespaces(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	log.Printf("[HUB-RBAC-DEBUG] Discovering accessible namespaces using database-driven approach")

	// PHASE 1: Get namespaces that actually contain resources (from database)
	namespacesWithResources, err := h.getNamespacesWithResourcesFromDatabase(ctx)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get namespaces from database: %v", err)
		return []string{}, nil
	}

	log.Printf("[HUB-RBAC-DEBUG] Found %d namespaces with resources in database", len(namespacesWithResources))

	// PHASE 2: Check user access to each namespace in parallel
	var accessibleNamespaces []string
	var mutex sync.Mutex
	var wg sync.WaitGroup

	for _, namespace := range namespacesWithResources {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()

			// Use SelfSubjectRulesReview to check namespace access
			rulesCheck := &authorizationv1.SelfSubjectRulesReview{
				Spec: authorizationv1.SelfSubjectRulesReviewSpec{
					Namespace: ns,
				},
			}

			result, err := client.AuthorizationV1().SelfSubjectRulesReviews().Create(
				ctx, rulesCheck, metav1.CreateOptions{})
			if err != nil {
				log.Printf("[HUB-RBAC-DEBUG] Failed SSRR check for namespace %s: %v", ns, err)
				return
			}

			// Check if user has any "list" permissions in this namespace
			hasListAccess := false
			for _, rule := range result.Status.ResourceRules {
				if contains(rule.Verbs, "list") || contains(rule.Verbs, "*") {
					hasListAccess = true
					break
				}
			}

			if hasListAccess {
				mutex.Lock()
				accessibleNamespaces = append(accessibleNamespaces, ns)
				log.Printf("[HUB-RBAC-DEBUG] Namespace %s: user has list access", ns)
				mutex.Unlock()
			} else {
				log.Printf("[HUB-RBAC-DEBUG] Namespace %s: user has no list access", ns)
			}
		}(namespace)
	}

	wg.Wait()

	log.Printf("[HUB-RBAC-DEBUG] Found %d accessible namespaces out of %d with resources",
		len(accessibleNamespaces), len(namespacesWithResources))

	return accessibleNamespaces, nil
}

// getNamespacesWithResourcesFromDatabase discovers namespaces that contain hub cluster resources
func (h *HubRBACClient) getNamespacesWithResourcesFromDatabase(ctx context.Context) ([]string, error) {
	if h.resourceDiscovery == nil || h.resourceDiscovery.db == nil {
		return nil, fmt.Errorf("database connection not available for namespace discovery")
	}

	// Query to find all namespaces that have resources on the hub cluster (local-cluster)
	query := `
		SELECT DISTINCT data->>'namespace' as namespace
		FROM search.resources
		WHERE cluster = 'local-cluster'
		  AND data->>'namespace' IS NOT NULL
		  AND data->>'namespace' != ''
		  AND data ? 'namespace'
		ORDER BY namespace`

	result, err := h.resourceDiscovery.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query namespaces from database: %w", err)
	}

	log.Printf("[HUB-RBAC-DEBUG] Database namespace query returned %d rows", len(result.Rows))

	var namespaces []string
	for _, row := range result.Rows {
		if len(row) < 1 {
			continue
		}

		if namespace, ok := row[0].(string); ok && namespace != "" {
			namespaces = append(namespaces, namespace)
			log.Printf("[HUB-RBAC-DEBUG] Database found namespace with resources: %s", namespace)
		}
	}

	return namespaces, nil
}

// Old hardcoded namespace functions removed - replaced with dynamic discovery approach from search-v2-api

// mapResourceToKind maps resource names to Kinds using basic heuristics
func (h *HubRBACClient) mapResourceToKind(resource string, apiGroups []string) string {
	// Handle wildcard
	if resource == "*" {
		return "*"
	}

	// Use algorithmic mapping (same as in rbac_resolver.go)
	return h.algorithmicResourceToKind(resource)
}

// algorithmicResourceToKind provides basic resource-to-kind mapping
func (h *HubRBACClient) algorithmicResourceToKind(resource string) string {
	if len(resource) == 0 {
		return ""
	}

	// Handle common plural-to-singular patterns
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

	// Capitalize first letter to get Kind
	if len(singular) == 0 {
		return ""
	}

	kind := strings.ToUpper(singular[:1]) + singular[1:]
	log.Printf("[HUB-RBAC-DEBUG] Mapped resource %s → Kind %s", resource, kind)

	return kind
}

// getUserUIDFromToken extracts user UID from token using TokenReview
func (h *HubRBACClient) getUserUIDFromToken(ctx context.Context, userToken string) (string, error) {
	// Create a client for TokenReview (using service account credentials)
	config := CreateServiceAccountConfig(h.config.SkipTLS)

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create client for token review: %w", err)
	}

	// Create TokenReview request
	tokenReview := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: strings.TrimPrefix(userToken, "Bearer "),
		},
	}

	// Submit TokenReview
	result, err := client.AuthenticationV1().TokenReviews().Create(ctx, tokenReview, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("token review failed: %w", err)
	}

	if !result.Status.Authenticated {
		return "", fmt.Errorf("token authentication failed")
	}

	userUID := result.Status.User.UID
	if userUID == "" {
		// Some users (like kubeadmin) may have empty UID, use username as fallback
		userUID = result.Status.User.Username
		log.Printf("[HUB-RBAC-DEBUG] Empty UID for user, using username as cache key: %s", userUID)
	}

	log.Printf("[HUB-RBAC-DEBUG] Extracted user UID for caching: %s", userUID)
	return userUID, nil
}

// getHubRBACCache returns the singleton cache instance
func getHubRBACCache(config *AuthConfig) *HubRBACCache {
	cacheOnce.Do(func() {
		// Default to 5 minutes like search-v2-api, configurable via DiscoveryTTL
		ttl := config.DiscoveryTTL
		if ttl == 0 {
			ttl = 5 * time.Minute
		}

		hubRBACCache = &HubRBACCache{
			cache:      make(map[string]*HubPermissionsCache),
			defaultTTL: ttl,
		}
		log.Printf("[HUB-RBAC-CACHE] Initialized cache with TTL: %v", ttl)

		// Start periodic cleanup goroutine
		go func() {
			ticker := time.NewTicker(ttl)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					hubRBACCache.cleanupExpiredEntries()
				}
			}
		}()
		log.Printf("[HUB-RBAC-CACHE] Started periodic cleanup goroutine (interval: %v)", ttl)
	})
	return hubRBACCache
}

// isValid checks if cached permissions are still valid
func (hpc *HubPermissionsCache) isValid() bool {
	return time.Since(hpc.CachedAt) < hpc.TTL
}

// getCachedPermissions retrieves cached permissions for a user
func (hc *HubRBACCache) getCachedPermissions(userUID string) (*HubPermissions, bool) {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	cachedData, exists := hc.cache[userUID]
	if !exists {
		log.Printf("[HUB-RBAC-CACHE] No cache entry for user UID: %s", userUID)
		return nil, false
	}

	if !cachedData.isValid() {
		log.Printf("[HUB-RBAC-CACHE] Cache entry expired for user UID: %s", userUID)
		// Clean up expired entry
		delete(hc.cache, userUID)
		return nil, false
	}

	log.Printf("[HUB-RBAC-CACHE] Cache hit for user UID: %s", userUID)
	return cachedData.Permissions, true
}

// setCachedPermissions stores permissions in cache for a user
func (hc *HubRBACCache) setCachedPermissions(userUID string, permissions *HubPermissions) {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	hc.cache[userUID] = &HubPermissionsCache{
		Permissions: permissions,
		CachedAt:    time.Now(),
		TTL:         hc.defaultTTL,
		UserUID:     userUID,
	}

	log.Printf("[HUB-RBAC-CACHE] Cached permissions for user UID: %s, TTL: %v", userUID, hc.defaultTTL)
}

// cleanupExpiredEntries removes expired cache entries (called periodically)
func (hc *HubRBACCache) cleanupExpiredEntries() {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	now := time.Now()
	expired := 0

	for userUID, cachedData := range hc.cache {
		if now.Sub(cachedData.CachedAt) >= cachedData.TTL {
			delete(hc.cache, userUID)
			expired++
		}
	}

	if expired > 0 {
		log.Printf("[HUB-RBAC-CACHE] Cleaned up %d expired cache entries", expired)
	}
}

// contains checks if a slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// getClusterScopedResourcesFromDatabase - DIRECT COPY of search-v2-api's proven approach
func (h *HubRBACClient) getClusterScopedResourcesFromDatabase(ctx context.Context) ([]struct {
	APIGroup string
	Resource string
	Kind     string
}, error) {
	log.Printf("[HUB-RBAC-DEBUG] Querying database for cluster-scoped resources (search-v2-api approach)")

	if h.resourceDiscovery == nil || h.resourceDiscovery.db == nil {
		return nil, fmt.Errorf("database connection not available for hub resource discovery")
	}

	// EXACT copy of search-v2-api SQL query (lines 226-229)
	query := `
		SELECT DISTINCT
			COALESCE(data->>'apigroup', '') as apigroup,
			COALESCE(data->>'kind_plural', data->>'kind') as resource,
			COALESCE(data->>'kind', '') as kind
		FROM search.resources
		WHERE data ? '_hubClusterResource'
		  AND (data ? 'namespace') IS FALSE
		  AND data->>'kind' IS NOT NULL
		  AND data->>'kind' != ''
		ORDER BY kind
	`

	result, err := h.resourceDiscovery.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	var resourceTypes []struct {
		APIGroup string
		Resource string
		Kind     string
	}

	log.Printf("[HUB-RBAC-DEBUG] Database query returned %d rows", len(result.Rows))

	for _, row := range result.Rows {
		if len(row) >= 3 {
			apigroup, _ := row[0].(string)
			resource, _ := row[1].(string)
			kind, _ := row[2].(string)

			if kind != "" && resource != "" {
				resourceTypes = append(resourceTypes, struct {
					APIGroup string
					Resource string
					Kind     string
				}{
					APIGroup: apigroup,
					Resource: resource,
					Kind:     kind,
				})

				log.Printf("[HUB-RBAC-DEBUG] Database found cluster-scoped: %s/%s → %s", apigroup, resource, kind)
			}
		}
	}

	log.Printf("[HUB-RBAC-DEBUG] Database discovery complete: %d cluster-scoped resources found", len(resourceTypes))
	return resourceTypes, nil
}