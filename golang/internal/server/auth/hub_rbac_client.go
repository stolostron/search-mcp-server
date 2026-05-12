package auth

import (
	"context"
	"fmt"
	"log"
	"slices"
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
	NamespacedKinds    map[string][]Resource   `json:"namespaced_resources"`
	HubClusterName         string                  `json:"hub_cluster_name"` // Dynamically detected hub cluster name
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
		permissions, err := h.createClusterAdminPermissions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster admin permissions: %w", err)
		}

		// Cache cluster admin permissions (if we have userUID)
		if userUID != "" {
			h.cache.setCachedPermissions(userUID, permissions)
		}

		return permissions, nil
	}

	log.Printf("[HUB-RBAC-DEBUG] User is not cluster admin - performing detailed discovery")

	// Detect hub cluster name dynamically
	hubClusterName, err := h.getHubClusterName(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect hub cluster name: %w", err)
	}

	hubPerms := &HubPermissions{
		ClusterScopedResources: []Resource{},
		NamespacedKinds:    make(map[string][]Resource),
		HubClusterName:         hubClusterName,
	}

	// 1. Get cluster-scoped permissions (like search-v2-api)
	clusterResources, err := h.getClusterScopedResources(ctx, userClient)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get cluster-scoped permissions: %v", err)
	} else {
		hubPerms.ClusterScopedResources = clusterResources
	}

	// 2. Get namespaced permissions (like search-v2-api)
	namespacedResources, err := h.getNamespacedKinds(ctx, userClient)
	if err != nil {
		log.Printf("[HUB-RBAC-DEBUG] Failed to get namespaced permissions: %v", err)
	} else {
		hubPerms.NamespacedKinds = namespacedResources
	}

	log.Printf("[HUB-RBAC-DEBUG] Hub permissions summary: %d cluster-scoped, %d namespaced",
		len(hubPerms.ClusterScopedResources), len(hubPerms.NamespacedKinds))

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
func (h *HubRBACClient) createClusterAdminPermissions(ctx context.Context) (*HubPermissions, error) {
	log.Printf("[HUB-RBAC-DEBUG] Creating wildcard permissions for cluster admin")

	// Even for cluster admin, we need to detect the hub cluster name dynamically
	hubClusterName, err := h.getHubClusterName(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect hub cluster name for cluster admin: %w", err)
	}

	return &HubPermissions{
		ClusterScopedResources: []Resource{
			{
				Group:    "*",
				Resource: "*",
				Kind:     "*",
				Verbs:    []string{"*"},
			},
		},
		NamespacedKinds: map[string][]Resource{
			"*": {
				{
					Group:    "*",
					Resource: "*",
					Kind:     "*",
					Verbs:    []string{"*"},
				},
			},
		},
		HubClusterName: hubClusterName,
	}, nil
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

	// PHASE 3: Parallelize SSAR API calls with concurrency limiting
	var resources []Resource
	var mutex sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrent API calls to prevent API server overload
	const maxConcurrent = 10
	semaphore := make(chan struct{}, maxConcurrent)

	for _, resourceType := range clusterScopedResourceTypes {
		wg.Add(1)
		go func(rt struct {
			APIGroup string
			Resource string
			Kind     string
		}) {
			defer wg.Done()

			// Acquire semaphore before making API call
			select {
			case semaphore <- struct{}{}:
				// Got semaphore, proceed with API call
			case <-ctx.Done():
				// Context canceled, abort
				log.Printf("[HUB-RBAC-DEBUG] Context canceled, aborting SSAR for %s", rt.Resource)
				return
			}
			defer func() { <-semaphore }() // Release semaphore when done

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
				// Check if context is canceled before retrying
				if ctx.Err() != nil {
					log.Printf("[HUB-RBAC-DEBUG] Context canceled, aborting retry for %s", rt.Resource)
					return
				}

				// Retry failed SSAR calls that cause missing resource permissions
				log.Printf("[HUB-RBAC-WARNING] SSAR failed for %s, retrying once: %v", rt.Resource, err)

				// Use context for sleep too
				select {
				case <-time.After(100 * time.Millisecond):
					// Continue with retry
				case <-ctx.Done():
					log.Printf("[HUB-RBAC-DEBUG] Context canceled during retry wait for %s", rt.Resource)
					return
				}

				// Single retry attempt
				result, err = client.AuthorizationV1().SelfSubjectAccessReviews().Create(
					ctx, accessCheck, metav1.CreateOptions{})

				if err != nil {
					log.Printf("[HUB-RBAC-ERROR] CRITICAL: SSAR failed for %s after retry - this will cause missing %s permissions: %v", rt.Resource, rt.Kind, err)
					return
				}
				log.Printf("[HUB-RBAC-DEBUG] SSAR retry succeeded for %s", rt.Resource)
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

// getNamespacedKinds uses parallel SelfSubjectRulesReview like search-v2-api
func (h *HubRBACClient) getNamespacedKinds(ctx context.Context, client kubernetes.Interface) (map[string][]Resource, error) {
	// Get all namespaces directly and let resource-specific filtering determine access
	namespaces, err := h.getNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespaces from database: %w", err)
	}

	log.Printf("[HUB-RBAC-DEBUG] Starting parallel discovery of %d namespaces from database", len(namespaces))

	// PHASE 3: Parallelize SSRR calls per namespace with concurrency limiting
	resourceMap := make(map[string][]Resource)
	var mutex sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrent API calls to prevent API server overload
	const maxConcurrent = 10
	semaphore := make(chan struct{}, maxConcurrent)

	for _, namespace := range namespaces {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()

			// Acquire semaphore before making API call
			select {
			case semaphore <- struct{}{}:
				// Got semaphore, proceed with API call
			case <-ctx.Done():
				// Context canceled, abort
				log.Printf("[HUB-RBAC-DEBUG] Context canceled, aborting SSRR for namespace %s", ns)
				return
			}
			defer func() { <-semaphore }() // Release semaphore when done

			// Create SelfSubjectRulesReview for this namespace
			rulesCheck := &authorizationv1.SelfSubjectRulesReview{
				Spec: authorizationv1.SelfSubjectRulesReviewSpec{
					Namespace: ns,
				},
			}

			result, err := client.AuthorizationV1().SelfSubjectRulesReviews().Create(
				ctx, rulesCheck, metav1.CreateOptions{})
			if err != nil {
				// Check if context is canceled before retrying
				if ctx.Err() != nil {
					log.Printf("[HUB-RBAC-DEBUG] Context canceled, aborting retry for namespace %s", ns)
					return
				}

				// Retry failed SSRR calls that cause missing namespace permissions
				log.Printf("[HUB-RBAC-WARNING] SSRR failed for namespace %s, retrying once: %v", ns, err)

				// Use context for sleep too
				select {
				case <-time.After(100 * time.Millisecond):
					// Continue with retry
				case <-ctx.Done():
					log.Printf("[HUB-RBAC-DEBUG] Context canceled during retry wait for namespace %s", ns)
					return
				}

				// Single retry attempt
				result, err = client.AuthorizationV1().SelfSubjectRulesReviews().Create(
					ctx, rulesCheck, metav1.CreateOptions{})

				if err != nil {
					log.Printf("[HUB-RBAC-ERROR] CRITICAL: SSRR failed for namespace %s after retry - this will cause missing namespace permissions: %v", ns, err)
					return
				}
				log.Printf("[HUB-RBAC-DEBUG] SSRR retry succeeded for namespace %s", ns)
			}

			var namespaceResources []Resource
			for _, rule := range result.Status.ResourceRules {
				// Only include rules that allow "list" verb
				if !slices.Contains(rule.Verbs, "list") && !slices.Contains(rule.Verbs, "*") {
					continue
				}

				// Skip specific-named permissions (only grant general namespace access)
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

				// Reduced logging verbosity - only count, not detailed kinds
				log.Printf("[HUB-RBAC-DEBUG] Hub namespace %s: %d resource types accessible", ns, len(namespaceResources))
			}
		}(namespace)
	}

	// Wait for all parallel SSRR calls to complete
	wg.Wait()

	log.Printf("[HUB-RBAC-DEBUG] Parallel namespace discovery complete: %d namespaces processed", len(resourceMap))
	return resourceMap, nil
}

// getHubClusterName dynamically detects the hub cluster name from the database
// Uses _hubClusterResource marker to identify which cluster is the hub
func (h *HubRBACClient) getHubClusterName(ctx context.Context) (string, error) {
	if h.resourceDiscovery == nil || h.resourceDiscovery.db == nil {
		return "", fmt.Errorf("database connection not available for hub cluster detection")
	}

	query := `
		SELECT cluster
		FROM search.resources
		WHERE data ? '_hubClusterResource'
		  AND data->>'kind' != 'Cluster'
		LIMIT 1`

	result, err := h.resourceDiscovery.db.Query(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to detect hub cluster name: %w", err)
	}

	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 {
		// Fallback to default for backwards compatibility
		log.Printf("[HUB-RBAC-WARNING] Could not detect hub cluster name, using default 'local-cluster'")
		return "local-cluster", nil
	}

	hubClusterName, ok := result.Rows[0][0].(string)
	if !ok {
		log.Printf("[HUB-RBAC-WARNING] Hub cluster name is not a string, using default 'local-cluster'")
		return "local-cluster", nil
	}
	log.Printf("[HUB-RBAC-DEBUG] Detected hub cluster name: %s", hubClusterName)
	return hubClusterName, nil
}

// getNamespaces discovers namespaces that exist in the hub cluster
func (h *HubRBACClient) getNamespaces(ctx context.Context) ([]string, error) {
	if h.resourceDiscovery == nil || h.resourceDiscovery.db == nil {
		return nil, fmt.Errorf("database connection not available for namespace discovery")
	}

	// Dynamically detect hub cluster name instead of hard-coding 'local-cluster'
	hubClusterName, err := h.getHubClusterName(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect hub cluster name: %w", err)
	}

	// Query to find distinct namespaces that exist on the hub cluster
	query := `
		SELECT DISTINCT data->>'namespace' as namespace
		FROM search.resources
		WHERE cluster = $1
		  AND data->>'namespace' IS NOT NULL
		  AND data->>'namespace' != ''
		  AND data ? 'namespace'
		ORDER BY namespace`

	result, err := h.resourceDiscovery.db.Query(ctx, query, hubClusterName)
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
		}
	}

	log.Printf("[HUB-RBAC-DEBUG] Database discovery found %d namespaces with resources", len(namespaces))
	return namespaces, nil
}

