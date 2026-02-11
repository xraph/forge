package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// HealthMonitor monitors the health of upstream targets.
type HealthMonitor struct {
	config HealthCheckConfig
	logger forge.Logger

	mu      sync.RWMutex
	targets map[string]*monitoredTarget // key: targetID
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// Callbacks
	onHealthChange func(event UpstreamHealthEvent)

	client *http.Client
}

type monitoredTarget struct {
	target           *Target
	routeID          string
	consecutiveFails int
	consecutiveOK    int
	lastCheck        time.Time
}

// NewHealthMonitor creates a new health monitor.
func NewHealthMonitor(config HealthCheckConfig, logger forge.Logger) *HealthMonitor {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: config.Timeout,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	return &HealthMonitor{
		config:  config,
		logger:  logger,
		targets: make(map[string]*monitoredTarget),
		stopCh:  make(chan struct{}),
		client: &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		},
	}
}

// SetOnHealthChange sets the health change callback.
func (hm *HealthMonitor) SetOnHealthChange(fn func(event UpstreamHealthEvent)) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.onHealthChange = fn
}

// Register registers a target for health monitoring.
func (hm *HealthMonitor) Register(routeID string, target *Target) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.targets[target.ID] = &monitoredTarget{
		target:  target,
		routeID: routeID,
	}
}

// Deregister removes a target from health monitoring.
func (hm *HealthMonitor) Deregister(targetID string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	delete(hm.targets, targetID)
}

// RecordPassiveFailure records a passive health check failure.
func (hm *HealthMonitor) RecordPassiveFailure(targetID string) {
	if !hm.config.EnablePassive {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	mt, ok := hm.targets[targetID]
	if !ok {
		return
	}

	mt.consecutiveFails++
	mt.consecutiveOK = 0

	if mt.consecutiveFails >= hm.config.PassiveFailThreshold && mt.target.Healthy {
		hm.setHealthy(mt, false)
	}
}

// RecordPassiveSuccess records a passive health check success.
func (hm *HealthMonitor) RecordPassiveSuccess(targetID string) {
	if !hm.config.EnablePassive {
		return
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	mt, ok := hm.targets[targetID]
	if !ok {
		return
	}

	mt.consecutiveOK++

	if mt.consecutiveOK >= hm.config.SuccessThreshold {
		mt.consecutiveFails = 0
	}
}

// Start starts the health monitor background loop.
func (hm *HealthMonitor) Start(ctx context.Context) {
	if !hm.config.Enabled {
		return
	}

	hm.wg.Add(1)

	go hm.loop(ctx)
}

// Stop stops the health monitor.
func (hm *HealthMonitor) Stop() {
	close(hm.stopCh)
	hm.wg.Wait()
	hm.client.CloseIdleConnections()
}

func (hm *HealthMonitor) loop(ctx context.Context) {
	defer hm.wg.Done()

	ticker := time.NewTicker(hm.config.Interval)
	defer ticker.Stop()

	// Initial check
	hm.checkAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopCh:
			return
		case <-ticker.C:
			hm.checkAll(ctx)
		}
	}
}

func (hm *HealthMonitor) checkAll(ctx context.Context) {
	hm.mu.RLock()
	targets := make([]*monitoredTarget, 0, len(hm.targets))
	for _, mt := range hm.targets {
		targets = append(targets, mt)
	}
	hm.mu.RUnlock()

	var wg sync.WaitGroup

	for _, mt := range targets {
		wg.Add(1)

		go func(mt *monitoredTarget) {
			defer wg.Done()

			hm.checkTarget(ctx, mt)
		}(mt)
	}

	wg.Wait()
}

func (hm *HealthMonitor) checkTarget(ctx context.Context, mt *monitoredTarget) {
	checkCtx, cancel := context.WithTimeout(ctx, hm.config.Timeout)
	defer cancel()

	url := mt.target.URL + hm.config.Path

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, url, nil)
	if err != nil {
		hm.handleCheckFailure(mt, err)

		return
	}

	req.Header.Set("User-Agent", "forge-gateway-health-check")

	resp, err := hm.client.Do(req)
	if err != nil {
		hm.handleCheckFailure(mt, err)

		return
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		hm.handleCheckSuccess(mt)
	} else {
		hm.handleCheckFailure(mt, fmt.Errorf("unhealthy status: %d", resp.StatusCode))
	}
}

func (hm *HealthMonitor) handleCheckSuccess(mt *monitoredTarget) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	mt.consecutiveOK++
	mt.consecutiveFails = 0
	mt.lastCheck = time.Now()

	if !mt.target.Healthy && mt.consecutiveOK >= hm.config.SuccessThreshold {
		hm.setHealthy(mt, true)
	}
}

func (hm *HealthMonitor) handleCheckFailure(mt *monitoredTarget, err error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	mt.consecutiveFails++
	mt.consecutiveOK = 0
	mt.lastCheck = time.Now()

	if mt.target.Healthy && mt.consecutiveFails >= hm.config.FailureThreshold {
		hm.logger.Warn("upstream health check failed",
			forge.F("target_id", mt.target.ID),
			forge.F("target_url", mt.target.URL),
			forge.F("consecutive_failures", mt.consecutiveFails),
			forge.F("error", err),
		)

		hm.setHealthy(mt, false)
	}
}

// setHealthy updates target health and emits event (must be called with lock held).
func (hm *HealthMonitor) setHealthy(mt *monitoredTarget, healthy bool) {
	previous := mt.target.Healthy
	mt.target.Healthy = healthy

	if hm.onHealthChange != nil {
		event := UpstreamHealthEvent{
			TargetID:  mt.target.ID,
			TargetURL: mt.target.URL,
			Healthy:   healthy,
			Previous:  previous,
			RouteID:   mt.routeID,
			Timestamp: time.Now(),
		}

		go hm.onHealthChange(event)
	}

	if healthy {
		hm.logger.Info("upstream became healthy",
			forge.F("target_id", mt.target.ID),
			forge.F("target_url", mt.target.URL),
		)
	} else {
		hm.logger.Warn("upstream became unhealthy",
			forge.F("target_id", mt.target.ID),
			forge.F("target_url", mt.target.URL),
		)
	}
}

// Health returns an error if no targets are healthy (for HealthManager integration).
func (hm *HealthMonitor) Health(_ context.Context) error {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	healthyCount := 0

	for _, mt := range hm.targets {
		if mt.target.Healthy {
			healthyCount++
		}
	}

	if len(hm.targets) > 0 && healthyCount == 0 {
		return fmt.Errorf("no healthy upstream targets (0/%d)", len(hm.targets))
	}

	return nil
}
