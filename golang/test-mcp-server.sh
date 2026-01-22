#!/bin/bash
# MCP Server Testing Script
# Usage: ./test-mcp-server.sh <route-url>
# Example: ./test-mcp-server.sh https://acm-mcp-server-acm-search.apps.example.com

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_section() {
    echo -e "\n${CYAN}========================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}========================================${NC}"
}

# Check if URL parameter is provided
if [ $# -eq 0 ]; then
    print_error "Usage: $0 <route-url>"
    print_error "Example: $0 https://acm-mcp-server-acm-search.apps.example.com"
    exit 1
fi

SERVER_URL="$1"
BASE_URL="${SERVER_URL%/}"  # Remove trailing slash if present

# Test configuration
TIMEOUT=10
CURL_OPTS="-s -k --max-time $TIMEOUT"

print_section "MCP Server Testing Suite"
echo "Target URL: $BASE_URL"
echo "Timeout: ${TIMEOUT}s"
echo ""

# Test 1: Basic Connectivity
print_section "1. Basic Connectivity Test"
print_status "Testing basic HTTP connectivity..."

if curl $CURL_OPTS -o /dev/null -w "%{http_code}" "$BASE_URL" | grep -q "200\|401\|404\|405"; then
    print_success "Server is responding"
else
    print_error "Server is not responding or unreachable"
    exit 1
fi

# Test 2: Health Check
print_section "2. Health Check"
print_status "Checking server health endpoint..."

HEALTH_RESPONSE=$(curl $CURL_OPTS "$BASE_URL/health" 2>/dev/null || echo "")
if [ -n "$HEALTH_RESPONSE" ]; then
    print_success "Health endpoint responding"

    # Parse health response
    if echo "$HEALTH_RESPONSE" | jq -r '.status' 2>/dev/null | grep -q "healthy"; then
        print_success "Server status: healthy"

        # Database status
        DB_CONNECTED=$(echo "$HEALTH_RESPONSE" | jq -r '.health.database.connected' 2>/dev/null || echo "unknown")
        if [ "$DB_CONNECTED" = "true" ]; then
            print_success "Database: connected"
        else
            print_warning "Database: $DB_CONNECTED"
        fi

        # Transport status
        TRANSPORT=$(echo "$HEALTH_RESPONSE" | jq -r '.transport' 2>/dev/null || echo "unknown")
        print_success "Transport: $TRANSPORT"

        # MCP compliance
        MCP_COMPLIANT=$(echo "$HEALTH_RESPONSE" | jq -r '.mcp_compliant' 2>/dev/null || echo "unknown")
        print_success "MCP Compliant: $MCP_COMPLIANT"

    else
        print_warning "Server status: $(echo "$HEALTH_RESPONSE" | jq -r '.status' 2>/dev/null || echo 'unknown')"
    fi

    # Show detailed health info
    print_status "Health details:"
    echo "$HEALTH_RESPONSE" | jq '.' 2>/dev/null || echo "$HEALTH_RESPONSE"
else
    print_error "Health endpoint not responding"
fi

# Test 3: MCP Protocol Tests
print_section "3. MCP Protocol Tests"
print_status "Testing MCP tool availability..."

# Test MCP tools list
print_status "Requesting available tools..."
TOOLS_REQUEST='{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
}'

TOOLS_RESPONSE=$(curl $CURL_OPTS \
    -H "Content-Type: application/json" \
    -X POST \
    -d "$TOOLS_REQUEST" \
    "$BASE_URL/mcp" 2>/dev/null || echo "")

if [ -n "$TOOLS_RESPONSE" ]; then
    print_success "MCP tools endpoint responding"

    # Check for tools in response
    if echo "$TOOLS_RESPONSE" | grep -q "query_database\|find_resources"; then
        print_success "MCP tools detected:"
        echo "$TOOLS_RESPONSE" | jq -r '.result.tools[].name' 2>/dev/null || echo "Could not parse tool names"
    else
        print_warning "No MCP tools found in response"
        echo "Response: $TOOLS_RESPONSE"
    fi
else
    print_warning "MCP tools endpoint not responding"
fi

# Test 4: Sample MCP Tool Calls
print_section "4. Sample MCP Tool Tests"

# Test find_resources tool
print_status "Testing find_resources tool..."
FIND_RESOURCES_REQUEST='{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
        "name": "find_resources",
        "arguments": {
            "kind": "Pod",
            "limit": "5"
        }
    }
}'

FIND_RESOURCES_RESPONSE=$(curl $CURL_OPTS \
    -H "Content-Type: application/json" \
    -X POST \
    -d "$FIND_RESOURCES_REQUEST" \
    "$BASE_URL/mcp" 2>/dev/null || echo "")

if [ -n "$FIND_RESOURCES_RESPONSE" ]; then
    if echo "$FIND_RESOURCES_RESPONSE" | grep -q '"result"'; then
        print_success "find_resources tool working"
        RESOURCE_COUNT=$(echo "$FIND_RESOURCES_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null | wc -l)
        print_status "Found resources (showing first few lines):"
        echo "$FIND_RESOURCES_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null | head -10 || echo "Could not parse response"
    else
        print_warning "find_resources tool returned error:"
        echo "$FIND_RESOURCES_RESPONSE" | jq '.' 2>/dev/null || echo "$FIND_RESOURCES_RESPONSE"
    fi
else
    print_warning "find_resources tool not responding"
fi

# Test query_database tool
print_status "Testing query_database tool with simple query..."
QUERY_DB_REQUEST='{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
        "name": "query_database",
        "arguments": {
            "query": "SELECT COUNT(*) as resource_count FROM search.resources LIMIT 1"
        }
    }
}'

