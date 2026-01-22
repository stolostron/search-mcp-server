package auth

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// KubernetesValidator handles token validation via Kubernetes APIs
type KubernetesValidator struct {
	config     *K8sConfig
	httpClient *http.Client
}

// NewKubernetesValidator creates a new Kubernetes validator
func NewKubernetesValidator(config *K8sConfig) *KubernetesValidator {
	// Create HTTP client with appropriate TLS settings
	transport := &http.Transport{}
	if !config.TLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &KubernetesValidator{
		config: config,
		httpClient: &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		},
	}
}

// ValidateBearerToken validates a bearer token using Kubernetes TokenReview API
func (v *KubernetesValidator) ValidateBearerToken(authHeader string) (*TokenValidationResult, error) {
	// Validate header format
	if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		return &TokenValidationResult{
			Valid: false,
			Error: "Invalid Bearer token format. Expected: Authorization: Bearer <token>",
		}, nil
	}

	token := authHeader[7:] // Remove 'Bearer ' prefix

	// Basic token length validation
	if len(token) < 10 {
		return &TokenValidationResult{
			Valid: false,
			Error: "Token too short",
		}, nil
	}

	// Get service account token for API authentication
	saToken, err := v.getServiceAccountToken()
	if err != nil {
		return &TokenValidationResult{
			Valid: false,
			Error: fmt.Sprintf("Failed to get service account token: %v", err),
		}, nil
	}

	// Create TokenReview request
	tokenReviewRequest := map[string]interface{}{
		"apiVersion": "authentication.k8s.io/v1",
		"kind":       "TokenReview",
		"spec": map[string]interface{}{
			"token": token,
		},
	}

	// Call Kubernetes TokenReview API
	response, err := v.callKubernetesAPI("/apis/authentication.k8s.io/v1/tokenreviews", tokenReviewRequest, saToken)
	if err != nil {
		return &TokenValidationResult{
			Valid: false,
			Error: fmt.Sprintf("TokenReview API call failed: %v", err),
		}, nil
	}

	// Parse TokenReview response
	status, ok := response["status"].(map[string]interface{})
	if !ok {
		return &TokenValidationResult{
			Valid: false,
			Error: "Invalid TokenReview response format",
		}, nil
	}

	authenticated, _ := status["authenticated"].(bool)
	if !authenticated {
		return &TokenValidationResult{
			Valid: false,
			Error: "Token not authenticated by Kubernetes API",
		}, nil
	}

	// Extract user information
	user, ok := status["user"].(map[string]interface{})
	if !ok {
		return &TokenValidationResult{
			Valid: false,
			Error: "No user information in TokenReview response",
		}, nil
	}

	username, _ := user["username"].(string)
	uid, _ := user["uid"].(string)
	groups := []string{}

	if groupsInterface, ok := user["groups"].([]interface{}); ok {
		for _, group := range groupsInterface {
			if groupStr, ok := group.(string); ok {
				groups = append(groups, groupStr)
			}
		}
	}

	userContext := &UserContext{
		Username:    username,
		UID:         uid,
		Groups:      groups,
		AuthMethod:  "bearer",
		ValidatedAt: time.Now(),
	}

	return &TokenValidationResult{
		Valid: true,
		User:  userContext,
	}, nil
}

// CheckACMAdminPermissions checks if a user has ACM administrator permissions
func (v *KubernetesValidator) CheckACMAdminPermissions(userContext *UserContext, userToken string) (bool, error) {
	// Check if user has cluster admin permissions
	hasClusterAdmin, err := v.checkSelfSubjectAccessReview(userToken, ClusterAdminPermission)
	if err != nil {
		return false, fmt.Errorf("cluster admin check failed: %w", err)
	}

	if hasClusterAdmin {
		return true, nil
	}

	// Check ACM-specific permissions
	hasACMAdmin, err := v.checkSelfSubjectAccessReview(userToken, ACMAdminPermission)
	if err != nil {
		return false, fmt.Errorf("ACM admin check failed: %w", err)
	}

	return hasACMAdmin, nil
}

// checkSelfSubjectAccessReview performs permission check using user's own token
func (v *KubernetesValidator) checkSelfSubjectAccessReview(userToken string, permission Permission) (bool, error) {
	selfSubjectAccessReview := map[string]interface{}{
		"apiVersion": "authorization.k8s.io/v1",
		"kind":       "SelfSubjectAccessReview",
		"spec": map[string]interface{}{
			"resourceAttributes": map[string]interface{}{
				"verb":     permission.Verb,
				"resource": permission.Resource,
				"group":    permission.Group,
			},
		},
	}

	// Use user's token for this call (not service account token)
	response, err := v.callKubernetesAPI("/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", selfSubjectAccessReview, userToken)
	if err != nil {
		return false, fmt.Errorf("SelfSubjectAccessReview API call failed: %w", err)
	}

	// Parse response
	status, ok := response["status"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid SelfSubjectAccessReview response format")
	}

	allowed, _ := status["allowed"].(bool)
	return allowed, nil
}

