# Dashboard Extension

A production-ready health and metrics dashboard extension for Forge with real-time monitoring, data export, and a modern web UI.

## 🎉 ForgeUI Integration Available!

The dashboard now supports **ForgeUI** - a modern, component-based UI framework for Go! This provides:
- ✅ Type-safe Go HTML generation with gomponents
- ✅ Beautiful shadcn-inspired components
- ✅ Server-side rendering with Alpine.js enhancement
- ✅ Integrated routing and theme system
- ✅ Better developer experience and maintainability

**See [FORGEUI_MIGRATION.md](FORGEUI_MIGRATION.md) for the ForgeUI integration guide and [examples/forgeui/](examples/forgeui/) for a complete example.**

## Features

- **Real-time Monitoring**: WebSocket-based live updates of health checks and metrics
- **Health Checks**: Monitor all registered services with detailed status information
- **Metrics Display**: View system metrics with historical trends
- **Beautiful UI**: Modern, responsive interface built with Tailwind CSS
- **Interactive Charts**: Visualize data with ApexCharts
- **Data Export**: Export dashboard data in JSON, CSV, and Prometheus formats
- **Dark Mode**: Auto, light, and dark theme support
- **Time-series Data**: Configurable data retention and history
- **Zero Build**: No compilation needed - uses CDN for assets

## Installation

The dashboard extension is included in Forge. Simply import it:

```go
import "github.com/xraph/forge/extensions/dashboard"
```

For ForgeUI integration, also import:

```go
import "github.com/xraph/forgeui"
```

## Quick Start

### Option 1: Standalone Dashboard (Original)

```go
package main

import (
    "context"
    "log"

    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/dashboard"
)

func main() {
    // Create Forge app
    app := forge.NewApp(forge.AppConfig{
        Name:        "my-app",
        Version:     "1.0.0",
        Environment: "production",
    })

    // Register dashboard extension
    app.RegisterExtension(dashboard.NewExtension(
        dashboard.WithPort(8080),
        dashboard.WithTitle("My App Dashboard"),
    ))

    // Start app
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        log.Fatal(err)
    }

    // Dashboard available at http://localhost:8080/dashboard
    select {}
}
```

### Option 2: ForgeUI Integration (Recommended)

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/dashboard"
    "github.com/xraph/forgeui"
)

func main() {
    // Create Forge app for backend
    forgeApp := forge.NewApp(forge.AppConfig{
        Name:    "my-app",
        Version: "1.0.0",
    })

    // Create ForgeUI app for web interface
    uiApp := forgeui.New(forgeui.WithDebug(true))

    // Configure dashboard
    dashboardConfig := dashboard.Config{
        BasePath:        "/dashboard",
        Title:           "My Application Dashboard",
        Theme:           "auto",
        EnableRealtime:  true,
        EnableExport:    true,
        RefreshInterval: 30 * time.Second,
    }

    // Create ForgeUI integration
    dashboardIntegration := dashboard.NewForgeUIIntegration(
        dashboardConfig,
        forgeApp.HealthManager(),
        forgeApp.Metrics(),
        forgeApp.Logger(),
        forgeApp.Container(),
    )

    // Register routes
    dashboardIntegration.RegisterRoutes(uiApp.Router())

    // Start services
    ctx := context.Background()
    dashboardIntegration.Start(ctx)
    defer dashboardIntegration.Stop(ctx)

    go forgeApp.Start(ctx)

    // Start server
    log.Println("Dashboard: http://localhost:8080/dashboard")
    http.ListenAndServe(":8080", uiApp.Router())
}
```

**Benefits of ForgeUI Integration:**
- Type-safe component-based UI
- Better performance with SSR
- Modern shadcn-inspired design
- Integrated theme system
- Easier customization and extension

## UI Features

### Overview Tab
- **Summary Cards**: Overall health, service count, metrics count, and system uptime
- **Interactive Charts**: Historical health and service count trends
- **Health Checks Table**: Detailed table of all health checks with status indicators
- **Services Grid**: Clickable service cards for quick access to details
- **Export Options**: One-click export in multiple formats

### Service Detail Modal
Click any service card to open a modal with:
- Service status and type
- Last health check information
- Service-specific metrics
- Health details and messages

### Metrics Report Tab
- **Statistics Overview**: Total metrics, breakdown by type (counters, gauges, histograms, timers)
- **Type Distribution Chart**: Pie chart showing metric type distribution
- **Active Collectors Table**: All registered metrics collectors with status
- **Top Metrics List**: Current values of top 20 metrics

### Theme Support
- **Dark Mode**: Full dark mode support for all UI elements
- **Theme Toggle**: Persistent theme preference via localStorage
- **Auto Detection**: Respects system preference if not explicitly set
- **Smooth Transitions**: Animated color transitions between themes

## Configuration

### Programmatic Configuration

```go
app.RegisterExtension(dashboard.NewExtension(
    dashboard.WithPort(8080),                          // Server port
    dashboard.WithBasePath("/dashboard"),              // Base URL path
    dashboard.WithTitle("My Dashboard"),               // Dashboard title
    dashboard.WithTheme("auto"),                       // Theme: auto, light, dark
    dashboard.WithRealtime(true),                      // Enable WebSocket updates
    dashboard.WithRefreshInterval(30*time.Second),     // Data refresh interval
    dashboard.WithExport(true),                        // Enable data export
    dashboard.WithHistoryDuration(1*time.Hour),        // Data retention period
    dashboard.WithMaxDataPoints(1000),                 // Max historical points
))
```

### YAML Configuration

Create a `config.yaml`:

```yaml
extensions:
  dashboard:
    port: 8080
    base_path: "/dashboard"
    
    # Features
    enable_auth: false
    enable_realtime: true
    enable_export: true
    export_formats:
      - json
      - csv
      - prometheus
    
    # Data collection
    refresh_interval: 30s
    history_duration: 1h
    max_data_points: 1000
    
    # UI settings
    theme: "auto"  # auto, light, dark
    title: "My Application Dashboard"
    
    # Server settings
    read_timeout: 30s
    write_timeout: 30s
    shutdown_timeout: 10s