// mapResourceToKind maps resource names to Kinds using resource discovery
func (h *HubRBACClient) mapResourceToKind(resource string, apiGroups []string) string {
	// Handle wildcard
	if resource == "*" {
		return "*"
	}

	// Use proper resource discovery instead of algorithmic fallbacks for security
	if h.resourceDiscovery != nil {
		// Determine API group from apiGroups slice
		apiGroup := ""
		if len(apiGroups) > 0 && apiGroups[0] != "" {
			apiGroup = apiGroups[0]
		}

		// Use resource discovery with a background context for internal mapping
		// This uses the same secure database-backed discovery as the rest of the system
		kind, result := h.resourceDiscovery.GetResourceKind(context.Background(), "", apiGroup, resource)
		if result != nil && result.Source != "not_found" {
			return kind
		}
	}

	// If resource discovery fails, return empty string (fail secure)
	log.Printf("[HUB-RBAC-DEBUG] Resource discovery failed for %s, returning empty kind for security", resource)
	return ""
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
		if ttl <= 0 {
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
	// 1. Snapshot the state with a Read Lock
	hc.mutex.RLock()
	cachedData, exists := hc.cache[userUID]

	if !exists {
		hc.mutex.RUnlock()
		log.Printf("[HUB-RBAC-CACHE] No cache entry for user UID: %s", userUID)
		return nil, false
	}

	if cachedData.isValid() {
		permissions := cachedData.Permissions
		hc.mutex.RUnlock()
		log.Printf("[HUB-RBAC-CACHE] Cache hit for user UID: %s", userUID)
		return permissions, true
	}
	hc.mutex.RUnlock() // Release before potentially upgrading

	// 2. State was invalid, acquire Write Lock to clean up
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	// 3. Re-verify the condition under the Write Lock
	cachedData, exists = hc.cache[userUID]
	if exists && !cachedData.isValid() {
		log.Printf("[HUB-RBAC-CACHE] Cache entry expired for user UID: %s", userUID)
		delete(hc.cache, userUID)
	}

	return nil, false
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

// getClusterScopedResourcesFromDatabase - FIXED to properly handle kind_plural fallback
func (h *HubRBACClient) getClusterScopedResourcesFromDatabase(ctx context.Context) ([]struct {
	APIGroup string
	Resource string
	Kind     string
}, error) {
	log.Printf("[HUB-RBAC-DEBUG] Querying database for cluster-scoped resources (search-v2-api approach)")

	if h.resourceDiscovery == nil || h.resourceDiscovery.db == nil {
		return nil, fmt.Errorf("database connection not available for hub resource discovery")
	}

	query := `
		SELECT DISTINCT
			COALESCE(data->>'apigroup', '') as apigroup,
			data->>'kind_plural' as resource,
			data->>'kind' as kind
		FROM search.resources
		WHERE data ? '_hubClusterResource'
		  AND (data ? 'namespace') IS FALSE
		  AND data->>'kind' IS NOT NULL
		  AND data->>'kind' != ''
		  AND data->>'kind_plural' IS NOT NULL
		  AND data->>'kind_plural' != ''
		  AND data->>'kind_plural' != 'null'
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
			}
		}
	}

	log.Printf("[HUB-RBAC-DEBUG] Database discovery complete: %d cluster-scoped resources found", len(resourceTypes))
	return resourceTypes, nil
}