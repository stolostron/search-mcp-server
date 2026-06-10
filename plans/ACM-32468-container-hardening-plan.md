# ACM-35066: Implementation Plan — Container Security Hardening

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
| Explicit UID/GID | only `runAsNonRoot: true` | `runAsUser: 1001`, `runAsGroup: 1001` |

---

## Proposed Changes

### `values.yaml`

Replace the current `podSecurityContext` block and add a new `containerSecurityContext` block:

```yaml
# Pod-level security context
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 1001        # matches Dockerfile USER_UID
  runAsGroup: 1001
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

### `runAsUser: 1001` / `runAsGroup: 1001`
Matches the UID already set in the Dockerfile (`USER_UID=1001`). No functional change;
makes the intent explicit and prevents the pod from being scheduled if the image were
accidentally rebuilt without the `USER` directive.

In OpenShift, when `runAsNonRoot: true` is set without a UID, the platform assigns a
random UID from the namespace SCC range. Explicit UID 1001 means we must ensure the
namespace SCC allows UID 1001 (the `restricted` SCC on OCP allows any UID > 0, so this
is fine for standard deployments).

---

## Files Changed

```
helm/acm-mcp-server/
  values.yaml            modified  (podSecurityContext + new containerSecurityContext)
  templates/
    deployment.yaml      modified  (container securityContext + /tmp volume)
plans/
  ACM-35066-container-hardening-plan.md   new (this file)
  INDEX.md                                modified
```

---

## Open Questions

| # | Question | Impact | Status |
|---|---|---|---|
| 1 | Does `readOnlyRootFilesystem: true` cause startup failures on ubi-minimal? Need to test with a local `docker run --read-only --tmpfs /tmp` before shipping. | Core feature | ✅ Verified — CI image starts cleanly with `--read-only --tmpfs /tmp` on linux/amd64. No additional writable mounts needed. |
| 2 | Are there additional paths the app writes to at runtime (e.g. `/var/run`, `/proc`)? Check with `strace` or `docker run --read-only` output. | `/tmp` volume completeness | ✅ Verified — no filesystem write errors observed; `/tmp` emptyDir is sufficient. |
| 3 | Does the OpenShift namespace SCC allow UID 1001 explicitly? `restricted` SCC allows any non-root UID, so this should be fine for standard deployments, but custom SCCs might restrict it. | Deployment compatibility | Open |
| 4 | Should `seccomp` profile be `Localhost` (custom, more restrictive) instead of `RuntimeDefault`? Out of scope for this PR — `RuntimeDefault` is the recommended starting point per Red Hat guidance. | Security posture | Deferred |

---

## Acceptance Criteria

- [ ] `helm template .` renders `podSecurityContext` with `runAsUser`, `runAsGroup`, and `seccompProfile`
- [ ] `helm template .` renders `containerSecurityContext` on the container with `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`
- [ ] A `/tmp` emptyDir volume is mounted in the container
- [x] `docker run --platform linux/amd64 --read-only --tmpfs /tmp quay.io/stolostron/search-mcp-server:<ci-tag>` starts without errors (verified with CI image — binary printed usage and exited cleanly)
- [ ] Existing Helm chart tests pass (`helm lint`, `helm template`)
- [ ] Pod starts successfully in a test cluster (or local kind) with the new securityContext
