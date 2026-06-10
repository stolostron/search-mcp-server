sessions:
- date: "2026-06-10"
  title: "[search-mcp] SAR-03: Create implementation plan for container security hardening"
  jira: "ACM-32468"
  jira_url: "https://redhat.atlassian.net/browse/ACM-32468"
  pr: "https://github.com/stolostron/search-mcp-server/pull/56"
  summary: "Plan the Helm chart changes to add full container security hardening: drop ALL capabilities, readOnlyRootFilesystem, allowPrivilegeEscalation=false, seccomp RuntimeDefault, explicit runAsUser/runAsGroup"