package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RedisSentinelCache implements high availability caching using Redis Sentinel
type RedisSentinelCache struct {
	*RedisCache
	sentinelClient *redis.SentinelClient
	failoverClient *redis.Client
	masterName     string
	sentinels      []string
	currentMaster  *SentinelMasterInfo
	masterMu       sync.RWMutex
	monitoring     bool
	monitorCtx     context.Context
	monitorCancel  context.CancelFunc
}

// SentinelMasterInfo contains information about the current master
type SentinelMasterInfo struct {
	Name              string            `json:"name"`
	Address           string            `json:"address"`
	Role              string            `json:"role"`
	Flags             []string          `json:"flags"`
	LastOkPing        time.Duration     `json:"last_ok_ping"`
	LastPing          time.Duration     `json:"last_ping"`
	DownAfter         time.Duration     `json:"down_after"`
	InfoRefresh       time.Duration     `json:"info_refresh"`
	NumSlaves         int               `json:"num_slaves"`
	NumOtherSentinels int               `json:"num_other_sentinels"`
	Quorum            int               `json:"quorum"`
	FailoverTimeout   time.Duration     `json:"failover_timeout"`
	ParallelSyncs     int               `json:"parallel_syncs"`
	CustomFields      map[string]string `json:"custom_fields"`
	LastFailover      time.Time         `json:"last_failover"`
}

// SentinelSlaveInfo contains information about a slave
type SentinelSlaveInfo struct {
	Name               string            `json:"name"`
	Address            string            `json:"address"`
	Role               string            `json:"role"`
	Flags              []string          `json:"flags"`
	LastOkPing         time.Duration     `json:"last_ok_ping"`
	LastPing           time.Duration     `json:"last_ping"`
	MasterLinkStatus   string            `json:"master_link_status"`
	MasterLinkDownTime time.Duration     `json:"master_link_down_time"`
	SlavePriority      int               `json:"slave_priority"`
	SlaveReplOffset    int64             `json:"slave_repl_offset"`
	CustomFields       map[string]string `json:"custom_fields"`
}

// SentinelInfo contains comprehensive sentinel information
type SentinelInfo struct {
	Masters   map[string]*SentinelMasterInfo  `json:"masters"`
	Slaves    map[string][]*SentinelSlaveInfo `json:"slaves"`
	Sentinels map[string][]*SentinelInfo      `json:"sentinels"`
	UpdatedAt time.Time                       `json:"updated_at"`
}

// NewRedisSentinelCache creates a new Redis Sentinel cache
func NewRedisSentinelCache(name string, config *cachecore.RedisConfig, logger common.Logger, metrics common.Metrics) (*RedisSentinelCache, error) {
	if !config.EnableSentinel {
		return nil, fmt.Errorf("sentinel mode not enabled in config")
	}

	if len(config.SentinelAddrs) == 0 {
		return nil, fmt.Errorf("no sentinel addresses specified")
	}

	if config.MasterName == "" {
		return nil, fmt.Errorf("master name not specified")
	}

	baseCache, err := NewRedisCache(name, config, logger, metrics)
	if err != nil {
		return nil, err
	}

	sentinelCache := &RedisSentinelCache{
		RedisCache: baseCache,
		masterName: config.MasterName,
		sentinels:  config.SentinelAddrs,
	}

	if err := sentinelCache.initSentinel(); err != nil {
		return nil, fmt.Errorf("failed to initialize sentinel: %w", err)
	}

	return sentinelCache, nil
}

// initSentinel initializes the sentinel connection and monitoring
func (r *RedisSentinelCache) initSentinel() error {
	// Create sentinel client
	sentinelClient := redis.NewSentinelClient(&redis.Options{
		Addr:     r.sentinels[0], // OnStart with first sentinel
		Password: r.config.SentinelPassword,
	})

	r.sentinelClient = sentinelClient

	// Create failover client
	failoverClient := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    r.masterName,
		SentinelAddrs: r.sentinels,
		Password:      r.config.Password,
		DB:            r.config.Database,
		PoolSize:      r.config.PoolSize,
		MinIdleConns:  r.config.MinIdleConns,
		MaxIdleConns:  r.config.MaxIdleConns,
		DialTimeout:   r.config.DialTimeout,
		ReadTimeout:   r.config.ReadTimeout,
		WriteTimeout:  r.config.WriteTimeout,
	})

	r.failoverClient = failoverClient
	r.client = failoverClient

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), r.config.DialTimeout)
	defer cancel()

	if err := r.failoverClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to ping master through sentinel: %w", err)
	}

	// Load master information
	if err := r.loadMasterInfo(ctx); err != nil {
		return fmt.Errorf("failed to load master info: %w", err)
	}

	// OnStart monitoring
	r.startMonitoring()

	if r.logger != nil {
		r.logger.Info("Redis Sentinel initialized",
			logger.String("name", r.name),
			logger.String("master", r.masterName),
			logger.String("master_addr", r.currentMaster.Address),
			logger.Int("sentinels", len(r.sentinels)),
		)
	}

	return nil
}

