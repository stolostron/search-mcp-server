#!/bin/bash

# Find Resources Engine Integration Test Runner
# This script runs comprehensive integration tests using the Ginkgo BDD framework
# Tests are production data compatible and work with read-only database access

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 Find Resources Engine Integration Test Runner (Ginkgo BDD)${NC}"
echo "================================================================"

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}❌ Error: Must be run from the golang directory${NC}"
    echo "Usage: cd golang && ./test/run-integration-tests.sh"
    exit 1
fi

# Function to check if PostgreSQL is running
check_postgres() {
    local host=$1
    local port=$2
    local timeout=5

    if timeout $timeout bash -c "</dev/tcp/$host/$port" 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

# Function to start test database with Docker
start_test_db() {
    echo -e "${YELLOW}🐳 Starting test PostgreSQL container...${NC}"

    # Stop existing container if running
    docker stop postgres-find-resources-test 2>/dev/null || true
    docker rm postgres-find-resources-test 2>/dev/null || true

    # Start new container
    docker run -d --name postgres-find-resources-test \
        -e POSTGRES_PASSWORD=testpass \
        -e POSTGRES_DB=testdb \
        -e POSTGRES_USER=postgres \
        -p 5433:5432 \
        postgres:15 > /dev/null

    echo -e "${BLUE}⏳ Waiting for database to be ready...${NC}"

    # Wait for database to be ready
    local max_attempts=30
    local attempt=1

    while [ $attempt -le $max_attempts ]; do
        if check_postgres localhost 5433; then
            echo -e "${GREEN}✅ Database is ready!${NC}"
            break
        fi

        if [ $attempt -eq $max_attempts ]; then
            echo -e "${RED}❌ Database failed to start after ${max_attempts} seconds${NC}"
            exit 1
        fi

        echo -n "."
        sleep 1
        attempt=$((attempt + 1))
    done
}

# Function to run integration tests
run_tests() {
    local test_db_url=$1

    echo -e "${BLUE}🧪 Running Find Resources Integration Tests (Ginkgo BDD Framework)${NC}"
    echo "Database: $test_db_url"
    echo -e "${GREEN}✅ Production Data Compatible • Read-only Database Support${NC}"
    echo ""

    export TEST_DATABASE_URL="$test_db_url"

    # Run the tests with verbose output
    echo -e "${YELLOW}📋 Ginkgo BDD Test Suite (50 total specs):${NC}"
    echo "• Database Infrastructure (35 specs)"
    echo "  - Database Connection, Query Execution, Pool Monitoring"
    echo "• FindResources Engine (15 specs)"
    echo "  - Basic List Operations (3 specs)"
    echo "  - Count and Aggregation Operations (3 specs)"
    echo "  - Filtering Operations (6 specs)"
    echo "  - Complex Queries (1 spec)"
    echo "  - Age and Time Calculations (1 spec)"
    echo "  - Input Validation and Security (1 spec)"
    echo ""
    echo -e "${BLUE}🔍 Production Data Compatible:${NC}"
    echo "• Auto-discovers available data in database"
    echo "• Works with read-only database access"
    echo "• Adapts tests to existing resource types and clusters"
    echo ""

    # Run Ginkgo integration tests
    echo -e "${BLUE}🧪 Running Ginkgo BDD Tests...${NC}"
    if go test -v -tags=integration ./test/integration -timeout=60s; then
        echo ""
        echo -e "${GREEN}🎉 All Ginkgo integration specs passed!${NC}"
        echo -e "${GREEN}✅ 49-50 specs should have passed successfully${NC}"
        return 0
    else
        echo ""
        echo -e "${RED}💥 Some Ginkgo specs failed!${NC}"
        echo -e "${YELLOW}💡 Tip: Use -ginkgo.focus=\"SpecName\" to run specific tests${NC}"
        return 1
    fi
}

# Function to cleanup
cleanup() {
    if [ "$STARTED_DOCKER" = "true" ]; then
        echo -e "${YELLOW}🧹 Cleaning up test database...${NC}"
        docker stop postgres-find-resources-test 2>/dev/null || true
        docker rm postgres-find-resources-test 2>/dev/null || true
        echo -e "${GREEN}✅ Cleanup complete${NC}"
    fi
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Main execution
main() {
    echo -e "${BLUE}🔍 Checking for existing database connection...${NC}"

    # Check if user has provided a database URL
    if [ -n "$TEST_DATABASE_URL" ]; then
        echo -e "${GREEN}✅ Using provided TEST_DATABASE_URL${NC}"
        run_tests "$TEST_DATABASE_URL"
    elif [ -n "$DATABASE_URL" ]; then
        echo -e "${YELLOW}⚠️  Using DATABASE_URL (consider setting TEST_DATABASE_URL for testing)${NC}"
        run_tests "$DATABASE_URL"
    else
        # Check if default test database is available
        if check_postgres localhost 5433; then
            echo -e "${GREEN}✅ Found database at localhost:5433${NC}"
            run_tests "postgresql://postgres:testpass@localhost:5433/testdb"
        elif check_postgres localhost 5432; then
            echo -e "${YELLOW}⚠️  Using localhost:5432 (consider dedicated test database)${NC}"
            run_tests "postgresql://postgres:testpass@localhost:5432/testdb"
        else
            echo -e "${YELLOW}⚠️  No database found, starting Docker container...${NC}"

            # Check if Docker is available
            if ! command -v docker &> /dev/null; then
                echo -e "${RED}❌ Docker is not available. Please either:${NC}"
                echo "1. Install Docker and rerun this script"
                echo "2. Set TEST_DATABASE_URL to your PostgreSQL instance"
                echo "3. Start PostgreSQL manually on localhost:5433"
                exit 1
            fi

            start_test_db
            STARTED_DOCKER=true
            run_tests "postgresql://postgres:testpass@localhost:5433/testdb"
        fi
    fi
}

# Parse command line arguments
case "${1:-}" in
    --help|-h)
        echo "Find Resources Engine Integration Test Runner (Ginkgo BDD Framework)"
        echo ""
        echo "Runs 50 comprehensive integration specs using Ginkgo BDD framework:"
        echo "• 35 Database Infrastructure specs"
        echo "• 15 FindResources Engine specs"
        echo ""
        echo "Usage:"
        echo "  $0                    # Auto-detect or start test database"
        echo "  $0 --start-db         # Force start new Docker test database"
        echo "  $0 --help             # Show this help"
        echo ""
        echo "Environment Variables:"
        echo "  TEST_DATABASE_URL     # Primary test database URL"
        echo "  DATABASE_URL          # Fallback database URL"
        echo "  TEST_DB_VERBOSE       # Enable verbose database logging"
        echo ""
        echo "Examples:"
        echo "  # Use auto-detection with production database"
        echo "  ./test/run-integration-tests.sh"
        echo ""
        echo "  # Use custom production database (read-only compatible)"
        echo "  TEST_DATABASE_URL='postgresql://user:pass@host:5432/search' ./test/run-integration-tests.sh"
        echo ""
        echo "  # Force new Docker database for basic connectivity testing"
        echo "  ./test/run-integration-tests.sh --start-db"
        echo ""
        echo "  # Run with verbose database logging"
        echo "  TEST_DB_VERBOSE=true ./test/run-integration-tests.sh"
        echo ""
        echo "  # Run specific Ginkgo test groups (after starting script):"
        echo "  go test -v -tags=integration ./test/integration -ginkgo.focus=\"FindResources\""
        echo "  go test -v -tags=integration ./test/integration -ginkgo.focus=\"Database\""
        exit 0
        ;;
    --start-db)
        echo -e "${YELLOW}🐳 Force starting new Docker test database...${NC}"
        start_test_db
        STARTED_DOCKER=true
        run_tests "postgresql://postgres:testpass@localhost:5433/testdb"
        ;;
    "")
        main
        ;;
    *)
        echo -e "${RED}❌ Unknown option: $1${NC}"
        echo "Use --help for usage information"
        exit 1
        ;;
esac