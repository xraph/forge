package search

import (
	"context"
	"testing"

	"github.com/xraph/forge/v2"
)

func newTestInMemorySearch() *InMemorySearch {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()
	return NewInMemorySearch(config, logger, metrics)
}

func TestInMemorySearch_Connect(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try connecting again
	err = search.Connect(ctx)
	if err != ErrAlreadyConnected {
		t.Errorf("expected ErrAlreadyConnected, got %v", err)
	}
}

func TestInMemorySearch_Disconnect(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	// Disconnect without connecting
	err := search.Disconnect(ctx)
	if err != ErrNotConnected {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}

	// Connect then disconnect
	err = search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = search.Disconnect(ctx)
	if err != nil {
		t.Fatalf("failed to disconnect: %v", err)
	}
}

func TestInMemorySearch_Ping(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	// Ping without connecting
	err := search.Ping(ctx)
	if err != ErrNotConnected {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}

	// Connect then ping
	err = search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = search.Ping(ctx)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestInMemorySearch_CreateIndex(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text", Searchable: true},
			{Name: "content", Type: "text", Searchable: true},
		},
	}

	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Try creating same index again
	err = search.CreateIndex(ctx, "test", schema)
	if err != ErrIndexAlreadyExists {
		t.Errorf("expected ErrIndexAlreadyExists, got %v", err)
	}
}

func TestInMemorySearch_DeleteIndex(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try deleting non-existent index
	err = search.DeleteIndex(ctx, "nonexistent")
	if err != ErrIndexNotFound {
		t.Errorf("expected ErrIndexNotFound, got %v", err)
	}

	// Create and delete index
	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	err = search.DeleteIndex(ctx, "test")
	if err != nil {
		t.Fatalf("failed to delete index: %v", err)
	}
}

func TestInMemorySearch_ListIndexes(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// List empty indexes
	indexes, err := search.ListIndexes(ctx)
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	if len(indexes) != 0 {
		t.Errorf("expected 0 indexes, got %d", len(indexes))
	}

	// Create indexes
	schema := IndexSchema{Fields: []FieldSchema{{Name: "field", Type: "text"}}}
	_ = search.CreateIndex(ctx, "index1", schema)
	_ = search.CreateIndex(ctx, "index2", schema)

	indexes, err = search.ListIndexes(ctx)
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	if len(indexes) != 2 {
		t.Errorf("expected 2 indexes, got %d", len(indexes))
	}
}

func TestInMemorySearch_GetIndexInfo(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try getting info for non-existent index
	_, err = search.GetIndexInfo(ctx, "nonexistent")
	if err != ErrIndexNotFound {
		t.Errorf("expected ErrIndexNotFound, got %v", err)
	}

	// Create index and get info
	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	info, err := search.GetIndexInfo(ctx, "test")
	if err != nil {
		t.Fatalf("failed to get index info: %v", err)
	}

	if info.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", info.Name)
	}
	if info.DocumentCount != 0 {
		t.Errorf("expected 0 documents, got %d", info.DocumentCount)
	}
}

func TestInMemorySearch_Index(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	doc := Document{
		ID: "1",
		Fields: map[string]interface{}{
			"title": "Test Document",
		},
	}

	err = search.Index(ctx, "test", doc)
	if err != nil {
		t.Fatalf("failed to index document: %v", err)
	}

	// Try indexing to non-existent index
	err = search.Index(ctx, "nonexistent", doc)
	if err != ErrIndexNotFound {
		t.Errorf("expected ErrIndexNotFound, got %v", err)
	}
}

func TestInMemorySearch_BulkIndex(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Doc 1"}},
		{ID: "2", Fields: map[string]interface{}{"title": "Doc 2"}},
		{ID: "3", Fields: map[string]interface{}{"title": "Doc 3"}},
	}

	err = search.BulkIndex(ctx, "test", docs)
	if err != nil {
		t.Fatalf("failed to bulk index: %v", err)
	}

	// Verify documents were indexed
	info, _ := search.GetIndexInfo(ctx, "test")
	if info.DocumentCount != 3 {
		t.Errorf("expected 3 documents, got %d", info.DocumentCount)
	}
}

