sessions:
---
- date: "2026-06-18"
  title: "[search-mcp] SAR-07: Create architecture documentation and data flow diagrams"
  jira: "ACM-35728"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35728"
  pr: ~
  plan: ~
  summary: "Authored docs/architecture.md covering component diagram, request lifecycle, authentication flow, RBAC/authorization, MCP tools, database layer, Kubernetes connectivity, deployment topology, full configuration reference, security properties, trust boundaries, and sensitive data inventory"
---
- date: "2026-06-15"
  title: "[search-mcp] SAR-09: Plan for creating and using a read-only PostgreSQL user"
  jira: "ACM-35503"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35503"
  pr: ~
  plan: "see search-v2-operator/plans/ACM-32474-readonly-postgres-user-plan.md"
  summary: "Plan search-v2-operator changes to provision dedicated read-only PostgreSQL roles for both search-v2-api and search-mcp-server, eliminating their use of the shared read-write searchuser credential"
---
- date: "2026-06-12"
  title: "[search-mcp] Hash token cache keys with SHA-256 in auth middleware"
  jira: "ACM-35364"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35364"
  pr: "https://github.com/stolostron/search-mcp-server/pull/59"
  plan: "plans/ACM-32471-token-cache-key-hashing-plan.md"
  summary: "Replace raw bearer tokens used as in-process cache keys with their SHA-256 hashes to eliminate plaintext credential exposure in heap memory"
- date: "2026-06-10"
  title: "[search-mcp] SAR-03: Create implementation plan for container security hardening"
  jira: "ACM-32468"
  jira_url: "https://redhat.atlassian.net/browse/ACM-32468"
  pr: "https://github.com/stolostron/search-mcp-server/pull/56"
  summary: "Plan the Helm chart changes to add full container security hardening: drop ALL capabilities, readOnlyRootFilesystem, allowPrivilegeEscalation=false, seccomp RuntimeDefault, explicit runAsUser/runAsGroup"
- date: "2026-06-08"
  title: "[search-mcp] SAR-01: Prompt injection mitigation (implemented)"
  jira: "ACM-32466"
  jira_url: "https://redhat.atlassian.net/browse/ACM-32466"
  pr: "https://github.com/stolostron/search-mcp-server/pull/55"
  plan: "plans/ACM-32466-prompt-injection-implementation-plan.md"
  summary: "Shipped internal/sanitize/ (always-redact regexes) wired into processListMode, processCountMode, and processHealthMode; MCP_SANITIZATION_MODE and integration tests deferred"
