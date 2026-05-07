package auth

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/stolostron/search-mcp-server/pkg/database"
)

// ResourceDiscovery manages dynamic resource-to-kind mappings using Kubernetes Discovery API
type ResourceDiscovery struct {
	config *AuthConfig
	db     *database.DatabaseConnection // Database connection for fleet-wide discovery
}

// ResourceCache holds discovered resource mappings with TTL
type ResourceCache struct {
	resourceToKind map[string]string // resource name → Kind name mapping
	apiGroupMap    map[string]string // resource → preferred API group
	lastUpdated    time.Time
	ttl            time.Duration
	mutex          sync.RWMutex
}

// Global shared cache instance - resource mappings are fleet-wide metadata (security approved)
var globalResourceCache = &ResourceCache{
	resourceToKind: make(map[string]string),
	apiGroupMap:    make(map[string]string),
	ttl:            5 * time.Minute, // Initial TTL - updated from config in GetSharedResourceDiscovery
}

// DiscoveryResult contains the result of a discovery operation
type DiscoveryResult struct {
	ResourceToKind map[string]string
	Source         string // "discovery", "cache", "hardcoded", "algorithmic"
	Error          error
}

// Shared ResourceDiscovery instance - cache is shared across all requests
var sharedResourceDiscovery *ResourceDiscovery

// GetSharedResourceDiscovery returns the shared ResourceDiscovery instance
func GetSharedResourceDiscovery(config *AuthConfig, db *database.DatabaseConnection) *ResourceDiscovery {
	if sharedResourceDiscovery == nil {
		sharedResourceDiscovery = &ResourceDiscovery{
			config: config,
			db:     db, // May be nil for backward compatibility
		}
		// Configure cache TTL from centralized config
		globalResourceCache.ttl = config.DiscoveryTTL
		log.Printf("[DISCOVERY-DEBUG] Configured discovery cache TTL: %v", config.DiscoveryTTL)
		if db == nil {
			log.Printf("[DISCOVERY-DEBUG] No database connection available, will use Kubernetes discovery fallback")
		}
	}
	return sharedResourceDiscovery
}

// NewResourceDiscovery creates a new resource discovery manager (DEPRECATED - use GetSharedResourceDiscovery)
func NewResourceDiscovery(config *AuthConfig, userToken string) *ResourceDiscovery {
	// Return the shared instance - userToken will be passed to discovery methods
	// NOTE: This deprecated function doesn't have database access, so it will fall back to Kubernetes discovery
	return GetSharedResourceDiscovery(config, nil)
}

// GetResourceKind maps a resource name to its Kubernetes Kind using discovery
func (rd *ResourceDiscovery) GetResourceKind(ctx context.Context, userToken, apiGroup, resource string) (string, *DiscoveryResult) {
	log.Printf("[DISCOVERY-DEBUG] Getting kind for resource: %s (group: %s)", resource, apiGroup)

	// FIXED CACHING LOGIC: Check if entire cache is fresh, not just individual resource
	if rd.isCacheFresh() {
		// Cache is fresh - check if resource exists in cache
		if kind, found := rd.getCachedMappingFromFreshCache(resource); found {
			log.Printf("[DISCOVERY-DEBUG] ✅ Cache hit: %s → %s", resource, kind)
			return kind, &DiscoveryResult{Source: "cache"}
		} else {
			// Cache is fresh but resource not found - trust cache and skip expensive discovery
			log.Printf("[DISCOVERY-DEBUG] 📋 Fresh cache miss: %s not in %d cached resources, skipping discovery",
				resource, rd.getCacheSize())
			// Go directly to fallback - no discovery needed
		}
	} else {
		// Cache is stale - refresh it with discovery
		log.Printf("[DISCOVERY-DEBUG] 🔄 Cache stale, refreshing via discovery...")
		if discovered, err := rd.discoverResources(ctx, userToken); err == nil {
			if kind, found := discovered[resource]; found {
				log.Printf("[DISCOVERY-DEBUG] ✅ Discovery success: %s → %s", resource, kind)
				return kind, &DiscoveryResult{
					ResourceToKind: discovered,
					Source:         "discovery",
				}
			}
			// Resource not found even after discovery - continue to fallbacks
		} else {
			log.Printf("[DISCOVERY-DEBUG] ❌ Discovery failed: %v", err)
			// Discovery failed - continue to fallback options
		}
	}

	// Resource not found in discovery or cache - return empty
	log.Printf("[DISCOVERY-DEBUG] ❌ Resource not found: %s", resource)
	return "", &DiscoveryResult{
		Source: "not_found",
	}
}

