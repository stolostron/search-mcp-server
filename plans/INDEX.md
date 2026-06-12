sessions:
---
- date: "2026-06-12"
  title: "[search-mcp] Hash token cache keys with SHA-256 in auth middleware"
  jira: "ACM-35364"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35364"
  pr: ~
  plan: "plans/ACM-32471-token-cache-key-hashing-plan.md"
  summary: "Replace raw bearer tokens used as in-process cache keys with their SHA-256 hashes to eliminate plaintext credential exposure in heap memory"
- date: "2026-06-10"
  title: "[search-mcp] SAR-03: Create implementation plan for container security hardening"
  jira: "ACM-32468"
  jira_url: "https://redhat.atlassian.net/browse/ACM-32468"
  pr: "https://github.com/stolostron/search-mcp-server/pull/56"
  summary: "Plan the Helm chart changes to add full container security hardening: drop ALL capabilities, readOnlyRootFilesystem, allowPrivilegeEscalation=false, seccomp RuntimeDefault, explicit runAsUser/runAsGroup"