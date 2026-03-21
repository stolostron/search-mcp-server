package auth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// ResourceDiscovery manages dynamic resource-to-kind mappings using Kubernetes Discovery API
type ResourceDiscovery struct {
	cache     *ResourceCache
	userToken string // User's bearer token for discovery calls
	config    *AuthConfig
}

// ResourceCache holds discovered resource mappings with TTL
type ResourceCache struct {
	resourceToKind map[string]string // resource name → Kind name mapping
	apiGroupMap    map[string]string // resource → preferred API group
	lastUpdated    time.Time
	ttl            time.Duration
	mutex          sync.RWMutex
}

// DiscoveryResult contains the result of a discovery operation
type DiscoveryResult struct {
	ResourceToKind map[string]string
	Source         string // "discovery", "cache", "hardcoded", "algorithmic"
	Error          error
}

// NewResourceDiscovery creates a new resource discovery manager
func NewResourceDiscovery(config *AuthConfig, userToken string) *ResourceDiscovery {
	return &ResourceDiscovery{
		cache: &ResourceCache{
			resourceToKind: make(map[string]string),
			apiGroupMap:    make(map[string]string),
			ttl:            1 * time.Hour, // Cache for 1 hour
		},
		userToken: userToken,
		config:    config,
	}
}

// GetResourceKind maps a resource name to its Kubernetes Kind using discovery
func (rd *ResourceDiscovery) GetResourceKind(ctx context.Context, apiGroup, resource string) (string, *DiscoveryResult) {
	log.Printf("[DISCOVERY-DEBUG] Getting kind for resource: %s (group: %s)", resource, apiGroup)

	// Step 1: Check cache first (performance optimization)
	if kind, found := rd.getCachedMapping(resource); found {
		log.Printf("[DISCOVERY-DEBUG] ✅ Cache hit: %s → %s", resource, kind)
		return kind, &DiscoveryResult{
			Source: "cache",
		}
	}

	// Step 2: Attempt live discovery with user's token
	if discovered, err := rd.discoverResources(ctx); err == nil {
		if kind, found := discovered[resource]; found {
			log.Printf("[DISCOVERY-DEBUG] ✅ Discovery success: %s → %s", resource, kind)
			return kind, &DiscoveryResult{
				ResourceToKind: discovered,
				Source:         "discovery",
			}
		}
	} else {
		log.Printf("[DISCOVERY-DEBUG] ❌ Discovery failed: %v", err)
		// Continue to fallback options
	}

	// Step 3: Fallback to hardcoded mapping for known resources
	if kind := rd.getHardcodedMapping(resource); kind != "" {
		log.Printf("[DISCOVERY-DEBUG] ✅ Hardcoded fallback: %s → %s", resource, kind)
		return kind, &DiscoveryResult{
			Source: "hardcoded",
		}
	}

	// Step 4: Last resort - algorithmic mapping
	kind := rd.algorithmicMapping(resource)
	log.Printf("[DISCOVERY-DEBUG] ⚠️  Algorithmic fallback: %s → %s", resource, kind)
	return kind, &DiscoveryResult{
		Source: "algorithmic",
	}
}

// getCachedMapping checks if we have a cached mapping for the resource
func (rd *ResourceDiscovery) getCachedMapping(resource string) (string, bool) {
	rd.cache.mutex.RLock()
	defer rd.cache.mutex.RUnlock()

	// Check if cache is still valid
	if time.Since(rd.cache.lastUpdated) > rd.cache.ttl {
		return "", false
	}

	kind, found := rd.cache.resourceToKind[resource]
	return kind, found
}

// discoverResources performs live discovery using Kubernetes Discovery API
func (rd *ResourceDiscovery) discoverResources(ctx context.Context) (map[string]string, error) {
	log.Printf("[DISCOVERY-DEBUG] Starting live resource discovery...")

	// Create discovery client with user's token
	userConfig, err := rd.createDiscoveryConfig()
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
func (rd *ResourceDiscovery) createDiscoveryConfig() (*rest.Config, error) {
	// Build Kubernetes API server URL
	var kubernetesURL string
	if rd.config.KubernetesURL != "" {
		kubernetesURL = rd.config.KubernetesURL
	} else {
		host := rd.config.KubernetesHost
		port := rd.config.KubernetesPort
		if host == "" || port == "" {
			return nil, fmt.Errorf("Kubernetes host/port not configured for discovery")
		}
		kubernetesURL = fmt.Sprintf("https://%s:%s", host, port)
	}

	// Create rest.Config with user's token (same as permission resolution)
	config := &rest.Config{
		Host:        kubernetesURL,
		BearerToken: strings.TrimPrefix(rd.userToken, "Bearer "),
		Timeout:     10 * time.Second, // Discovery timeout
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: rd.config.SkipTLS,
		},
	}

	return config, nil
}

