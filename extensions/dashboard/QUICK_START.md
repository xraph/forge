# Quick Start Guide - Enhanced Dashboard

## Run the Example

```bash
cd v2/extensions/dashboard/examples/basic
go run main.go
```

Then open your browser to: **http://localhost:9090/dashboard**

---

## What's New? 🎉

### 1. Fixed Theme Switcher 🌓
- **Before:** Theme only affected some elements
- **After:** Entire page (including background) transitions smoothly between light and dark modes
- **How to test:** Click the sun/moon icon in the top-right corner

### 2. Service Detail View 🔍
- **Before:** Could only see basic service information
- **After:** Click any service card to see detailed metrics, health status, and more
- **How to test:** 
  1. Go to the Overview tab
  2. Scroll down to "Registered Services"
  3. Click on the "cache" service card
  4. A modal will open with detailed information

### 3. Comprehensive Metrics Report 📊
- **Before:** Metrics were scattered and not well organized
- **After:** Dedicated "Metrics Report" tab with complete breakdown
- **How to test:**
  1. Click the "Metrics Report" tab at the top
  2. View:
     - Statistics overview (total metrics, counts by type)
     - Pie chart showing metric distribution
     - Table of active collectors
     - List of top metrics with current values

---

## UI Overview

### Header
```
┌──────────────────────────────────────────────────────────────────┐
│  Dashboard Title                              🟢 Live    🌓 Theme │
└──────────────────────────────────────────────────────────────────┘
```

### Tab Navigation
```
┌──────────────────────────────────────────────────────────────────┐
│  Overview  |  Metrics Report                                     │
└──────────────────────────────────────────────────────────────────┘
```

### Overview Tab
```
┌─────────────┬─────────────┬─────────────┬─────────────┐
│   Health    │  Services   │   Metrics   │   Uptime    │
│   Healthy   │    1/1      │     156     │   2h 30m    │
└─────────────┴─────────────┴─────────────┴─────────────┘

┌───────────────────────┬───────────────────────┐
│ Service Health Chart  │ Service Count Chart   │
│     [Line Graph]      │    [Line Graph]       │
└───────────────────────┴───────────────────────┘

┌─────────────────────────────────────────────────────┐
│              Health Checks Table                    │
│  Service  │  Status   │  Message  │  Duration       │
│  cache    │  Healthy  │  OK       │  2ms            │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│           Registered Services                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
│  │  cache   │  │ database │  │  redis   │ ← Click!│
│  │ Healthy  │  │ Healthy  │  │ Degraded │         │
│  └──────────┘  └──────────┘  └──────────┘         │
└─────────────────────────────────────────────────────┘

[Export JSON] [Export CSV] [Export Prometheus]
```

### Service Detail Modal (Opens when you click a service)
```
┌────────────────────────────────────────────────────┐
│  cache Service Details                         [X] │
├────────────────────────────────────────────────────┤
│                                                    │
│  Status:             ● Healthy                     │
│  Type:               service                       │
│  Last Health Check:  2025-10-23 14:30:25          │
│                                                    │
│  Health Details:                                   │
│  ┌──────────────────────────────────────────────┐ │
│  │ Cache is operational and responding normally │ │
│  └──────────────────────────────────────────────┘ │
│                                                    │
│  Metrics (12):                                     │
│  ┌──────────────────────────────────────────────┐ │
│  │ cache_hits_total              1,234          │ │
│  │ cache_misses_total            56             │ │
│  │ cache_size_bytes              1,048,576      │ │
│  │ cache_evictions_total         12             │ │
│  │ ...                                          │ │
│  └──────────────────────────────────────────────┘ │
│                                                    │
└────────────────────────────────────────────────────┘
```

