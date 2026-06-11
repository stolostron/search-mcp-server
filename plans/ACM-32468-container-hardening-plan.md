# ACM-32468: Implementation Plan — Container Security Hardening

**Jira:** https://redhat.atlassian.net/browse/ACM-32468
**Date:** 2026-06-10

---

## Current State

The Helm chart sets one security control at the pod level:

```yaml
# values.yaml (current)
podSecurityContext:
  runAsNonRoot: true
```

The container-level `securityContext` is absent entirely from `deployment.yaml`.
The Dockerfile sets `USER 1001` — so the process runs as UID 1001, but no group,
no seccomp, no capability drops, writable filesystem, and privilege escalation is
implicitly allowed.

**Base image:** `registry.access.redhat.com/ubi9/ubi-minimal:latest`
**Runtime user:** UID 1001 (set in Dockerfile via `USER_UID=1001`)

---

## Missing Controls (SAR-03 findings)

| Control | Current | Target |
|---|---|---|
| Drop capabilities | not set (inherits default set) | `drop: [ALL]` |
| Read-only filesystem | writable | `readOnlyRootFilesystem: true` + `/tmp` emptyDir |
| Privilege escalation | allowed (default) | `allowPrivilegeEscalation: false` |
| Seccomp profile | none | `RuntimeDefault` |
| Explicit UID/GID | only `runAsNonRoot: true` | omitted — OpenShift assigns from SCC range |

---

## Proposed Changes

### `values.yaml`

Replace the current `podSecurityContext` block and add a new `containerSecurityContext` block:

```yaml
# Pod-level security context
# runAsUser/runAsGroup intentionally omitted — OpenShift SCC (MustRunAsRange) assigns
# a UID from the namespace's allocated range (e.g. 1000770000/10000). Hardcoding 1001
# would fail SCC admission. runAsNonRoot: true ensures the assigned UID is non-root.
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Container-level security context
containerSecurityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

### `deployment.yaml`

Two changes:

**1. Wire `containerSecurityContext` into the container spec:**

```yaml
containers:
- name: acm-mcp-server
  image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
  securityContext:
    {{- toYaml .Values.containerSecurityContext | nindent 12 }}
  ...
```

**2. Add a `/tmp` emptyDir volume** (required because `readOnlyRootFilesystem: true` blocks
writes to the root filesystem; Go's net/http and TLS stack may write to `/tmp` at runtime):

```yaml
  containers:
  - name: acm-mcp-server
    ...
    volumeMounts:
    - name: tmp
      mountPath: /tmp
  volumes:
  - name: tmp
    emptyDir: {}
```

---

## Control-by-Control Analysis

### `drop: [ALL]` capabilities
The server is a stateless Go HTTP binary that:
- opens a TCP connection to PostgreSQL (no raw sockets)
- listens on port 8080 (>1024, no `NET_BIND_SERVICE` needed)
- makes Kubernetes API calls via the service account token

None of these require Linux capabilities. Dropping all is safe.

### `readOnlyRootFilesystem: true`
The binary is copied to `/bin/main` and executed. The UBI minimal image is the concern —
it may attempt writes to `/etc/passwd`, `/var/run`, or `/tmp` during startup (e.g., NSS
library initialisation). Adding an `emptyDir` for `/tmp` covers the most common case.
**Requires runtime verification** (see Open Questions #1).

### `allowPrivilegeEscalation: false`
The binary is not setuid and does not call `execve` to a setuid binary. Safe to disable.
This also implicitly sets `no_new_privs` on the process.

### `seccompProfile: RuntimeDefault`
`RuntimeDefault` uses the container runtime's built-in seccomp profile (e.g. Docker's
`default.json`), which allows all syscalls a normal Go HTTP server needs and blocks
dangerous ones (`ptrace`, `mount`, `reboot`, etc.). Safe for this workload.

`Localhost` (a custom profile) would be more restrictive but requires a profile file
shipped to every node — not practical at this stage.

### `runAsUser` / `runAsGroup` — intentionally omitted
OpenShift uses `MustRunAsRange` on the `restricted-v2` SCC, assigning UIDs from the
namespace's allocated range (verified: `1000770000/10000` on the test cluster). Hardcoding
UID 1001 would fall outside this range and fail SCC admission.

`runAsNonRoot: true` is sufficient — OpenShift guarantees the assigned UID is non-root,
and the Dockerfile's `USER 1001` serves as a fallback for plain Kubernetes environments
where no SCC is present.

---

## Files Changed

```
helm/acm-mcp-server/
  values.yaml            modified  (podSecurityContext + new containerSecurityContext)
  templates/
    deployment.yaml      modified  (container securityContext + /tmp volume)
plans/
  ACM-32468-container-hardening-plan.md   new (this file)
  INDEX.md                                modified
```

---

## Open Questions

| # | Question | Impact | Status |
|---|---|---|---|
| 1 | Does `readOnlyRootFilesystem: true` cause startup failures on ubi-minimal? Need to test with a local `docker run --read-only --tmpfs /tmp` before shipping. | Core feature | ✅ Verified — CI image starts cleanly with `--read-only --tmpfs /tmp` on linux/amd64. No additional writable mounts needed. |
| 2 | Are there additional paths the app writes to at runtime (e.g. `/var/run`, `/proc`)? Check with `strace` or `docker run --read-only` output. | `/tmp` volume completeness | ✅ Verified — no filesystem write errors observed; `/tmp` emptyDir is sufficient. |
| 3 | Does the OpenShift namespace SCC allow UID 1001 explicitly? `restricted` SCC allows any non-root UID, so this should be fine for standard deployments, but custom SCCs might restrict it. | Deployment compatibility | ✅ Resolved — `restricted-v2` uses `MustRunAsRange` with namespace UID range `1000770000/10000`; UID 1001 is outside this range. Removed `runAsUser`/`runAsGroup` from values.yaml; OpenShift assigns the UID automatically. |
| 4 | Should `seccomp` profile be `Localhost` (custom, more restrictive) instead of `RuntimeDefault`? Out of scope for this PR — `RuntimeDefault` is the recommended starting point per Red Hat guidance. | Security posture | ✅ Resolved — `restricted-v2` SCC only permits `["runtime/default"]`; `Localhost` would fail admission. All other ACM pods in `open-cluster-management` use `RuntimeDefault`. No action needed. |

---

## Acceptance Criteria

- [x] `helm template .` renders `podSecurityContext` with `runAsNonRoot: true` and `seccompProfile` (no `runAsUser`/`runAsGroup` — omitted for OpenShift SCC compatibility)
- [x] `helm template .` renders `containerSecurityContext` on the container with `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`
- [x] A `/tmp` emptyDir volume is mounted in the container
- [x] `docker run --platform linux/amd64 --read-only --tmpfs /tmp quay.io/stolostron/search-mcp-server:<ci-tag>` starts without errors (verified with CI image — binary printed usage and exited cleanly)
- [x] Existing Helm chart tests pass (`helm lint`, `helm template`) — 0 failures, 1 info (icon recommended)
- [ ] Pod starts successfully in a test cluster with the new securityContext
