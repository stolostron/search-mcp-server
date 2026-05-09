# Multi-stage build for ACM MCP Server (Go)
FROM golang:1.25 AS builder

WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with memory optimizations for cross-compilation
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" \
    -o bin/acm-mcp-server-go cmd/server/main.go

# Final stage - minimal UBI image following OpenShift best practices
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /app

# Copy the compiled binary from builder stage
COPY --from=builder /app/bin/acm-mcp-server-go .

# Expose port (will be configurable via environment)
EXPOSE 8080

# Let OpenShift handle user assignment - don't force specific UID
CMD ["./acm-mcp-server-go"]