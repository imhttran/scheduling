# Multi-stage build for Go backend
# Stage 1: Build
FROM golang:1.27-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o scheduling .

# Stage 2: Runtime
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/scheduling .

# Create non-root user
RUN addgroup app && adduser -D -S -G app app
USER app

# Expose port (default port for your app, adjust if needed)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/scheduling", "-health"] || exit 1

CMD ["/app/scheduling"]
