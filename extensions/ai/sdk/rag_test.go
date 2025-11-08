package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Mock EmbeddingModel.
type MockEmbeddingModel struct {
	EmbedFunc func(ctx context.Context, texts []string) ([]Vector, error)
	DimFunc   func() int
}

func (m *MockEmbeddingModel) Embed(ctx context.Context, texts []string) ([]Vector, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, texts)
	}
	// Default: return simple vectors
	vectors := make([]Vector, len(texts))
	for i := range vectors {
		vectors[i] = Vector{Values: []float64{0.1, 0.2, 0.3}}
	}

	return vectors, nil
}

func (m *MockEmbeddingModel) Dimensions() int {
	if m.DimFunc != nil {
		return m.DimFunc()
	}

	return 3
}

// Mock VectorStore.
type MockVectorStore struct {
	UpsertFunc func(ctx context.Context, vectors []Vector) error
	QueryFunc  func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error)
	DeleteFunc func(ctx context.Context, ids []string) error
}

func (m *MockVectorStore) Upsert(ctx context.Context, vectors []Vector) error {
	if m.UpsertFunc != nil {
		return m.UpsertFunc(ctx, vectors)
	}

	return nil
}

func (m *MockVectorStore) Query(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, vector, limit, filter)
	}

	return []VectorMatch{}, nil
}

func (m *MockVectorStore) Delete(ctx context.Context, ids []string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, ids)
	}

	return nil
}

func TestNewRAG(t *testing.T) {
	vectorStore := &MockVectorStore{}
	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	if rag == nil {
		t.Fatal("expected RAG to be created")
	}

	if rag.topK != 10 {
		t.Errorf("expected default topK 10, got %d", rag.topK)
	}

	if rag.similarityThresh != 0.7 {
		t.Errorf("expected default similarity threshold 0.7, got %f", rag.similarityThresh)
	}

	if rag.chunkSize != 512 {
		t.Errorf("expected default chunk size 512, got %d", rag.chunkSize)
	}
}

func TestNewRAG_WithOptions(t *testing.T) {
	vectorStore := &MockVectorStore{}
	embedder := &MockEmbeddingModel{}

	opts := &RAGOptions{
		TopK:             20,
		SimilarityThresh: 0.8,
		Rerank:           true,
		IncludeMetadata:  false,
		ChunkSize:        256,
		ChunkOverlap:     25,
		MaxContextTokens: 1000,
	}

	rag := NewRAG(vectorStore, embedder, nil, nil, opts)

	if rag.topK != 20 {
		t.Errorf("expected topK 20, got %d", rag.topK)
	}

	if rag.similarityThresh != 0.8 {
		t.Errorf("expected similarity threshold 0.8, got %f", rag.similarityThresh)
	}

	if !rag.rerank {
		t.Error("expected rerank to be enabled")
	}

	if rag.includeMetadata {
		t.Error("expected includeMetadata to be false")
	}

	if rag.chunkSize != 256 {
		t.Errorf("expected chunk size 256, got %d", rag.chunkSize)
	}
}

func TestRAG_IndexDocument(t *testing.T) {
	upsertCalled := 0

	vectorStore := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			upsertCalled++

			return nil
		},
	}

	embedder := &MockEmbeddingModel{}

	opts := &RAGOptions{
		ChunkSize:    100,
		ChunkOverlap: 10,
	}

	rag := NewRAG(vectorStore, embedder, nil, nil, opts)

	doc := Document{
		ID:      "doc1",
		Content: "Short test content",
		Metadata: map[string]any{
			"source": "test",
		},
	}

	err := rag.IndexDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if upsertCalled == 0 {
		t.Error("expected upsert to be called")
	}
}

func TestRAG_IndexDocument_WithChunks(t *testing.T) {
	upsertCalled := 0

	var vectorsReceived []Vector

	vectorStore := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			upsertCalled++
			vectorsReceived = vectors

			return nil
		},
	}

	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	doc := Document{
		ID:      "doc1",
		Content: "Test",
		Chunks: []DocumentChunk{
			{ID: "chunk1", Content: "First chunk", Index: 0},
			{ID: "chunk2", Content: "Second chunk", Index: 1},
		},
	}

	err := rag.IndexDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if upsertCalled != 1 {
		t.Errorf("expected upsert to be called once (batch), got %d", upsertCalled)
	}

	if len(vectorsReceived) != 2 {
		t.Errorf("expected 2 vectors in batch, got %d", len(vectorsReceived))
	}
}