// loadMasterInfo loads information about the current master
func (r *RedisSentinelCache) loadMasterInfo(ctx context.Context) error {
	masterAddr, err := r.sentinelClient.GetMasterAddrByName(ctx, r.masterName).Result()
	if err != nil {
		return fmt.Errorf("failed to get master address: %w", err)
	}

	if len(masterAddr) != 2 {
		return fmt.Errorf("invalid master address format")
	}

	// Get detailed master information
	masters, err := r.sentinelClient.Masters(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to get masters info: %w", err)
	}

	r.masterMu.Lock()
	defer r.masterMu.Unlock()

	for _, master := range masters {
		if masterSlice, ok := master.([]interface{}); ok {
			masterInfo := r.parseMasterInfo(masterSlice)
			if masterInfo.Name == r.masterName {
				r.currentMaster = masterInfo
				break
			}
		}
	}

	if r.currentMaster == nil {
		return fmt.Errorf("master %s not found", r.masterName)
	}

	return nil
}

// parseMasterInfo parses master information from Redis response
func (r *RedisSentinelCache) parseMasterInfo(master []interface{}) *SentinelMasterInfo {
	info := &SentinelMasterInfo{
		CustomFields: make(map[string]string),
	}

	for i := 0; i < len(master); i += 2 {
		if i+1 >= len(master) {
			break
		}

		key := fmt.Sprintf("%v", master[i])
		value := fmt.Sprintf("%v", master[i+1])

		switch key {
		case "name":
			info.Name = value
		case "ip":
			if i+2 < len(master) && fmt.Sprintf("%v", master[i+2]) == "port" {
				port := fmt.Sprintf("%v", master[i+3])
				info.Address = fmt.Sprintf("%s:%s", value, port)
				i += 2 // Skip port fields
			}
		case "role-reported":
			info.Role = value
		case "flags":
			info.Flags = strings.Split(value, ",")
		case "last-ok-ping-reply":
			if d, err := time.ParseDuration(value + "ms"); err == nil {
				info.LastOkPing = d
			}
		case "last-ping-reply":
			if d, err := time.ParseDuration(value + "ms"); err == nil {
				info.LastPing = d
			}
		case "down-after-milliseconds":
			if d, err := time.ParseDuration(value + "ms"); err == nil {
				info.DownAfter = d
			}
		case "info-refresh":
			if d, err := time.ParseDuration(value + "ms"); err == nil {
				info.InfoRefresh = d
			}
		case "num-slaves":
			info.NumSlaves = parseInt32(value)
		case "num-other-sentinels":
			info.NumOtherSentinels = parseInt32(value)
		case "quorum":
			info.Quorum = parseInt32(value)
		case "failover-timeout":
			if d, err := time.ParseDuration(value + "ms"); err == nil {
				info.FailoverTimeout = d
			}
		case "parallel-syncs":
			info.ParallelSyncs = parseInt32(value)
		default:
			info.CustomFields[key] = value
		}
	}

	return info
}

// startMonitoring starts monitoring sentinel events
func (r *RedisSentinelCache) startMonitoring() {
	r.monitorCtx, r.monitorCancel = context.WithCancel(context.Background())
	r.monitoring = true

	go r.monitorSentinel()
	go r.monitorMaster()
}

// stopMonitoring stops sentinel monitoring
func (r *RedisSentinelCache) stopMonitoring() {
	if r.monitorCancel != nil {
		r.monitorCancel()
	}
	r.monitoring = false
}

// monitorSentinel monitors sentinel events
func (r *RedisSentinelCache) monitorSentinel() {
	defer func() {
		if err := recover(); err != nil {
			if r.logger != nil {
				r.logger.Error("sentinel monitor panic recovered",
					logger.String("cache", r.name),
					logger.Any("panic", err),
				)
			}
		}
	}()

	pubsub := r.sentinelClient.Subscribe(r.monitorCtx,
		"+switch-master",
		"+slave",
		"+sdown",
		"+odown",
		"+failover-start",
		"+failover-end",
	)
	defer pubsub.Close()

	for {
		select {
		case <-r.monitorCtx.Done():
			return
		case msg := <-pubsub.Channel():
			if msg == nil {
				continue
			}
			r.handleSentinelEvent(msg.Channel, msg.Payload)
		}
	}
}

