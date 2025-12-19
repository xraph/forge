# Cron Job Extension

A production-grade cron job scheduler for Forge with distributed support, execution history, metrics, and web UI.

## Features

- ✅ **Flexible Scheduling**: Use standard cron expressions with seconds precision
- ✅ **Multiple Job Types**: Code-based handlers or shell commands
- ✅ **Execution History**: Track all job executions with detailed status
- ✅ **Retry Logic**: Automatic retries with exponential backoff
- ✅ **Concurrency Control**: Configurable worker pool for job execution
- ✅ **Storage Backends**: In-memory, database, or Redis
- ✅ **Distributed Mode**: Leader election and distributed locking (requires consensus extension)
- ✅ **REST API**: Full HTTP API for job management
- ✅ **Metrics**: Prometheus metrics for observability
- ✅ **Config-Based Jobs**: Define jobs in YAML/JSON files
- ✅ **Web UI**: Dashboard for job management and monitoring

## Quick Start

### Simple Mode (Single Instance)

```go
package main

import (
    "context"
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/cron"
)

func main() {
    app := forge.New()
    
    // Register cron extension
    cronExt := cron.NewExtension(
        cron.WithMode("simple"),
        cron.WithStorage("memory"),
        cron.WithMaxConcurrentJobs(5),
    )
    app.RegisterExtension(cronExt)
    
    // Register job handlers
    app.AfterRegister(func(ctx context.Context) error {
        registry := forge.MustResolve[*cron.JobRegistry](app.Container(), "cron.registry")
        
        // Register a handler
        registry.Register("sendReport", func(ctx context.Context, job *cron.Job) error {
            // Your job logic here
            return nil
        })
        
        // Create a job programmatically
        scheduler := forge.MustResolve[cron.Scheduler](app.Container(), "cron.scheduler")
        scheduler.AddJob(&cron.Job{
            ID:          "daily-report",
            Name:        "Daily Report",
            Schedule:    "0 9 * * *", // Every day at 9 AM
            HandlerName: "sendReport",
            Enabled:     true,
        })
        
        return nil
    })
    
    app.Run(context.Background())
}
```

### Configuration File

Create `jobs.yaml`:

```yaml
jobs:
  - id: cleanup-temp
    name: Cleanup Temporary Files
    schedule: "0 2 * * *"  # Daily at 2 AM
    command: /usr/local/bin/cleanup.sh
    timeout: 10m
    maxRetries: 3
    enabled: true
    
  - id: backup-database
    name: Database Backup
    schedule: "0 0 * * *"  # Daily at midnight
    command: /usr/local/bin/backup-db.sh
    args:
      - --compress
      - --output=/backups
    timeout: 30m
    enabled: true
```

Load jobs from config:

```go
app.AfterRegister(func(ctx context.Context) error {
    loader := cron.NewJobLoader(app.Logger(), registry)
    jobs, err := loader.LoadFromFile(ctx, "jobs.yaml")
    if err != nil {
        return err
    }
    
    for _, job := range jobs {
        scheduler.AddJob(job)
    }
    
    return nil
})
```

## Configuration

```yaml
extensions:
  cron:
    mode: simple                    # "simple" or "distributed"
    storage: memory                 # "memory", "database", or "redis"
    max_concurrent_jobs: 10
    default_timeout: 5m
    default_timezone: UTC
    max_retries: 3
    retry_backoff: 1s
    retry_multiplier: 2.0
    max_retry_backoff: 30s
    history_retention_days: 30
    enable_api: true
    api_prefix: /api/cron
    enable_web_ui: true
    enable_metrics: true
```

## Cron Schedule Format

The scheduler supports standard cron expressions with optional seconds:

```
┌────────────── second (0-59) [optional]
│ ┌──────────── minute (0-59)
│ │ ┌────────── hour (0-23)
│ │ │ ┌──────── day of month (1-31)
│ │ │ │ ┌────── month (1-12 or JAN-DEC)
│ │ │ │ │ ┌──── day of week (0-6 or SUN-SAT)
│ │ │ │ │ │
│ │ │ │ │ │
* * * * * *
```