func TestInMemorySearch_Get(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	doc := Document{
		ID:     "1",
		Fields: map[string]interface{}{"title": "Test"},
	}
	_ = search.Index(ctx, "test", doc)

	// Get existing document
	retrieved, err := search.Get(ctx, "test", "1")
	if err != nil {
		t.Fatalf("failed to get document: %v", err)
	}
	if retrieved.ID != "1" {
		t.Errorf("expected id '1', got '%s'", retrieved.ID)
	}

	// Get non-existent document
	_, err = search.Get(ctx, "test", "999")
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestInMemorySearch_Delete(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	doc := Document{
		ID:     "1",
		Fields: map[string]interface{}{"title": "Test"},
	}
	_ = search.Index(ctx, "test", doc)

	// Delete existing document
	err = search.Delete(ctx, "test", "1")
	if err != nil {
		t.Fatalf("failed to delete document: %v", err)
	}

	// Verify deleted
	_, err = search.Get(ctx, "test", "1")
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound after delete, got %v", err)
	}

	// Delete non-existent document
	err = search.Delete(ctx, "test", "999")
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestInMemorySearch_Update(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	doc := Document{
		ID:     "1",
		Fields: map[string]interface{}{"title": "Original"},
	}
	_ = search.Index(ctx, "test", doc)

	// Update existing document
	updated := Document{
		ID:     "1",
		Fields: map[string]interface{}{"title": "Updated"},
	}
	err = search.Update(ctx, "test", "1", updated)
	if err != nil {
		t.Fatalf("failed to update document: %v", err)
	}

	// Verify updated
	retrieved, _ := search.Get(ctx, "test", "1")
	if retrieved.Fields["title"] != "Updated" {
		t.Error("document was not updated")
	}

	// Update non-existent document
	err = search.Update(ctx, "test", "999", updated)
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestInMemorySearch_Search(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
			{Name: "content", Type: "text"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Index test documents
	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Go Programming", "content": "Learn Go"}},
		{ID: "2", Fields: map[string]interface{}{"title": "Python Tutorial", "content": "Learn Python"}},
		{ID: "3", Fields: map[string]interface{}{"title": "Go Best Practices", "content": "Advanced Go"}},
	}
	_ = search.BulkIndex(ctx, "test", docs)

	// Search for "Go"
	query := SearchQuery{
		Index: "test",
		Query: "go",
		Limit: 10,
	}
	results, err := search.Search(ctx, query)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if results.Total < 2 {
		t.Errorf("expected at least 2 results, got %d", results.Total)
	}

	// Verify results are sorted by score
	if len(results.Hits) > 1 {
		for i := 0; i < len(results.Hits)-1; i++ {
			if results.Hits[i].Score < results.Hits[i+1].Score {
				t.Error("results not sorted by score")
			}
		}
	}
}

func TestInMemorySearch_SearchWithFilters(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{
			{Name: "title", Type: "text"},
			{Name: "status", Type: "keyword"},
		},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Doc 1", "status": "published"}},
		{ID: "2", Fields: map[string]interface{}{"title": "Doc 2", "status": "draft"}},
		{ID: "3", Fields: map[string]interface{}{"title": "Doc 3", "status": "published"}},
	}
	_ = search.BulkIndex(ctx, "test", docs)

	query := SearchQuery{
		Index: "test",
		Query: "doc",
		Filters: []Filter{
			{Field: "status", Operator: "=", Value: "published"},
		},
		Limit: 10,
	}

	results, err := search.Search(ctx, query)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if results.Total != 2 {
		t.Errorf("expected 2 results, got %d", results.Total)
	}
}

func TestInMemorySearch_SearchPagination(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Index 10 documents
	docs := make([]Document, 10)
	for i := 0; i < 10; i++ {
		docs[i] = Document{
			ID:     string(rune('0' + i)),
			Fields: map[string]interface{}{"title": "Document"},
		}
	}
	_ = search.BulkIndex(ctx, "test", docs)

	// Test pagination
	query := SearchQuery{
		Index:  "test",
		Query:  "document",
		Offset: 5,
		Limit:  3,
	}

	results, err := search.Search(ctx, query)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if results.Total != 10 {
		t.Errorf("expected total 10, got %d", results.Total)
	}

	if len(results.Hits) != 3 {
		t.Errorf("expected 3 hits, got %d", len(results.Hits))
	}
}

func TestInMemorySearch_Suggest(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Go Programming"}},
		{ID: "2", Fields: map[string]interface{}{"title": "Go Best Practices"}},
		{ID: "3", Fields: map[string]interface{}{"title": "Python Tutorial"}},
	}
	_ = search.BulkIndex(ctx, "test", docs)

	query := SuggestQuery{
		Index: "test",
		Query: "go",
		Field: "title",
		Limit: 5,
	}

	results, err := search.Suggest(ctx, query)
	if err != nil {
		t.Fatalf("suggest failed: %v", err)
	}

	if len(results.Suggestions) < 2 {
		t.Errorf("expected at least 2 suggestions, got %d", len(results.Suggestions))
	}
}

