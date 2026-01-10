package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// MemoryTier represents the tier of memory.
type MemoryTier string

const (
	// MemoryTierWorking is immediate, volatile memory (limited capacity).
	MemoryTierWorking MemoryTier = "working"
	// MemoryTierShortTerm is recent memory (hours to days, medium capacity).
	MemoryTierShortTerm MemoryTier = "short_term"
	// MemoryTierLongTerm is persistent memory (permanent, unlimited capacity).
	MemoryTierLongTerm MemoryTier = "long_term"
	// MemoryTierEpisodic is event-based memory (specific experiences).
	MemoryTierEpisodic MemoryTier = "episodic"
)

// MemoryEntry represents a single memory entry.
type MemoryEntry struct {
	ID          string         `json:"id"`
	Tier        MemoryTier     `json:"tier"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata"`
	Embedding   []float64      `json:"embedding,omitempty"`
	Importance  float64        `json:"importance"` // 0.0 to 1.0
	AccessCount int            `json:"access_count"`
	LastAccess  time.Time      `json:"last_access"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
}

// EpisodicMemory represents a specific episode or event.
type EpisodicMemory struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Participants []string       `json:"participants,omitempty"`
	Location     string         `json:"location,omitempty"`
	Metadata     map[string]any `json:"metadata"`
	Memories     []string       `json:"memories"` // Memory entry IDs
	Timestamp    time.Time      `json:"timestamp"`
	Duration     time.Duration  `json:"duration"`
	Importance   float64        `json:"importance"`
}

// MemoryManager manages multi-tier memory for agents.
type MemoryManager struct {
	agentID string
	logger  forge.Logger
	metrics forge.Metrics
	store   StateStore
	vector  VectorStore
	embedFn func(context.Context, string) ([]float64, error)

	// Memory storage
	working  map[string]*MemoryEntry
	episodic map[string]*EpisodicMemory
	mu       sync.RWMutex

	// ID generation
	idCounter uint64

	// Configuration
	workingCapacity   int
	shortTermTTL      time.Duration
	importanceDecay   float64
	consolidationTime time.Time
	autoConsolidate   bool
}

// MemoryManagerOptions configures the memory manager.
type MemoryManagerOptions struct {
	AgentID           string
	WorkingCapacity   int           // Max entries in working memory (default: 10)
	ShortTermTTL      time.Duration // TTL for short-term memory (default: 24h)
	ImportanceDecay   float64       // Importance decay rate (default: 0.1)
	AutoConsolidate   bool          // Automatically consolidate memories (default: true)
	EmbeddingFunction func(context.Context, string) ([]float64, error)
}

// NewMemoryManager creates a new memory manager.
func NewMemoryManager(
	store StateStore,
	vector VectorStore,
	logger forge.Logger,
	metrics forge.Metrics,
	opts *MemoryManagerOptions,
) *MemoryManager {
	mm := &MemoryManager{
		logger:            logger,
		metrics:           metrics,
		store:             store,
		vector:            vector,
		working:           make(map[string]*MemoryEntry),
		episodic:          make(map[string]*EpisodicMemory),
		workingCapacity:   10,
		shortTermTTL:      24 * time.Hour,
		importanceDecay:   0.1,
		consolidationTime: time.Now(),
		autoConsolidate:   true,
	}

	if opts != nil {
		mm.agentID = opts.AgentID
		if opts.WorkingCapacity > 0 {
			mm.workingCapacity = opts.WorkingCapacity
		}

		if opts.ShortTermTTL > 0 {
			mm.shortTermTTL = opts.ShortTermTTL
		}

		if opts.ImportanceDecay > 0 {
			mm.importanceDecay = opts.ImportanceDecay
		}

		mm.autoConsolidate = opts.AutoConsolidate
		mm.embedFn = opts.EmbeddingFunction
	}

	return mm
}

// Store adds a memory to the appropriate tier.
func (mm *MemoryManager) Store(ctx context.Context, content string, metadata map[string]any, importance float64) (*MemoryEntry, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.idCounter++
	entry := &MemoryEntry{
		ID:          fmt.Sprintf("mem_%s_%d", mm.agentID, mm.idCounter),
		Tier:        MemoryTierWorking,
		Content:     content,
		Metadata:    metadata,
		Importance:  importance,
		AccessCount: 0,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
	}

	// Generate embedding if function provided
	if mm.embedFn != nil {
		embedding, err := mm.embedFn(ctx, content)
		if err != nil {
			if mm.logger != nil {
				mm.logger.Warn("Failed to generate embedding", F("error", err.Error()))
			}
		} else {
			entry.Embedding = embedding
		}
	}

	// Add to working memory
	mm.working[entry.ID] = entry

	// Check capacity and consolidate if needed
	if len(mm.working) > mm.workingCapacity {
		if err := mm.consolidateWorking(ctx); err != nil {
			if mm.logger != nil {
				mm.logger.Warn("Failed to consolidate working memory", F("error", err.Error()))
			}
		}
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.store", "tier", string(MemoryTierWorking)).Inc()
	}

	return entry, nil
}

// Recall retrieves memories based on query and tier.
func (mm *MemoryManager) Recall(ctx context.Context, query string, tier MemoryTier, limit int) ([]*MemoryEntry, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	var memories []*MemoryEntry

	switch tier {
	case MemoryTierWorking:
		// Search working memory
		for _, entry := range mm.working {
			memories = append(memories, entry)
		}

	case MemoryTierShortTerm, MemoryTierLongTerm:
		// Search persistent storage via vector store
		if mm.vector != nil && mm.embedFn != nil {
			embedding, err := mm.embedFn(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("failed to generate query embedding: %w", err)
			}

			filter := map[string]any{
				"tier":     string(tier),
				"agent_id": mm.agentID,
			}

			matches, err := mm.vector.Query(ctx, embedding, limit, filter)
			if err != nil {
				return nil, fmt.Errorf("failed to query vector store: %w", err)
			}

			// Convert matches to memory entries
			for _, match := range matches {
				entry := &MemoryEntry{
					ID:       match.ID,
					Tier:     tier,
					Metadata: match.Metadata,
				}
				// Fetch full content from metadata
				if content, ok := match.Metadata["content"].(string); ok {
					entry.Content = content
				}

				if importance, ok := match.Metadata["importance"].(float64); ok {
					entry.Importance = importance
				}

				memories = append(memories, entry)
			}
		}

	case MemoryTierEpisodic:
		// Search episodic memories
		for _, episode := range mm.episodic {
			// Simple keyword search (in production, use semantic search)
			// This is placeholder logic
			memories = append(memories, &MemoryEntry{
				ID:         episode.ID,
				Tier:       MemoryTierEpisodic,
				Content:    episode.Description,
				Metadata:   episode.Metadata,
				Importance: episode.Importance,
				CreatedAt:  episode.Timestamp,
			})
		}
	}

	// Sort by importance and recency
	sort.Slice(memories, func(i, j int) bool {
		// Combine importance with recency
		scoreI := memories[i].Importance*0.7 + float64(memories[i].AccessCount)*0.3
		scoreJ := memories[j].Importance*0.7 + float64(memories[j].AccessCount)*0.3

		return scoreI > scoreJ
	})

	// Limit results
	if len(memories) > limit {
		memories = memories[:limit]
	}

	// Update access counts
	for _, entry := range memories {
		if original, ok := mm.working[entry.ID]; ok {
			original.AccessCount++
			original.LastAccess = time.Now()
		}
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.recall", "tier", string(tier)).Inc()
		mm.metrics.Histogram("forge.ai.sdk.memory.recall_count").Observe(float64(len(memories)))
	}

	return memories, nil
}

// consolidateWorking moves less important memories from working to short-term.
func (mm *MemoryManager) consolidateWorking(ctx context.Context) error {
	// Sort memories by importance and access
	type scoredEntry struct {
		entry *MemoryEntry
		score float64
	}

	scored := make([]scoredEntry, 0, len(mm.working))
	for _, entry := range mm.working {
		// Calculate retention score
		recency := time.Since(entry.LastAccess).Hours()
		score := entry.Importance + float64(entry.AccessCount)*0.1 - recency*mm.importanceDecay
		scored = append(scored, scoredEntry{entry, score})
	}

	// Sort by score (lowest first for removal)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score < scored[j].score
	})

	// Move lowest scoring entries to short-term
	toMove := len(scored) - mm.workingCapacity
	if toMove <= 0 {
		return nil
	}

	// Move entries until we've removed enough from working memory
	moved := 0
	for i := 0; i < len(scored) && moved < toMove; i++ {
		entry := scored[i].entry

		// Only consolidate if we have vector store and embedding
		// Otherwise keep in working memory
		if mm.vector == nil || len(entry.Embedding) == 0 {
			continue
		}

		// Move to short-term tier
		entry.Tier = MemoryTierShortTerm
		expiresAt := time.Now().Add(mm.shortTermTTL)
		entry.ExpiresAt = &expiresAt

		// Store in vector database
		vector := Vector{
			ID:     entry.ID,
			Values: entry.Embedding,
			Metadata: map[string]any{
				"tier":         string(MemoryTierShortTerm),
				"content":      entry.Content,
				"importance":   entry.Importance,
				"agent_id":     mm.agentID,
				"created_at":   entry.CreatedAt.Unix(),
				"expires_at":   entry.ExpiresAt.Unix(),
				"access_count": entry.AccessCount,
			},
		}
		if err := mm.vector.Upsert(ctx, []Vector{vector}); err != nil {
			return fmt.Errorf("failed to store in vector database: %w", err)
		}

		// Remove from working memory only after successful storage
		delete(mm.working, entry.ID)

		moved++

		if mm.logger != nil {
			mm.logger.Debug("Consolidated memory to short-term",
				F("memory_id", entry.ID),
				F("importance", entry.Importance),
			)
		}
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.consolidations").Inc()
		mm.metrics.Histogram("forge.ai.sdk.memory.consolidated_count").Observe(float64(moved))
	}

	return nil
}

// Promote elevates a memory from short-term to long-term.
func (mm *MemoryManager) Promote(ctx context.Context, memoryID string) error {
	// Update tier in vector store
	if mm.vector == nil {
		return errors.New("vector store not configured")
	}

	// Fetch the memory (without holding lock to avoid deadlock)
	memories, err := mm.Recall(ctx, "", MemoryTierShortTerm, 1000)
	if err != nil {
		return fmt.Errorf("failed to fetch memory: %w", err)
	}

	var targetMemory *MemoryEntry

	for _, mem := range memories {
		if mem.ID == memoryID {
			targetMemory = mem

			break
		}
	}

	if targetMemory == nil {
		return fmt.Errorf("memory not found: %s", memoryID)
	}

	// Now acquire lock for the update
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Update to long-term
	targetMemory.Tier = MemoryTierLongTerm
	targetMemory.ExpiresAt = nil // No expiration for long-term

	// Update in vector store
	if len(targetMemory.Embedding) > 0 {
		vector := Vector{
			ID:     targetMemory.ID,
			Values: targetMemory.Embedding,
			Metadata: map[string]any{
				"tier":         string(MemoryTierLongTerm),
				"content":      targetMemory.Content,
				"importance":   targetMemory.Importance,
				"agent_id":     mm.agentID,
				"created_at":   targetMemory.CreatedAt.Unix(),
				"access_count": targetMemory.AccessCount,
			},
		}
		if err := mm.vector.Upsert(ctx, []Vector{vector}); err != nil {
			return fmt.Errorf("failed to update vector database: %w", err)
		}
	}

	if mm.logger != nil {
		mm.logger.Info("Promoted memory to long-term", F("memory_id", memoryID))
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.promotions").Inc()
	}

	return nil
}

// CreateEpisode creates an episodic memory from multiple memories.
func (mm *MemoryManager) CreateEpisode(ctx context.Context, title, description string, memoryIDs []string, metadata map[string]any) (*EpisodicMemory, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	episode := &EpisodicMemory{
		ID:          fmt.Sprintf("episode_%d", time.Now().UnixNano()),
		Title:       title,
		Description: description,
		Metadata:    metadata,
		Memories:    memoryIDs,
		Timestamp:   time.Now(),
		Importance:  0.8, // Default high importance for episodes
	}

	mm.episodic[episode.ID] = episode

	// Persist to state store
	if mm.store != nil {
		data, err := json.Marshal(episode)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal episode: %w", err)
		}

		episodeState := &AgentState{
			AgentID:   mm.agentID,
			SessionID: "episode_" + episode.ID,
			Version:   1,
			Data: map[string]any{
				"episode": string(data),
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := mm.store.Save(ctx, episodeState); err != nil {
			return nil, fmt.Errorf("failed to save episode: %w", err)
		}
	}

	if mm.logger != nil {
		mm.logger.Info("Created episodic memory",
			F("episode_id", episode.ID),
			F("title", title),
			F("memory_count", len(memoryIDs)),
		)
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.episodes_created").Inc()
	}

	return episode, nil
}

// GetEpisode retrieves an episodic memory by ID.
func (mm *MemoryManager) GetEpisode(episodeID string) (*EpisodicMemory, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	episode, ok := mm.episodic[episodeID]
	if !ok {
		return nil, fmt.Errorf("episode not found: %s", episodeID)
	}

	return episode, nil
}

// Forget removes a memory by ID from all tiers.
func (mm *MemoryManager) Forget(ctx context.Context, memoryID string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Remove from working memory
	delete(mm.working, memoryID)

	// Remove from vector store
	if mm.vector != nil {
		if err := mm.vector.Delete(ctx, []string{memoryID}); err != nil {
			return fmt.Errorf("failed to delete from vector store: %w", err)
		}
	}

	if mm.logger != nil {
		mm.logger.Debug("Forgot memory", F("memory_id", memoryID))
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.forgettings").Inc()
	}

	return nil
}

// PruneExpired removes expired short-term memories.
func (mm *MemoryManager) PruneExpired(ctx context.Context) (int, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	pruned := 0

	// Note: In a real implementation, you'd query the vector store
	// for expired memories and delete them based on ExpiresAt
	// This is a simplified version

	if mm.logger != nil {
		mm.logger.Debug("Pruned expired memories", F("count", pruned))
	}

	if mm.metrics != nil {
		mm.metrics.Counter("forge.ai.sdk.memory.prunings").Inc()
		mm.metrics.Histogram("forge.ai.sdk.memory.pruned_count").Observe(float64(pruned))
	}

	return pruned, nil
}

// GetStats returns memory statistics.
func (mm *MemoryManager) GetStats() MemoryStats {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	return MemoryStats{
		WorkingCount:      len(mm.working),
		EpisodicCount:     len(mm.episodic),
		WorkingCapacity:   mm.workingCapacity,
		LastConsolidation: mm.consolidationTime,
	}
}

// MemoryStats represents memory statistics.
type MemoryStats struct {
	WorkingCount      int
	ShortTermCount    int
	LongTermCount     int
	EpisodicCount     int
	WorkingCapacity   int
	LastConsolidation time.Time
}

// Clear removes all memories from all tiers.
func (mm *MemoryManager) Clear(ctx context.Context) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.working = make(map[string]*MemoryEntry)
	mm.episodic = make(map[string]*EpisodicMemory)

	if mm.logger != nil {
		mm.logger.Info("Cleared all memories")
	}

	return nil
}
