package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/stolostron/search-mcp-server/pkg/database"
)

// AuthMiddleware handles HTTP authentication for MCP requests
type AuthMiddleware struct {
	validator    *KubernetesValidator
	config       *AuthConfig
	db           *database.DatabaseConnection // Database connection for RBAC resolution
	tokenCache   map[string]*cachedToken
	cacheMutex   sync.RWMutex
	cleanupTicker *time.Ticker
}

// cachedToken represents a cached token validation result
type cachedToken struct {
	result    *TokenValidationResult
	expiresAt time.Time
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(config *AuthConfig, db *database.DatabaseConnection) (*AuthMiddleware, error) {
	if !config.EnableAuth {
		return &AuthMiddleware{config: config, db: db}, nil
	}

	k8sConfig, err := config.GetKubernetesConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	validator := NewKubernetesValidator(k8sConfig)

	middleware := &AuthMiddleware{
		validator:  validator,
		config:     config,
		db:         db,
		tokenCache: make(map[string]*cachedToken),
	}

	// Start cache cleanup if caching is enabled
	if config.CacheTokens {
		middleware.cleanupTicker = time.NewTicker(time.Minute)
		go middleware.cleanupExpiredTokens()
	}

	return middleware, nil
}

// Handler returns the HTTP middleware handler
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication if disabled
		if !m.config.EnableAuth {
			next.ServeHTTP(w, r)
			return
		}

		// Skip authentication for health check endpoint
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("[AUTH] %s %s - Checking authorization", r.Method, r.URL.Path)

		// Extract authorization header
		authHeader, headerSource := m.extractAuthHeader(r)
		if authHeader == "" {
			log.Printf("[AUTH] Missing authorization header")
			m.sendAuthError(w, "Missing authorization header",
				`Either "Authorization: Bearer <token>" or "kubernetes-authorization: Bearer <token>" required`,
				http.StatusUnauthorized)
			return
		}

		// Validate token (with caching if enabled)
		validationResult, err := m.validateToken(authHeader)
		if err != nil {
			log.Printf("[AUTH] Token validation error: %v", err)
			m.sendAuthError(w, "Internal authentication error", err.Error(), http.StatusInternalServerError)
			return
		}

		if !validationResult.Valid {
			log.Printf("[AUTH] Token validation failed: %s", validationResult.Error)
			m.sendAuthError(w, "Token validation failed", validationResult.Error, http.StatusUnauthorized)
			return
		}

		// Update user context with header source
		validationResult.User.HeaderSource = headerSource

		// Granular RBAC - use permission resolution for authorization
		userToken := authHeader  // Full "Bearer <token>" string
		queryFilters, err := m.resolveUserPermissions(r.Context(), userToken)
		if err != nil {
			// SECURITY: Permission resolution failure = access denied
			log.Printf("[RBAC-SECURITY] Permission resolution failed for user %s, denying access: %v", validationResult.User.Username, err)
			log.Printf("[RBAC-SECURITY] Security-first design: K8s API failures result in authentication failure")
			m.sendAuthError(w, "Permission resolution failed",
				"Unable to resolve user permissions from Kubernetes API. Access denied for security.",
				http.StatusInternalServerError)
			return
		}

		// Check if user has ANY meaningful ACM-related permissions
		if len(queryFilters.PermissionSources) == 0 {
			log.Printf("[RBAC] No ACM permissions found for user: %s - denying access", validationResult.User.Username)
			m.sendAuthError(w, "Access denied",
				"No ACM-related permissions found. User must have at least read access to ACM resources.",
				http.StatusForbidden)
			return
		}

		// Enhance UserContext with resolved query filters
		validationResult.User.QueryFilters = queryFilters
		log.Printf("[RBAC] Successfully resolved permissions for user %s: %d permission sources",
			validationResult.User.Username,
			len(queryFilters.PermissionSources))

		log.Printf("[AUTH] Access granted for user: %s (via %s header)", validationResult.User.Username, headerSource)

		// Add user context to request
		ctx := WithUserContext(r.Context(), validationResult.User)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractAuthHeader extracts authorization header, supporting both standard and custom headers
func (m *AuthMiddleware) extractAuthHeader(r *http.Request) (string, string) {
	// Check standard Authorization header first
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		return authHeader, "Authorization"
	}

	// Check custom kubernetes-authorization header
	if authHeader := r.Header.Get("kubernetes-authorization"); authHeader != "" {
		return authHeader, "kubernetes-authorization"
	}

	return "", ""
}

// validateToken validates a token with optional caching
func (m *AuthMiddleware) validateToken(authHeader string) (*TokenValidationResult, error) {
	// Check cache if enabled
	if m.config.CacheTokens {
		if result := m.getCachedToken(authHeader); result != nil {
			return result, nil
		}
	}

	// Validate token via Kubernetes API
	result, err := m.validator.ValidateBearerToken(authHeader)
	if err != nil {
		return nil, err
	}

	// Cache result if enabled and valid
	if m.config.CacheTokens && result.Valid {
		m.cacheToken(authHeader, result)
	}

	return result, nil
}

// getCachedToken retrieves a cached token validation result
func (m *AuthMiddleware) getCachedToken(authHeader string) *TokenValidationResult {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	if cached, exists := m.tokenCache[authHeader]; exists {
		if time.Now().Before(cached.expiresAt) {
			return cached.result
		}
	}

	return nil
}

// cacheToken caches a token validation result
func (m *AuthMiddleware) cacheToken(authHeader string, result *TokenValidationResult) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	m.tokenCache[authHeader] = &cachedToken{
		result:    result,
		expiresAt: time.Now().Add(m.config.CacheTTL),
	}
}

// cleanupExpiredTokens removes expired tokens from cache
func (m *AuthMiddleware) cleanupExpiredTokens() {
	for range m.cleanupTicker.C {
		m.cacheMutex.Lock()
		now := time.Now()
		for token, cached := range m.tokenCache {
			if now.After(cached.expiresAt) {
				delete(m.tokenCache, token)
			}
		}
		m.cacheMutex.Unlock()
	}
}

// sendAuthError sends a structured authentication error response
func (m *AuthMiddleware) sendAuthError(w http.ResponseWriter, message, details string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    -32001, // Custom authentication error code
			"message": message,
			"data": map[string]interface{}{
				"details":    details,
				"statusCode": statusCode,
			},
		},
		"id": nil,
	}

	_ = json.NewEncoder(w).Encode(errorResponse)
}

// Close cleanups resources used by the middleware
func (m *AuthMiddleware) Close() {
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
	}
}

// GetUserContext is a helper function to extract user context from request
func GetUserContext(r *http.Request) *UserContext {
	return UserFromContext(r.Context())
}

// RequireAuth is a helper middleware for endpoints that require authentication
func RequireAuth(authMiddleware *AuthMiddleware, next http.HandlerFunc) http.HandlerFunc {
	return authMiddleware.Handler(http.HandlerFunc(next)).ServeHTTP
}


// GetAuthorizedTools returns tools available to the authenticated user
func GetAuthorizedTools(userCtx *UserContext) []string {
	// Require valid authentication
	if userCtx == nil {
		log.Printf("[SECURITY] GetAuthorizedTools called with nil userCtx - denying all access")
		return []string{} // Return empty list - no tools authorized
	}

	tools := []string{}

	// Authenticated users get find_resources
	tools = append(tools, "find_resources")

	return tools
}