func TestInMemorySearch_Autocomplete(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Apple"}},
		{ID: "2", Fields: map[string]interface{}{"title": "Application"}},
		{ID: "3", Fields: map[string]interface{}{"title": "Banana"}},
	}
	_ = search.BulkIndex(ctx, "test", docs)

	query := AutocompleteQuery{
		Index: "test",
		Query: "app",
		Field: "title",
		Limit: 5,
	}

	results, err := search.Autocomplete(ctx, query)
	if err != nil {
		t.Fatalf("autocomplete failed: %v", err)
	}

	if len(results.Completions) < 2 {
		t.Errorf("expected at least 2 completions, got %d", len(results.Completions))
	}
}

func TestInMemorySearch_Stats(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Get stats before any operations
	stats, err := search.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if stats.IndexCount != 0 {
		t.Errorf("expected 0 indexes, got %d", stats.IndexCount)
	}

	// Create index and add documents
	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	_ = search.CreateIndex(ctx, "test", schema)
	docs := []Document{
		{ID: "1", Fields: map[string]interface{}{"title": "Doc 1"}},
	}
	_ = search.BulkIndex(ctx, "test", docs)

	// Get stats after operations
	stats, err = search.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if stats.IndexCount != 1 {
		t.Errorf("expected 1 index, got %d", stats.IndexCount)
	}

	if stats.DocumentCount != 1 {
		t.Errorf("expected 1 document, got %d", stats.DocumentCount)
	}

	if stats.Uptime == 0 {
		t.Error("expected non-zero uptime")
	}
}

func TestInMemorySearch_NotConnectedErrors(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	// Test all methods return ErrNotConnected
	schema := IndexSchema{}
	doc := Document{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"CreateIndex", func() error { return search.CreateIndex(ctx, "test", schema) }},
		{"DeleteIndex", func() error { return search.DeleteIndex(ctx, "test") }},
		{"ListIndexes", func() error { _, err := search.ListIndexes(ctx); return err }},
		{"GetIndexInfo", func() error { _, err := search.GetIndexInfo(ctx, "test"); return err }},
		{"Index", func() error { return search.Index(ctx, "test", doc) }},
		{"BulkIndex", func() error { return search.BulkIndex(ctx, "test", []Document{doc}) }},
		{"Get", func() error { _, err := search.Get(ctx, "test", "1"); return err }},
		{"Delete", func() error { return search.Delete(ctx, "test", "1") }},
		{"Update", func() error { return search.Update(ctx, "test", "1", doc) }},
		{"Search", func() error { _, err := search.Search(ctx, SearchQuery{Index: "test"}); return err }},
		{"Suggest", func() error { _, err := search.Suggest(ctx, SuggestQuery{Index: "test"}); return err }},
		{"Autocomplete", func() error { _, err := search.Autocomplete(ctx, AutocompleteQuery{Index: "test"}); return err }},
		{"Stats", func() error { _, err := search.Stats(ctx); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err != ErrNotConnected {
				t.Errorf("%s: expected ErrNotConnected, got %v", tt.name, err)
			}
		})
	}
}

func TestInMemorySearch_ConcurrentAccess(t *testing.T) {
	search := newTestInMemorySearch()
	ctx := context.Background()

	err := search.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	err = search.CreateIndex(ctx, "test", schema)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			doc := Document{
				ID:     string(rune('0' + id)),
				Fields: map[string]interface{}{"title": "Test"},
			}
			_ = search.Index(ctx, "test", doc)
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all documents were indexed
	info, _ := search.GetIndexInfo(ctx, "test")
	if info.DocumentCount != 10 {
		t.Errorf("expected 10 documents, got %d", info.DocumentCount)
	}
}

func BenchmarkInMemorySearch_Index(b *testing.B) {
	search := newTestInMemorySearch()
	ctx := context.Background()
	_ = search.Connect(ctx)

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	_ = search.CreateIndex(ctx, "test", schema)

	doc := Document{
		ID:     "1",
		Fields: map[string]interface{}{"title": "Test Document"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = search.Index(ctx, "test", doc)
	}
}

func BenchmarkInMemorySearch_Search(b *testing.B) {
	search := newTestInMemorySearch()
	ctx := context.Background()
	_ = search.Connect(ctx)

	schema := IndexSchema{
		Fields: []FieldSchema{{Name: "title", Type: "text"}},
	}
	_ = search.CreateIndex(ctx, "test", schema)

	// Index 100 documents
	for i := 0; i < 100; i++ {
		doc := Document{
			ID:     string(rune('0' + i%10)),
			Fields: map[string]interface{}{"title": "Test Document"},
		}
		_ = search.Index(ctx, "test", doc)
	}

	query := SearchQuery{
		Index: "test",
		Query: "test",
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = search.Search(ctx, query)
	}
}
