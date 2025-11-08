package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

func TestNewMemoryManager(t *testing.T) {
	store := &MockStateStore{}
	vector := &MockVectorStore{}
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	opts := &MemoryManagerOptions{
		AgentID:         "agent-1",
		WorkingCapacity: 5,
		ShortTermTTL:    12 * time.Hour,
		ImportanceDecay: 0.2,
		AutoConsolidate: true,
	}

	mm := NewMemoryManager(store, vector, logger, metrics, opts)

	if mm == nil {
		t.Fatal("expected memory manager to be created")
	}

	if mm.agentID != "agent-1" {
		t.Errorf("expected agent ID 'agent-1', got '%s'", mm.agentID)
	}

	if mm.workingCapacity != 5 {
		t.Errorf("expected working capacity 5, got %d", mm.workingCapacity)
	}

	if mm.shortTermTTL != 12*time.Hour {
		t.Errorf("expected short term TTL 12h, got %v", mm.shortTermTTL)
	}
}

func TestNewMemoryManager_Defaults(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, nil)

	if mm == nil {
		t.Fatal("expected memory manager with defaults")
	}

	if mm.workingCapacity != 10 {
		t.Errorf("expected default working capacity 10, got %d", mm.workingCapacity)
	}

	if mm.shortTermTTL != 24*time.Hour {
		t.Errorf("expected default short term TTL 24h, got %v", mm.shortTermTTL)
	}

	if mm.importanceDecay != 0.1 {
		t.Errorf("expected default importance decay 0.1, got %f", mm.importanceDecay)
	}
}

func TestMemoryManager_Store(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID:         "agent-1",
		WorkingCapacity: 10,
	})

	metadata := map[string]any{
		"type": "conversation",
	}

	entry, err := mm.Store(context.Background(), "User asked about weather", metadata, 0.8)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if entry == nil {
		t.Fatal("expected memory entry to be created")
	}

	if entry.Content != "User asked about weather" {
		t.Error("expected content to match")
	}

	if entry.Importance != 0.8 {
		t.Errorf("expected importance 0.8, got %f", entry.Importance)
	}

	if entry.Tier != MemoryTierWorking {
		t.Errorf("expected tier 'working', got '%s'", entry.Tier)
	}

	// Verify it's in working memory
	if len(mm.working) != 1 {
		t.Errorf("expected 1 entry in working memory, got %d", len(mm.working))
	}
}

func TestMemoryManager_Store_WithEmbedding(t *testing.T) {
	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2, 0.3}, nil
	}

	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		WorkingCapacity:   10,
		EmbeddingFunction: embedFn,
	})

	entry, err := mm.Store(context.Background(), "Test content", nil, 0.5)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(entry.Embedding) == 0 {
		t.Error("expected embedding to be generated")
	}

	if len(entry.Embedding) != 3 {
		t.Errorf("expected 3 embedding dimensions, got %d", len(entry.Embedding))
	}
}

func TestMemoryManager_Recall_Working(t *testing.T) {
	mockVector := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return nil
		},
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:         "agent-1",
		WorkingCapacity: 10, // Ensure enough capacity
	})

	t.Logf("Working capacity: %d", mm.workingCapacity)

	// Store some memories
	_, err1 := mm.Store(context.Background(), "Memory 1", nil, 0.9)
	if err1 != nil {
		t.Fatalf("failed to store memory 1: %v", err1)
	}

	t.Logf("After mem1: working=%d", len(mm.working))

	_, err2 := mm.Store(context.Background(), "Memory 2", nil, 0.7)
	if err2 != nil {
		t.Fatalf("failed to store memory 2: %v", err2)
	}

	t.Logf("After mem2: working=%d", len(mm.working))

	_, err3 := mm.Store(context.Background(), "Memory 3", nil, 0.5)
	if err3 != nil {
		t.Fatalf("failed to store memory 3: %v", err3)
	}

	t.Logf("After mem3: working=%d", len(mm.working))

	memories, err := mm.Recall(context.Background(), "test query", MemoryTierWorking, 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(memories) != 3 {
		t.Errorf("expected 3 memories, got %d", len(memories))
	}

	// Should be sorted by importance (highest first)
	if len(memories) >= 2 && memories[0].Importance < memories[1].Importance {
		t.Error("expected memories to be sorted by importance")
	}
}

