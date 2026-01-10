package sdk

import (
	"strings"
	"testing"
)

func TestCitationBuilder(t *testing.T) {
	t.Run("build basic citation", func(t *testing.T) {
		citation := NewCitation(CitationTypeDocument).
			WithTitle("Test Document").
			WithAuthor("John Doe").
			WithURL("https://example.com").
			WithScore(0.95).
			Build()

		if citation.Type != CitationTypeDocument {
			t.Errorf("expected type document, got %s", citation.Type)
		}

		if citation.Title != "Test Document" {
			t.Errorf("expected title 'Test Document', got '%s'", citation.Title)
		}

		if citation.Author != "John Doe" {
			t.Errorf("expected author 'John Doe', got '%s'", citation.Author)
		}

		if citation.Score != 0.95 {
			t.Errorf("expected score 0.95, got %f", citation.Score)
		}
	})

	t.Run("build RAG citation", func(t *testing.T) {
		citation := NewCitation(CitationTypeRAG).
			WithDocumentID("doc-123").
			WithChunkID("chunk-456").
			WithExcerpt("This is a relevant excerpt...").
			WithScore(0.85).
			WithConfidence(0.9).
			Build()

		if citation.Type != CitationTypeRAG {
			t.Errorf("expected type rag, got %s", citation.Type)
		}

		if citation.DocumentID != "doc-123" {
			t.Errorf("expected doc ID 'doc-123', got '%s'", citation.DocumentID)
		}

		if citation.ChunkID != "chunk-456" {
			t.Errorf("expected chunk ID 'chunk-456', got '%s'", citation.ChunkID)
		}
	})

	t.Run("build citation with location", func(t *testing.T) {
		citation := NewCitation(CitationTypeCode).
			WithTitle("source.go").
			WithPage(5).
			WithLines(10, 20).
			WithOffset(100, 200).
			Build()

		if citation.Page != 5 {
			t.Errorf("expected page 5, got %d", citation.Page)
		}

		if citation.StartLine != 10 || citation.EndLine != 20 {
			t.Errorf("expected lines 10-20, got %d-%d", citation.StartLine, citation.EndLine)
		}
	})
}

func TestCitationManager(t *testing.T) {
	t.Run("add and get citations", func(t *testing.T) {
		manager := NewCitationManager()

		citation := NewCitation(CitationTypeDocument).
			WithTitle("Doc 1").
			Build()

		refNum := manager.AddCitation(citation)
		if refNum != 1 {
			t.Errorf("expected ref num 1, got %d", refNum)
		}

		retrieved := manager.GetCitation(1)
		if retrieved == nil {
			t.Fatal("citation should not be nil")
		}

		if retrieved.Title != "Doc 1" {
			t.Errorf("expected title 'Doc 1', got '%s'", retrieved.Title)
		}
	})

	t.Run("avoid duplicate citations", func(t *testing.T) {
		manager := NewCitationManager()

		citation1 := NewCitation(CitationTypeRAG).
			WithDocumentID("doc-1").
			WithChunkID("chunk-1").
			Build()

		citation2 := NewCitation(CitationTypeRAG).
			WithDocumentID("doc-1").
			WithChunkID("chunk-1").
			Build()

		ref1 := manager.AddCitation(citation1)
		ref2 := manager.AddCitation(citation2)

		// Same doc+chunk should return same ref
		if ref1 != ref2 {
			t.Errorf("expected same ref for duplicate citation, got %d and %d", ref1, ref2)
		}

		citations := manager.GetCitations()
		if len(citations) != 1 {
			t.Errorf("expected 1 citation, got %d", len(citations))
		}
	})

	t.Run("multiple citations", func(t *testing.T) {
		manager := NewCitationManager()

		for i := range 5 {
			citation := NewCitation(CitationTypeDocument).
				WithDocumentID(string(rune('a' + i))).
				WithChunkID(string(rune('1' + i))).
				Build()
			manager.AddCitation(citation)
		}

		citations := manager.GetCitations()
		if len(citations) != 5 {
			t.Errorf("expected 5 citations, got %d", len(citations))
		}
	})

	t.Run("create cited content", func(t *testing.T) {
		manager := NewCitationManager()

		manager.AddCitation(NewCitation(CitationTypeDocument).
			WithDocumentID("doc-1").
			WithChunkID("chunk-1").
			WithTitle("Source 1").Build())
		manager.AddCitation(NewCitation(CitationTypeDocument).
			WithDocumentID("doc-2").
			WithChunkID("chunk-2").
			WithTitle("Source 2").Build())

		cited := manager.CreateCitedContent("According to research [1] and studies [2]...")

		if len(cited.Citations) != 2 {
			t.Errorf("expected 2 citations, got %d", len(cited.Citations))
		}

		if len(cited.CitationMap) != 2 {
			t.Errorf("expected 2 citation map entries, got %d", len(cited.CitationMap))
		}
	})
}

