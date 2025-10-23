# HLS Distributed Mode Example

This example demonstrates running HLS in distributed mode with consensus-based coordination.

## Prerequisites

1. Forge framework
2. Storage extension
3. Consensus extension
4. Go 1.21+

## Features Demonstrated

- **Distributed Consensus**: Uses Raft for leader election and coordination
- **Leader-Based Operations**: Stream creation/deletion only on leader nodes
- **Automatic Failover**: Leader re-election on failure
- **Cluster Awareness**: Headers showing node and leader information
- **Stream Distribution**: Streams accessible from any node in the cluster

## Running a 3-Node Cluster

### Terminal 1 - Node 1 (Bootstrap Node)

```bash
go run main.go -node=node1 -port=8081 -raft=7001
```

### Terminal 2 - Node 2

```bash
go run main.go -node=node2 -port=8082 -raft=7002 -peers=localhost:7001
```

### Terminal 3 - Node 3

```bash
go run main.go -node=node3 -port=8083 -raft=7003 -peers=localhost:7001
```

## API Usage

### Create Stream (Leader Only)

```bash
# This will only work on the leader node
curl -X POST http://localhost:8081/hls/streams \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Live Event",
    "type": "live",
    "target_duration": 6,
    "dvr_window_size": 10
  }'
```

If you try this on a follower node, you'll get a redirect response with the leader's address.

### List Streams (Any Node)

```bash
# Works on any node
curl http://localhost:8081/hls/streams

# Or from another node
curl http://localhost:8082/hls/streams
```

### Check Cluster Status

Check response headers to see cluster information:

```bash
curl -v http://localhost:8081/hls/streams
```

Headers:
- `X-HLS-Node`: Current node ID
- `X-HLS-Leader`: Leader node ID  
- `X-HLS-Is-Leader`: `true` if this is the leader

### Get Master Playlist (Any Node)

```bash
curl http://localhost:8081/hls/STREAM_ID/master.m3u8
```

Playlists are accessible from any node since they're stored in shared storage.

## Testing Failover

1. Start all 3 nodes
2. Note which node is the leader (check logs or headers)
3. Kill the leader node (Ctrl+C)
4. Watch the remaining nodes elect a new leader
5. Stream creation now works on the new leader

```bash
# Kill the leader and watch logs in other terminals
# They will show: "node became HLS cluster leader"
```

## Architecture

```
                Load Balancer
                      |
    +-----------------+-----------------+
    |                 |                 |
  Node 1            Node 2            Node 3
(Leader)         (Follower)        (Follower)
    |                 |                 |
    +-----------------+-----------------+
              Consensus Layer
             (Raft Protocol)
                      |
              Shared Storage
             (Local/S3/GCS)
```

### Leader Responsibilities

- Stream creation and deletion
- Configuration changes
- Cluster coordination

### Follower Responsibilities

- Serve playlists and segments
- Forward write requests to leader
- Participate in consensus voting

### All Nodes

- Read operations (playlists, segments, stats)
- Health checks
- Metrics collection

## Configuration Options

### Distributed Mode

```go
hls.WithDistributed(true)
hls.WithNodeID("unique-node-id")
hls.WithClusterID("hls-cluster")
hls.WithFailover(true)
```

### Consensus Backend

```go
consensus.WithNodeID("node-1")
consensus.WithClusterID("hls-cluster")
consensus.WithBindAddress("0.0.0.0:7000")
consensus.WithTransportType("tcp")
consensus.WithStorageType("boltdb")
```

### Shared Storage

For production, use S3/GCS for shared storage:

```go
storage.NewExtension(storage.Config{
    Backends: map[string]storage.BackendConfig{
        "default": {
            Type: "s3",
            Config: map[string]interface{}{
                "bucket":  "hls-streams",
                "region":  "us-east-1",
                "endpoint": "",
            },
        },
    },
    Default: "default",
})
```

## Monitoring

Each node logs cluster status every 10 seconds:

```
Cluster Status:
  Node: node1
  Leader: node1
  Is Leader: true
  Cluster Size: 3
  Has Quorum: true
  Healthy Nodes: 3/3
  Active Streams: 5
```

## Production Considerations

1. **Shared Storage**: Use S3, GCS, or Azure for true multi-node access
2. **Load Balancer**: Distribute requests across all nodes
3. **Health Checks**: Configure LB to remove unhealthy nodes
4. **Monitoring**: Use Prometheus metrics from consensus and HLS
5. **Backup**: Regular backups of Raft state
6. **Network**: Ensure low-latency connections between Raft nodes
7. **Quorum**: Maintain odd number of nodes (3, 5, 7) for proper quorum

## Troubleshooting

### Split Brain

- Ensure proper network connectivity between nodes
- Use odd number of nodes
- Configure appropriate timeouts

### Leader Election Failures

- Check Raft logs for details
- Verify all nodes can communicate
- Ensure clocks are synchronized

### Stream Not Found

- Verify shared storage is properly configured
- Check that all nodes use the same storage backend
- Look for storage replication delays

## Next Steps

- Add load balancer in front of nodes
- Implement graceful shutdown with stream migration
- Add advanced monitoring and alerting
- Configure CDN for segment delivery
- Implement multi-region deployment