// handleSentinelEvent handles sentinel events
func (r *RedisSentinelCache) handleSentinelEvent(channel, payload string) {
	parts := strings.Fields(payload)

	switch channel {
	case "+switch-master":
		if len(parts) >= 4 && parts[0] == r.masterName {
			oldAddr := fmt.Sprintf("%s:%s", parts[1], parts[2])
			newAddr := fmt.Sprintf("%s:%s", parts[3], parts[4])

			r.handleMasterSwitch(oldAddr, newAddr)
		}
	case "+failover-start":
		if len(parts) >= 1 && parts[0] == r.masterName {
			r.handleFailoverStart()
		}
	case "+failover-end":
		if len(parts) >= 1 && parts[0] == r.masterName {
			r.handleFailoverEnd()
		}
	case "+sdown":
		r.handleNodeDown(payload, false)
	case "+odown":
		r.handleNodeDown(payload, true)
	}

	if r.logger != nil {
		r.logger.Debug("sentinel event received",
			logger.String("cache", r.name),
			logger.String("channel", channel),
			logger.String("payload", payload),
		)
	}
}

// handleMasterSwitch handles master switch events
func (r *RedisSentinelCache) handleMasterSwitch(oldAddr, newAddr string) {
	r.masterMu.Lock()
	defer r.masterMu.Unlock()

	if r.currentMaster != nil {
		r.currentMaster.Address = newAddr
		r.currentMaster.LastFailover = time.Now()
	}

	if r.logger != nil {
		r.logger.Warn("master switch detected",
			logger.String("cache", r.name),
			logger.String("master", r.masterName),
			logger.String("old_addr", oldAddr),
			logger.String("new_addr", newAddr),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.sentinel.master_switch", "cache", r.name).Inc()
	}

	// Refresh master info
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := r.loadMasterInfo(ctx); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to reload master info after switch",
				logger.String("cache", r.name),
				logger.Error(err),
			)
		}
	}
}

// handleFailoverStart handles failover start events
func (r *RedisSentinelCache) handleFailoverStart() {
	if r.logger != nil {
		r.logger.Warn("failover started",
			logger.String("cache", r.name),
			logger.String("master", r.masterName),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.sentinel.failover_start", "cache", r.name).Inc()
	}
}

// handleFailoverEnd handles failover end events
func (r *RedisSentinelCache) handleFailoverEnd() {
	if r.logger != nil {
		r.logger.Info("failover completed",
			logger.String("cache", r.name),
			logger.String("master", r.masterName),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.sentinel.failover_end", "cache", r.name).Inc()
	}
}

// handleNodeDown handles node down events
func (r *RedisSentinelCache) handleNodeDown(payload string, objective bool) {
	downType := "subjective"
	if objective {
		downType = "objective"
	}

	if r.logger != nil {
		r.logger.Warn("node down detected",
			logger.String("cache", r.name),
			logger.String("type", downType),
			logger.String("details", payload),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.sentinel.node_down", "cache", r.name, "type", downType).Inc()
	}
}

// monitorMaster periodically checks master health
func (r *RedisSentinelCache) monitorMaster() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.monitorCtx.Done():
			return
		case <-ticker.C:
			if err := r.checkMasterHealth(); err != nil {
				if r.logger != nil {
					r.logger.Warn("master health check failed",
						logger.String("cache", r.name),
						logger.String("master", r.masterName),
						logger.Error(err),
					)
				}
			}
		}
	}
}

// checkMasterHealth checks the health of the current master
func (r *RedisSentinelCache) checkMasterHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if we can still reach the master
	if err := r.failoverClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("master ping failed: %w", err)
	}

	// Refresh master info
	if err := r.loadMasterInfo(ctx); err != nil {
		return fmt.Errorf("failed to refresh master info: %w", err)
	}

	return nil
}

// GetMasterInfo returns current master information
func (r *RedisSentinelCache) GetMasterInfo() *SentinelMasterInfo {
	r.masterMu.RLock()
	defer r.masterMu.RUnlock()
	return r.currentMaster
}