// updateCache updates the in-memory cache with fresh discovery results
func (rd *ResourceDiscovery) updateCache(discovered map[string]string) {
	rd.cache.mutex.Lock()
	defer rd.cache.mutex.Unlock()

	rd.cache.resourceToKind = discovered
	rd.cache.lastUpdated = time.Now()

	log.Printf("[DISCOVERY-DEBUG] Cache updated with %d resource mappings (TTL: %v)",
		len(discovered), rd.cache.ttl)
}

// getHardcodedMapping provides fallback mappings for known resources
func (rd *ResourceDiscovery) getHardcodedMapping(resource string) string {
	// Preserve the existing hardcoded mappings as fallback
	// This ensures backward compatibility and handles discovery failures
	hardcodedMap := map[string]string{
		// Core Kubernetes resources
		"pods":                     "Pod",
		"services":                 "Service",
		"configmaps":               "ConfigMap",
		"secrets":                  "Secret",
		"events":                   "Event",
		"deployments":              "Deployment",
		"replicasets":              "ReplicaSet",
		"daemonsets":               "DaemonSet",
		"statefulsets":             "StatefulSet",
		"ingresses":                "Ingress",
		"routes":                   "Route",
		"persistentvolumes":        "PersistentVolume",
		"persistentvolumeclaims":   "PersistentVolumeClaim",
		"nodes":                    "Node",
		"namespaces":               "Namespace",
		"serviceaccounts":          "ServiceAccount",
		"roles":                    "Role",
		"rolebindings":             "RoleBinding",
		"clusterroles":             "ClusterRole",
		"clusterrolebindings":      "ClusterRoleBinding",

		// KubeVirt resources (comprehensive mapping)
		"virtualmachines":                    "VirtualMachine",
		"virtualmachineinstances":           "VirtualMachineInstance",
		"virtualmachineinstancepresets":     "VirtualMachineInstancePreset",
		"virtualmachineinstancereplicasets": "VirtualMachineInstanceReplicaSet",
		"virtualmachineinstancemigrations":  "VirtualMachineInstanceMigration",
		"kubevirts":                         "KubeVirt",
		"virtualmachinesnapshots":           "VirtualMachineSnapshot",
		"virtualmachinesnapshotcontents":    "VirtualMachineSnapshotContent",
		"virtualmachinerestores":            "VirtualMachineRestore",
		"virtualmachinepools":               "VirtualMachinePool",
		"virtualmachineclones":              "VirtualMachineClone",
		"virtualmachineexports":             "VirtualMachineExport",
		"virtualmachineinstancetypes":       "VirtualMachineInstancetype",
		"virtualmachineclusterinstancetypes": "VirtualMachineClusterInstancetype",
		"virtualmachinepreferences":         "VirtualMachinePreference",
		"virtualmachineclusterpreferences":  "VirtualMachineClusterPreference",
		"migrationpolicies":                 "MigrationPolicy",

		// Common operator resources (extend as needed)
		"applications":     "Application",     // ArgoCD
		"virtualservices":  "VirtualService",  // Istio
		"pipelines":        "Pipeline",        // Tekton
		"pipelineruns":     "PipelineRun",     // Tekton
	}

	if kind, exists := hardcodedMap[resource]; exists {
		return kind
	}

	return "" // Not found in hardcoded map
}

// algorithmicMapping provides last-resort mapping by capitalizing resource names
func (rd *ResourceDiscovery) algorithmicMapping(resource string) string {
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

	// Capitalize first letter
	if len(singular) == 0 {
		return ""
	}

	return strings.ToUpper(singular[:1]) + singular[1:]
}

// GetCacheStats returns information about the discovery cache for debugging
func (rd *ResourceDiscovery) GetCacheStats() map[string]interface{} {
	rd.cache.mutex.RLock()
	defer rd.cache.mutex.RUnlock()

	return map[string]interface{}{
		"cache_size":     len(rd.cache.resourceToKind),
		"last_updated":   rd.cache.lastUpdated,
		"age_minutes":    time.Since(rd.cache.lastUpdated).Minutes(),
		"ttl_hours":      rd.cache.ttl.Hours(),
		"is_expired":     time.Since(rd.cache.lastUpdated) > rd.cache.ttl,
	}
}