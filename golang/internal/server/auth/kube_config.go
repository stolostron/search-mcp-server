package auth

import (
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

// GetBaseKubernetesConfig creates a base Kubernetes config with centralized rate limiting
// Mirrors search-v2-api's centralized approach to prevent client-side throttling
func GetBaseKubernetesConfig() *rest.Config {
	return &rest.Config{
		// High rate limits to prevent client-side throttling (like search-v2-api)
		QPS:   250,
		Burst: 100,
	}
}

// CreateDiscoveryConfig creates a config for Kubernetes discovery calls with user token
func CreateDiscoveryConfig(kubernetesURL, userToken string, timeout time.Duration, skipTLS bool) *rest.Config {
	config := GetBaseKubernetesConfig()

	if kubernetesURL != "" {
		// Custom URL specified (testing with mock servers)
		config.Host = kubernetesURL
		config.BearerToken = strings.TrimPrefix(userToken, "Bearer ")
		config.Timeout = timeout
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: skipTLS,
		}
	} else {
		// Production: Build config manually with user token
		config.Host = "https://kubernetes.default.svc:443"
		config.BearerToken = strings.TrimPrefix(userToken, "Bearer ")
		config.Timeout = timeout
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: false,
			CAFile:   "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		}
	}

	return config
}

// CreateUserConfig creates a config for user-impersonated Kubernetes calls
func CreateUserConfig(userToken string, timeout time.Duration, skipTLS bool) *rest.Config {
	config := GetBaseKubernetesConfig()

	config.Host = "https://kubernetes.default.svc:443"
	config.BearerToken = strings.TrimPrefix(userToken, "Bearer ")
	config.Timeout = timeout
	config.TLSClientConfig = rest.TLSClientConfig{
		Insecure: skipTLS,
		CAFile:   "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
	}

	return config
}

// CreateTokenReviewConfig creates a config for TokenReview API calls
func CreateTokenReviewConfig(timeout time.Duration, skipTLS bool) *rest.Config {
	config := GetBaseKubernetesConfig()

	config.Host = "https://kubernetes.default.svc:443"
	config.Timeout = timeout
	config.TLSClientConfig = rest.TLSClientConfig{
		Insecure: skipTLS,
		CAFile:   "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
	}

	return config
}

// CreateHubRBACConfig creates a base config for hub RBAC operations (no user token initially)
func CreateHubRBACConfig(skipTLS bool) *rest.Config {
	config := GetBaseKubernetesConfig()

	config.Host = "https://kubernetes.default.svc:443"
	config.BearerToken = "" // Will be set per-request with user token

	if skipTLS {
		// Testing mode: skip TLS verification entirely
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: true,
		}
	} else {
		// Production mode: use proper CA file
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: false,
			CAFile:   "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		}
	}

	return config
}

// CreateServiceAccountConfig creates a config using service account credentials
func CreateServiceAccountConfig(skipTLS bool) *rest.Config {
	config := GetBaseKubernetesConfig()

	config.Host = "https://kubernetes.default.svc:443"
	config.BearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	if skipTLS {
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: true,
		}
	} else {
		config.TLSClientConfig = rest.TLSClientConfig{
			Insecure: false,
			CAFile:   "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		}
	}

	return config
}