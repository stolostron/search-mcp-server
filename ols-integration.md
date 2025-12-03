# OpenShift Lightspeed Integration with Search MCP Server

This guide explains how to configure OpenShift Lightspeed (OLS) to use the Search MCP Server for enhanced cluster insights and resource management capabilities.

## Prerequisites

- OpenShift Lightspeed (OLS) deployed and running in your ACM Hub cluster


## Integration Steps

### Step 1: Deploy Search MCP Server

Deploy the Search MCP Server into the cluster is running by following the deployment instructions in the [Quick Start section](./README.md#quick-start).

### Step 2: Configure OLSConfig Custom Resource

Update your OLS configuration to include the Search MCP Server by adding the following section to your `OLSConfig` Custom Resource:

```yaml
  mcpServers:
    - name: acm
      streamableHTTP:
        enableSSE: true
        headers:
          kubernetes-authorization: kubernetes
        sseReadTimeout: 10
        timeout: 30
        url: 'http://acm-search-mcp-server-service.acm-search.svc.cluster.local/sse'
```

**Important Notes:**
- If you already have existing `mcpServers` configured in your OLSConfig, add the above configuration to the existing list rather than replacing it
- Use the exact configuration shown above verbatim for proper functionality
- The service URL assumes the Search MCP Server is deployed in the `acm-search` namespace (default)

#### Example: Adding to Existing Configuration

If your OLSConfig already has MCP servers configured:

```yaml
apiVersion: ols.openshift.io/v1alpha1
kind: OLSConfig
metadata:
  name: cluster
spec:
  # ... existing configuration ...
  mcpServers:
    - name: existing-server
      # ... existing server config ...
    - name: acm
      streamableHTTP:
        enableSSE: true
        headers:
          kubernetes-authorization: kubernetes
        sseReadTimeout: 10
        timeout: 30
        url: 'http://acm-search-mcp-server-service.acm-search.svc.cluster.local/sse'
```

#### Example: New Configuration

If this is your first MCP server:

```yaml
apiVersion: ols.openshift.io/v1alpha1
kind: OLSConfig
metadata:
  name: cluster
spec:
  # ... existing configuration ...
  mcpServers:
    - name: acm
      streamableHTTP:
        enableSSE: true
        headers:
          kubernetes-authorization: kubernetes
        sseReadTimeout: 10
        timeout: 30
        url: 'http://acm-search-mcp-server-service.acm-search.svc.cluster.local/sse'
```

### Step 3: Security Hardening (Optional)

If the Search MCP Server is only accessed by OLS within the cluster and not needed externally, you can remove the predefined route for enhanced security:

```bash
# Delete the external route (if it exists)
oc delete route acm-search-mcp-server-route -n acm-search
```

This ensures the MCP server is only accessible internally via the service URL and not exposed externally.

## Verification

After applying the configuration:

1. Verify the OLSConfig is updated:
   ```bash
   oc get olsconfig cluster -o yaml
   ```

2. Check that OLS pods restart and pick up the new configuration:
   ```bash
   oc get pods -n openshift-lightspeed
   ```

3. Open the OLS console and make a simple query like:
  - `how many clusters are in the fleet` 
  - and you should see a tool called `find_resources` being used for answering the question.

## Capabilities

Once integrated, OLS will have access to:
- Query ACM Search for resource information across managed clusters
- Find and analyze Kubernetes resources with advanced filtering
- Perform fleet-wide resource discovery and analysis

