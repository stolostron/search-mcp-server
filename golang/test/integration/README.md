# Integration Tests for Find Resources Engine

This directory contains comprehensive integration tests using the **Ginkgo BDD framework** that test the complete Find Resources Engine functionality end-to-end with a real PostgreSQL database.

## Test Framework

The integration tests use **Ginkgo v2** for better organization and reporting:
- **58 total specs** across all integration test suites
- **BDD-style** organization with `Describe` and `It` blocks
- **Automatic test discovery** and parallel execution
- **Comprehensive reporting** with detailed failure information

## Test Coverage

### **📋 Database Infrastructure (35 specs)**
- **Database Connection** - Connection pooling, timeouts, health checks
- **Query Execution** - Parameterized queries, limits, error handling
- **Pool Monitoring** - Connection statistics, utilization, stress testing
- **Table Operations** - Schema discovery, table data, size calculation

### **🔍 FindResources Engine (23 specs)**

#### **Core Functionality**
- ✅ **Basic List Query** - Resource listing with dynamic data discovery
- ✅ **Count Mode** - Resource counting with groupBy functionality
- ✅ **Summary Mode** - Fleet overview with aggregated statistics
- ✅ **Health Mode** - Health analysis with status categorization

#### **Advanced Filtering**
- ✅ **Kind Filter** - Single and multiple kind filtering
- ✅ **Namespace Filter** - Namespace-specific queries
- ✅ **Cluster Filter** - Cluster-specific resource queries
- ✅ **Status Filter** - Status-based filtering with kind-aware logic
- ✅ **Text Search** - Free-text search across resource data
- ✅ **Label Selector** - Kubernetes-style label filtering
- ✅ **Cluster Selector** - Cluster label filtering
- ✅ **Time Filters** - Age-based filtering (newer/older than)
- ✅ **Count Only Mode** - Minimal count-focused results
- ✅ **Complex Queries** - Multiple advanced filters combined

#### **Features & Security**
- ✅ **Sorting & Limiting** - Result ordering and pagination
- ✅ **Age Calculation** - Human-readable resource age formats
- ✅ **Input Validation** - Invalid argument handling
- ✅ **SQL Injection Protection** - Security testing with malicious inputs

## Database Requirements

### **Production Database Compatible**
The tests work with **existing production databases**:
- ✅ **Read-only access** - No write permissions required
- ✅ **Real data testing** - Uses actual ACM search database resources
- ✅ **Schema compatibility** - Works with `search.resources` and `search.edges` tables
- ✅ **Dynamic discovery** - Adapts to whatever data exists in the database

### **Required Schema**
```sql
-- Existing ACM search database schema
CREATE SCHEMA search;
CREATE TABLE search.resources (
    uid TEXT PRIMARY KEY,           -- Unique resource ID
    cluster TEXT NOT NULL,          -- Cluster name
    data JSONB NOT NULL             -- Full Kubernetes resource JSON
);
CREATE TABLE search.edges (
    -- Optional: relationship data for advanced testing
);
```

## Running the Tests

### **1. Quick Test (Auto-detection)**
```bash
# Run all 50 integration tests with Ginkgo
go test -v -tags=integration ./test/integration

# Short mode skips all integration tests
go test -short ./test/integration

# With verbose database logging
TEST_DB_VERBOSE=true go test -v -tags=integration ./test/integration
```

### **2. Production Database**
```bash
# Test against real ACM search database
export DATABASE_URL="postgresql://user:pass@acm-hub:5432/search"
go test -v -tags=integration ./test/integration
```

### **3. Local Development Database**
```bash
# Set your development database URL
export DATABASE_URL="postgresql://postgres:pgadmin1234@localhost:5432/search"
go test -v -tags=integration ./test/integration
```

### **4. Docker Test Database**
```bash
# Start empty PostgreSQL for basic connectivity testing
docker run -d --name postgres-test \
  -e POSTGRES_PASSWORD=testpass \
  -e POSTGRES_DB=search \
  -p 5433:5432 postgres:15

export DATABASE_URL="postgresql://postgres:testpass@localhost:5433/search"
go test -v -tags=integration ./test/integration

# Cleanup
docker stop postgres-test && docker rm postgres-test
```

### **5. Specific Test Groups**
```bash
# Test only FindResources functionality
go test -v -tags=integration ./test/integration -ginkgo.focus="FindResources"

# Test only database infrastructure
go test -v -tags=integration ./test/integration -ginkgo.focus="Database"

# Test only basic operations
go test -v -tags=integration ./test/integration -ginkgo.focus="Basic.*Operations"

# Run with parallel execution
go test -v -tags=integration ./test/integration -ginkgo.procs=4
```

## Test Data Strategy