// getCachedMapping checks if we have a cached mapping for the resource (DEPRECATED - use isCacheFresh + getCachedMappingFromFreshCache)
func (rd *ResourceDiscovery) getCachedMapping(resource string) (string, bool) {
	globalResourceCache.mutex.RLock()
	defer globalResourceCache.mutex.RUnlock()

	// Check if cache is still valid
	if time.Since(globalResourceCache.lastUpdated) > globalResourceCache.ttl {
		return "", false
	}

	kind, found := globalResourceCache.resourceToKind[resource]
	return kind, found
}

// isCacheFresh checks if the entire resource cache is within TTL (PERFORMANCE FIX)
func (rd *ResourceDiscovery) isCacheFresh() bool {
	globalResourceCache.mutex.RLock()
	defer globalResourceCache.mutex.RUnlock()

	return time.Since(globalResourceCache.lastUpdated) <= globalResourceCache.ttl
}

// getCachedMappingFromFreshCache gets resource mapping from cache (assumes cache is fresh)
func (rd *ResourceDiscovery) getCachedMappingFromFreshCache(resource string) (string, bool) {
	globalResourceCache.mutex.RLock()
	defer globalResourceCache.mutex.RUnlock()

	kind, found := globalResourceCache.resourceToKind[resource]
	return kind, found
}

// getCacheSize returns the number of cached resource mappings
func (rd *ResourceDiscovery) getCacheSize() int {
	globalResourceCache.mutex.RLock()
	defer globalResourceCache.mutex.RUnlock()

	return len(globalResourceCache.resourceToKind)
}

// discoverResources performs resource discovery based on configuration
func (rd *ResourceDiscovery) discoverResources(ctx context.Context, userToken string) (map[string]string, error) {
	switch rd.config.DiscoverySource {
	case "database":
		log.Printf("[DISCOVERY-DEBUG] Using database-driven discovery (fleet-wide coverage)")
		return rd.discoverResourcesFromDatabase(ctx)
	case "kubernetes":
		log.Printf("[DISCOVERY-DEBUG] Using Kubernetes API discovery (hub-only coverage)")
		return rd.discoverResourcesFromKubernetes(ctx, userToken)
	default:
		log.Printf("[DISCOVERY-DEBUG] Unknown discovery source '%s', defaulting to database", rd.config.DiscoverySource)
		return rd.discoverResourcesFromDatabase(ctx)
	}
}

// discoverResourcesFromDatabase queries the ACM search database for fleet-wide resource types
func (rd *ResourceDiscovery) discoverResourcesFromDatabase(ctx context.Context) (map[string]string, error) {
	log.Printf("[DISCOVERY-DEBUG] Starting fleet-wide resource discovery from database...")

	if rd.db == nil {
		return nil, fmt.Errorf("database connection not available for discovery")
	}

	// Query for all unique Kind and resource values across the entire fleet
	query := `
		SELECT DISTINCT
			data->>'kind' as kind,
			COALESCE(data->>'kind_plural', LOWER(data->>'kind') || 's') as resource
		FROM search.resources
		WHERE data->>'kind' IS NOT NULL
		  AND data->>'kind' != ''
		  AND data->>'kind' != 'null'
		ORDER BY kind
	`

	result, err := rd.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	log.Printf("[DISCOVERY-DEBUG] Database query returned %d rows", len(result.Rows))

	// Convert query results to resource-to-kind mappings
	discovered := make(map[string]string)
	kindCount := 0

	for _, row := range result.Rows {
		if len(row) >= 2 {
			if kind, ok := row[0].(string); ok && kind != "" {
				if resourceName, ok := row[1].(string); ok && resourceName != "" {
					discovered[resourceName] = kind
					kindCount++
					log.Printf("[DISCOVERY-DEBUG] Fleet discovery: %s → %s", resourceName, kind)
				}
			}
		}
	}

	log.Printf("[DISCOVERY-DEBUG] Fleet discovery complete: %d unique resource types found across all clusters", kindCount)

	// Update cache with fleet-wide discovery results
	rd.updateCache(discovered)

	return discovered, nil
}