func TestMemoryManager_Recall_ShortTerm(t *testing.T) {
	// Mock vector store that returns results
	mockVector := &MockVectorStore{
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return []VectorMatch{
				{
					ID:    "mem1",
					Score: 0.9,
					Metadata: map[string]any{
						"content":    "Short term memory",
						"importance": 0.8,
					},
				},
			}, nil
		},
	}

	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		EmbeddingFunction: embedFn,
	})

	memories, err := mm.Recall(context.Background(), "test query", MemoryTierShortTerm, 5)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(memories) == 0 {
		t.Error("expected to recall short-term memories")
	}

	if memories[0].Content != "Short term memory" {
		t.Error("expected content to be retrieved")
	}
}

func TestMemoryManager_Recall_Limit(t *testing.T) {
	mockVector := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return nil
		},
	}

	// Provide embedding function so memories can be consolidated
	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		WorkingCapacity:   3, // Set capacity to 3 to test consolidation
		EmbeddingFunction: embedFn,
	})

	// Store 5 memories (more than capacity)
	for range 5 {
		mm.Store(context.Background(), "Memory", nil, 0.5)
	}

	// Recall with limit of 3
	memories, err := mm.Recall(context.Background(), "", MemoryTierWorking, 3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(memories) != 3 {
		t.Errorf("expected 3 memories (limit), got %d", len(memories))
	}
}

func TestMemoryManager_ConsolidateWorking(t *testing.T) {
	mockVector := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return nil
		},
	}

	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		WorkingCapacity:   2,
		EmbeddingFunction: embedFn,
	})

	// Store more than capacity
	mm.Store(context.Background(), "Memory 1", nil, 0.9)
	mm.Store(context.Background(), "Memory 2", nil, 0.7)
	mm.Store(context.Background(), "Memory 3", nil, 0.5) // Should trigger consolidation

	// Working memory should be at capacity
	if len(mm.working) > mm.workingCapacity {
		t.Errorf("expected working memory to be at capacity %d, got %d", mm.workingCapacity, len(mm.working))
	}
}

func TestMemoryManager_Promote(t *testing.T) {
	mockVector := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return nil
		},
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return []VectorMatch{
				{
					ID:    "mem1",
					Score: 0.9,
					Metadata: map[string]any{
						"content":    "Memory to promote",
						"importance": 0.8,
					},
				},
			}, nil
		},
	}

	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		EmbeddingFunction: embedFn,
	})

	err := mm.Promote(context.Background(), "mem1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMemoryManager_Promote_NotFound(t *testing.T) {
	mockVector := &MockVectorStore{
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return []VectorMatch{}, nil
		},
	}

	embedFn := func(ctx context.Context, text string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:           "agent-1",
		EmbeddingFunction: embedFn,
	})

	err := mm.Promote(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent memory")
	}
}