### Metrics Report Tab
```
┌─────────────┬─────────────┬─────────────┬─────────────┐
│   Total     │  Counters   │   Gauges    │ Histograms  │
│    156      │     42      │     68      │     31      │
└─────────────┴─────────────┴─────────────┴─────────────┘

┌───────────────────────────────────────────────────────┐
│           Metrics by Type                             │
│              [Pie Chart]                              │
│         Counters: 27%                                 │
│         Gauges: 44%                                   │
│         Histograms: 20%                               │
│         Timers: 9%                                    │
└───────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────┐
│           Active Collectors                           │
│  Name      │  Type   │  Metrics  │  Status            │
│  system    │  custom │  45       │  ● active          │
│  http      │  custom │  23       │  ● active          │
└───────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────┐
│           Top Metrics                                 │
│  http_requests_total (counter)            45,678      │
│  memory_usage_bytes (gauge)               1,073,741…  │
│  request_duration_ms (timer)              123.45      │
│  ...                                                  │
└───────────────────────────────────────────────────────┘
```

---

## Testing Checklist

### Theme Switcher
- [ ] Click theme toggle in header
- [ ] Verify entire page changes color (including background)
- [ ] Verify charts update their theme
- [ ] Reload page and verify theme persists

### Service Detail
- [ ] Click on a service card in the Overview tab
- [ ] Verify modal opens with service name in title
- [ ] Verify status, type, and last check time are shown
- [ ] Verify metrics list displays
- [ ] Click outside or on [X] to close modal
- [ ] Try in both light and dark modes

### Metrics Report
- [ ] Click "Metrics Report" tab
- [ ] Verify statistics cards show numbers
- [ ] Verify pie chart renders
- [ ] Verify collectors table has data
- [ ] Verify top metrics list shows values
- [ ] Try in both light and dark modes

### General
- [ ] Verify WebSocket shows "Live" when connected
- [ ] Verify data updates every 30 seconds
- [ ] Verify export buttons work
- [ ] Verify responsive design on mobile/tablet
- [ ] Verify no console errors in browser dev tools

---

## API Endpoints for Testing

### Get Service Detail
```bash
curl http://localhost:9090/dashboard/api/service-detail?name=cache | jq
```

### Get Metrics Report
```bash
curl http://localhost:9090/dashboard/api/metrics-report | jq
```

### Get Overview
```bash
curl http://localhost:9090/dashboard/api/overview | jq
```

### Export Prometheus
```bash
curl http://localhost:9090/dashboard/export/prometheus
```

---

## Troubleshooting

### Dashboard doesn't load
- Check that port 9090 is not already in use
- Verify the example is running: `ps aux | grep basic`
- Check application logs for errors

### Theme doesn't change
- Clear browser localStorage: `localStorage.clear()`
- Hard refresh: Ctrl+Shift+R (Windows/Linux) or Cmd+Shift+R (Mac)
- Check browser console for JavaScript errors

### Service modal doesn't open
- Verify JavaScript is enabled
- Check browser console for errors
- Ensure service name is valid

### Metrics Report shows "Loading..."
- Wait a few seconds (data loads on first tab click)
- Check network tab in browser dev tools
- Verify `/api/metrics-report` endpoint responds

### Charts don't render
- Verify ApexCharts CDN is accessible
- Check browser console for errors
- Ensure browser supports ES6 JavaScript

---

## Next Steps

1. **Customize the Dashboard:**
   - Modify `Config` in your application
   - Change port, title, theme, etc.

2. **Add Your Services:**
   - Register your extensions with the Forge app
   - They will automatically appear in the dashboard

3. **Monitor in Production:**
   - Deploy with your application
   - Use for real-time monitoring
   - Export data for external analysis

4. **Extend Further:**
   - Add custom metrics
   - Implement service-specific health checks
   - Create custom data visualizations

---

## Support

For issues or questions:
- See `README.md` for detailed documentation
- See `ENHANCEMENTS.md` for feature details
- See `IMPLEMENTATION_SUMMARY.md` for technical details
- Check examples in `examples/basic/`

---

**Happy Monitoring! 📊✨**