func TestCitationFormatter(t *testing.T) {
	citation := NewCitation(CitationTypeDocument).
		WithTitle("Test Document").
		WithAuthor("Jane Doe").
		WithURL("https://example.com").
		Build()

	t.Run("numeric style", func(t *testing.T) {
		formatter := NewCitationFormatter(CitationStyleNumeric)

		ref := formatter.FormatReference(citation, 1)
		if ref != "[1]" {
			t.Errorf("expected '[1]', got '%s'", ref)
		}
	})

	t.Run("footnote style", func(t *testing.T) {
		formatter := NewCitationFormatter(CitationStyleFootnote)

		ref := formatter.FormatReference(citation, 1)
		if ref != "¹" {
			t.Errorf("expected '¹', got '%s'", ref)
		}
	})

	t.Run("inline style", func(t *testing.T) {
		formatter := NewCitationFormatter(CitationStyleInline)

		ref := formatter.FormatReference(citation, 1)
		if !strings.Contains(ref, "Test Document") {
			t.Errorf("inline style should contain title, got '%s'", ref)
		}
	})

	t.Run("markdown style", func(t *testing.T) {
		formatter := NewCitationFormatter(CitationStyleMarkdown)

		ref := formatter.FormatReference(citation, 1)
		if ref != "[^1]" {
			t.Errorf("expected '[^1]', got '%s'", ref)
		}
	})

	t.Run("format bibliography", func(t *testing.T) {
		formatter := NewCitationFormatter(CitationStyleNumeric)

		bib := formatter.FormatBibliography(citation, 1)

		if !strings.Contains(bib, "[1]") {
			t.Error("bibliography should contain reference number")
		}

		if !strings.Contains(bib, "Jane Doe") {
			t.Error("bibliography should contain author")
		}

		if !strings.Contains(bib, "Test Document") {
			t.Error("bibliography should contain title")
		}
	})
}

func TestCitationExtractor(t *testing.T) {
	extractor := NewCitationExtractor().
		WithMinScore(0.5).
		WithMaxExcerptLen(100)

	t.Run("extract from retrieval result", func(t *testing.T) {
		result := &RetrievalResult{
			Query: "test query",
			Documents: []RetrievedDocument{
				{
					Document: DocumentChunk{
						ID:      "chunk-1",
						Content: "This is relevant content that should be cited.",
						Metadata: map[string]any{
							"doc_id": "doc-1",
							"title":  "Source Document",
						},
					},
					Score: 0.9,
				},
				{
					Document: DocumentChunk{
						ID:      "chunk-2",
						Content: "Less relevant content.",
						Metadata: map[string]any{
							"doc_id": "doc-2",
						},
					},
					Score: 0.6,
				},
				{
					Document: DocumentChunk{
						ID:      "chunk-3",
						Content: "Not relevant enough.",
						Metadata: map[string]any{
							"doc_id": "doc-3",
						},
					},
					Score: 0.3, // Below threshold
				},
			},
		}

		citations := extractor.FromRetrievalResult(result)

		// Should only extract 2 citations (score >= 0.5)
		if len(citations) != 2 {
			t.Errorf("expected 2 citations, got %d", len(citations))
		}

		// Check first citation
		if citations[0].Score != 0.9 {
			t.Errorf("expected score 0.9, got %f", citations[0].Score)
		}

		if citations[0].ChunkID != "chunk-1" {
			t.Errorf("expected chunk ID 'chunk-1', got '%s'", citations[0].ChunkID)
		}
	})
}

func TestCitationPatternDetector(t *testing.T) {
	detector := NewCitationPatternDetector()

	t.Run("detect numeric citations", func(t *testing.T) {
		text := "According to research [1] and studies [2], this is important [3]."
		matches := detector.FindCitations(text)

		if len(matches) != 3 {
			t.Errorf("expected 3 matches, got %d", len(matches))
		}
	})

	t.Run("detect markdown footnotes", func(t *testing.T) {
		text := "First point[^1] and second point[^2]."
		matches := detector.FindCitations(text)

		if len(matches) != 2 {
			t.Errorf("expected 2 matches, got %d", len(matches))
		}
	})
}

func TestCitationInjector(t *testing.T) {
	t.Run("inject citations", func(t *testing.T) {
		injector := NewCitationInjector(CitationStyleNumeric)

		citation1 := NewCitation(CitationTypeDocument).
			WithDocumentID("doc-1").
			WithChunkID("chunk-1").
			WithTitle("Source 1").
			Build()

		marker := injector.AddCitation(citation1)
		if marker != "[1]" {
			t.Errorf("expected '[1]', got '%s'", marker)
		}

		citation2 := NewCitation(CitationTypeDocument).
			WithDocumentID("doc-2").
			WithChunkID("chunk-2").
			WithTitle("Source 2").
			Build()

		content := "This is a claim"
		result := injector.InjectCitation(content, len(content), citation2)

		if !strings.Contains(result, "[2]") { // Second citation
			t.Error("result should contain citation marker")
		}
	})
}
