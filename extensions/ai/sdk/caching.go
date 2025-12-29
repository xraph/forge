package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// SemanticCache provides similarity-based caching with embeddings.
type SemanticCache struct {
	vectorStore VectorStore
	cacheStore  CacheStore
	logger      forge.Logger
	metrics     forge.Metrics

	// Configuration
	similarityThreshold float64
	ttl                 time.Duration
	maxCacheSize        int

	// Stats
	hits   int64
	misses int64
	mu     sync.RWMutex
}

// SemanticCacheConfig configures semantic caching.
type SemanticCacheConfig struct {
	SimilarityThreshold float64       // Minimum similarity score (0-1)
	TTL                 time.Duration // Cache entry TTL
	MaxCacheSize        int           // Maximum number of entries
	EnableMetrics       bool
}

// CachedEntry represents a cached result.
type CachedEntry struct {
	Query      string
	Response   string
	Embedding  []float64
	Usage      Usage
	Timestamp  time.Time
	Hits       int
	Similarity float64
}

// NewSemanticCache creates a new semantic cache.
func NewSemanticCache(
	vectorStore VectorStore,
	cacheStore CacheStore,
	logger forge.Logger,
	metrics forge.Metrics,
	config SemanticCacheConfig,
) *SemanticCache {
	if config.SimilarityThreshold == 0 {
		config.SimilarityThreshold = 0.95
	}

	if config.TTL == 0 {
		config.TTL = 1 * time.Hour
	}

	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = 10000
	}

	return &SemanticCache{
		vectorStore:         vectorStore,
		cacheStore:          cacheStore,
		logger:              logger,
		metrics:             metrics,
		similarityThreshold: config.SimilarityThreshold,
		ttl:                 config.TTL,
		maxCacheSize:        config.MaxCacheSize,
	}
}

// Get retrieves from cache using semantic similarity.
func (sc *SemanticCache) Get(ctx context.Context, query string, embedding []float64) (*CachedEntry, error) {
	// First try exact match in cache store
	cacheKey := sc.generateKey(query)
	if sc.cacheStore != nil {
		if data, found, err := sc.cacheStore.Get(ctx, cacheKey); err == nil && found {
			var entry CachedEntry
			if err := json.Unmarshal(data, &entry); err == nil {
				sc.recordHit()

				return &entry, nil
			}
		}
	}

	// Try semantic similarity search
	if sc.vectorStore != nil && len(embedding) > 0 {
		matches, err := sc.vectorStore.Query(ctx, embedding, 1, map[string]any{
			"type": "cache",
		})
		if err == nil && len(matches) > 0 {
			match := matches[0]
			if match.Score >= sc.similarityThreshold {
				// Found similar query
				if entryData, ok := match.Metadata["entry"].(string); ok {
					var entry CachedEntry
					if err := json.Unmarshal([]byte(entryData), &entry); err == nil {
						entry.Similarity = match.Score

						sc.recordHit()

						if sc.logger != nil {
							sc.logger.Debug("semantic cache hit",
								F("query", query),
								F("similarity", match.Score),
							)
						}

						return &entry, nil
					}
				}
			}
		}
	}

	sc.recordMiss()

	return nil, nil
}

// Set stores in cache with semantic indexing.
func (sc *SemanticCache) Set(ctx context.Context, query string, response string, embedding []float64, usage Usage) error {
	entry := CachedEntry{
		Query:     query,
		Response:  response,
		Embedding: embedding,
		Usage:     usage,
		Timestamp: time.Now(),
		Hits:      0,
	}

	// Store in cache store
	cacheKey := sc.generateKey(query)
	if sc.cacheStore != nil {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry: %w", err)
		}

		if err := sc.cacheStore.Set(ctx, cacheKey, data, sc.ttl); err != nil {
			return fmt.Errorf("failed to set cache: %w", err)
		}
	}

	// Store embedding in vector store for similarity search
	if sc.vectorStore != nil && len(embedding) > 0 {
		entryJSON, _ := json.Marshal(entry)

		vector := Vector{
			ID:     cacheKey,
			Values: embedding,
			Metadata: map[string]any{
				"query":     query,
				"entry":     string(entryJSON),
				"type":      "cache",
				"timestamp": time.Now().Unix(),
			},
		}
		if err := sc.vectorStore.Upsert(ctx, []Vector{vector}); err != nil {
			return fmt.Errorf("failed to index vector: %w", err)
		}
	}

	if sc.logger != nil {
		sc.logger.Debug("cached entry",
			F("query", query),
			F("response_length", len(response)),
		)
	}

	return nil
}

// Clear clears the cache.
func (sc *SemanticCache) Clear(ctx context.Context) error {
	if sc.cacheStore != nil {
		return sc.cacheStore.Clear(ctx)
	}

	return nil
}

// GetStats returns cache statistics.
func (sc *SemanticCache) GetStats() CacheStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	total := sc.hits + sc.misses

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(sc.hits) / float64(total)
	}

	return CacheStats{
		Hits:    sc.hits,
		Misses:  sc.misses,
		HitRate: hitRate,
	}
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Hits    int64
	Misses  int64
	HitRate float64
}

func (sc *SemanticCache) recordHit() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.hits++

	if sc.metrics != nil {
		sc.metrics.Counter("forge.ai.sdk.cache.hits").Inc()
	}
}

func (sc *SemanticCache) recordMiss() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.misses++

	if sc.metrics != nil {
		sc.metrics.Counter("forge.ai.sdk.cache.misses").Inc()
	}
}

func (sc *SemanticCache) generateKey(query string) string {
	hash := sha256.Sum256([]byte(query))

	return fmt.Sprintf("cache:%x", hash)
}
