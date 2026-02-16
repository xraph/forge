# Forge Consensus Extension

Production-grade distributed consensus extension for Forge that enables high availability, leader election, and distributed coordination using the Raft consensus algorithm.

## Features

### Core Capabilities
- **Raft Consensus**: Industry-standard Raft algorithm implementation
- **Leader Election**: Automatic leader election with configurable timeouts
- **Log Replication**: Consistent log replication across all nodes
- **Snapshot & Compaction**: Automatic snapshotting and log compaction
- **Membership Changes**: Dynamic cluster membership with joint consensus

### Transport Layer
- **Multiple Transports**: gRPC (production) and TCP support
- **Connection Pooling**: Efficient connection management
- **TLS/mTLS**: Secure communication with mutual TLS
- **Compression**: Optional message compression for reduced bandwidth

### Service Discovery
- **Static Configuration**: Simple peer list for small clusters
- **DNS Discovery**: DNS-based service discovery
- **Consul Integration**: HashiCorp Consul service discovery
- **Kubernetes**: Native Kubernetes service discovery
- **etcd**: etcd-based discovery support

### Storage Backends
- **BadgerDB**: High-performance embedded key-value store (default)
- **BoltDB**: Reliable embedded database
- **Pebble**: RocksDB-inspired LSM storage
- **PostgreSQL**: Distributed PostgreSQL backend (experimental)

### Observability
- **Prometheus Metrics**: Comprehensive metrics for monitoring
- **Health Checks**: Multi-level health checking
- **Distributed Tracing**: OpenTelemetry tracing support
- **Structured Logging**: Contextual logging with correlation IDs

### Resilience
- **Circuit Breakers**: Automatic failure detection and recovery
- **Retry Logic**: Exponential backoff with jitter
- **Timeouts**: Configurable timeouts at all layers
- **Failover**: Automatic leader failover

### Security
- **TLS/mTLS**: Transport layer security
- **Authentication**: Node authentication
- **Authorization**: Role-based access control (RBAC)
- **Encryption**: Optional data encryption at rest

### Administration
- **REST API**: Complete admin API for cluster management
- **Health Endpoints**: Health and readiness checks
- **Metrics Endpoints**: Real-time metrics
- **CLI Tools**: Command-line tools for administration

## Quick Start

### Installation

```bash
go get github.com/xraph/forge/extensions/consensus
```

### Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/consensus"
)

func main() {
    // Create Forge app
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
    
    // Start app
    if err := app.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    
    // Get consensus service
    consensusService, _ := forge.Inject[*consensus.Service](app.Container())
    
    // Check if leader
    if consensusService.IsLeader() {
        log.Println("This node is the leader!")
    }
    
    // Apply a command (only on leader)
    cmd := consensus.Command{
        Type: "set",
        Payload: map[string]interface{}{
            "key":   "user:123",
            "value": "John Doe",
        },
    }
    
    if err := consensusService.Apply(context.Background(), cmd); err != nil {
        log.Printf("Failed to apply command: %v", err)
    }
    
    // Block forever
    select {}
}
```

### Configuration File

```yaml
# config.yaml
extensions:
  consensus:
    node_id: "node-1"
    cluster_id: "production-cluster"
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
    
    raft:
      heartbeat_interval: 1s
      election_timeout_min: 5s
      election_timeout_max: 10s
      snapshot_interval: 30m
      snapshot_threshold: 10000
    
    transport:
      type: grpc
      enable_compression: true
      max_message_size: 4194304  # 4MB
      timeout: 10s
    
    storage:
      type: badger
      path: ./data/consensus
      sync_writes: true
    
    discovery:
      type: kubernetes
      namespace: default
      service_name: forge-consensus
    
    security:
      enable_tls: true
      cert_file: /etc/certs/server.crt
      key_file: /etc/certs/server.key
      ca_file: /etc/certs/ca.crt
      enable_mtls: true
    
    admin_api:
      enabled: true
      path_prefix: /consensus
    
    observability:
      metrics:
        enabled: true
        collection_interval: 15s
      tracing:
        enabled: true
        sample_rate: 0.1
      logging:
        level: info
        log_raft_details: false
```

## Usage Examples

### Leader Election

The consensus extension automatically handles leader election. You can check leadership status:

```go
service := consensusService.Service()

// Check if this node is the leader
if service.IsLeader() {
    log.Println("I am the leader")
}

// Get current leader ID
leaderID := service.GetLeader()
log.Printf("Current leader: %s", leaderID)