Examples:
- `0 9 * * *` - Every day at 9 AM
- `*/15 * * * *` - Every 15 minutes
- `0 0 * * 0` - Every Sunday at midnight
- `0 9 * * 1-5` - Weekdays at 9 AM
- `30 2 1 * *` - 2:30 AM on the first of every month

## REST API

### Job Management

- `GET /api/cron/jobs` - List all jobs
- `POST /api/cron/jobs` - Create a job
- `GET /api/cron/jobs/:id` - Get job details
- `PUT /api/cron/jobs/:id` - Update a job
- `DELETE /api/cron/jobs/:id` - Delete a job
- `POST /api/cron/jobs/:id/trigger` - Manually trigger a job
- `POST /api/cron/jobs/:id/enable` - Enable a job
- `POST /api/cron/jobs/:id/disable` - Disable a job

### Execution History

- `GET /api/cron/executions` - List all executions
- `GET /api/cron/jobs/:id/executions` - Get job execution history
- `GET /api/cron/executions/:id` - Get execution details

### Statistics

- `GET /api/cron/stats` - Get scheduler statistics
- `GET /api/cron/jobs/:id/stats` - Get job statistics

### Health

- `GET /api/cron/health` - Health check

## Distributed Mode

For multi-instance deployments with leader election:

```yaml
extensions:
  cron:
    mode: distributed
    storage: redis
    redis_connection: default
    leader_election: true
    consensus_extension: consensus
    heartbeat_interval: 5s
    lock_ttl: 30s
```

Distributed mode ensures only one instance schedules jobs, with automatic failover.

## Metrics

Prometheus metrics exposed:

- `cron_jobs_total` - Total registered jobs
- `cron_executions_total` - Total executions by status
- `cron_execution_duration_seconds` - Execution duration histogram
- `cron_scheduler_lag_seconds` - Lag between scheduled and actual time
- `cron_executor_queue_size` - Current executor queue size
- `cron_leader_status` - Leader status (0=follower, 1=leader)

## Web UI

Access the web UI at `/cron/ui` (configurable) to:

- View all scheduled jobs
- Monitor execution history
- Manually trigger jobs
- Enable/disable jobs
- View real-time statistics

## Advanced Usage

### Retry Configuration

Configure retries per job:

```go
job := &cron.Job{
    ID:          "retry-example",
    Name:        "Job with Custom Retry",
    Schedule:    "*/5 * * * *",
    HandlerName: "unreliableTask",
    MaxRetries:  5,
    Timeout:     2 * time.Minute,
    Enabled:     true,
}
```

### Job Middleware

Add middleware to job handlers:

```go
// Logging middleware
loggingMiddleware := cron.CreateLoggingMiddleware(func(ctx context.Context, job *cron.Job, err error) {
    logger.Info("Job executed",
        "job_id", job.ID,
        "duration", time.Since(start),
        "error", err,
    )
})

// Panic recovery middleware
panicMiddleware := cron.CreatePanicRecoveryMiddleware(func(ctx context.Context, job *cron.Job, recovered interface{}) {
    logger.Error("Job panicked", "panic", recovered)
})

// Register with middleware
registry.RegisterWithMiddleware("myJob", handler, loggingMiddleware, panicMiddleware)
```

### Programmatic Job Management

```go
// Get extension instance
cronExt := app.Extension("cron").(*cron.Extension)

// Create job
job := &cron.Job{
    ID:       "dynamic-job",
    Name:     "Dynamically Created Job",
    Schedule: "0 * * * *",
    Command:  "/usr/local/bin/script.sh",
    Enabled:  true,
}
cronExt.CreateJob(ctx, job)

// Update job
update := &cron.JobUpdate{
    Enabled: forge.Ptr(false),
}
cronExt.UpdateJob(ctx, "dynamic-job", update)

// Delete job
cronExt.DeleteJob(ctx, "dynamic-job")

// Trigger job
executionID, err := cronExt.TriggerJob(ctx, "my-job")
```

## Security Considerations

- Command injection: Validate all command inputs
- API authentication: Integrate with auth extension
- Rate limiting: Limit manual job triggers
- Audit logging: Track all job modifications
- Secrets: Use environment variables, never hardcode

## License

See the main Forge license.

