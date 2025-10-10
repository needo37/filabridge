# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies for CGO compilation
RUN apk add --no-cache git build-base

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite

# Create app directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Create directory for database
RUN mkdir -p /app/data

# Expose port
EXPOSE 5000

# Set environment variables
ENV GIN_MODE=release
ENV DB_FILE=/app/data/filabridge.db

# Run the application
CMD ["./main"]