// GetSlaves returns information about slaves
func (r *RedisSentinelCache) GetSlaves(ctx context.Context) ([]*SentinelSlaveInfo, error) {
	slaves, err := r.sentinelClient.Sentinels(ctx, r.masterName).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get slaves: %w", err)
	}

	slaveInfos := make([]*SentinelSlaveInfo, 0, len(slaves))
	for _, slave := range slaves {
		slaveInfo := r.parseSlaveInfo(slave)
		slaveInfos = append(slaveInfos, slaveInfo)
	}

	return slaveInfos, nil
}

// parseSlaveInfo parses slave information from Redis response
func (r *RedisSentinelCache) parseSlaveInfo(slave map[string]string) *SentinelSlaveInfo {
	info := &SentinelSlaveInfo{
		CustomFields: make(map[string]string),
	}

	info.CustomFields = slave

	// for i := 0; i < len(slave); i += 2 {
	// 	if i+1 >= len(slave) {
	// 		break
	// 	}
	//
	// 	key := fmt.Sprintf("%v", slave[i])
	// 	value := fmt.Sprintf("%v", slave[i+1])
	//
	// 	switch key {
	// 	case "name":
	// 		info.Name = value
	// 	case "ip":
	// 		if i+2 < len(slave) && fmt.Sprintf("%v", slave[i+2]) == "port" {
	// 			port := fmt.Sprintf("%v", slave[i+3])
	// 			info.Address = fmt.Sprintf("%s:%s", value, port)
	// 			i += 2 // Skip port fields
	// 		}
	// 	case "role-reported":
	// 		info.Role = value
	// 	case "flags":
	// 		info.Flags = strings.Split(value, ",")
	// 	case "last-ok-ping-reply":
	// 		if d, err := time.ParseDuration(value + "ms"); err == nil {
	// 			info.LastOkPing = d
	// 		}
	// 	case "last-ping-reply":
	// 		if d, err := time.ParseDuration(value + "ms"); err == nil {
	// 			info.LastPing = d
	// 		}
	// 	case "master-link-status":
	// 		info.MasterLinkStatus = value
	// 	case "master-link-down-time":
	// 		if d, err := time.ParseDuration(value + "ms"); err == nil {
	// 			info.MasterLinkDownTime = d
	// 		}
	// 	case "slave-priority":
	// 		info.SlavePriority = parseInt32(value)
	// 	case "slave-repl-offset":
	// 		info.SlaveReplOffset = parseInt64(value)
	// 	default:
	// 		info.CustomFields[key] = value
	// 	}
	// }

	return info
}

// GetSentinelInfo returns comprehensive sentinel information
func (r *RedisSentinelCache) GetSentinelInfo(ctx context.Context) (*SentinelInfo, error) {
	info := &SentinelInfo{
		Masters:   make(map[string]*SentinelMasterInfo),
		Slaves:    make(map[string][]*SentinelSlaveInfo),
		Sentinels: make(map[string][]*SentinelInfo),
		UpdatedAt: time.Now(),
	}

	// Get masters
	masters, err := r.sentinelClient.Masters(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get masters: %w", err)
	}

	for _, master := range masters {
		if masterSlice, ok := master.([]interface{}); ok {
			masterInfo := r.parseMasterInfo(masterSlice)
			info.Masters[masterInfo.Name] = masterInfo

			// Get slaves for this master
			slaves, err := r.GetSlaves(ctx)
			if err == nil {
				info.Slaves[masterInfo.Name] = slaves
			}
		}
	}

	return info, nil
}

// ForceFailover forces a failover for the master
func (r *RedisSentinelCache) ForceFailover(ctx context.Context) error {
	if err := r.sentinelClient.Failover(ctx, r.masterName).Err(); err != nil {
		return fmt.Errorf("failed to force failover: %w", err)
	}

	if r.logger != nil {
		r.logger.Warn("forced failover initiated",
			logger.String("cache", r.name),
			logger.String("master", r.masterName),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.sentinel.forced_failover", "cache", r.name).Inc()
	}

	return nil
}

// IsMonitoring returns whether sentinel monitoring is active
func (r *RedisSentinelCache) IsMonitoring() bool {
	return r.monitoring
}

// Stop stops the sentinel cache and monitoring
func (r *RedisSentinelCache) Stop(ctx context.Context) error {
	r.stopMonitoring()

	if r.sentinelClient != nil {
		if err := r.sentinelClient.Close(); err != nil {
			if r.logger != nil {
				r.logger.Error("failed to close sentinel client", logger.Error(err))
			}
		}
	}

	return r.RedisCache.Stop(ctx)
}

// Helper functions
func parseInt32(s string) int {
	if i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32); err == nil {
		return int(i)
	}
	return 0
}

func parseInt64(s string) int64 {
	if i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
		return i
	}
	return 0
}