```

Load configuration:

```go
app.RegisterExtension(dashboard.NewExtension(
    dashboard.WithRequireConfig(true),
))
```

## API Endpoints

### Overview Data
```
GET /dashboard/api/overview
```

Returns overview of system health, services, metrics, and uptime.

Response:
```json
{
  "timestamp": "2025-10-22T10:30:00Z",
  "overall_health": "healthy",
  "total_services": 5,
  "healthy_services": 5,
  "total_metrics": 42,
  "uptime": 3600000000000,
  "version": "1.0.0",
  "environment": "production"
}
```

### Health Checks
```
GET /dashboard/api/health
```

Returns detailed health check results for all services.

### Metrics
```
GET /dashboard/api/metrics
```

Returns all current metrics.

### Services
```
GET /dashboard/api/services
```

Returns list of registered services.

### Historical Data
```
GET /dashboard/api/history
```

Returns time-series historical data.

## Data Export

### JSON Export
```
GET /dashboard/export/json
```

Exports complete dashboard snapshot as JSON.

### CSV Export
```
GET /dashboard/export/csv
```

Exports dashboard data as CSV file.

### Prometheus Metrics
```
GET /dashboard/export/prometheus
```

Exports metrics in Prometheus text format for scraping.

## WebSocket API

Connect to real-time updates:

```javascript
const ws = new WebSocket('ws://localhost:8080/dashboard/ws');

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  
  switch (message.type) {
    case 'overview':
      console.log('Overview update:', message.data);
      break;
    case 'health':
      console.log('Health update:', message.data);
      break;
  }
};

// Send ping
ws.send(JSON.stringify({ type: 'ping' }));
```

## Advanced Usage

### Custom Dashboard Server

Access the dashboard server for advanced operations:

```go
dashboard := dashboard.NewExtension()
app.RegisterExtension(dashboard)

// After app starts
server := dashboard.Server()

// Access collector
collector := server.GetCollector()
overview := collector.CollectOverview(ctx)

// Access history
history := server.GetHistory()
data := history.GetAll()
```

### Integration with DI Container

The dashboard server is registered in the DI container:

```go
// Resolve dashboard from container
dashboardServer, err := forge.Resolve[*dashboard.DashboardServer](app.Container(), "dashboard")
if err != nil {
    log.Fatal(err)
}

// Use dashboard server
collector := dashboardServer.GetCollector()
```

## UI Features

### Overview Cards
- Overall system health status
- Service counts (healthy/total)
- Total metrics tracked
- System uptime

### Charts
- Service health history
- Service count trends
- Interactive with zoom/pan
- Auto-updating with WebSocket

### Health Checks Table
- Real-time service status
- Health check messages
- Check duration
- Last checked timestamp

### Services List
- All registered services
- Service types
- Current status

### Export Buttons
- Quick access to JSON export
- CSV download
- Prometheus metrics

### Theme Toggle
- Auto (system preference)
- Light mode
- Dark mode
- Persistent per session

## Performance

- **Efficient Data Collection**: Configurable refresh intervals
- **Memory-Efficient**: Ring buffer for historical data
- **Concurrent Operations**: Thread-safe data access
- **WebSocket Optimizations**: Client-side reconnection, heartbeats
- **Minimal Overhead**: ~5MB memory, <1% CPU when idle

## Production Considerations

### Security

1. **Enable Authentication**: Use `WithAuth(true)` and implement auth middleware
2. **HTTPS**: Always use TLS in production
3. **CORS**: Configure proper CORS policies
4. **Rate Limiting**: Add rate limiting for API endpoints

### Monitoring

The dashboard monitors itself:
- Health check for server status
- Metrics for request counts
- WebSocket connection counts
- Export operation tracking

### Scalability

- WebSocket supports 100+ concurrent connections
- Historical data auto-trimmed based on retention policy
- Efficient memory usage with ring buffers
- Graceful degradation if WebSocket fails

## Troubleshooting

### Dashboard Won't Start

Check logs for:
- Port conflicts (default 8080)
- Permission issues
- Configuration errors

### WebSocket Not Connecting

1. Check browser console for errors
2. Verify firewall allows WebSocket
3. Ensure `enable_realtime: true` in config
4. Check WebSocket URL (ws:// for HTTP, wss:// for HTTPS)

### No Data Showing

1. Verify other extensions are registered before dashboard
2. Check that services implement health checks
3. Ensure metrics are being collected
4. Check API endpoint responses in browser network tab

### High Memory Usage

1. Reduce `max_data_points` in config
2. Shorten `history_duration`
3. Decrease `refresh_interval`

## Examples

See `examples/basic/` for a complete working example.

## Dependencies

- **gorilla/websocket**: WebSocket support
- **Tailwind CSS**: Styling (via CDN)
- **ApexCharts**: Data visualization (via CDN)

## License

Part of the Forge framework. See main repository for license information.

## Contributing

Contributions welcome! Please see the main Forge repository for contribution guidelines.

## Support

- GitHub Issues: Report bugs and request features
- Documentation: Full Forge documentation
- Examples: See examples/ directory