// Get current role
role := service.GetRole() // "follower", "candidate", or "leader"
log.Printf("My role: %s", role)
```

### Applying Commands

Only the leader can apply commands to the state machine:

```go
cmd := consensus.Command{
    Type: "create_user",
    Payload: map[string]interface{}{
        "id":    "123",
        "name":  "Alice",
        "email": "alice@example.com",
    },
}

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := service.Apply(ctx, cmd)
if consensus.IsNotLeaderError(err) {
    // Redirect to leader
    leaderID := service.GetLeader()
    log.Printf("Not leader. Redirect to: %s", leaderID)
} else if err != nil {
    log.Printf("Failed to apply command: %v", err)
}
```

### Reading Data

Reads can be performed on any node (eventual consistency) or restricted to the leader (strong consistency):

```go
// Read from any node (eventual consistency)
result, err := service.Read(ctx, "user:123")

// For strong consistency, check leadership first
if service.IsLeader() {
    result, err := service.Read(ctx, "user:123")
}
```

### Cluster Management

Add or remove nodes dynamically:

```go
// Add a new node (only leader can do this)
err := service.AddNode(ctx, "node-4", "10.0.1.13", 7000)

// Remove a node
err := service.RemoveNode(ctx, "node-4")

// Get cluster information
info := service.GetClusterInfo()
fmt.Printf("Cluster ID: %s\n", info.ID)
fmt.Printf("Leader: %s\n", info.Leader)
fmt.Printf("Total Nodes: %d\n", info.TotalNodes)
fmt.Printf("Active Nodes: %d\n", info.ActiveNodes)
fmt.Printf("Has Quorum: %v\n", info.HasQuorum)
```

### Leadership Transfer

Transfer leadership to another node:

```go
// Transfer leadership to node-2
err := service.TransferLeadership(ctx, "node-2")

// Or step down as leader (triggers new election)
err := service.StepDown(ctx)
```

### Snapshots

Create snapshots manually or rely on automatic snapshots:

```go
// Manual snapshot
err := service.Snapshot(ctx)

// Automatic snapshots are configured via:
// - snapshot_interval: time between snapshots
// - snapshot_threshold: number of log entries before snapshot
```

### Health Checks

```go
// Simple health check
err := service.HealthCheck(ctx)
if err != nil {
    log.Printf("Unhealthy: %v", err)
}

// Detailed health status
status := service.GetHealthStatus(ctx)
fmt.Printf("Status: %s\n", status.Status)
fmt.Printf("Leader: %v\n", status.Leader)
fmt.Printf("Has Quorum: %v\n", status.HasQuorum)
fmt.Printf("Active Nodes: %d/%d\n", status.ActiveNodes, status.TotalNodes)

for _, check := range status.Details {
    fmt.Printf("  %s: %v - %s\n", check.Name, check.Healthy, check.Message)
}
```

### Middleware Integration

Use consensus middleware to enforce leadership:

```go
import (
    "github.com/xraph/forge/extensions/consensus/middleware"
)

// Get leadership checker
checker, _ := forge.Inject[*consensus.LeadershipChecker](app.Container())
leadershipMW := middleware.NewLeadershipMiddleware(checker, logger)

// Require leader for write endpoints
router.POST("/api/users", leadershipMW.RequireLeader()(createUserHandler))

// Add leader information to all responses
router.Use(leadershipMW.AddLeaderHeader())

// Route reads to any node, writes to leader only
router.Use(leadershipMW.ReadOnlyRouting())

// Enforce consistency levels
router.GET("/api/users/:id", 
    leadershipMW.ConsistencyMiddleware(middleware.ConsistencyStrong)(getUserHandler))
```

## Admin API

The consensus extension provides a REST API for administration:

### Health Check
```bash
curl http://localhost:8080/consensus/health
```

### Cluster Status
```bash
curl http://localhost:8080/consensus/status
```

### List Nodes
```bash
curl http://localhost:8080/consensus/nodes
```

### Get Leader
```bash
curl http://localhost:8080/consensus/leader
```

### Metrics
```bash
curl http://localhost:8080/consensus/metrics
```

### Add Node
```bash
curl -X POST http://localhost:8080/consensus/add-node \
  -H "Content-Type: application/json" \
  -d '{
    "node_id": "node-4",
    "address": "10.0.1.13",
    "port": 7000
  }'
```

### Remove Node
```bash
curl -X POST http://localhost:8080/consensus/remove-node \
  -H "Content-Type: application/json" \
  -d '{"node_id": "node-4"}'
```

### Transfer Leadership
```bash
curl -X POST http://localhost:8080/consensus/transfer-leadership \
  -H "Content-Type: application/json" \
  -d '{"target_node_id": "node-2"}'
```

### Create Snapshot
```bash
curl -X POST http://localhost:8080/consensus/snapshot
```

## Monitoring

### Prometheus Metrics

The extension exports comprehensive Prometheus metrics:

```prometheus
# Node metrics
forge_consensus_cluster_size
forge_consensus_cluster_healthy_nodes
forge_consensus_is_leader
forge_consensus_has_quorum
forge_consensus_term

