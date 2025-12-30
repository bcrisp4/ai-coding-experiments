# OpAMP GitOps Control Plane

A telemetry agent control plane using the [OpAMP protocol](https://opentelemetry.io/docs/specs/opamp/) with Git as the source of truth for agent configurations. Manages OpenTelemetry Collectors across hybrid environments (Kubernetes and VMs).

## Features

- **OpAMP Server**: WebSocket-based agent communication using the open-telemetry/opamp-go library
- **GitOps Configuration**: Pull configurations from Git repositories with automatic sync
- **Label-Based Config Resolution**: Assign configurations based on agent labels (similar to Kubernetes selectors)
- **Config Validation**: YAML syntax and OTel Collector schema validation before deployment
- **REST API**: Query agents, trigger syncs, view effective configurations
- **Persistent Storage**: SQLite-backed agent registry (interface allows swapping to PostgreSQL)

## Quick Start

### Local Development

```bash
# Clone the repository
git clone https://github.com/bcrisp4/opamp-control-plane
cd opamp-control-plane

# Run with Go
go run ./cmd/server -config configs/server.yaml

# Or build and run
go build -o opamp-control-plane ./cmd/server
./opamp-control-plane
```

### Using Docker Compose

```bash
# Start the control plane and a demo OTel Collector
docker-compose up -d

# View logs
docker-compose logs -f control-plane

# Check agent status via API
curl http://localhost:8080/api/v1/agents
```

## Configuration

### Server Configuration

Create a `configs/server.yaml` file:

```yaml
server:
  http_addr: ":8080"      # REST API and webhook endpoint
  opamp_addr: ":4320"     # OpAMP WebSocket endpoint

storage:
  type: "sqlite"
  sqlite:
    path: "./data/opamp.db"

git:
  repo_url: "https://github.com/your-org/opamp-configs.git"
  branch: "main"
  poll_interval: "60s"
  local_path: "./data/configs"
  # Authentication (use environment variables for secrets)
  username: "${GIT_USERNAME}"
  password: "${GIT_PASSWORD}"

validation:
  enabled: true
  strict_otel_schema: false  # If true, validates component references

logging:
  level: "info"
  format: "json"
```

### Git Repository Structure

Your configuration repository should follow this structure:

```
opamp-configs/
├── base/
│   └── collector.yaml          # Base OTel Collector config
├── overlays/
│   ├── production/
│   │   └── collector.yaml      # Production-specific overrides
│   └── staging/
│       └── collector.yaml      # Staging-specific overrides
└── agents/
    ├── _selectors.yaml         # Label selector -> config mapping
    ├── kubernetes-daemonset/
    │   └── collector.yaml      # K8s DaemonSet specific config
    └── vm-gateway/
        └── collector.yaml      # VM gateway specific config
```

### Label Selectors

Define how agents get assigned configurations in `agents/_selectors.yaml`:

```yaml
selectors:
  - name: "kubernetes-daemonset"
    match:
      labels:
        deployment: kubernetes
        role: daemonset
    config: kubernetes-daemonset/collector.yaml
    overlay: production

  - name: "vm-gateway"
    match:
      labels:
        deployment: vm
        role: gateway
    config: vm-gateway/collector.yaml
    overlay: production
```

## API Reference

### List Agents
```bash
GET /api/v1/agents
GET /api/v1/agents?status=connected
GET /api/v1/agents?config_status=applied
```

### Get Agent Details
```bash
GET /api/v1/agents/{instance_uid}
```

### Get Agent's Effective Config
```bash
GET /api/v1/agents/{instance_uid}/config

# Get raw YAML
curl -H "Accept: text/yaml" http://localhost:8080/api/v1/agents/{id}/config
```

### Trigger Git Sync
```bash
POST /api/v1/sync
```

### Health Check
```bash
GET /health
GET /ready
```

## Deploying to Kubernetes

### Prerequisites

- Kubernetes cluster (1.25+)
- kubectl configured
- Container registry access (for custom images)

### Step 1: Build and Push Image

```bash
# Build the image
docker build -t your-registry/opamp-control-plane:latest .

# Push to your registry
docker push your-registry/opamp-control-plane:latest
```

### Step 2: Configure Secrets

Edit `deploy/kubernetes/base/secret.yaml` with your Git credentials:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: opamp-control-plane-secrets
  namespace: opamp-system
type: Opaque
stringData:
  GIT_USERNAME: "your-username"
  GIT_PASSWORD: "your-token-or-password"
  GIT_WEBHOOK_SECRET: "your-webhook-secret"
```

### Step 3: Configure Control Plane

Edit `deploy/kubernetes/base/configmap.yaml` to set your Git repository:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: opamp-control-plane-config
  namespace: opamp-system
data:
  server.yaml: |
    server:
      http_addr: ":8080"
      opamp_addr: ":4320"
    git:
      repo_url: "https://github.com/your-org/opamp-configs.git"
      branch: "main"
      poll_interval: "60s"
    # ... rest of config
```

### Step 4: Update Image Reference

Edit `deploy/kubernetes/base/deployment.yaml`:

```yaml
containers:
  - name: control-plane
    image: your-registry/opamp-control-plane:latest  # Update this
```

### Step 5: Deploy Control Plane

```bash
# Apply the base manifests
kubectl apply -k deploy/kubernetes/base/

# Verify deployment
kubectl -n opamp-system get pods
kubectl -n opamp-system get svc
```

### Step 6: Deploy OTel Collectors

Deploy the OTel Collector DaemonSet that connects to the control plane:

```bash
# Apply the collector manifests
kubectl apply -k deploy/kubernetes/otel-collector/

# Verify collectors are running
kubectl -n opamp-system get pods -l app.kubernetes.io/name=otel-collector
```

### Step 7: Verify Integration

```bash
# Port-forward to access the API
kubectl -n opamp-system port-forward svc/opamp-control-plane 8080:8080

# In another terminal, check connected agents
curl http://localhost:8080/api/v1/agents
```

Expected output:
```json
{
  "agents": [
    {
      "instance_uid": "abc123...",
      "status": "connected",
      "labels": {
        "deployment": "kubernetes",
        "role": "daemonset"
      },
      "effective_config_name": "kubernetes-daemonset",
      "config_status": "applied"
    }
  ],
  "count": 1
}
```

### Exposing the Control Plane

For production, expose the control plane via Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: opamp-control-plane
  namespace: opamp-system
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  rules:
    - host: opamp.your-domain.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: opamp-control-plane
                port:
                  number: 8080
```

**Note**: WebSocket connections require proper timeout configuration in your ingress controller.

## Integrating OTel Collectors

### Kubernetes (with OpAMP Supervisor)

The OTel Collector needs to run with an OpAMP supervisor to receive remote configuration. The supervisor connects to the control plane and manages the collector process.

See `deploy/kubernetes/otel-collector/daemonset.yaml` for a complete example.

Key supervisor configuration:
```yaml
server:
  endpoint: ws://opamp-control-plane:4320/v1/opamp

agent:
  executable: /otelcol-contrib

capabilities:
  reports_effective_config: true
  reports_health: true
  accepts_remote_config: true
```

### VM Deployment

For VMs, install the OTel Collector with the OpAMP extension:

1. Download the collector:
```bash
curl -LO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.115.0/otelcol-contrib_0.115.0_linux_amd64.tar.gz
tar xzf otelcol-contrib_0.115.0_linux_amd64.tar.gz
```

2. Create supervisor config (`/etc/otel/supervisor.yaml`):
```yaml
server:
  endpoint: ws://your-control-plane:4320/v1/opamp

agent:
  executable: /usr/local/bin/otelcol-contrib
  description:
    identifying_attributes:
      service.name: otel-collector
      deployment: vm
      role: gateway

storage:
  directory: /var/lib/otel/supervisor

capabilities:
  reports_effective_config: true
  reports_health: true
  accepts_remote_config: true
```

3. Run with systemd:
```ini
[Unit]
Description=OpenTelemetry Collector with OpAMP
After=network.target

[Service]
ExecStart=/usr/local/bin/otelcol-contrib --config /etc/otel/supervisor.yaml
Restart=always
User=otel

[Install]
WantedBy=multi-user.target
```

## GitOps Workflow

1. **Edit configurations** in your Git repository
2. **Commit and push** changes
3. **Control plane syncs** automatically (or via webhook)
4. **Configs validated** before deployment
5. **Agents updated** via OpAMP
6. **Status tracked** in the control plane

### Setting Up Webhooks

For immediate updates on Git push, configure a webhook in your Git provider:

**GitHub**:
- URL: `https://your-control-plane/webhook/git`
- Content type: `application/json`
- Secret: Your webhook secret
- Events: `push`

**GitLab**:
- URL: `https://your-control-plane/webhook/git`
- Secret Token: Your webhook secret
- Trigger: Push events

## Troubleshooting

### Agent Not Connecting

1. Check network connectivity:
```bash
# From agent pod/VM
curl -v http://control-plane:8080/health
```

2. Check WebSocket upgrade:
```bash
curl -v -H "Upgrade: websocket" \
     -H "Connection: Upgrade" \
     http://control-plane:4320/v1/opamp
```

3. Verify agent labels match a selector

### Config Not Applying

1. Check validation errors:
```bash
curl http://localhost:8080/api/v1/agents/{id}
# Look at remote_config_status.error_message
```

2. View effective config:
```bash
curl http://localhost:8080/api/v1/agents/{id}/config
```

3. Check control plane logs:
```bash
kubectl -n opamp-system logs -l app.kubernetes.io/name=opamp-control-plane
```

### Git Sync Issues

1. Manually trigger sync:
```bash
curl -X POST http://localhost:8080/api/v1/sync
```

2. Check Git credentials and repository access

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Git Repository                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ webhook / poll
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    OpAMP Control Plane                           │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────────────┐ │
│  │  Git Sync   │──│ Config Store │──│   Config Resolver       │ │
│  │  Service    │  │  (in-memory) │  │  (label-based matching) │ │
│  └─────────────┘  └──────────────┘  └─────────────────────────┘ │
│         │                                       │                │
│  ┌─────────────────────────────────────────────┴──────────────┐ │
│  │                    OpAMP Server (WebSocket)                │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    Agent Registry (SQLite)                 │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ OpAMP (WebSocket)
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Managed OTel Collectors                       │
│  ┌──────────────────┐  ┌──────────────────┐                     │
│  │ Collector + Supervisor (K8s)           │  ...                │
│  └──────────────────┘  └──────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o opamp-control-plane ./cmd/server
```

### Project Structure

```
opamp-control-plane/
├── cmd/server/         # Entry point
├── internal/
│   ├── opamp/          # OpAMP server implementation
│   ├── registry/       # Agent storage (SQLite)
│   ├── gitsync/        # Git synchronization
│   ├── config/         # Config resolution and validation
│   └── api/            # REST API handlers
├── pkg/models/         # Shared types
├── configs/            # Server configuration
├── deploy/kubernetes/  # K8s manifests
└── example-configs/    # Example OTel configs
```

## License

Apache 2.0
