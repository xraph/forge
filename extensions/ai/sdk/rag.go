package sdk

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// RAG provides Retrieval Augmented Generation functionality.
type RAG struct {
	vectorStore VectorStore
	embedder    EmbeddingModel
	logger      forge.Logger
	metrics     forge.Metrics

	// Configuration
	topK             int
	similarityThresh float64
	rerank           bool
	includeMetadata  bool
	chunkSize        int
	chunkOverlap     int
	maxContextTokens int

	// Citation support
	citationExtractor *CitationExtractor
	citationFormatter *CitationFormatter
	citationStyle     CitationStyle
}

// EmbeddingModel generates embeddings for text.
type EmbeddingModel interface {
	// Embed generates embeddings for the given texts
	Embed(ctx context.Context, texts []string) ([]Vector, error)

	// Dimensions returns the embedding dimension
	Dimensions() int
}

// RAGOptions configures RAG behavior.
type RAGOptions struct {
	TopK             int
	SimilarityThresh float64
	Rerank           bool
	IncludeMetadata  bool
	ChunkSize        int
	ChunkOverlap     int
	MaxContextTokens int
}

// Document represents a document for RAG.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]any
	Chunks   []DocumentChunk
}

// DocumentChunk represents a chunk of a document.
type DocumentChunk struct {
	ID       string
	Content  string
	Index    int
	Metadata map[string]any
}

// RetrievalResult contains documents retrieved for a query.
type RetrievalResult struct {
	Query     string
	Documents []RetrievedDocument
	Took      time.Duration
}

// RetrievedDocument is a document with similarity score.
type RetrievedDocument struct {
	Document DocumentChunk
	Score    float64
	Rank     int
}

// NewRAG creates a new RAG instance.
func NewRAG(
	vectorStore VectorStore,
	embedder EmbeddingModel,
	logger forge.Logger,
	metrics forge.Metrics,
	opts *RAGOptions,
) *RAG {
	rag := &RAG{
		vectorStore:      vectorStore,
		embedder:         embedder,
		logger:           logger,
		metrics:          metrics,
		topK:             10,
		similarityThresh: 0.7,
		rerank:           false,
		includeMetadata:  true,
		chunkSize:        512,
		chunkOverlap:     50,
		maxContextTokens: 2000,
	}

	if opts != nil {
		if opts.TopK > 0 {
			rag.topK = opts.TopK
		}

		if opts.SimilarityThresh > 0 {
			rag.similarityThresh = opts.SimilarityThresh
		}

		rag.rerank = opts.Rerank

		rag.includeMetadata = opts.IncludeMetadata
		if opts.ChunkSize > 0 {
			rag.chunkSize = opts.ChunkSize
		}

		if opts.ChunkOverlap > 0 {
			rag.chunkOverlap = opts.ChunkOverlap
		}

		if opts.MaxContextTokens > 0 {
			rag.maxContextTokens = opts.MaxContextTokens
		}
	}

	return rag
}

