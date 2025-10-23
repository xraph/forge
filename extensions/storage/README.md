# Storage Extension - Production-Ready Implementation

A robust, production-ready object storage extension for Forge with comprehensive resilience, security, and observability features.

## Features

### 🔒 Security
- **Path Validation**: Comprehensive path traversal protection
- **Input Sanitization**: Automatic key sanitization
- **Secure Presigned URLs**: Cryptographically signed URLs with expiration
- **Content Type Validation**: Strict content type checking
- **Metadata Validation**: Size and format constraints

### 🛡️ Resilience
- **Circuit Breaker Pattern**: Prevent cascade failures with automatic recovery
- **Exponential Backoff Retry**: Configurable retry logic with backoff
- **Rate Limiting**: Token bucket algorithm for rate limiting
- **Timeout Management**: Configurable operation timeouts
- **Non-Retryable Error Detection**: Smart error classification

### ⚡ Performance
- **Buffer Pooling**: `sync.Pool` for zero-allocation I/O
- **Concurrent Operations**: Thread-safe file operations with fine-grained locking
- **ETag Caching**: Cached MD5 checksums
- **Chunked Uploads**: Large file support with multipart uploads
- **Connection Pooling**: Optimized for cloud backends

### 📊 Observability
- **Comprehensive Metrics**: Upload/download counts, durations, error rates
- **Health Checks**: Multi-backend health monitoring
- **Structured Logging**: Contextual logging with trace IDs
- **Circuit Breaker State Tracking**: Real-time resilience monitoring

### 🗄️ Backend Support
- **Enhanced Local**: Production-ready local filesystem with atomic operations
- **AWS S3**: Full S3 support with presigned URLs
- **GCS**: Google Cloud Storage (coming soon)
- **Azure Blob**: Azure Blob Storage (coming soon)

## Installation

### Basic Installation
```bash
cd v2
go get -u github.com/xraph/forge/extensions/storage
```

### With AWS S3 Support
```bash
go get -u github.com/aws/aws-sdk-go-v2/aws
go get -u github.com/aws/aws-sdk-go-v2/config
go get -u github.com/aws/aws-sdk-go-v2/service/s3
go get -u github.com/aws/aws-sdk-go-v2/feature/s3/manager
```

## Configuration

### YAML Configuration
```yaml
extensions:
  storage:
    default: "local"
    use_enhanced_backend: true
    
    # Backends configuration
    backends:
      local:
        type: "local"
        config:
          root_dir: "./storage"
          base_url: "http://localhost:8080/files"
          secret: "your-secret-key-here"
          chunk_size: 5242880  # 5MB
          max_upload_size: 5368709120  # 5GB
      
      s3:
        type: "s3"
        config:
          region: "us-east-1"
          bucket: "my-bucket"
          prefix: "uploads"
          access_key_id: "${AWS_ACCESS_KEY_ID}"
          secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
    
    # Resilience configuration
    resilience:
      max_retries: 3
      initial_backoff: "100ms"
      max_backoff: "10s"
      backoff_multiplier: 2.0
      
      circuit_breaker_enabled: true
      circuit_breaker_threshold: 5
      circuit_breaker_timeout: "60s"
      circuit_breaker_half_open_max: 3
      
      rate_limit_enabled: true
      rate_limit_per_sec: 100
      rate_limit_burst: 200
      
      operation_timeout: "30s"
    
    # Features
    enable_presigned_urls: true
    presign_expiry: "15m"
    max_upload_size: 5368709120  # 5GB
    chunk_size: 5242880          # 5MB
    
    # CDN configuration
    enable_cdn: false
    cdn_base_url: ""
```

### Programmatic Configuration
```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/storage"
)

func main() {
    // Create app
    app := forge.New()

    // Configure storage
    config := storage.DefaultConfig()
    config.Default = "s3"
    config.UseEnhancedBackend = true
    config.Backends["s3"] = storage.BackendConfig{
        Type: "s3",
        Config: map[string]interface{}{
            "region": "us-east-1",
            "bucket": "my-bucket",
        },
    }
    
    // Customize resilience
    config.Resilience.MaxRetries = 5
    config.Resilience.CircuitBreakerThreshold = 10

    // Register extension
    app.Use(storage.NewExtension(config))

    app.Run()
}
```

## Usage

### Basic Operations

