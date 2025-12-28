# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /opamp-control-plane ./cmd/server

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /opamp-control-plane /app/opamp-control-plane

# Copy default config
COPY configs/server.yaml /app/configs/server.yaml

# Create data directory
RUN mkdir -p /app/data

# Create non-root user
RUN adduser -D -u 1000 opamp
RUN chown -R opamp:opamp /app
USER opamp

EXPOSE 8080 4320

ENTRYPOINT ["/app/opamp-control-plane"]
CMD ["-config", "/app/configs/server.yaml"]