// callKubernetesAPI makes HTTP calls to Kubernetes API
func (v *KubernetesValidator) callKubernetesAPI(path string, requestBody interface{}, token string) (map[string]interface{}, error) {
	// Determine API URL
	var apiURL string
	if v.config.URL != "" {
		apiURL = v.config.URL + path
	} else {
		apiURL = fmt.Sprintf("https://%s:%s%s", v.config.Host, v.config.Port, path)
	}

	// Marshal request body
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Make request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kubernetes API returned status %d: %s", resp.StatusCode, string(responseBody))
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	return response, nil
}

// getServiceAccountToken retrieves the service account token for API authentication
func (v *KubernetesValidator) getServiceAccountToken() (string, error) {
	// Use direct token value if provided
	if v.config.Token != "" {
		return v.config.Token, nil
	}

	// Read token from file
	tokenPath := v.config.TokenPath
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" // Default K8s path
	}

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read service account token from %s: %w", tokenPath, err)
	}

	return string(bytes.TrimSpace(tokenBytes)), nil
}

// LoadAuthConfig loads authentication configuration with smart defaults
func LoadAuthConfig() *AuthConfig {
	config := &AuthConfig{
		AuthTimeout: 5 * time.Second,
		CacheTokens: true,
		CacheTTL:    5 * time.Minute,
	}

	// Determine if auth should be enabled
	config.EnableAuth = shouldEnableAuth()

	// Load Kubernetes connection details
	config.KubernetesHost = os.Getenv("KUBERNETES_SERVICE_HOST")
	config.KubernetesPort = os.Getenv("KUBERNETES_SERVICE_PORT")

	// Local testing overrides
	config.KubernetesURL = os.Getenv("MCP_K8S_URL")
	config.TokenValue = os.Getenv("MCP_SA_TOKEN")
	config.TokenPath = os.Getenv("MCP_SA_TOKEN_PATH")
	config.KubeconfigPath = os.Getenv("MCP_KUBECONFIG")

	// Security options
	if skipTLSStr := os.Getenv("MCP_K8S_SKIP_TLS"); skipTLSStr != "" {
		config.SkipTLS, _ = strconv.ParseBool(skipTLSStr)
	}

	if timeoutStr := os.Getenv("MCP_AUTH_TIMEOUT"); timeoutStr != "" {
		if duration, err := time.ParseDuration(timeoutStr); err == nil {
			config.AuthTimeout = duration
		}
	}

	return config
}

// shouldEnableAuth determines if authentication should be enabled based on environment
func shouldEnableAuth() bool {
	// Explicit setting always wins
	if envValue := os.Getenv("MCP_ENABLE_AUTH"); envValue != "" {
		enabled, _ := strconv.ParseBool(envValue)
		return enabled
	}

	// Smart default: enable auth if running in Kubernetes
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// GetKubernetesConfig converts AuthConfig to K8sConfig for the validator
func (c *AuthConfig) GetKubernetesConfig() (*K8sConfig, error) {
	if !c.EnableAuth {
		return nil, nil // Auth disabled
	}

	k8sConfig := &K8sConfig{
		Timeout:   c.AuthTimeout,
		TLSVerify: !c.SkipTLS,
	}

	// Production auto-detection
	if c.KubernetesHost != "" && c.KubernetesPort != "" {
		k8sConfig.Host = c.KubernetesHost
		k8sConfig.Port = c.KubernetesPort
		k8sConfig.TokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		return k8sConfig, nil
	}

	// Local testing - manual URL
	if c.KubernetesURL != "" {
		k8sConfig.URL = c.KubernetesURL

		// Token source priority
		if c.TokenValue != "" {
			k8sConfig.Token = c.TokenValue
		} else if c.TokenPath != "" {
			k8sConfig.TokenPath = c.TokenPath
		} else if c.KubeconfigPath != "" {
			k8sConfig.KubeconfigPath = c.KubeconfigPath
		} else {
			return nil, fmt.Errorf("auth enabled but no token source provided")
		}

		return k8sConfig, nil
	}

	// Fallback - try to find kubeconfig
	if kubeconfigPath := defaultKubeconfigPath(); kubeconfigPath != "" {
		k8sConfig.KubeconfigPath = kubeconfigPath
		return k8sConfig, nil
	}

	return nil, fmt.Errorf("auth enabled but no Kubernetes connection details found")
}

// defaultKubeconfigPath returns the default kubeconfig path if it exists
func defaultKubeconfigPath() string {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.ExpandEnv("$HOME/.kube/config")
	}

	if _, err := os.Stat(kubeconfigPath); err == nil {
		return kubeconfigPath
	}

	return ""
}