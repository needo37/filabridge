# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies for CGO compilation
RUN apk add --no-cache git build-base

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies with cache mount
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy templates directory separately for better layer caching
COPY templates/ ./templates/

# Copy static files directory
COPY static/ ./static/

# Copy source code
COPY *.go ./

# Build the application with cache mounts
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install runtime dependencies
# Using --no-scripts to work around Alpine 3.23 trigger script issues with QEMU emulation on arm64
RUN apk update && apk --no-cache --no-scripts add ca-certificates sqlite

# Create app directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Copy static files from builder stage
COPY --from=builder /app/static ./static

# Create directory for database
RUN mkdir -p /app/data

# Expose port
EXPOSE 5000

# Set environment variables
ENV GIN_MODE=release
ENV FILABRIDGE_DB_PATH=/app/data

# Run the application
CMD ["./main"]