```go
// Get storage manager from DI
storageManager := forge.Must[*storage.StorageManager](app.Container(), "storage")

ctx := context.Background()

// Upload file
file, _ := os.Open("document.pdf")
defer file.Close()

err := storageManager.Upload(ctx, "documents/report.pdf", file,
    storage.WithContentType("application/pdf"),
    storage.WithMetadata(map[string]string{
        "user_id": "12345",
        "uploaded_by": "john@example.com",
    }),
)

// Download file
reader, err := storageManager.Download(ctx, "documents/report.pdf")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Copy data
io.Copy(os.Stdout, reader)

// Check if exists
exists, err := storageManager.Exists(ctx, "documents/report.pdf")

// Get metadata
metadata, err := storageManager.Metadata(ctx, "documents/report.pdf")
fmt.Printf("Size: %d, Last Modified: %v\n", metadata.Size, metadata.LastModified)

// List files
objects, err := storageManager.List(ctx, "documents/",
    storage.WithRecursive(true),
    storage.WithLimit(100),
)

// Delete file
err = storageManager.Delete(ctx, "documents/report.pdf")
```

### Advanced Operations

```go
// Copy file
err := storageManager.Copy(ctx, "source.pdf", "destination.pdf")

// Move file
err := storageManager.Move(ctx, "old-location.pdf", "new-location.pdf")

// Generate presigned upload URL
uploadURL, err := storageManager.PresignUpload(ctx, "uploads/file.pdf", 15*time.Minute)
// Share uploadURL with client for direct upload

// Generate presigned download URL
downloadURL, err := storageManager.PresignDownload(ctx, "downloads/file.pdf", 1*time.Hour)
// Share downloadURL with client for direct download
```

### Health Checks

```go
// Basic health check
err := storageManager.Health(ctx)
if err != nil {
    log.Printf("Storage unhealthy: %v", err)
}

// Detailed health check
health, err := storageManager.HealthDetailed(ctx, true) // Check all backends
fmt.Printf("Healthy: %v, %d/%d backends healthy\n",
    health.Healthy, health.HealthyCount, health.BackendCount)

for name, backend := range health.Backends {
    fmt.Printf("Backend %s: %v (response time: %v)\n",
        name, backend.Healthy, backend.ResponseTime)
}

// Check specific backend
backendHealth, err := storageManager.BackendHealth(ctx, "s3")
```

### Using Specific Backend

```go
// Get specific backend
s3Backend := storageManager.Backend("s3")
if s3Backend != nil {
    err := s3Backend.Upload(ctx, "key", data)
}
```

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Storage Extension                     │
├─────────────────────────────────────────────────────────┤
│                    Storage Manager                       │
│  ┌────────────────────────────────────────────────────┐ │
│  │              Health Checker                        │ │
│  │  • Multi-backend health monitoring                 │ │
│  │  • Write/List-based checks                         │ │
│  │  • Concurrent health checks                        │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                  Resilience Layer                        │
│  ┌────────────────┬──────────────┬──────────────────┐  │
│  │ Circuit Breaker │ Rate Limiter │ Retry w/ Backoff │  │
│  │ • Open/Closed   │ • Token Bucket│ • Exponential   │  │
│  │ • Half-Open     │ • Burst Support│ • Timeout      │  │
│  └────────────────┴──────────────┴──────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    Storage Backends                      │
│  ┌──────────────┬──────────────┬──────────────────────┐│
│  │ Local Enhanced│    AWS S3    │  GCS / Azure (Soon) ││
│  │ • Atomic ops  │ • Presigned  │  • Coming soon      ││
│  │ • File locking│ • Multipart  │                     ││
│  │ • Buffer pool │ • Pagination │                     ││
│  └──────────────┴──────────────┴──────────────────────┘│
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                Cross-Cutting Concerns                    │
│  ┌────────────────┬──────────────┬──────────────────┐  │
│  │ Path Validator │ Buffer Pool  │ Security         │  │
│  │ • Traversal    │ • sync.Pool  │ • Validation     │  │
│  │ • Sanitization │ • Zero-alloc │ • Encryption     │  │
│  └────────────────┴──────────────┴──────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Resilience Patterns

