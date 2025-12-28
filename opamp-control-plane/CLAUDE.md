# CLAUDE.md - OpAMP Control Plane

## Project Overview

This is an OpAMP (Open Agent Management Protocol) control plane that manages OpenTelemetry Collector configurations using GitOps principles. It's written in Go and uses Git as the source of truth for agent configurations.

## Build Commands

```bash
# Build the server
go build -o opamp-control-plane ./cmd/server

# Run tests
go test ./...

# Run with verbose output
go test -v ./...

# Run the server
./opamp-control-plane -config configs/server.yaml

# Or run directly with Go
go run ./cmd/server -config configs/server.yaml
```

## Project Structure

- `cmd/server/` - Main entry point
- `internal/opamp/` - OpAMP WebSocket server using opamp-go
- `internal/registry/` - Agent storage with SQLite implementation
- `internal/gitsync/` - Git repository synchronization
- `internal/config/` - Config resolution, merging, and validation
- `internal/api/` - REST API handlers
- `pkg/models/` - Shared data types
- `deploy/kubernetes/` - Kubernetes manifests
- `example-configs/` - Example OTel Collector configurations

## Key Dependencies

- `open-telemetry/opamp-go` - OpAMP protocol implementation
- `go-git/go-git/v5` - Git operations
- `modernc.org/sqlite` - Pure Go SQLite driver
- `gopkg.in/yaml.v3` - YAML parsing

## Testing the Server

```bash
# Start the server
go run ./cmd/server

# In another terminal, check health
curl http://localhost:8080/health

# List agents
curl http://localhost:8080/api/v1/agents

# Trigger git sync
curl -X POST http://localhost:8080/api/v1/sync
```

## Docker Commands

```bash
# Build image
docker build -t opamp-control-plane .

# Run with docker-compose
docker-compose up -d

# View logs
docker-compose logs -f control-plane
```

## Architecture Notes

1. **OpAMP Server** - Uses WebSocket for persistent connections with agents
2. **Config Resolution** - Label-based matching (first match wins from _selectors.yaml)
3. **Config Merging** - Base config + overlay + agent-specific config (deep merge)
4. **Validation** - YAML syntax + OTel Collector schema validation
5. **Storage Interface** - SQLite implementation, but interface allows swapping

## API Endpoints

- `GET /health` - Health check
- `GET /ready` - Readiness probe
- `GET /api/v1/agents` - List all agents
- `GET /api/v1/agents/{id}` - Get agent details
- `GET /api/v1/agents/{id}/config` - Get agent's effective config
- `DELETE /api/v1/agents/{id}` - Remove agent
- `POST /api/v1/sync` - Trigger git sync
- `GET /api/v1/selectors` - List config selectors
- `/v1/opamp` - OpAMP WebSocket endpoint

## Configuration

The server reads from `configs/server.yaml`. Environment variables are expanded using `${VAR}` syntax.