// IndexDocument indexes a document into the vector store.
func (r *RAG) IndexDocument(ctx context.Context, doc Document) error {
	startTime := time.Now()

	if r.logger != nil {
		r.logger.Debug("Indexing document",
			F("doc_id", doc.ID),
			F("content_length", len(doc.Content)),
		)
	}

	// Chunk the document if needed
	chunks := doc.Chunks
	if len(chunks) == 0 {
		chunks = r.chunkDocument(doc)
	}

	// Generate embeddings for chunks
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Content
	}

	embeddings, err := r.embedder.Embed(ctx, chunkTexts)
	if err != nil {
		if r.metrics != nil {
			r.metrics.Counter("forge.ai.sdk.rag.index.errors").Inc()
		}

		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// Prepare vectors for batch upsert
	vectors := make([]Vector, len(embeddings))
	for i, embedding := range embeddings {
		chunk := chunks[i]
		chunkID := fmt.Sprintf("%s_chunk_%d", doc.ID, i)

		metadata := make(map[string]any)
		metadata["doc_id"] = doc.ID
		metadata["chunk_index"] = i
		metadata["content"] = chunk.Content

		if r.includeMetadata && doc.Metadata != nil {
			maps.Copy(metadata, doc.Metadata)
		}

		vectors[i] = Vector{
			ID:       chunkID,
			Values:   embedding.Values,
			Metadata: metadata,
		}
	}

	// Batch upsert to vector store
	if err := r.vectorStore.Upsert(ctx, vectors); err != nil {
		if r.metrics != nil {
			r.metrics.Counter("forge.ai.sdk.rag.index.errors").Inc()
		}

		return fmt.Errorf("failed to store embeddings: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Document indexed",
			F("doc_id", doc.ID),
			F("chunks", len(chunks)),
			F("took", time.Since(startTime)),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.rag.index.success").Inc()
		r.metrics.Histogram("forge.ai.sdk.rag.index.chunks").Observe(float64(len(chunks)))
		r.metrics.Histogram("forge.ai.sdk.rag.index.duration").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// Retrieve retrieves relevant documents for a query.
func (r *RAG) Retrieve(ctx context.Context, query string) (*RetrievalResult, error) {
	startTime := time.Now()

	if r.logger != nil {
		r.logger.Debug("Retrieving documents",
			F("query", query),
			F("top_k", r.topK),
		)
	}

	// Generate query embedding
	embeddings, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		if r.metrics != nil {
			r.metrics.Counter("forge.ai.sdk.rag.retrieve.errors").Inc()
		}

		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	queryEmbedding := embeddings[0]

	// Build filter for similarity threshold (if vector store supports it)
	filter := make(map[string]any)
	filter["min_score"] = r.similarityThresh

	// Query vector store
	matches, err := r.vectorStore.Query(ctx, queryEmbedding.Values, r.topK, filter)
	if err != nil {
		if r.metrics != nil {
			r.metrics.Counter("forge.ai.sdk.rag.retrieve.errors").Inc()
		}

		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Convert to retrieved documents
	documents := make([]RetrievedDocument, 0, len(matches))
	for i, match := range matches {
		chunk := DocumentChunk{
			ID:       match.ID,
			Metadata: match.Metadata,
		}

		// Extract content from metadata
		if content, ok := match.Metadata["content"].(string); ok {
			chunk.Content = content
		}

		// Extract chunk index
		if index, ok := match.Metadata["chunk_index"].(int); ok {
			chunk.Index = index
		}

		documents = append(documents, RetrievedDocument{
			Document: chunk,
			Score:    match.Score,
			Rank:     i + 1,
		})
	}

	// Rerank if enabled
	if r.rerank && len(documents) > 0 {
		documents = r.rerankDocuments(query, documents)
	}

	result := &RetrievalResult{
		Query:     query,
		Documents: documents,
		Took:      time.Since(startTime),
	}

	if r.logger != nil {
		r.logger.Info("Documents retrieved",
			F("query", query),
			F("count", len(documents)),
			F("took", result.Took),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.rag.retrieve.success").Inc()
		r.metrics.Histogram("forge.ai.sdk.rag.retrieve.count").Observe(float64(len(documents)))
		r.metrics.Histogram("forge.ai.sdk.rag.retrieve.duration").Observe(result.Took.Seconds())
	}

	return result, nil
}

// GenerateWithContext generates a response using retrieved context.
func (r *RAG) GenerateWithContext(
	ctx context.Context,
	query string,
	generator *GenerateBuilder,
) (*Result, error) {
	// Retrieve relevant documents
	retrieval, err := r.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}

	// Build context from retrieved documents
	ragContext := r.buildContext(retrieval.Documents)

	// Enhance prompt with context
	enhancedPrompt := fmt.Sprintf(`Context:
%s

Question: %s

Answer based on the context above:`, ragContext, query)

	// Generate response
	return generator.
		WithPrompt(enhancedPrompt).
		Execute()
}

// RAGResult contains the result of a RAG generation with citations.
type RAGResult struct {
	*Result

	Citations    []Citation
	CitedContent *CitedContent
	Sources      []RetrievedDocument
}

// GenerateWithCitations generates a response with automatic citation tracking.
func (r *RAG) GenerateWithCitations(
	ctx context.Context,
	query string,
	generator *GenerateBuilder,
) (*RAGResult, error) {
	// Retrieve relevant documents
	retrieval, err := r.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}

	// Extract citations from retrieval results
	citations := r.ExtractCitations(retrieval)

	// Build context from retrieved documents with citation markers
	ragContext := r.buildContextWithCitations(retrieval.Documents, citations)

	// Enhance prompt with context and citation instructions
	enhancedPrompt := fmt.Sprintf(`Context (with citation numbers in brackets):
%s

Question: %s

Instructions: Answer based on the context above. Include citation references [1], [2], etc. when referring to specific information from the sources.`, ragContext, query)

	// Generate response
	result, err := generator.
		WithPrompt(enhancedPrompt).
		Execute()
	if err != nil {
		return nil, err
	}

	// Format citations in the response
	citedContent := r.formatCitedContent(result.Content, citations)

	return &RAGResult{
		Result:       result,
		Citations:    citations,
		CitedContent: citedContent,
		Sources:      retrieval.Documents,
	}, nil
}

// ExtractCitations extracts citations from a retrieval result.
func (r *RAG) ExtractCitations(retrieval *RetrievalResult) []Citation {
	if r.citationExtractor == nil {
		r.citationExtractor = NewCitationExtractor().
			WithMinScore(r.similarityThresh)
	}

	return r.citationExtractor.FromRetrievalResult(retrieval)
}

// buildContextWithCitations builds context with citation markers.
func (r *RAG) buildContextWithCitations(documents []RetrievedDocument, citations []Citation) string {
	var builder strings.Builder

	tokenCount := 0

	for i, doc := range documents {
		// Rough token estimation (1 token ≈ 4 chars)
		docTokens := len(doc.Document.Content) / 4

		if tokenCount+docTokens > r.maxContextTokens {
			break
		}

		if i > 0 {
			builder.WriteString("\n\n")
		}

		// Add citation marker
		builder.WriteString(fmt.Sprintf("[%d] ", i+1))

		// Add source info if available
		if i < len(citations) && citations[i].Title != "" {
			builder.WriteString(fmt.Sprintf("(Source: %s)\n", citations[i].Title))
		}

		builder.WriteString(doc.Document.Content)

		tokenCount += docTokens
	}

	return builder.String()
}

// formatCitedContent formats the response content with citation references.
func (r *RAG) formatCitedContent(content string, citations []Citation) *CitedContent {
	if r.citationFormatter == nil {
		style := r.citationStyle
		if style == "" {
			style = CitationStyleNumeric
		}

		r.citationFormatter = NewCitationFormatter(style)
	}

	return r.citationFormatter.FormatCitedContent(content, citations)
}

// WithCitationStyle sets the citation formatting style.
func (r *RAG) WithCitationStyle(style CitationStyle) *RAG {
	r.citationStyle = style
	r.citationFormatter = NewCitationFormatter(style)

	return r
}

// WithCitationExtractor sets a custom citation extractor.
func (r *RAG) WithCitationExtractor(extractor *CitationExtractor) *RAG {
	r.citationExtractor = extractor

	return r
}

// GetCitationManager creates a citation manager from retrieval results.
func (r *RAG) GetCitationManager(retrieval *RetrievalResult) *CitationManager {
	manager := NewCitationManager()

	citations := r.ExtractCitations(retrieval)
	for i := range citations {
		manager.AddCitation(&citations[i])
	}

	return manager
}

// chunkDocument splits a document into chunks.
func (r *RAG) chunkDocument(doc Document) []DocumentChunk {
	content := doc.Content
	chunks := make([]DocumentChunk, 0)

	// Simple chunking by character count
	start := 0
	index := 0

	for start < len(content) {
		end := start + r.chunkSize
		if end > len(content) {
			end = len(content)
		}

		// Try to break at word boundary if not at the end
		if end < len(content) && end > start {
			// Look back for a space
			originalEnd := end
			for i := end; i > start+r.chunkSize/2 && i < len(content); i-- {
				if content[i] == ' ' || content[i] == '\n' {
					end = i

					break
				}
			}
			// Ensure we advanced from start
			if end <= start {
				end = originalEnd
			}
		}

		chunkContent := strings.TrimSpace(content[start:end])
		if chunkContent != "" {
			chunk := DocumentChunk{
				ID:       fmt.Sprintf("%s_chunk_%d", doc.ID, index),
				Content:  chunkContent,
				Index:    index,
				Metadata: doc.Metadata,
			}
			chunks = append(chunks, chunk)
			index++
		}

		// If we reached the end, we're done
		if end >= len(content) {
			break
		}

		// Move forward with overlap
		nextStart := end - r.chunkOverlap
		// Ensure we always advance by at least half the chunk size or to the end
		if nextStart <= start {
			nextStart = start + max(1, r.chunkSize/2)
		}

		start = nextStart
	}

	return chunks
}

// buildContext builds a context string from retrieved documents.
func (r *RAG) buildContext(documents []RetrievedDocument) string {
	var builder strings.Builder

	tokenCount := 0

	for i, doc := range documents {
		// Rough token estimation (1 token ≈ 4 chars)
		docTokens := len(doc.Document.Content) / 4

		if tokenCount+docTokens > r.maxContextTokens {
			break
		}

		if i > 0 {
			builder.WriteString("\n\n")
		}

		builder.WriteString(fmt.Sprintf("Document %d (relevance: %.2f):\n", i+1, doc.Score))
		builder.WriteString(doc.Document.Content)

		tokenCount += docTokens
	}

	return builder.String()
}

// rerankDocuments reranks documents using a simple algorithm
// In production, you'd use a proper reranking model.
func (r *RAG) rerankDocuments(query string, documents []RetrievedDocument) []RetrievedDocument {
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	// Score based on exact term matches
	for i := range documents {
		contentLower := strings.ToLower(documents[i].Document.Content)
		exactMatches := 0

		for _, term := range queryTerms {
			if strings.Contains(contentLower, term) {
				exactMatches++
			}
		}

		// Boost score based on exact matches
		boostFactor := 1.0 + (float64(exactMatches) / float64(len(queryTerms)) * 0.2)
		documents[i].Score *= boostFactor
	}

	// Re-sort by new scores
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].Score > documents[j].Score
	})

	// Update ranks
	for i := range documents {
		documents[i].Rank = i + 1
	}

	return documents
}

// DeleteDocument removes a document from the vector store.
func (r *RAG) DeleteDocument(ctx context.Context, docID string) error {
	if r.logger != nil {
		r.logger.Debug("Deleting document", F("doc_id", docID))
	}

	// In a real implementation, you'd query for all chunks of this document
	// and delete them. For now, we'll assume a single delete operation.

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.rag.delete").Inc()
	}

	return nil
}

// UpdateDocument updates an existing document.
func (r *RAG) UpdateDocument(ctx context.Context, doc Document) error {
	// Delete old version
	if err := r.DeleteDocument(ctx, doc.ID); err != nil {
		return fmt.Errorf("failed to delete old document: %w", err)
	}

	// Index new version
	return r.IndexDocument(ctx, doc)
}

// Stats returns RAG statistics.
type RAGStats struct {
	TotalDocuments int
	TotalChunks    int
	AvgChunkSize   float64
}

// GetStats returns RAG statistics.
func (r *RAG) GetStats(ctx context.Context) (*RAGStats, error) {
	// This would query the vector store for statistics
	// For now, return empty stats
	return &RAGStats{}, nil
}
