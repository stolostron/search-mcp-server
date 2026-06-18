# ACM-32469: Implementation Plan — Enable TLS Verification by Default

**Jira:** https://redhat.atlassian.net/browse/ACM-32469
**Sub-task:** https://redhat.atlassian.net/browse/ACM-35727
**Date:** 2026-06-18

---

## Problem

The Helm chart ships with `authentication.skipTLS: true` (`values.yaml:66`), which sets
`MCP_K8S_SKIP_TLS=true` in the deployment. This disables TLS certificate verification on
all K8s API connections, leaving the authentication channel vulnerable to man-in-the-middle
attacks that could expose cluster-admin bearer tokens in transit.

SAR finding: SAR-04 (SDLC-10288 — Security Architecture Review for ACM MCP Server 0.1).

---

## Current State

### Helm chart default (insecure)

```yaml
# helm/acm-mcp-server/values.yaml:62-66
authentication:
  enabled: true
  # Skip TLS verification when connecting to Kubernetes API (for testing/dev clusters)
  skipTLS: true   # <-- ships insecure
```

### Deployment template wires the value into the pod env

```yaml
# helm/acm-mcp-server/templates/deployment.yaml:63-64
- name: MCP_K8S_SKIP_TLS
  value: "{{ .Values.authentication.skipTLS }}"
```

### Data flow

```
values.yaml                  deployment.yaml              Go Source
───────────                  ───────────────              ─────────
authentication:              env MCP_K8S_SKIP_TLS         ServerConfig.SkipTLSVerify
  skipTLS: true  ─────────►   = "true"         ────────►   (config.go:46,106)
                                                                  │
                                                                  ▼
                                                          AuthConfig.SkipTLS
                                                            (types.go:214)
                                                                  │
                                               ┌──────────────────┼──────────────┐
                                               ▼                  ▼              ▼
                                        K8sConfig.TLSVerify   rest.TLSClient   tls.Config
                                        = !SkipTLS            .Insecure        .InsecureSkipVerify
                                        (types.go:325)        (kube_config.go) (k8s_validator.go:31)
```

---

## Key Findings

1. **Go code already defaults `SkipTLSVerify` to `false`** (`config.go:106`). The insecure
   default exists only in the Helm chart — the fix is minimal.

2. **The `rest.Config` path already handles `skipTLS: false` correctly** — all 5 functions
   in `kube_config.go` set `CAFile: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"`
   when `skipTLS=false`. No changes needed there.

3. **No extra CA volume mounts needed** — the service account CA at
   `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt` is automatically mounted by
   Kubernetes. The deployment's `automountServiceAccountToken` default covers this.

4. **`KubernetesValidator` HTTP transport has a gap** (`k8s_validator.go:27-41`) — when
   `TLSVerify=true`, it creates a bare `&http.Transport{}` without configuring the CA
   certificate. The `rest.Config` path already handles it, but the raw `http.Client` path
   (used for TokenReview via `ValidateBearerToken`) needs explicit CA loading.

5. **All test files use `SkipTLS: true`** because they connect to `httptest.Server` mock
   servers with self-signed certs. This is correct and must not change.

---

## Implementation Steps

### Step 1: Change Helm chart default (LOW RISK)

**File:** `helm/acm-mcp-server/values.yaml:66`

```yaml
# Before
  skipTLS: true

# After
  # Skip TLS verification when connecting to Kubernetes API
  # Default: false (TLS verification enabled for production security)
  # Set to true only for development/testing with self-signed certificates
  skipTLS: false
```

### Step 2: Fix `KubernetesValidator` CA certificate loading (MEDIUM RISK)

**File:** `internal/server/auth/k8s_validator.go:27-41`

When `TLSVerify=true`, load the in-cluster CA certificate into the HTTP transport so the
raw `http.Client` path verifies the K8s API server certificate correctly:

```go
func NewKubernetesValidator(config *K8sConfig) *KubernetesValidator {
    transport := &http.Transport{}
    if !config.TLSVerify {
        // #nosec G402 -- intentional for test environments only, controlled by config.TLSVerify
        transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
    } else {
        // Load the in-cluster CA certificate for proper TLS verification.
        // Falls back to the system trust store if the file is absent (local dev).
        caCert, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
        if err == nil {
            caCertPool := x509.NewCertPool()
            caCertPool.AppendCertsFromPEM(caCert)
            transport.TLSClientConfig = &tls.Config{RootCAs: caCertPool}
        }
    }

    return &KubernetesValidator{
        config: config,
        httpClient: &http.Client{
            Timeout:   config.Timeout,
            Transport: transport,
        },
    }
}
```

New imports required: `crypto/x509`, `os` (already imported).

### Step 3: Update non-Helm deployment template (LOW RISK)

**File:** `k8s/deployment_docker.template.yaml`

Verify the env var `MCP_K8S_SKIP_TLS` is either absent (Go default is `false`) or
explicitly set to `"false"`. Remove or correct it if set to `"true"`.

### Step 4: Verify tests pass

- All existing unit/integration tests set `SkipTLS: true` explicitly on their `AuthConfig`
  structs — they are not affected by the Helm default change.
- Run `go test ./...` to confirm no regressions.
- Add a unit test for `NewKubernetesValidator` that verifies TLS config is set correctly
  when `TLSVerify=true` (CA pool loaded) and when `TLSVerify=false` (InsecureSkipVerify).

### Step 5: Update documentation (NO RISK)

**File:** `docs/authentication-authorization.md`

Update to reflect the new secure default and document the override option:

```
MCP_K8S_SKIP_TLS   bool   false   Skip TLS verification for K8s API (testing only).
                                   Never set to true in production.
```

---

## Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Existing deployments break on upgrade | Low | In-cluster CA is auto-mounted; `kube_config.go` already references it correctly |
| `KubernetesValidator` HTTP client fails without explicit CA | Medium | Step 2 loads the CA cert into the transport; falls back to system trust store |
| Tests break | Very Low | Tests explicitly set `SkipTLS: true`; Helm default change does not affect them |

---

## Files to Modify

```
helm/acm-mcp-server/
  values.yaml                              modified  (skipTLS: true → false)
internal/server/auth/
  k8s_validator.go                         modified  (CA cert loading when TLSVerify=true)
k8s/
  deployment_docker.template.yaml          verify    (MCP_K8S_SKIP_TLS default)
docs/
  authentication-authorization.md          modified  (document secure default)
plans/
  ACM-32469-enable-tls-verification-plan.md  new (this file)
  INDEX.md                                   modified
```

---

## Open Questions

| # | Question | Impact | Status |
|---|---|---|---|
| 1 | Does the `deployment_docker.template.yaml` explicitly set `MCP_K8S_SKIP_TLS=true`? If so, must be corrected. | Completeness | Open |
| 2 | Are there any existing production deployments using `skipTLS: true` explicitly in a values override? Upgrade path: they keep working since the helm value is still configurable. | Upgrade safety | Open |

---

## Acceptance Criteria

- [ ] `helm template .` renders `MCP_K8S_SKIP_TLS` as `"false"` by default
- [ ] `NewKubernetesValidator` loads the in-cluster CA cert into the transport when `TLSVerify=true`
- [ ] `go test ./...` passes with no regressions
- [ ] Pod starts successfully on a test cluster with `skipTLS: false`
- [ ] Token validation, RBAC resolution, and resource discovery work with TLS verification enabled
- [ ] Documentation updated to reflect secure default
