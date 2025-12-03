# ACM Search MCP Server Context

You have access to `find_resources` tool for querying Kubernetes resources across ACM-managed clusters in an OpenShift fleet.

## Core Usage

```bash
# Basic resource discovery
find_resources kind=Pod
find_resources kind=Node
find_resources kind=ManagedCluster

# Filtering
find_resources kind=Pod status="Failed,Error,CrashLoopBackOff"
find_resources kind=Pod cluster="cluster-name"
find_resources kind=Pod cluster="cluster-name" namespace="openshift-*"
find_resources labels="app=nginx,env=prod"

# Analysis modes
find_resources kind=Pod outputMode=health
find_resources kind=Pod outputMode=count groupBy=status
find_resources kind=Pod outputMode=summary 

# Text search (EXPENSIVE - use sparingly)
find_resources kind=Pod textSearch="OutOfMemory" limit=5
```

## Query Strategy

- **Specific issues**: Start narrow (`kind=Pod status="Failed" namespace="myapp"`)
- **Unknown problems**: Start broad (`find_resources kind=Pod outputMode=health`)
- **Data exploration**: Use samples (`find_resources kind=Pod limit=3`)

## Key Parameters

- `kind=` - Resource type (Pod, Node, Route, etc.)
- `status=` - Resource status (comma-separated: "Running,Failed")
- `cluster=` - Specific cluster name
- `namespace=` - Namespace (supports wildcards: "openshift-*")
- `labels=` - Label selectors ("key=value,key2=value2")
- `outputMode=` - Output format (health, count, summary)
- `groupBy=` - Group results (status, cluster, namespace)
- `limit=` - Limit results (use for large queries)
- `textSearch=` - Full-text search (expensive, use with other filters)

## Common Resource Types

**Workloads**: Pod, Deployment, Job, CronJob
**Infrastructure**: Node, PersistentVolume
**Networking**: Service, Route, Ingress
**ACM**: ManagedCluster, MultiClusterHub, Application
**OpenShift**: ClusterOperator, ClusterVersion

Each result includes: name, namespace, kind, cluster, age, and full JSON data.

## Best Practices

- Use `outputMode=health` for fleet overviews
- Use `limit=` for large queries to avoid token waste
- Avoid `textSearch=` unless specific filters don't work
- Leverage wildcards in namespace filtering
- Check the `data` column for detailed resource information