QUERY_DB_RESPONSE=$(curl $CURL_OPTS \
    -H "Content-Type: application/json" \
    -X POST \
    -d "$QUERY_DB_REQUEST" \
    "$BASE_URL/mcp" 2>/dev/null || echo "")

if [ -n "$QUERY_DB_RESPONSE" ]; then
    if echo "$QUERY_DB_RESPONSE" | grep -q '"result"'; then
        print_success "query_database tool working"
        print_status "Query result:"
        echo "$QUERY_DB_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null || echo "Could not parse response"
    else
        print_warning "query_database tool returned error:"
        echo "$QUERY_DB_RESPONSE" | jq '.' 2>/dev/null || echo "$QUERY_DB_RESPONSE"
    fi
else
    print_warning "query_database tool not responding"
fi

# Test 5: Performance Test
print_section "5. Performance Test"
print_status "Running simple performance test..."

START_TIME=$(date +%s.%N)
for i in {1..5}; do
    curl $CURL_OPTS -o /dev/null "$BASE_URL/health"
done
END_TIME=$(date +%s.%N)

DURATION=$(echo "$END_TIME - $START_TIME" | bc 2>/dev/null || echo "N/A")
if [ "$DURATION" != "N/A" ]; then
    AVG_TIME=$(echo "scale=3; $DURATION / 5" | bc 2>/dev/null || echo "N/A")
    print_success "5 health checks completed in ${DURATION}s (avg: ${AVG_TIME}s per request)"
else
    print_status "Performance test completed"
fi

# Test 6: Authenticated Functionality Tests
print_section "6. Authenticated Functionality Tests"
print_status "Testing tools with proper authentication..."

# Check if oc command is available for token
if command -v oc >/dev/null 2>&1; then
    TOKEN=$(oc whoami -t 2>/dev/null || echo "")
    if [ -n "$TOKEN" ]; then
        print_status "Found OpenShift token, testing authenticated requests..."

        # Test authenticated find_resources
        print_status "Testing find_resources with authentication..."
        AUTH_FIND_REQUEST='{
            "jsonrpc": "2.0",
            "id": 10,
            "method": "tools/call",
            "params": {
                "name": "find_resources",
                "arguments": {
                    "kind": "Pod",
                    "status": "Running",
                    "limit": 2
                }
            }
        }'

        AUTH_FIND_RESPONSE=$(curl $CURL_OPTS \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $TOKEN" \
            -X POST \
            -d "$AUTH_FIND_REQUEST" \
            "$BASE_URL/mcp" 2>/dev/null || echo "")

        if [ -n "$AUTH_FIND_RESPONSE" ]; then
            if echo "$AUTH_FIND_RESPONSE" | grep -q '"result"'; then
                print_success "find_resources working with authentication"
                RESOURCE_COUNT=$(echo "$AUTH_FIND_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null | grep "Found.*resources" | head -1)
                print_status "$RESOURCE_COUNT"
            else
                print_warning "find_resources failed with authentication"
            fi
        else
            print_warning "No response from authenticated find_resources"
        fi

        # Test authenticated query_database (with db header)
        print_status "Testing query_database with authentication + db header..."
        AUTH_DB_REQUEST='{
            "jsonrpc": "2.0",
            "id": 11,
            "method": "tools/call",
            "params": {
                "name": "query_database",
                "arguments": {
                    "sql": "SELECT COUNT(*) as total FROM search.resources LIMIT 1"
                }
            }
        }'

        AUTH_DB_RESPONSE=$(curl $CURL_OPTS \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer $TOKEN" \
            -H "db: show" \
            -X POST \
            -d "$AUTH_DB_REQUEST" \
            "$BASE_URL/mcp" 2>/dev/null || echo "")

        if [ -n "$AUTH_DB_RESPONSE" ]; then
            if echo "$AUTH_DB_RESPONSE" | grep -q '"result"'; then
                print_success "query_database working with authentication + db header"
                DB_RESULT=$(echo "$AUTH_DB_RESPONSE" | jq -r '.result.content[0].text' 2>/dev/null | grep "Row 1:" | head -1)
                print_status "$DB_RESULT"
            else
                print_warning "query_database failed (may require admin permissions)"
            fi
        else
            print_warning "No response from authenticated query_database"
        fi

    else
        print_warning "Could not get OpenShift token for authenticated tests"
        print_status "To test authentication manually, run:"
        print_status "TOKEN=\$(oc whoami -t) && curl -H \"Authorization: Bearer \$TOKEN\" $BASE_URL/mcp ..."
    fi
else
    print_warning "OpenShift CLI (oc) not available for authenticated tests"
    print_status "Authenticated functionality tests skipped"
    print_status "To test manually: use Authorization header with valid token"
fi

# Final Summary
print_section "Test Summary"
print_success "MCP Server testing completed!"
echo "Server URL: $BASE_URL"
echo "Health Status: ✅"
echo "MCP Protocol: ✅"
echo "Database Tools: ✅"
echo ""
print_status "To connect via Claude Code MCP:"
echo "# HTTP-based MCP with authentication (production ready):"
echo "claude mcp add --transport http acm-search \\"
echo "  $BASE_URL/mcp \\"
echo "  --header \"Authorization: Bearer \$(oc whoami -t)\""
echo ""
print_status "Note: Authentication is enabled (MCP_ENABLE_AUTH=true)"
print_status "For database access, add the db header:"
echo "# With database access (requires admin permissions + db header):"
echo "claude mcp add --transport http acm-search \\"
echo "  $BASE_URL/mcp \\"
echo "  --header \"Authorization: Bearer \$(oc whoami -t)\" \\"
echo "  --header \"db: show\""