// discoverResourcesFromKubernetes performs live discovery using Kubernetes Discovery API
func (rd *ResourceDiscovery) discoverResourcesFromKubernetes(ctx context.Context, userToken string) (map[string]string, error) {
	log.Printf("[DISCOVERY-DEBUG] Starting live resource discovery...")

	// Create discovery client with user's token
	userConfig, err := rd.createDiscoveryConfig(userToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery config: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(userConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Get all server-supported API resources
	log.Printf("[DISCOVERY-DEBUG] Calling ServerPreferredResources()...")
	apiResourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		// ServerPreferredResources can return partial results with errors
		// This is common and acceptable - we work with what we get
		log.Printf("[DISCOVERY-DEBUG] Discovery returned partial results: %v", err)
	}

	// Build resource-to-kind mapping from discovery results
	discovered := make(map[string]string)
	resourceCount := 0

	for _, apiResourceList := range apiResourceLists {
		if apiResourceList == nil {
			continue
		}

		groupVersion, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			log.Printf("[DISCOVERY-DEBUG] Failed to parse group version %s: %v", apiResourceList.GroupVersion, err)
			continue
		}

		log.Printf("[DISCOVERY-DEBUG] Processing API group: %s", groupVersion.String())

		for _, apiResource := range apiResourceList.APIResources {
			// Map resource name to Kind
			resourceName := apiResource.Name
			kind := apiResource.Kind

			if resourceName != "" && kind != "" {
				discovered[resourceName] = kind
				resourceCount++
				log.Printf("[DISCOVERY-DEBUG]   Mapped: %s → %s (group: %s)",
					resourceName, kind, groupVersion.Group)
			}
		}
	}

	log.Printf("[DISCOVERY-DEBUG] Discovery complete: %d resource mappings found", resourceCount)

	// Update cache with fresh discovery results
	rd.updateCache(discovered)

	return discovered, nil
}

// createDiscoveryConfig creates a Kubernetes config for discovery calls
func (rd *ResourceDiscovery) createDiscoveryConfig(userToken string) (*rest.Config, error) {
	return CreateDiscoveryConfig(rd.config.KubernetesURL, userToken, 10*time.Second, rd.config.SkipTLS), nil
}

// updateCache updates the shared in-memory cache with fresh discovery results
func (rd *ResourceDiscovery) updateCache(discovered map[string]string) {
	globalResourceCache.mutex.Lock()
	defer globalResourceCache.mutex.Unlock()

	globalResourceCache.resourceToKind = discovered
	globalResourceCache.lastUpdated = time.Now()

	log.Printf("[DISCOVERY-DEBUG] Cache updated with %d resource mappings (TTL: %v)",
		len(discovered), globalResourceCache.ttl)
}

// Removed getHardcodedMapping and algorithmicMapping functions
// to simplify discovery logic per reviewer feedback

// Removed kindToResourceName function - now using database kind_plural field directly

// GetCacheStats returns information about the shared discovery cache for debugging
func (rd *ResourceDiscovery) GetCacheStats() map[string]interface{} {
	globalResourceCache.mutex.RLock()
	defer globalResourceCache.mutex.RUnlock()

	return map[string]interface{}{
		"cache_size":     len(globalResourceCache.resourceToKind),
		"last_updated":   globalResourceCache.lastUpdated,
		"age_minutes":    time.Since(globalResourceCache.lastUpdated).Minutes(),
		"ttl_hours":      globalResourceCache.ttl.Hours(),
		"is_expired":     time.Since(globalResourceCache.lastUpdated) > globalResourceCache.ttl,
	}
}