#### Circuit Breaker States
```
    ┌─────────┐
    │ Closed  │──────► Failures ≥ Threshold
    └────┬────┘                 │
         │                      ▼
    Success             ┌───────────┐
         │              │   Open    │
         │              └─────┬─────┘
         │                    │
    ┌────▼─────┐              │ Timeout
    │Half-Open │◄─────────────┘
    └──────────┘
```

## Metrics

The extension exposes the following metrics:

### Operation Metrics
- `storage_uploads` - Total upload count
- `storage_downloads` - Total download count
- `storage_deletes` - Total delete count
- `storage_upload_bytes` - Total bytes uploaded
- `storage_upload_duration` - Upload duration histogram
- `storage_download_duration` - Download duration histogram

### Error Metrics
- `storage_upload_errors` - Upload error count
- `storage_download_errors` - Download error count
- `storage_delete_errors` - Delete error count

### Resilience Metrics
- `storage_retries` - Retry attempt count
- `storage_max_retries_exceeded` - Max retries exceeded count
- `storage_circuit_breaker_open` - Circuit breaker open events
- `storage_circuit_breaker_state` - Current circuit breaker state (0=closed, 1=open, 2=half-open)
- `storage_rate_limit_allowed` - Rate limit allowed requests
- `storage_rate_limit_rejected` - Rate limit rejected requests

### Health Metrics
- `storage_backend_health` - Backend health status (1=healthy, 0=unhealthy)
- `storage_health_check_duration` - Health check duration histogram

## Testing

### Run All Tests
```bash
cd v2/extensions/storage
go test -v
```

### Run Integration Tests
```bash
go test -v -run 'TestStorageManager_.*'
```

### Run Short Tests (Skip Integration)
```bash
go test -v -short
```

### Run Benchmarks
```bash
go test -v -bench=. -benchmem
```

### Coverage
```bash
go test -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Production Considerations

### Local Backend
- Use fast SSD storage for best performance
- Configure regular backups
- Monitor disk space
- Use proper file permissions (0750 for directories, 0640 for files)
- Generate and securely store the secret key

### S3 Backend
- Use IAM roles instead of access keys when possible
- Enable versioning for important buckets
- Configure lifecycle policies for automatic archival
- Use VPC endpoints for better performance
- Enable server-side encryption
- Set appropriate CORS policies for presigned URLs

### Resilience Tuning
- **Circuit Breaker Threshold**: Higher for stable backends, lower for unstable
- **Retry Count**: 3-5 retries for most cases
- **Backoff**: Start at 100ms, max 10s
- **Rate Limit**: Based on backend capacity and costs
- **Timeouts**: 30s for normal ops, 5min for large uploads

### Monitoring
- Track error rates and set up alerts
- Monitor circuit breaker state changes
- Watch for rate limit rejections
- Track P95/P99 latencies
- Set up health check alerts

## Security Best Practices

1. **Never commit secrets**: Use environment variables or secret managers
2. **Rotate secrets regularly**: Especially presigned URL secrets
3. **Validate all inputs**: The extension validates, but add application-level checks
4. **Use HTTPS**: Always use HTTPS for presigned URLs
5. **Set short expiry**: Use minimum necessary expiry for presigned URLs
6. **Implement access control**: Add application-level authorization
7. **Audit file access**: Log all storage operations
8. **Scan uploaded files**: Implement virus scanning for user uploads

## Performance Tips

1. **Use buffer pooling**: Reuse buffers for multiple operations
2. **Batch operations**: Group related operations
3. **Use streaming**: For large files, stream instead of loading into memory
4. **Enable CDN**: For public assets, use CDN with presigned URLs
5. **Optimize chunks**: Tune chunk size based on file sizes
6. **Parallel uploads**: Use goroutines for multiple file uploads
7. **Cache metadata**: Cache frequently accessed metadata

## Troubleshooting

### Circuit Breaker Keeps Opening
- Check backend health and connectivity
- Review timeout configuration
- Examine error logs for root cause
- Consider increasing threshold or timeout

### High Latency
- Check network latency to backend
- Review buffer pool configuration
- Monitor disk I/O (local backend)
- Check for lock contention

### Rate Limit Exceeded
- Increase rate limit configuration
- Implement client-side queuing
- Use multiple backends for load distribution

### Path Validation Errors
- Review path validation rules
- Use `SanitizeKey()` to clean paths
- Check for special characters

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for development guidelines.

## License

See [LICENSE](../../LICENSE) for license information.

