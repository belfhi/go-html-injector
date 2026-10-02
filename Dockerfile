# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /html-injector .

# Final stage
FROM alpine:3.24

# Install CA certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

# Create a non-root user
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /html-injector .

# Switch to non-root user
USER app

# Expose the proxy port
EXPOSE 8080

# Run the proxy
ENTRYPOINT ["./html-injector"]