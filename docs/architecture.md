# search-mcp-server: Architecture & Security Documentation

---

## Table of Contents

1. [Overview](#overview)
2. [Component Diagram](#component-diagram)
3. [Request Lifecycle (Data Flow)](#request-lifecycle-data-flow)
4. [Authentication Flow](#authentication-flow)
5. [Authorization and RBAC](#authorization-and-rbac)
6. [MCP Tools](#mcp-tools)
7. [Database Layer](#database-layer)
8. [Kubernetes Connectivity](#kubernetes-connectivity)
9. [Transport Layer](#transport-layer)
10. [Deployment Topology](#deployment-topology)
11. [Configuration Reference](#configuration-reference)
12. [Security Properties](#security-properties)
13. [Trust Boundaries](#trust-boundaries)
14. [Sensitive Data Inventory](#sensitive-data-inventory)

---

## Overview

`search-mcp-server` is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server
that gives AI agents access to the ACM Search database — a PostgreSQL store of Kubernetes
resource data aggregated from all clusters managed by Red Hat Advanced Cluster Management.

**What it does:**

- Exposes a single MCP tool (`find_resources`) that lets an LLM search for Kubernetes resources
  across the entire managed fleet.
- Enforces the authenticated user's existing Kubernetes RBAC — the user can only see resources
  they are already authorized to access on the managed clusters.
- Sanitizes all returned resource data against a prompt-injection detection ruleset before
  passing it to the LLM.

**What it does not do:**

- Write to or modify any Kubernetes resource.
- Write to or modify the search database.
- Store credentials, tokens, or user data persistently.
- Grant any access beyond what the user's existing Kubernetes RBAC permits.

---

## Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                        MCP Client (LLM / Agent)                     │
│   Claude Desktop (stdio) │ Claude.ai / Cline / cursor (HTTP/MCP)   │
└──────────────┬───────────┴──────────────────────┬────────────────────┘
               │ stdin/stdout                      │ HTTPS (via Route)
               │                                  │
┌──────────────▼──────────────────────────────────▼────────────────────┐
│                         search-mcp-server (Pod)                       │
│                                                                       │
│  ┌─────────────┐    ┌──────────────────────────────────────────────┐ │
│  │    STDIO    │    │             HTTP Transport (:8080)            │ │
│  │  Transport  │    │  CORS → Auth Middleware → MCP Handler        │ │
│  │  (no auth)  │    │  GET/POST /health  (bypasses auth)           │ │
│  └──────┬──────┘    │  GET      /metrics (auth protected)         │ │
│         │           │  POST     /mcp     (auth protected)         │ │
│         │           └───────────────────┬──────────────────────────┘ │
│         │                               │                             │
│         └───────────┬───────────────────┘                             │
│                     │                                                 │
│          ┌──────────▼──────────┐                                     │
│          │   find_resources    │ ← MCP tool (single tool exposed)    │
│          │   (core.go)         │                                     │
│          └──────────┬──────────┘                                     │
│                     │                                                 │
│          ┌──────────▼──────────┐   ┌──────────────────────────────┐ │
│          │   SQL Builder       │   │   Sanitizer                  │ │
│          │   (RBAC-filtered    │   │   (prompt injection           │ │
│          │    parameterized    │   │    detection on all          │ │
│          │    queries)         │   │    returned JSONB fields)    │ │
│          └──────────┬──────────┘   └──────────────────────────────┘ │
│                     │                                                 │
└─────────────────────┼─────────────────────────────────────────────────┘
                      │ pgx/v5 pool (TLS optional)
                      ▼
         ┌────────────────────────┐
         │  search-postgres       │
         │  (ACM PostgreSQL DB)   │
         │  search.resources      │
         └────────────────────────┘

         ┌────────────────────────────────────────────────────┐
         │  Kubernetes API Server  (https://kubernetes.default.svc:443) │
         │                                                    │
         │  POST /tokenreviews          ← SA token            │
         │  POST /selfsubjectaccessreviews ← user token       │
         │  POST /selfsubjectrulesreviews  ← user token       │
         │  GET  /userpermissions (clusterview CRs) ← user token │
         └────────────────────────────────────────────────────┘
```

---

## Request Lifecycle (Data Flow)

The following describes the end-to-end path for an authenticated HTTP MCP request.

```
1. MCP Client sends:
   POST /mcp
   Authorization: Bearer <user-token>
   {"jsonrpc":"2.0","method":"tools/call","params":{"name":"find_resources","arguments":{...}}}

2. CORS Middleware
   └── Adds Access-Control-Allow-* headers; handles OPTIONS preflight

3. Auth Middleware (transport_http.go → auth/middleware.go)
   ├── /health? → bypass (no auth required)
   ├── Extract token from Authorization or kubernetes-authorization header
   ├── Check in-memory token cache (SHA-256 keyed)
   │   ├── Cache hit → use cached UserContext
   │   └── Cache miss → validate with Kubernetes TokenReview API (SA token used)
   ├── Resolve user's RBAC permissions
   │   ├── Source 1: clusterview UserPermission CRs  (user token)
   │   └── Source 2: Hub K8s SSAR/SSRR              (user token)
   ├── No permissions resolved? → HTTP 403
   └── Attach UserContext{QueryFilters} to request context

4. MCP Handler (transport_http.go)
   ├── Decode JSON-RPC method
   ├── tools/call → check user is authorized for tool name → dispatch
   └── find_resources handler

5. find_resources (findresources/core.go)
   ├── Parse and validate arguments
   ├── Build parameterized SQL with RBAC WHERE clause
   │   └── Each PermissionSource → (cluster = $N AND kind IN (...)) OR ...
   ├── Execute read-only SELECT against search.resources
   ├── Sanitize each row's JSONB data (prompt injection scan)
   └── Format results (list / count / summary / health)

6. Response returned to MCP Client as JSON-RPC result
```

### STDIO Path (no authentication)

```
MCP Client (stdin) → STDIO Transport → find_resources (userCtx=nil)
                                     → SQL with no RBAC WHERE clause
                                     → All resources returned
                                     → Sanitized → stdout
```

The STDIO transport has no authentication. It is intended for local use (Claude Desktop,
development) where the operator controls the process environment and accepts responsibility
for access control.

---

## Authentication Flow

**Implementation:** `internal/server/auth/middleware.go`, `auth/k8s_validator.go`

```
HTTP Request
     │
     ▼
Extract Bearer token
  • Authorization: Bearer <token>       (checked first)
  • kubernetes-authorization: Bearer <token>  (fallback)
     │
     ▼
SHA-256 hash of raw header string
     │
     ├── Cache hit? ──────────────────────────────► use cached result
     │                                                       │
     └── Cache miss                                          │
              │                                              │
              ▼                                              │
   POST /apis/authentication.k8s.io/v1/tokenreviews         │
   (server uses its SA token to call this)                  │
   TokenReview.spec.token = <user token>                    │
              │                                              │
              ├── Invalid/expired → HTTP 401                │
              │                                              │
              └── Valid → {username, uid, groups}           │
                       │                                    │
                       ▼                                    │
              Store in cache (deep clone,                   │
              TTL from MCP_AUTH_CACHE_TTL)                  │
                       │                                    │
                       └────────────────────────────────────┘
                                        │
                                        ▼
                            Resolve RBAC permissions
                            (see Authorization section)
```

### Token Cache Security

| Property | Detail |
|---|---|
| Cache key | `sha256(raw "Authorization: Bearer <token>" header string)` — token never stored plaintext |
| Cache value | Deep clone of `TokenValidationResult`; mutable request-scoped fields (`HeaderSource`, `QueryFilters`) zeroed before storing |
| TTL | `MCP_AUTH_CACHE_TTL` (default 5 minutes) |
| Eviction | Background goroutine every 60 seconds removes expired entries |
| Isolation | Each cache lookup returns a fresh copy — no shared mutable state between concurrent requests |

---

## Authorization and RBAC

**Implementation:** `internal/server/auth/rbac_resolver.go`, `auth/hub_rbac_client.go`

The server resolves the authenticated user's permissions from two independent sources and
merges them with OR logic. The result is used to build SQL WHERE clauses that restrict every
database query.

### Source 1: UserPermission CRs (Managed Cluster Access)

```
RBACResolver.resolveUserPermissionAPI()
     │
     ▼
K8s dynamic client (user's token)
GET clusterview.open-cluster-management.io/v1alpha1/userpermissions
     │
     ▼
For each CR:
  status.Bindings[].{cluster, namespaces}
  status.ClusterRoleDefinition.Rules[].{verbs, resources, apiGroups}
     │
     ├── verb must include "list" or "*"
     ├── resource name → Kind via ResourceDiscovery
     └── Produces: PermissionSource{
           Source: "userpermission-cr",
           ClusterScopedKinds: {cluster → [{Kind, APIGroup}]},
           NamespacedKinds:    {"cluster/namespace" → [{Kind, APIGroup}]}
         }
```

### Source 2: Hub Kubernetes API (Hub Cluster Access)

```
RBACResolver.resolveHubKubernetesAPI()
     │
     ▼
SelfSubjectAccessReview: {verb:"*", resource:"*", group:"*"}
     │
     ├── cluster-admin? ──► wildcard permissions (all hub resources)
     │
     └── Not admin:
          ├── Cluster-scoped resources:
          │     Parallel SelfSubjectAccessReviews (concurrency=10)
          │     for each resource type in search.resources
          │
          └── Namespaced resources:
                Parallel SelfSubjectRulesReviews (concurrency=10)
                per namespace found in DB for hub cluster
```

Hub cluster name is detected dynamically:
```sql
SELECT cluster FROM search.resources
WHERE data ? '_hubClusterResource' AND data->>'kind' != 'Cluster'
LIMIT 1
```

Hub RBAC results are cached per user UID with TTL (`MCP_DISCOVERY_TTL`, default 5 minutes).

### SQL RBAC Filter Construction

**Implementation:** `internal/findresources/core.go:applyAuthorizationFilters()`

```
PermissionSources (merged with OR)
     │
     ├── Source A (userpermission-cr):
     │     (cluster = $1 AND data->>'namespace' = $2 AND data->>'kind' IN ($3,$4,...))
     │     OR (cluster = $5 AND data->>'kind' IN ($6,...))   ← cluster-scoped
     │
     └── Source B (hub-kubernetes):
           (cluster = $N AND data->>'kind' IN ($M,...))

Final query:
  SELECT uid, cluster, data FROM search.resources
  WHERE (<rbac_conditions>)
    AND <user-supplied filters>   ← kind, name, namespace, etc.
  ORDER BY ... LIMIT N
```

**Fail-secure guarantees:**

| Condition | Outcome |
|---|---|
| RBAC resolution returns error | HTTP 500; access denied |
| No permission sources resolved | HTTP 403; access denied |
| `userCtx == nil` (auth enabled, no context) | Query returns `WHERE FALSE` (zero rows) |
| STDIO transport | `userCtx == nil` → no RBAC filter; all data returned |

---

## MCP Tools

**Implementation:** `internal/server/tools.go`, `internal/findresources/core.go`

Exactly one MCP tool is exposed: **`find_resources`**

### find_resources Parameters

| Parameter | Type | Default | Description |
|---|---|---|---|
| `kind` | string | — | Resource kind(s), comma-separated (e.g. `Pod,Deployment`) |
| `name` | string | — | Exact name or glob pattern |
| `namespace` | string | — | Namespace(s), comma-separated, or wildcard patterns |
| `cluster` | string | — | Cluster name(s), comma-separated |
| `labelSelector` | string | — | Kubernetes label selector syntax |
| `clusterSelector` | string | — | Filter clusters by cluster labels |
| `status` | string | — | Status filter |
| `textSearch` | string | — | Full JSONB text search |
| `ageNewerThan` | string | — | Relative age filter (e.g. `1h`, `2d`, `1w`) |
| `ageOlderThan` | string | — | Relative age filter |
| `outputMode` | enum | `list` | `list`, `count`, `summary`, `health` |
| `groupBy` | string | — | `status`, `namespace`, `cluster`, `kind`, `label:<key>` |
| `countOnly` | bool | `false` | Return counts only |
| `limit` | int | `50` | Max results (1–1000) |
| `sortBy` | string | `name` | `name`, `created`, `namespace`, `cluster` |
| `sortOrder` | enum | `asc` | `asc` or `desc` |
| `stream` | bool | `false` | Accepted but not yet functional per-request — streaming is controlled at the transport level, not per-call (see Known Limitations) |

### Tool Authorization (HTTP transport)

```
tools/call received
     │
     ├── auth enabled AND userCtx == nil? → HTTP 401
     ├── GetAuthorizedTools(userCtx):
     │     authenticated user → ["find_resources"]
     │     nil user context   → []
     └── tool name in authorized list? → dispatch
                                  else → HTTP 403
```

### Prompt Injection Sanitization

**Implementation:** `internal/sanitize/sanitize.go`, `sanitize/patterns.go`

All resource `data` JSONB returned from database queries passes through the sanitizer before
being included in MCP responses.

**Field policies:**

| Fields | Policy | Rationale |
|---|---|---|
| `name`, `namespace`, `kind`, `cluster`, `created`, `_uid`, `_hubClusterName` | Skip (no sanitization) | DNS-safe identifiers; no free text |
| `status`, `annotation`, `label`, and all other JSONB fields | SanitizeStrings | May contain arbitrary user content |

**Detection:** Compiled regular expressions (see `internal/sanitize/patterns.go`) covering patterns including:
- Instruction overrides (`ignore previous instructions`, `system prompt`, etc.)
| - Role assumption (`you are now`, `act as`, `pretend to be`)
- Data exfiltration patterns (`send to`, `transmit`, `POST to`)
- Formatting attacks (excessive whitespace, ANSI codes)
- Common jailbreak phrases

**Response on detection:** Field value replaced with
`"[REDACTED: potential prompt injection detected]"` and a log line emitted.
Sanitization is **always-on** with no configuration toggle.

---

## Database Layer

**Implementation:** `pkg/database/connection.go`, `pkg/database/queries.go`

### Connection

- **Driver:** `github.com/jackc/pgx/v5` with `pgxpool`
- **Credentials:** From `DATABASE_URL` environment variable (full PostgreSQL URI)
- **Pool config:**

| Setting | Default | Env var |
|---|---|---|
| Max connections | 20 | `DB_MAX_CONNECTIONS` |
| Idle timeout | 30s | `DB_IDLE_TIMEOUT` |
| Connect timeout | 2s | `DB_CONNECT_TIMEOUT` |

### Read-Only Enforcement

`pkg/database/queries.go:validateQuery()` rejects any query that:

- Does not start with `SELECT` or `WITH`
- Contains a statement separator (`;`)
- Starts with a mutation keyword: `INSERT`, `UPDATE`, `DELETE`, `DROP`, `CREATE`, `ALTER`,
  `TRUNCATE`, `GRANT`, `REVOKE`, `BEGIN`, `COMMIT`, `ROLLBACK`, and others

SQL injection protection is at the driver level via parameterized `$N` placeholders — the
validation layer is a defence-in-depth measure, not the primary injection defence.

### Primary Table

All resource queries target `search.resources`:

```sql
-- Schema (relevant columns)
search.resources (
  uid     TEXT,      -- Kubernetes resource UID
  cluster TEXT,      -- Cluster name
  data    JSONB      -- Full resource metadata as JSONB
)
```

Key JSONB fields accessed: `kind`, `apigroup`, `namespace`, `name`, `created`,
`_hubClusterResource`, `_hubClusterName`.

---

## Kubernetes Connectivity

**Implementation:** `internal/server/auth/kube_config.go`, `auth/k8s_validator.go`

### Service Account

The pod's ServiceAccount (`acm-mcp-server`) is granted exactly one ClusterRole permission:

```yaml
rules:
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews"]
  verbs:     ["create"]
```

The service account has **no read access** to any Kubernetes resource. All other K8s API
calls (SSAR, SSRR, UserPermission CRs) are made using the **end user's own bearer token**,
so the server never has access to cluster data beyond what the user themselves has.

### API Calls Made

| API | Auth used | Purpose |
|---|---|---|
| `POST /apis/authentication.k8s.io/v1/tokenreviews` | SA token | Validate user's bearer token; extract username/uid/groups |
| `POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews` | User's token | Check user's cluster-admin status; probe cluster-scoped resource access |
| `POST /apis/authorization.k8s.io/v1/selfsubjectrulesreviews` | User's token | Enumerate user's namespaced resource rules per namespace |
| `GET clusterview.open-cluster-management.io/v1alpha1/userpermissions` | User's token | Read managed-cluster cross-cluster permission CRs |
| `POST /apis/authentication.k8s.io/v1/tokenreviews` (second call) | SA token | Extract user UID for hub RBAC cache key |
| `GET /apis` (discovery) | User's token | Resource-to-Kind mapping (kubernetes discovery mode only) |

### TLS Configuration

| Setting | Value |
|---|---|
| Default | TLS verification enabled; CA loaded from `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt` |
| `rest.Config` path | `CAFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt` (all 5 config functions in `kube_config.go`) |
| `http.Client` path | `x509.CertPool` loaded from same CA file (`k8s_validator.go:NewKubernetesValidator`) |
| Override | `MCP_K8S_SKIP_TLS=true` (never in production — see SAR-04) |

### In-Cluster vs. Out-of-Cluster Detection

```
KUBERNETES_SERVICE_HOST env var set?
     ├── Yes → in-cluster mode
     │          API URL: https://kubernetes.default.svc:443
     │          Token:   /var/run/secrets/kubernetes.io/serviceaccount/token
     │          CA:      /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
     │          Auth auto-enabled
     │
     └── No → out-of-cluster / development mode
               MCP_K8S_URL + MCP_SA_TOKEN / MCP_KUBECONFIG overrides
               Auth auto-disabled (unless MCP_ENABLE_AUTH=true)
```

---

## Transport Layer

**Implementation:** `internal/server/transport.go`, `transport_http.go`, `transport_stdio.go`

### Transport Selection

| `MCP_TRANSPORT_MODE` | Behavior |
|---|---|
| `http` | HTTP server on `MCP_HTTP_HOST:MCP_HTTP_PORT` (default `0.0.0.0:8080`) |
| `stdio` | Read from stdin, write to stdout; streaming disabled |
| `auto` (default) | STDIO if stdin is a terminal; otherwise HTTP |

### HTTP Transport

**Bind address:** `MCP_HTTP_HOST:MCP_HTTP_PORT` (default `0.0.0.0:8080`)

**HTTP server timeouts:**

| Timeout | Value |
|---|---|
| ReadTimeout | `MCP_REQUEST_TIMEOUT` (30s) |
| WriteTimeout | `MCP_REQUEST_TIMEOUT` × 2 (60s) |
| IdleTimeout | 120s |
| ReadHeaderTimeout | 10s |
| MaxHeaderBytes | 1 MB |

**Middleware chain:**

```
Incoming request
     └── corsMiddleware
              └── authMiddleware
                       └── mux (route dispatch)
                                ├── /health  → healthHandler (no auth)
                                ├── /metrics → metricsHandler (auth)
                                └── /mcp     → mcpHandler (auth)
```

**MCP methods handled at `/mcp`:**

| JSON-RPC method | Handler | Notes |
|---|---|---|
| `initialize` | `handleInitialize` | Returns server name, version, capabilities |
| `notifications/initialized` | `handleNotificationsInitialized` | Fire-and-forget acknowledgement |
| `tools/list` | `handleToolsList` | Returns `["find_resources"]` for authenticated users |
| `tools/call` | `handleToolsCall` | Dispatches to `find_resources`; auth-checks tool name |

### STDIO Transport

- No authentication — `userCtx` is `nil`; no RBAC SQL filters applied
- All fleet data accessible to the process owner
- Streaming disabled
- Intended for Claude Desktop integration and local development

---

## Deployment Topology

### Kubernetes Resources (Helm Chart)

| Resource | Kind | Key Details |
|---|---|---|
| `acm-mcp-server` | `ServiceAccount` | `automountServiceAccountToken: true` |
| `acm-mcp-server-auth-role` | `ClusterRole` | `tokenreviews:create` only |
| `acm-mcp-server-auth-binding` | `ClusterRoleBinding` | Binds ClusterRole to ServiceAccount |
| `acm-mcp-server` | `Deployment` | 1 replica; non-root; read-only root filesystem; all caps dropped |
| `acm-mcp-server` | `Service` | `ClusterIP`; port 80 → 8080 |
| `acm-mcp-server` | `Route` | OpenShift; edge TLS; insecure redirect |
| `acm-search-mcp-secret` | `Secret` | `database-url` key; auto-discovered from ACM search-postgres |

### Container Security Profile

```yaml
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault     # Blocks dangerous syscalls (ptrace, mount, etc.)

containerSecurityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: [ALL]

volumes:
- name: tmp
  emptyDir: {}              # Only writable mount (/tmp for Go TLS runtime)
```

UID is not hardcoded — OpenShift assigns from the namespace SCC UID range
(`MustRunAsRange`). The Dockerfile sets `USER 1001` as a fallback for non-OpenShift
environments.

### Database Secret Auto-Discovery

At `helm install` time, the Helm template:
1. Reads the `MultiClusterHub` CR to locate the ACM operator namespace
2. Reads the `search-postgres` Secret from that namespace
3. Extracts `database-user`, `database-password`, `database-name`
4. Constructs `postgresql://<user>:<password>@search-postgres.<ns>.svc.cluster.local:5432/<db>`
5. Stores base64-encoded in `acm-search-mcp-secret.data.database-url`

**Failure scenarios:** If the `MultiClusterHub` CR is not found or the `search-postgres`
Secret does not exist in the ACM namespace, the constructed `DATABASE_URL` is empty. The
pod exits immediately at startup with a usage message (fail-safe; no silent misconfiguration).

Before running `helm install`, verify the prerequisites exist:

```bash
# Confirm MultiClusterHub CR is present
kubectl get multiclusterhub -A

# Confirm search-postgres secret exists in the ACM namespace
kubectl get secret search-postgres -n <acm-namespace>
```

If either is missing, ACM is not fully installed — resolve the ACM installation before
deploying the MCP server.

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | — (required) | PostgreSQL connection URI (credentials included) |
| `MCP_TRANSPORT_MODE` | `auto` | `auto`, `http`, or `stdio` |
| `MCP_HTTP_PORT` | `8080` | HTTP listen port |
| `MCP_HTTP_HOST` | `0.0.0.0` | HTTP bind address |
| `MCP_ENABLE_AUTH` | auto | `true`/`false`; auto-enabled when `KUBERNETES_SERVICE_HOST` is set |
| `MCP_AUTH_TIMEOUT` | `5s` | Timeout for K8s auth API calls |
| `MCP_AUTH_CACHE` | `true` | Cache validated tokens in memory |
| `MCP_AUTH_CACHE_TTL` | `5m` | Token cache TTL |
| `MCP_DISCOVERY_TTL` | `5m` | Hub RBAC and resource discovery cache TTL |
| `MCP_DISCOVERY_SOURCE` | `database` | `database` (production) or `kubernetes` (dev) |
| `MCP_K8S_SKIP_TLS` | `false` | Skip K8s API TLS verification — **never `true` in production** |
| `MCP_K8S_URL` | — | Override K8s API URL (testing only) |
| `MCP_SA_TOKEN` | — | Override SA token value (testing only) |
| `MCP_SA_TOKEN_PATH` | — | Override SA token file path (testing only) |
| `MCP_KUBECONFIG` | — | Use kubeconfig file (testing only) |
| `MCP_REQUEST_TIMEOUT` | `30s` | HTTP and DB query timeout |
| `MCP_STREAM_TIMEOUT` | `300s` | Streaming response timeout |
| `MCP_ENABLE_STREAMING` | `true` | Enable streaming for large result sets |
| `MCP_MAX_RESPONSE_SIZE` | `1000` | Max resources before streaming kicks in |
| `MCP_ENABLE_CORS` | `true` | Enable CORS headers |
| `MCP_ALLOWED_ORIGINS` | `*` | Comma-separated allowed CORS origins |
| `MCP_ENABLE_METRICS` | `true` | Enable `/metrics` endpoint |
| `MCP_ENABLE_HEALTH` | `true` | Enable `/health` endpoint |
| `LOG_LEVEL` | `info` | `info` or `debug` |
| `DB_MAX_CONNECTIONS` | `20` | PostgreSQL connection pool max size |
| `DB_IDLE_TIMEOUT` | `30` | Pool idle connection timeout (seconds) |
| `DB_CONNECT_TIMEOUT` | `2` | Connection attempt timeout (seconds) |
| `KUBERNETES_SERVICE_HOST` | (auto-injected) | K8s API host; presence triggers in-cluster mode and enables auth |
| `KUBERNETES_SERVICE_PORT` | (auto-injected) | K8s API port |

### Helm Values → Environment Variables

| `values.yaml` key | Env var | Default |
|---|---|---|
| `authentication.enabled` | `MCP_ENABLE_AUTH` | `true` |
| `authentication.skipTLS` | `MCP_K8S_SKIP_TLS` | `false` |
| `app.logLevel` | `LOG_LEVEL` | `info` |
| `app.displayName` | `APP_DISPLAY_NAME` | `MCP Server for Red Hat ACM` |
| `database.url` (via secret) | `DATABASE_URL` | auto-discovered |
| `service.targetPort` | `PORT` | `8080` |

---

## Security Properties

### Explicit Security Guarantees

| Property | Mechanism | Location |
|---|---|---|
| Authentication required for all data access | Auth middleware; HTTP 401 on missing/invalid token | `auth/middleware.go:63-70` |
| Authorization filters every SQL query | RBAC WHERE clause injected for every `find_resources` call | `findresources/core.go:applyAuthorizationFilters` |
| No privilege escalation beyond user's own RBAC | All K8s permission calls use user's token, not SA token | `auth/rbac_resolver.go`, `hub_rbac_client.go` |
| Token never stored plaintext in cache | Cache keyed by SHA-256 hash of raw header | `auth/middleware.go:183-185` |
| Cache isolation between concurrent requests | Deep clone returned per request; mutable fields zeroed before storage | `auth/middleware.go:190-205` |
| Database is read-only | SQL validation rejects mutation keywords; parameterized queries prevent injection | `pkg/database/queries.go:validateQuery` |
| Prompt injection defence | Regex sanitizer (see `internal/sanitize/patterns.go`) on all returned JSONB free-text fields | `internal/sanitize/` |
| Service account minimal privilege | Only `tokenreviews:create`; no data-read permissions | `helm/templates/rbac.yaml` |
| Container hardening | No capabilities; read-only filesystem; non-root; `RuntimeDefault` seccomp | `helm/acm-mcp-server/values.yaml` |
| TLS on K8s API connections | CA cert loaded from SA mount; `InsecureSkipVerify` requires explicit opt-in | `auth/k8s_validator.go:NewKubernetesValidator` |
| Credentials never logged | `DATABASE_URL` redacted at startup log; tokens never logged | `cmd/server/main.go:redactDatabaseURL` |

### Known Limitations and Trade-offs

| Item | Detail |
|---|---|
| STDIO transport has no auth | Intentional design for local/developer use. Process owner controls access. Any data the process can reach is accessible. |
| Hub RBAC cache is in-memory, per-pod | Cache is not shared across replicas. Multiple replicas will each build their own cache. Not a security issue — stale cache evicts on TTL. |
| `MCP_ALLOWED_ORIGINS: *` default | CORS wildcard is acceptable for an API server. Operators should restrict to known origins in locked-down environments. |
| PostgreSQL TLS optional | `DATABASE_URL` controls TLS via `sslmode` parameter. Default auto-discovered connection does not enforce `sslmode=require`. Consider enforcing in the secret template for production. |
| Streaming timeout | Long-running streams (`MCP_STREAM_TIMEOUT=300s`) hold HTTP connections. Consider reducing in high-connection environments. |

---

## Trust Boundaries

```
┌─────────────────────────────────────────────────────────────────────┐
│  UNTRUSTED                                                          │
│  • MCP client request content (Authorization header, tool args)    │
│  • All resource data returned from the database (sanitized)        │
└───────────────────────────┬─────────────────────────────────────────┘
                            │ trust boundary: token validated by K8s
┌───────────────────────────▼─────────────────────────────────────────┐
│  TRUSTED (post-authentication)                                      │
│  • Resolved user identity (username, uid, groups from TokenReview) │
│  • User's RBAC permissions (from K8s APIs using user's token)      │
└───────────────────────────┬─────────────────────────────────────────┘
                            │ trust boundary: RBAC-filtered parameterized SQL
┌───────────────────────────▼─────────────────────────────────────────┐
│  TRUSTED INFRASTRUCTURE                                             │
│  • PostgreSQL (search-postgres) — read-only access                 │
│  • Kubernetes API server — SA has only tokenreviews:create         │
│  • Service account token — auto-mounted, short-lived JWT           │
└─────────────────────────────────────────────────────────────────────┘
```

### Inbound Trust Decisions

| Input | Trust decision | Mechanism |
|---|---|---|
| Bearer token | Validated against K8s TokenReview | `k8s_validator.go:ValidateBearerToken` |
| Tool arguments (kind, namespace, etc.) | Validated and parameterized; never interpolated into SQL | `findresources/core.go`, `utils/sqlbuilder.go` |
| Resource data from DB | Sanitized for prompt injection before use | `sanitize/sanitize.go` |

---

## Sensitive Data Inventory

| Data | At-rest location | In-transit | Exposure controls |
|---|---|---|---|
| PostgreSQL password | `acm-search-mcp-secret` K8s Secret → `DATABASE_URL` env var | Encrypted via TLS (if `sslmode=require`) | Pod can read; startup log redacts it |
| Service account JWT | `/var/run/secrets/kubernetes.io/serviceaccount/token` | HTTPS to K8s API | Used only for `tokenreviews:create`; pod-local |
| User bearer tokens | HTTP `Authorization` header (in-flight only) | TLS at OpenShift Route | Never logged; cache key is SHA-256 hash |
| Resolved RBAC permissions | In-memory `HubRBACCache` (keyed by user UID) | Not transmitted | TTL-evicted; never persisted to disk |
| Token validation cache | In-memory `tokenCache` (SHA-256 keyed) | Not transmitted | TTL-evicted; deep-cloned per request |
| Resource JSONB data | `search.resources` PostgreSQL table | TLS (optional) | RBAC-filtered on read; sanitized before LLM |