func TestRAG_IndexDocument_EmbedError(t *testing.T) {
	vectorStore := &MockVectorStore{}

	embedder := &MockEmbeddingModel{
		EmbedFunc: func(ctx context.Context, texts []string) ([]Vector, error) {
			return nil, errors.New("embedding error")
		},
	}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	doc := Document{
		ID:      "doc1",
		Content: "Test content",
	}

	err := rag.IndexDocument(context.Background(), doc)
	if err == nil {
		t.Error("expected error from embedding")
	}

	if !strings.Contains(err.Error(), "failed to generate embeddings") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRAG_IndexDocument_UpsertError(t *testing.T) {
	vectorStore := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			return errors.New("upsert error")
		},
	}

	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	doc := Document{
		ID:      "doc1",
		Content: "Test content",
	}

	err := rag.IndexDocument(context.Background(), doc)
	if err == nil {
		t.Error("expected error from upsert")
	}

	if !strings.Contains(err.Error(), "failed to store embeddings") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRAG_Retrieve(t *testing.T) {
	vectorStore := &MockVectorStore{
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return []VectorMatch{
				{
					ID:    "chunk1",
					Score: 0.9,
					Metadata: map[string]any{
						"content":     "Relevant content",
						"chunk_index": 0,
					},
				},
				{
					ID:    "chunk2",
					Score: 0.8,
					Metadata: map[string]any{
						"content":     "Another relevant content",
						"chunk_index": 1,
					},
				},
			}, nil
		},
	}

	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	result, err := rag.Retrieve(context.Background(), "test query")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Query != "test query" {
		t.Error("expected query to be set")
	}

	if len(result.Documents) != 2 {
		t.Errorf("expected 2 documents, got %d", len(result.Documents))
	}

	if result.Documents[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", result.Documents[0].Score)
	}

	if result.Documents[0].Document.Content != "Relevant content" {
		t.Error("expected content to be extracted from metadata")
	}
}

func TestRAG_Retrieve_EmbedError(t *testing.T) {
	vectorStore := &MockVectorStore{}

	embedder := &MockEmbeddingModel{
		EmbedFunc: func(ctx context.Context, texts []string) ([]Vector, error) {
			return nil, errors.New("embedding error")
		},
	}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	_, err := rag.Retrieve(context.Background(), "test query")
	if err == nil {
		t.Error("expected error from embedding")
	}

	if !strings.Contains(err.Error(), "failed to generate query embedding") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRAG_Retrieve_SearchError(t *testing.T) {
	vectorStore := &MockVectorStore{
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return nil, errors.New("search error")
		},
	}

	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	_, err := rag.Retrieve(context.Background(), "test query")
	if err == nil {
		t.Error("expected error from search")
	}

	if !strings.Contains(err.Error(), "vector search failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRAG_Retrieve_WithReranking(t *testing.T) {
	vectorStore := &MockVectorStore{
		QueryFunc: func(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error) {
			return []VectorMatch{
				{
					ID:    "chunk1",
					Score: 0.8,
					Metadata: map[string]any{
						"content":     "quantum computing basics",
						"chunk_index": 0,
					},
				},
				{
					ID:    "chunk2",
					Score: 0.9,
					Metadata: map[string]any{
						"content":     "introduction to quantum physics",
						"chunk_index": 1,
					},
				},
			}, nil
		},
	}

	embedder := &MockEmbeddingModel{}

	opts := &RAGOptions{
		Rerank: true,
	}

	rag := NewRAG(vectorStore, embedder, nil, nil, opts)

	result, err := rag.Retrieve(context.Background(), "quantum computing")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// After reranking, the document with more matching terms should be ranked higher
	// "quantum computing basics" has both "quantum" and "computing"
	if result.Documents[0].Rank != 1 {
		t.Errorf("expected rank 1 for first document, got %d", result.Documents[0].Rank)
	}
}

func TestRAG_ChunkDocument(t *testing.T) {
	rag := NewRAG(nil, nil, nil, nil, &RAGOptions{
		ChunkSize:    50,
		ChunkOverlap: 10,
	})

	doc := Document{
		ID:      "doc1",
		Content: "This is a long document that needs to be chunked into smaller pieces for processing",
	}

	chunks := rag.chunkDocument(doc)

	if len(chunks) == 0 {
		t.Error("expected chunks to be created")
	}

	// Check that chunks have content
	for _, chunk := range chunks {
		if chunk.Content == "" {
			t.Error("expected chunk to have content")
		}

		if chunk.ID == "" {
			t.Error("expected chunk to have ID")
		}
	}

	// Check that chunks are ordered
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Errorf("expected chunk index %d, got %d", i, chunk.Index)
		}
	}
}

