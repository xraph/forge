package llm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// EmbeddingRequest represents an embedding request
type EmbeddingRequest struct {
	Provider       string                 `json:"provider"`
	Model          string                 `json:"model"`
	Input          []string               `json:"input"`
	User           string                 `json:"user,omitempty"`
	Dimensions     *int                   `json:"dimensions,omitempty"`
	EncodingFormat string                 `json:"encoding_format,omitempty"`
	Context        map[string]interface{} `json:"context"`
	Metadata       map[string]interface{} `json:"metadata"`
	RequestID      string                 `json:"request_id"`
}

// EmbeddingResponse represents an embedding response
type EmbeddingResponse struct {
	Object    string                 `json:"object"`
	Data      []EmbeddingData        `json:"data"`
	Model     string                 `json:"model"`
	Provider  string                 `json:"provider"`
	Usage     *LLMUsage              `json:"usage,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	RequestID string                 `json:"request_id"`
}

// EmbeddingData represents a single embedding
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
	Text      string    `json:"text,omitempty"`
}

// EmbeddingCollection represents a collection of embeddings
type EmbeddingCollection struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Model       string                 `json:"model"`
	Provider    string                 `json:"provider"`
	Dimensions  int                    `json:"dimensions"`
	Embeddings  []EmbeddingItem        `json:"embeddings"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// EmbeddingItem represents an item in an embedding collection
type EmbeddingItem struct {
	ID        string                 `json:"id"`
	Text      string                 `json:"text"`
	Embedding []float64              `json:"embedding"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
}

// EmbeddingIndex represents an index for efficient similarity search
type EmbeddingIndex struct {
	Collection *EmbeddingCollection
	index      map[string][]float64
	normalized bool
}

// SimilarityResult represents a similarity search result
type SimilarityResult struct {
	Item     EmbeddingItem `json:"item"`
	Score    float64       `json:"score"`
	Distance float64       `json:"distance"`
	Rank     int           `json:"rank"`
}

// SimilaritySearchOptions represents options for similarity search
type SimilaritySearchOptions struct {
	TopK        int                    `json:"top_k"`
	Threshold   float64                `json:"threshold"`
	IncludeText bool                   `json:"include_text"`
	Filters     map[string]interface{} `json:"filters"`
	Metric      string                 `json:"metric"` // cosine, euclidean, dot_product
}

// EmbeddingBuilder provides a fluent interface for building embedding requests
type EmbeddingBuilder struct {
	request EmbeddingRequest
}

// NewEmbeddingBuilder creates a new embedding builder
func NewEmbeddingBuilder() *EmbeddingBuilder {
	return &EmbeddingBuilder{
		request: EmbeddingRequest{
			Input:    make([]string, 0),
			Context:  make(map[string]interface{}),
			Metadata: make(map[string]interface{}),
		},
	}
}

// WithProvider sets the provider
func (b *EmbeddingBuilder) WithProvider(provider string) *EmbeddingBuilder {
	b.request.Provider = provider
	return b
}

// WithModel sets the model
func (b *EmbeddingBuilder) WithModel(model string) *EmbeddingBuilder {
	b.request.Model = model
	return b
}

// WithInput sets the input texts
func (b *EmbeddingBuilder) WithInput(input ...string) *EmbeddingBuilder {
	b.request.Input = input
	return b
}

// AddInput adds input text
func (b *EmbeddingBuilder) AddInput(text string) *EmbeddingBuilder {
	b.request.Input = append(b.request.Input, text)
	return b
}

// WithUser sets the user ID
func (b *EmbeddingBuilder) WithUser(user string) *EmbeddingBuilder {
	b.request.User = user
	return b
}

// WithDimensions sets the embedding dimensions
func (b *EmbeddingBuilder) WithDimensions(dimensions int) *EmbeddingBuilder {
	b.request.Dimensions = &dimensions
	return b
}

// WithEncodingFormat sets the encoding format
func (b *EmbeddingBuilder) WithEncodingFormat(format string) *EmbeddingBuilder {
	b.request.EncodingFormat = format
	return b
}

// WithContext adds context data
func (b *EmbeddingBuilder) WithContext(key string, value interface{}) *EmbeddingBuilder {
	b.request.Context[key] = value
	return b
}

// WithMetadata adds metadata
func (b *EmbeddingBuilder) WithMetadata(key string, value interface{}) *EmbeddingBuilder {
	b.request.Metadata[key] = value
	return b
}

// WithRequestID sets the request ID
func (b *EmbeddingBuilder) WithRequestID(id string) *EmbeddingBuilder {
	b.request.RequestID = id
	return b
}

// Build returns the built embedding request
func (b *EmbeddingBuilder) Build() EmbeddingRequest {
	return b.request
}

// Execute executes the embedding request using the provided LLM manager
func (b *EmbeddingBuilder) Execute(ctx context.Context, manager *LLMManager) (EmbeddingResponse, error) {
	return manager.Embed(ctx, b.request)
}

// NewEmbeddingCollection creates a new embedding collection
func NewEmbeddingCollection(name, description, model, provider string, dimensions int) *EmbeddingCollection {
	return &EmbeddingCollection{
		Name:        name,
		Description: description,
		Model:       model,
		Provider:    provider,
		Dimensions:  dimensions,
		Embeddings:  make([]EmbeddingItem, 0),
		Metadata:    make(map[string]interface{}),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// AddEmbedding adds an embedding to the collection
func (c *EmbeddingCollection) AddEmbedding(item EmbeddingItem) error {
	if len(item.Embedding) != c.Dimensions {
		return fmt.Errorf("embedding dimensions (%d) don't match collection dimensions (%d)", len(item.Embedding), c.Dimensions)
	}

	item.CreatedAt = time.Now()
	c.Embeddings = append(c.Embeddings, item)
	c.UpdatedAt = time.Now()

	return nil
}

// AddEmbeddings adds multiple embeddings to the collection
func (c *EmbeddingCollection) AddEmbeddings(items []EmbeddingItem) error {
	for _, item := range items {
		if err := c.AddEmbedding(item); err != nil {
			return err
		}
	}
	return nil
}

// RemoveEmbedding removes an embedding from the collection
func (c *EmbeddingCollection) RemoveEmbedding(id string) error {
	for i, item := range c.Embeddings {
		if item.ID == id {
			c.Embeddings = append(c.Embeddings[:i], c.Embeddings[i+1:]...)
			c.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("embedding with ID %s not found", id)
}

// GetEmbedding retrieves an embedding by ID
func (c *EmbeddingCollection) GetEmbedding(id string) (*EmbeddingItem, error) {
	for _, item := range c.Embeddings {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("embedding with ID %s not found", id)
}

// Size returns the number of embeddings in the collection
func (c *EmbeddingCollection) Size() int {
	return len(c.Embeddings)
}

// Clear removes all embeddings from the collection
func (c *EmbeddingCollection) Clear() {
	c.Embeddings = make([]EmbeddingItem, 0)
	c.UpdatedAt = time.Now()
}

// CreateIndex creates an index for efficient similarity search
func (c *EmbeddingCollection) CreateIndex() *EmbeddingIndex {
	index := &EmbeddingIndex{
		Collection: c,
		index:      make(map[string][]float64),
		normalized: false,
	}

	for _, item := range c.Embeddings {
		index.index[item.ID] = item.Embedding
	}

	return index
}

// CreateNormalizedIndex creates a normalized index for cosine similarity
func (c *EmbeddingCollection) CreateNormalizedIndex() *EmbeddingIndex {
	index := &EmbeddingIndex{
		Collection: c,
		index:      make(map[string][]float64),
		normalized: true,
	}

	for _, item := range c.Embeddings {
		normalized := normalizeVector(item.Embedding)
		index.index[item.ID] = normalized
	}

	return index
}

// Search performs similarity search on the collection
func (c *EmbeddingCollection) Search(queryEmbedding []float64, options SimilaritySearchOptions) ([]SimilarityResult, error) {
	if len(queryEmbedding) != c.Dimensions {
		return nil, fmt.Errorf("query embedding dimensions (%d) don't match collection dimensions (%d)", len(queryEmbedding), c.Dimensions)
	}

	results := make([]SimilarityResult, 0)

	for _, item := range c.Embeddings {
		// Apply filters if specified
		if !matchesFilters(item, options.Filters) {
			continue
		}

		var score float64
		var distance float64

		switch options.Metric {
		case "euclidean":
			distance = euclideanDistance(queryEmbedding, item.Embedding)
			score = 1.0 / (1.0 + distance) // Convert distance to similarity score
		case "dot_product":
			score = dotProduct(queryEmbedding, item.Embedding)
			distance = 1.0 - score
		default: // cosine (default)
			score = cosineSimilarity(queryEmbedding, item.Embedding)
			distance = 1.0 - score
		}

		if score >= options.Threshold {
			result := SimilarityResult{
				Item:     item,
				Score:    score,
				Distance: distance,
			}

			if !options.IncludeText {
				result.Item.Text = ""
			}

			results = append(results, result)
		}
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Add rank and limit results
	for i := range results {
		results[i].Rank = i + 1
	}

	if options.TopK > 0 && len(results) > options.TopK {
		results = results[:options.TopK]
	}

	return results, nil
}

// SearchText performs similarity search using text query
func (c *EmbeddingCollection) SearchText(ctx context.Context, manager *LLMManager, query string, options SimilaritySearchOptions) ([]SimilarityResult, error) {
	// Generate embedding for the query
	request := EmbeddingRequest{
		Provider: c.Provider,
		Model:    c.Model,
		Input:    []string{query},
	}

	response, err := manager.Embed(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned for query")
	}

	return c.Search(response.Data[0].Embedding, options)
}

// Search performs similarity search using the index
func (idx *EmbeddingIndex) Search(queryEmbedding []float64, options SimilaritySearchOptions) ([]SimilarityResult, error) {
	if len(queryEmbedding) != idx.Collection.Dimensions {
		return nil, fmt.Errorf("query embedding dimensions (%d) don't match collection dimensions (%d)", len(queryEmbedding), idx.Collection.Dimensions)
	}

	var normalizedQuery []float64
	if idx.normalized {
		normalizedQuery = normalizeVector(queryEmbedding)
	} else {
		normalizedQuery = queryEmbedding
	}

	results := make([]SimilarityResult, 0)

	for _, item := range idx.Collection.Embeddings {
		// Apply filters if specified
		if !matchesFilters(item, options.Filters) {
			continue
		}

		indexEmbedding := idx.index[item.ID]
		var score float64
		var distance float64

		switch options.Metric {
		case "euclidean":
			distance = euclideanDistance(normalizedQuery, indexEmbedding)
			score = 1.0 / (1.0 + distance)
		case "dot_product":
			score = dotProduct(normalizedQuery, indexEmbedding)
			distance = 1.0 - score
		default: // cosine (default)
			if idx.normalized {
				score = dotProduct(normalizedQuery, indexEmbedding) // Dot product for normalized vectors equals cosine similarity
			} else {
				score = cosineSimilarity(normalizedQuery, indexEmbedding)
			}
			distance = 1.0 - score
		}

		if score >= options.Threshold {
			result := SimilarityResult{
				Item:     item,
				Score:    score,
				Distance: distance,
			}

			if !options.IncludeText {
				result.Item.Text = ""
			}

			results = append(results, result)
		}
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Add rank and limit results
	for i := range results {
		results[i].Rank = i + 1
	}

	if options.TopK > 0 && len(results) > options.TopK {
		results = results[:options.TopK]
	}

	return results, nil
}

// Helper functions for similarity calculations

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0.0 || normB == 0.0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// euclideanDistance calculates Euclidean distance between two vectors
func euclideanDistance(a, b []float64) float64 {
	if len(a) != len(b) {
		return math.Inf(1)
	}

	var sum float64
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return math.Sqrt(sum)
}

// dotProduct calculates dot product between two vectors
func dotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var product float64
	for i := 0; i < len(a); i++ {
		product += a[i] * b[i]
	}

	return product
}

// normalizeVector normalizes a vector to unit length
func normalizeVector(vector []float64) []float64 {
	var norm float64
	for _, v := range vector {
		norm += v * v
	}
	norm = math.Sqrt(norm)

	if norm == 0.0 {
		return vector
	}

	normalized := make([]float64, len(vector))
	for i, v := range vector {
		normalized[i] = v / norm
	}

	return normalized
}

// matchesFilters checks if an embedding item matches the given filters
func matchesFilters(item EmbeddingItem, filters map[string]interface{}) bool {
	if filters == nil || len(filters) == 0 {
		return true
	}

	for key, expectedValue := range filters {
		if actualValue, exists := item.Metadata[key]; exists {
			if actualValue != expectedValue {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

// EmbeddingCluster represents a cluster of similar embeddings
type EmbeddingCluster struct {
	ID        string          `json:"id"`
	Centroid  []float64       `json:"centroid"`
	Items     []EmbeddingItem `json:"items"`
	Size      int             `json:"size"`
	Coherence float64         `json:"coherence"`
}

// ClusterEmbeddings clusters embeddings using k-means
func (c *EmbeddingCollection) ClusterEmbeddings(k int, maxIterations int) ([]EmbeddingCluster, error) {
	if len(c.Embeddings) < k {
		return nil, fmt.Errorf("not enough embeddings (%d) for %d clusters", len(c.Embeddings), k)
	}

	// Initialize centroids randomly
	centroids := make([][]float64, k)
	for i := 0; i < k; i++ {
		centroids[i] = make([]float64, c.Dimensions)
		copy(centroids[i], c.Embeddings[i%len(c.Embeddings)].Embedding)
	}

	// K-means iterations
	for iter := 0; iter < maxIterations; iter++ {
		clusters := make([][]EmbeddingItem, k)
		for i := range clusters {
			clusters[i] = make([]EmbeddingItem, 0)
		}

		// Assign each embedding to nearest centroid
		for _, item := range c.Embeddings {
			bestCluster := 0
			bestDistance := euclideanDistance(item.Embedding, centroids[0])

			for i := 1; i < k; i++ {
				distance := euclideanDistance(item.Embedding, centroids[i])
				if distance < bestDistance {
					bestDistance = distance
					bestCluster = i
				}
			}

			clusters[bestCluster] = append(clusters[bestCluster], item)
		}

		// Update centroids
		converged := true
		for i := 0; i < k; i++ {
			if len(clusters[i]) == 0 {
				continue
			}

			newCentroid := make([]float64, c.Dimensions)
			for _, item := range clusters[i] {
				for j, val := range item.Embedding {
					newCentroid[j] += val
				}
			}

			for j := range newCentroid {
				newCentroid[j] /= float64(len(clusters[i]))
			}

			// Check for convergence
			if euclideanDistance(centroids[i], newCentroid) > 0.001 {
				converged = false
			}

			centroids[i] = newCentroid
		}

		if converged {
			break
		}
	}

	// Create cluster results
	results := make([]EmbeddingCluster, 0, k)
	for i := 0; i < k; i++ {
		cluster := EmbeddingCluster{
			ID:       fmt.Sprintf("cluster_%d", i),
			Centroid: centroids[i],
			Items:    make([]EmbeddingItem, 0),
		}

		// Assign items to cluster
		for _, item := range c.Embeddings {
			bestCluster := 0
			bestDistance := euclideanDistance(item.Embedding, centroids[0])

			for j := 1; j < k; j++ {
				distance := euclideanDistance(item.Embedding, centroids[j])
				if distance < bestDistance {
					bestDistance = distance
					bestCluster = j
				}
			}

			if bestCluster == i {
				cluster.Items = append(cluster.Items, item)
			}
		}

		cluster.Size = len(cluster.Items)

		// Calculate coherence (average similarity within cluster)
		if cluster.Size > 1 {
			var totalSimilarity float64
			count := 0
			for i := 0; i < cluster.Size; i++ {
				for j := i + 1; j < cluster.Size; j++ {
					similarity := cosineSimilarity(cluster.Items[i].Embedding, cluster.Items[j].Embedding)
					totalSimilarity += similarity
					count++
				}
			}
			cluster.Coherence = totalSimilarity / float64(count)
		} else {
			cluster.Coherence = 1.0
		}

		if cluster.Size > 0 {
			results = append(results, cluster)
		}
	}

	return results, nil
}