func TestMemoryManager_CreateEpisode(t *testing.T) {
	store := &MockStateStore{
		SaveFunc: func(ctx context.Context, state *AgentState) error {
			return nil
		},
	}

	mm := NewMemoryManager(store, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	memoryIDs := []string{"mem1", "mem2", "mem3"}
	metadata := map[string]any{
		"category": "meeting",
	}

	episode, err := mm.CreateEpisode(
		context.Background(),
		"Daily Standup",
		"Team discussed project progress",
		memoryIDs,
		metadata,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if episode == nil {
		t.Fatal("expected episode to be created")
	}

	if episode.Title != "Daily Standup" {
		t.Error("expected title to match")
	}

	if len(episode.Memories) != 3 {
		t.Errorf("expected 3 memory IDs, got %d", len(episode.Memories))
	}

	if episode.Importance != 0.8 {
		t.Errorf("expected default importance 0.8, got %f", episode.Importance)
	}

	// Verify it's stored
	if len(mm.episodic) != 1 {
		t.Errorf("expected 1 episode in storage, got %d", len(mm.episodic))
	}
}

func TestMemoryManager_GetEpisode(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	// Create an episode
	episode, _ := mm.CreateEpisode(
		context.Background(),
		"Test Episode",
		"Description",
		[]string{"mem1"},
		nil,
	)

	// Retrieve it
	retrieved, err := mm.GetEpisode(episode.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved.ID != episode.ID {
		t.Error("expected retrieved episode to match")
	}

	if retrieved.Title != "Test Episode" {
		t.Error("expected title to match")
	}
}

func TestMemoryManager_GetEpisode_NotFound(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	_, err := mm.GetEpisode("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent episode")
	}
}

func TestMemoryManager_Forget(t *testing.T) {
	mockVector := &MockVectorStore{
		DeleteFunc: func(ctx context.Context, ids []string) error {
			return nil
		},
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	// Store a memory
	entry, _ := mm.Store(context.Background(), "Memory to forget", nil, 0.5)

	// Verify it exists
	if len(mm.working) != 1 {
		t.Fatal("expected memory to be stored")
	}

	// Forget it
	err := mm.Forget(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it's gone
	if len(mm.working) != 0 {
		t.Errorf("expected memory to be forgotten, got %d in working", len(mm.working))
	}
}

func TestMemoryManager_PruneExpired(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	count, err := mm.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Count should be 0 in this simple test
	if count < 0 {
		t.Errorf("expected non-negative count, got %d", count)
	}
}

func TestMemoryManager_GetStats(t *testing.T) {
	mockVector := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return nil
		},
	}

	mm := NewMemoryManager(nil, mockVector, nil, nil, &MemoryManagerOptions{
		AgentID:         "agent-1",
		WorkingCapacity: 10, // Enough to hold both memories
	})

	// Store some memories
	mm.Store(context.Background(), "Memory 1", nil, 0.8)
	mm.Store(context.Background(), "Memory 2", nil, 0.7)

	// Create an episode
	mm.CreateEpisode(context.Background(), "Episode", "Desc", []string{"mem1"}, nil)

	stats := mm.GetStats()

	if stats.WorkingCount != 2 {
		t.Errorf("expected 2 in working, got %d", stats.WorkingCount)
	}

	if stats.EpisodicCount != 1 {
		t.Errorf("expected 1 episodic, got %d", stats.EpisodicCount)
	}

	if stats.WorkingCapacity != 10 {
		t.Errorf("expected working capacity 10, got %d", stats.WorkingCapacity)
	}
}

func TestMemoryManager_Clear(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	// Store some data
	mm.Store(context.Background(), "Memory", nil, 0.5)
	mm.CreateEpisode(context.Background(), "Episode", "Desc", []string{}, nil)

	// Clear
	err := mm.Clear(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify everything is cleared
	if len(mm.working) != 0 {
		t.Error("expected working memory to be cleared")
	}

	if len(mm.episodic) != 0 {
		t.Error("expected episodic memory to be cleared")
	}
}

func TestMemoryEntry_AccessTracking(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	entry, _ := mm.Store(context.Background(), "Test memory", nil, 0.5)

	initialAccessCount := entry.AccessCount

	// Recall should increment access count
	mm.Recall(context.Background(), "", MemoryTierWorking, 10)

	// Check if access was tracked (in working memory)
	if updatedEntry, ok := mm.working[entry.ID]; ok {
		if updatedEntry.AccessCount <= initialAccessCount {
			t.Error("expected access count to be incremented")
		}
	}
}

func TestMemoryTier_Constants(t *testing.T) {
	if MemoryTierWorking != "working" {
		t.Error("expected working tier constant to be 'working'")
	}

	if MemoryTierShortTerm != "short_term" {
		t.Error("expected short term tier constant to be 'short_term'")
	}

	if MemoryTierLongTerm != "long_term" {
		t.Error("expected long term tier constant to be 'long_term'")
	}

	if MemoryTierEpisodic != "episodic" {
		t.Error("expected episodic tier constant to be 'episodic'")
	}
}

func TestMemoryManager_ThreadSafety(t *testing.T) {
	mm := NewMemoryManager(nil, nil, nil, nil, &MemoryManagerOptions{
		AgentID: "agent-1",
	})

	done := make(chan bool)

	// Concurrent writes
	go func() {
		for range 10 {
			mm.Store(context.Background(), "Memory", nil, 0.5)
		}

		done <- true
	}()

	// Concurrent reads
	go func() {
		for range 10 {
			mm.Recall(context.Background(), "", MemoryTierWorking, 10)
			mm.GetStats()
		}

		done <- true
	}()

	// Wait for both
	<-done
	<-done

	// If we get here without data races, the test passes
}