# Operations metrics
forge_consensus_operations_total
forge_consensus_operations_failed
forge_consensus_operations_per_sec
forge_consensus_operation_duration_seconds

# Election metrics
forge_consensus_leader_elections_total
forge_consensus_leader_election_duration_seconds

# Log metrics
forge_consensus_commit_index
forge_consensus_last_applied
forge_consensus_log_size
forge_consensus_log_entries

# Snapshot metrics
forge_consensus_snapshots_total
forge_consensus_snapshot_duration_seconds
```

### Grafana Dashboard

A complete Grafana dashboard is available at `examples/grafana/consensus-dashboard.json`.

## Production Considerations

### Cluster Size

- **Minimum**: 3 nodes for fault tolerance
- **Recommended**: 5 nodes for production
- **Maximum**: 7-9 nodes (more nodes = slower consensus)

### Performance Tuning

```yaml
raft:
  # Faster heartbeats for quicker failure detection
  heartbeat_interval: 500ms
  
  # Shorter election timeout for faster recovery
  election_timeout_min: 2s
  election_timeout_max: 4s
  
  # Larger batch size for higher throughput
  replication_batch_size: 500
  max_append_entries: 128

transport:
  # Enable compression for reduced bandwidth
  enable_compression: true
  compression_level: 6
  
  # Larger message size for bulk operations
  max_message_size: 16777216  # 16MB

storage:
  # Increase batch size for higher write throughput
  max_batch_size: 5000
  max_batch_delay: 5ms
```

### High Availability

1. **Deploy across availability zones**: Distribute nodes across different AZs
2. **Use persistent storage**: Ensure data is persisted to disk
3. **Monitor health**: Set up health checks and alerting
4. **Backup snapshots**: Regularly backup snapshot data
5. **Test failover**: Regularly test node failures

### Security Hardening

```yaml
security:
  # Always enable TLS in production
  enable_tls: true
  enable_mtls: true
  
  # Use strong cipher suites
  # Rotate certificates regularly
  
  # Enable encryption at rest
  enable_encryption: true
  encryption_key: "${CONSENSUS_ENCRYPTION_KEY}"
```

## Troubleshooting

### No Leader Elected

**Symptoms**: Cluster has no leader, writes fail

**Causes**:
- Network partitions
- Majority of nodes down
- Clock skew between nodes
- Incorrect peer configuration

**Solutions**:
- Check network connectivity between nodes
- Ensure at least N/2 + 1 nodes are healthy
- Synchronize clocks with NTP
- Verify peer configuration

### Split Brain

**Symptoms**: Multiple nodes think they are leader

**Causes**:
- Network partition
- Inconsistent peer configuration

**Prevention**:
- Use odd number of nodes (3, 5, 7)
- Deploy across availability zones
- Use proper network policies

### High Latency

**Symptoms**: Slow command application

**Causes**:
- Large log entries
- Slow disk I/O
- Network latency
- Too many nodes

**Solutions**:
- Reduce message size
- Use faster disks (SSD/NVMe)
- Reduce cluster size
- Enable compression
- Increase snapshot frequency

### Log Growth

**Symptoms**: Disk space filling up

**Solutions**:
- Reduce `snapshot_interval`
- Lower `snapshot_threshold`
- Enable automatic compaction
- Monitor disk usage

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Forge Consensus Extension                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Raft Core  │  │   Election   │  │   Cluster    │          │
│  │              │◄─┤   Manager    │◄─┤   Manager    │          │
│  └──────┬───────┘  └──────────────┘  └──────────────┘          │
│         │                                                         │
│  ┌──────▼───────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Storage    │  │  Transport   │  │  Discovery   │          │
│  │   Layer      │  │   Layer      │  │   Service    │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│         │                  │                  │                  │
│  ┌──────▼──────────────────▼──────────────────▼────────┐        │
│  │           State Machine & Event Emitter              │        │
│  └──────────────────────────────────────────────────────┘        │
│         │                                                         │
│  ┌──────▼──────────────────────────────────────────────┐        │
│  │    Health • Metrics • Observability • Admin API      │        │
│  └──────────────────────────────────────────────────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Support

- **Documentation**: [https://forge.xraph.dev/consensus](https://forge.xraph.dev/consensus)
- **Issues**: [GitHub Issues](https://github.com/xraph/forge/issues)
- **Discussions**: [GitHub Discussions](https://github.com/xraph/forge/discussions)
- **Discord**: [Forge Community](https://discord.gg/forge)

## Acknowledgments

- Based on the [Raft Consensus Algorithm](https://raft.github.io/)
- Inspired by etcd, Consul, and HashiCorp Raft
- Built on the [Forge Framework](https://github.com/xraph/forge)