### **🔄 Production Data Compatible**
The tests **discover and adapt** to existing data:

```bash
# Example with real ACM data:
# Discovered data: 6730 total resources, 10 kinds, 3 clusters, 10 namespaces

# Tests automatically use:
# - Most common resource kinds for testing
# - Available clusters and namespaces
# - Real resource statuses and ages
# - Actual data structure and relationships
```

### **📊 Data Discovery Process**
1. **Query total resources** in `search.resources`
2. **Discover available kinds** (Pod, Service, Deployment, etc.)
3. **Find active clusters** across the fleet
4. **Identify namespaces** with resources
5. **Adapt test cases** to use discovered data
6. **Skip tests gracefully** if insufficient data

### **💡 Benefits**
- **Realistic testing** with actual production data patterns
- **No test data contamination** of production systems
- **Read-only compatible** - works with restricted database access
- **Scales automatically** - tests adapt to large or small datasets

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DATABASE_URL` | Primary database connection string | None |
| `DB_CONNECTION_STRING` | Alternative database URL | None |
| `POSTGRES_CONNECTION_STRING` | PostgreSQL-specific URL | None |
| `TEST_DATABASE_URL` | Test-specific override | None |
| `TEST_DB_VERBOSE` | Enable verbose database logging | `false` |

**Auto-detection order**: `DATABASE_URL` → `DB_CONNECTION_STRING` → `POSTGRES_CONNECTION_STRING` → `TEST_DATABASE_URL` → default

## Troubleshooting

### **Connection Issues**
```bash
# Test database connectivity
psql $DATABASE_URL -c "SELECT COUNT(*) FROM search.resources"

# Check if database accepts connections
nc -zv hostname 5432

# Verify schema exists
psql $DATABASE_URL -c "\dt search.*"
```

### **Permission Issues**
The tests only require **read permissions**:
```sql
-- Minimum required permissions
GRANT USAGE ON SCHEMA search TO test_user;
GRANT SELECT ON search.resources TO test_user;
GRANT SELECT ON search.edges TO test_user; -- Optional
```

### **Test Failures**
```bash
# Run with detailed Ginkgo reporting
go test -v -tags=integration ./test/integration -ginkgo.verbose

# Run with database debug logging
TEST_DB_VERBOSE=true go test -v -tags=integration ./test/integration

# Focus on failing tests
go test -v -tags=integration ./test/integration -ginkgo.focus="failing.*test.*name"

# Check for race conditions
go test -race -v -tags=integration ./test/integration
```

### **Data Issues**
```bash
# Verify test data is available
psql $DATABASE_URL -c "
SELECT
  COUNT(*) as total_resources,
  COUNT(DISTINCT data->>'kind') as unique_kinds,
  COUNT(DISTINCT cluster) as clusters
FROM search.resources;"
```

## Integration with CI/CD

### **GitHub Actions Example**
```yaml
name: Integration Tests
on: [push, pull_request]

jobs:
  integration-tests:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_PASSWORD: testpass
          POSTGRES_DB: search
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run Integration Tests
        env:
          DATABASE_URL: postgresql://postgres:testpass@localhost:5432/search
        run: |
          cd golang
          go test -v -tags=integration ./test/integration
```

## Test Results Example

```bash
=== RUN   TestIntegration
Running Suite: Integration Suite - /golang/test/integration
================================================================
Random Seed: 1768841993

Will run 58 of 58 specs

[Database Integration Tests]
  Database Connection
    ✓ should successfully test connection
    ✓ should retrieve database information
    ✓ should execute basic queries
    # ... (35 database infrastructure specs)

[FindResources Integration Tests]
  Basic List Operations
    ✓ should execute basic list query with discovered data
    ✓ should handle empty result sets gracefully
    ✓ should handle sorting and limiting
  Count and Aggregation Operations
    ✓ should execute count mode with status grouping
    ✓ should execute summary mode
    ✓ should execute health mode
  Filtering Operations
    ✓ should filter by namespace
    ✓ should filter by multiple kinds
    ✓ should filter by cluster
    ✓ should filter by status
    ✓ should handle text search
  Complex Queries
    ✓ should execute complex query with multiple filters
  Age and Time Calculations
    ✓ should calculate resource ages correctly
  Input Validation and Security
    ✓ should validate invalid arguments
    ✓ should protect against SQL injection

Ran 57 of 58 Specs in 0.462 seconds
SUCCESS! -- 57 Passed | 0 Failed | 0 Pending | 1 Skipped

--- PASS: TestIntegration (0.46s)
PASS
ok  	github.com/stolostron/search-mcp-server/test/integration	1.200s
```

This comprehensive integration test suite provides confidence for production deployment by testing against real database operations with actual ACM search data patterns.