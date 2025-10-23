# Consensus Extension - Quick Start

Get started with the Forge Consensus Extension in 5 minutes!

## What is This?

The Consensus Extension turns your Forge application into a distributed, highly-available system using the Raft consensus algorithm. It provides:

- **Leader Election**: Automatic election of a leader node
- **Data Replication**: Consistent data replication across all nodes
- **High Availability**: Automatic failover and recovery
- **Cluster Management**: Dynamic node addition/removal
- **Production Ready**: Built-in metrics, health checks, and admin API

## Installation

```bash
# The extension is part of Forge v2
go get github.com/xraph/forge/extensions/consensus
```

## Minimal Example

Create a 3-node cluster for high availability:

```go
package main

import (
    "context"
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/consensus"
)

func main() {
    app := forge.New()
    
    // Add consensus extension
    app.RegisterExtension(consensus.NewExtension(
        consensus.WithNodeID("node-1"),
        consensus.WithClusterID("my-cluster"),
        consensus.WithBindAddress("0.0.0.0", 7000),
        consensus.WithPeers([]consensus.PeerConfig{
            {ID: "node-1", Address: "localhost", Port: 7000},
            {ID: "node-2", Address: "localhost", Port: 7001},
            {ID: "node-3", Address: "localhost", Port: 7002},
        }),
    ))
    
    app.Start(context.Background())
    
    // Your app is now highly available!
    select {}
}
```

## Running a Cluster

### Start 3 Nodes Locally

**Terminal 1 (Node 1)**:
```bash
go run main.go -node=node-1 -port=7000 -http=8080
```

**Terminal 2 (Node 2)**:
```bash
go run main.go -node=node-2 -port=7001 -http=8081
```

**Terminal 3 (Node 3)**:
```bash
go run main.go -node=node-3 -port=7002 -http=8082
```

Visit http://localhost:8080 to see the cluster status!

## Configuration File

For production, use a configuration file:

```yaml
# config.yaml
extensions:
  consensus:
    node_id: "${NODE_ID}"
    cluster_id: "production"
    bind_addr: "0.0.0.0"
    bind_port: 7000
    
    peers:
      - id: "node-1"
        address: "10.0.1.10"
        port: 7000
      - id: "node-2"
        address: "10.0.1.11"
        port: 7000
      - id: "node-3"
        address: "10.0.1.12"
        port: 7000
    
    storage:
      type: badger
      path: /data/consensus
    
    security:
      enable_tls: true
      cert_file: /certs/server.crt
      key_file: /certs/server.key
```

Then load it:

```go
app := forge.New(forge.WithConfigFile("config.yaml"))
app.RegisterExtension(consensus.NewExtension())
app.Start(context.Background())
```

## Common Operations

### Check Leadership

```go
service, _ := forge.Resolve[*consensus.Service](app.Container(), "consensus")

if service.IsLeader() {
    log.Println("I am the leader!")
}

leaderID := service.GetLeader()
log.Printf("Current leader: %s", leaderID)
```

### Apply Commands (Leader Only)

```go
cmd := consensus.Command{
    Type: "create_user",
    Payload: map[string]interface{}{
        "id": "123",
        "name": "Alice",
    },
}

err := service.Apply(ctx, cmd)
if err != nil {
    log.Printf("Error: %v", err)
}
```

### Read Data (Any Node)

```go
result, err := service.Read(ctx, "user:123")
```

### Cluster Management

```go
// Add a node
service.AddNode(ctx, "node-4", "10.0.1.13", 7000)

// Remove a node
service.RemoveNode(ctx, "node-4")

// Get cluster info
info := service.GetClusterInfo()
fmt.Printf("Cluster has %d nodes, %d are healthy\n", 
    info.TotalNodes, info.ActiveNodes)
```

## Using Middleware

Enforce leadership for write endpoints:

```go
import "github.com/xraph/forge/extensions/consensus/middleware"

checker, _ := forge.Resolve[*consensus.LeadershipChecker](
    app.Container(), "consensus:leadership")
leadershipMW := middleware.NewLeadershipMiddleware(checker, logger)

// Only leader can handle this endpoint
router.POST("/api/users", leadershipMW.RequireLeader()(createUserHandler))

// Route reads to any node, writes to leader
router.Use(leadershipMW.ReadOnlyRouting())
```

## Admin API

The extension provides a REST API for administration:

```bash
# Check health
curl http://localhost:8080/consensus/health

# Get cluster status
curl http://localhost:8080/consensus/status

# Get metrics
curl http://localhost:8080/consensus/metrics

# Create snapshot
curl -X POST http://localhost:8080/consensus/snapshot
```

## Monitoring

### Prometheus Metrics

Metrics are automatically exported:

```prometheus
forge_consensus_is_leader
forge_consensus_cluster_size
forge_consensus_has_quorum
forge_consensus_operations_total
forge_consensus_operations_per_sec
```

### Health Checks

```bash
# Kubernetes-style health checks
curl http://localhost:8080/consensus/health
```

## Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o app .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/app .
EXPOSE 7000 8080
CMD ["./app"]
```

```yaml
# docker-compose.yml
version: '3.8'
services:
  node1:
    build: .
    environment:
      - NODE_ID=node-1
    ports:
      - "7000:7000"
      - "8080:8080"
  
  node2:
    build: .
    environment:
      - NODE_ID=node-2
    ports:
      - "7001:7000"
      - "8081:8080"
  
  node3:
    build: .
    environment:
      - NODE_ID=node-3
    ports:
      - "7002:7000"
      - "8082:8080"
```

## Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: forge-consensus
spec:
  serviceName: forge-consensus
  replicas: 3
  selector:
    matchLabels:
      app: forge-consensus
  template:
    metadata:
      labels:
        app: forge-consensus
    spec:
      containers:
      - name: forge
        image: your-app:latest
        env:
        - name: NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        ports:
        - containerPort: 7000
          name: consensus
        - containerPort: 8080
          name: http
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 10Gi
```

## Troubleshooting

### No Leader Elected

**Problem**: Cluster has no leader

**Solution**:
1. Check network connectivity between nodes
2. Ensure at least 2 out of 3 nodes are running
3. Check logs for election errors

```bash
# Check status on each node
curl http://node1:8080/consensus/status
curl http://node2:8080/consensus/status
curl http://node3:8080/consensus/status
```

### Write Operations Failing

**Problem**: Cannot write data

**Solution**:
1. Check if you're sending writes to the leader
2. Check if cluster has quorum

```go
// Check leadership
if !service.IsLeader() {
    leaderID := service.GetLeader()
    // Redirect to leader
}

// Check quorum
info := service.GetClusterInfo()
if !info.HasQuorum {
    // Wait for quorum
}
```

## Best Practices

1. **Always use 3 or 5 nodes** for production
2. **Deploy across availability zones** for fault tolerance
3. **Monitor metrics** continuously
4. **Use TLS in production** for security
5. **Test failover scenarios** regularly
6. **Backup snapshots** to external storage

## Next Steps

- Read the [Full Documentation](README.md)
- Study the [Implementation Guide](_impl_docs/IMPLEMENTATION_GUIDE.md)
- Check out more [Examples](examples/)
- Review [Configuration Options](README.md#configuration)
- Set up [Monitoring](README.md#monitoring)

## Getting Help

- **Documentation**: https://forge.xraph.dev/consensus
- **Issues**: https://github.com/xraph/forge/issues
- **Discord**: https://discord.gg/forge

## License

MIT License