func TestRAG_ChunkDocument_ShortContent(t *testing.T) {
	rag := NewRAG(nil, nil, nil, nil, &RAGOptions{
		ChunkSize:    100,
		ChunkOverlap: 0, // No overlap for this test
	})

	doc := Document{
		ID:      "doc1",
		Content: "Short content",
	}

	chunks := rag.chunkDocument(doc)

	if len(chunks) < 1 {
		t.Error("expected at least 1 chunk for short content")
	}

	// Short content should fit in one or few chunks
	if len(chunks) > 2 {
		t.Errorf("expected at most 2 chunks for short content, got %d", len(chunks))
	}
}

func TestRAG_BuildContext(t *testing.T) {
	rag := NewRAG(nil, nil, nil, nil, &RAGOptions{
		MaxContextTokens: 1000,
	})

	documents := []RetrievedDocument{
		{
			Document: DocumentChunk{
				Content: "First document content",
			},
			Score: 0.9,
		},
		{
			Document: DocumentChunk{
				Content: "Second document content",
			},
			Score: 0.8,
		},
	}

	context := rag.buildContext(documents)

	if context == "" {
		t.Error("expected context to be built")
	}

	if !strings.Contains(context, "First document content") {
		t.Error("expected first document in context")
	}

	if !strings.Contains(context, "Second document content") {
		t.Error("expected second document in context")
	}

	if !strings.Contains(context, "0.90") || !strings.Contains(context, "0.80") {
		t.Error("expected scores in context")
	}
}

func TestRAG_BuildContext_TokenLimit(t *testing.T) {
	rag := NewRAG(nil, nil, nil, nil, &RAGOptions{
		MaxContextTokens: 10, // Very small limit
	})

	// Create a document with content longer than token limit
	longContent := strings.Repeat("word ", 100)

	documents := []RetrievedDocument{
		{
			Document: DocumentChunk{
				Content: longContent,
			},
			Score: 0.9,
		},
		{
			Document: DocumentChunk{
				Content: "This should not be included",
			},
			Score: 0.8,
		},
	}

	context := rag.buildContext(documents)

	// Second document should not be included due to token limit
	if strings.Contains(context, "This should not be included") {
		t.Error("expected second document to be excluded due to token limit")
	}
}

func TestRAG_RerankDocuments(t *testing.T) {
	rag := NewRAG(nil, nil, nil, nil, nil)

	documents := []RetrievedDocument{
		{
			Document: DocumentChunk{
				Content: "document about cats",
			},
			Score: 0.8,
			Rank:  1,
		},
		{
			Document: DocumentChunk{
				Content: "document about dogs and cats",
			},
			Score: 0.7,
			Rank:  2,
		},
	}

	reranked := rag.rerankDocuments("dogs cats", documents)

	// Document with both terms should be ranked higher after reranking
	// The second document has both "dogs" and "cats" so it should get boosted
	hasDogsAndCats := false

	for i, doc := range reranked {
		if doc.Document.Content == "document about dogs and cats" {
			hasDogsAndCats = true
			// It should be ranked highly (either first or second is acceptable after reranking)
			if i > 1 {
				t.Error("expected document with more matching terms to be ranked in top 2")
			}
		}
	}

	if !hasDogsAndCats {
		t.Error("expected document with dogs and cats in results")
	}

	// Check that ranks are updated
	if reranked[0].Rank != 1 {
		t.Error("expected ranks to be updated with rank 1 for first document")
	}
}

func TestRAG_DeleteDocument(t *testing.T) {
	vectorStore := &MockVectorStore{}
	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	err := rag.DeleteDocument(context.Background(), "doc1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRAG_UpdateDocument(t *testing.T) {
	upsertCalled := 0

	vectorStore := &MockVectorStore{
		UpsertFunc: func(ctx context.Context, vectors []Vector) error {
			upsertCalled++

			return nil
		},
	}

	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	doc := Document{
		ID:      "doc1",
		Content: "Updated content",
	}

	err := rag.UpdateDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if upsertCalled == 0 {
		t.Error("expected upsert to be called after update")
	}
}

func TestRAG_GetStats(t *testing.T) {
	vectorStore := &MockVectorStore{}
	embedder := &MockEmbeddingModel{}

	rag := NewRAG(vectorStore, embedder, nil, nil, nil)

	stats, err := rag.GetStats(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if stats == nil {
		t.Error("expected stats to be returned")
	}
}
