package sdk

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// Citation represents a reference to a source document or data.
type Citation struct {
	// ID is a unique identifier for the citation
	ID string `json:"id"`

	// Type categorizes the citation source
	Type CitationType `json:"type"`

	// Source information
	Title      string `json:"title,omitempty"`
	Author     string `json:"author,omitempty"`
	URL        string `json:"url,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	ChunkID    string `json:"chunk_id,omitempty"`

	// Content excerpt
	Excerpt string `json:"excerpt,omitempty"`

	// Location within source
	Page        int `json:"page,omitempty"`
	StartLine   int `json:"start_line,omitempty"`
	EndLine     int `json:"end_line,omitempty"`
	StartOffset int `json:"start_offset,omitempty"`
	EndOffset   int `json:"end_offset,omitempty"`

	// Relevance information
	Score      float64 `json:"score,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`

	// Metadata
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// CitationType categorizes citation sources.
type CitationType string

const (
	CitationTypeDocument  CitationType = "document"
	CitationTypeWebpage   CitationType = "webpage"
	CitationTypeAPI       CitationType = "api"
	CitationTypeDatabase  CitationType = "database"
	CitationTypeFile      CitationType = "file"
	CitationTypeCode      CitationType = "code"
	CitationTypeRAG       CitationType = "rag"
	CitationTypeKnowledge CitationType = "knowledge"
	CitationTypeMemory    CitationType = "memory"
)

// CitedContent represents content with inline citations.
type CitedContent struct {
	// Original content with citation markers
	RawContent string `json:"raw_content"`

	// Processed content with citation references
	Content string `json:"content"`

	// All citations referenced in the content
	Citations []Citation `json:"citations"`

	// Mapping of citation markers to citation IDs
	CitationMap map[string]string `json:"citation_map"`
}

// CitationBuilder provides a fluent API for building citations.
type CitationBuilder struct {
	citation *Citation
}

// NewCitation creates a new citation builder.
func NewCitation(citationType CitationType) *CitationBuilder {
	return &CitationBuilder{
		citation: &Citation{
			ID:        generateCitationID(),
			Type:      citationType,
			Metadata:  make(map[string]any),
			CreatedAt: time.Now(),
		},
	}
}

// WithID sets the citation ID.
func (b *CitationBuilder) WithID(id string) *CitationBuilder {
	b.citation.ID = id

	return b
}

// WithTitle sets the citation title.
func (b *CitationBuilder) WithTitle(title string) *CitationBuilder {
	b.citation.Title = title

	return b
}

// WithAuthor sets the citation author.
func (b *CitationBuilder) WithAuthor(author string) *CitationBuilder {
	b.citation.Author = author

	return b
}

// WithURL sets the citation URL.
func (b *CitationBuilder) WithURL(url string) *CitationBuilder {
	b.citation.URL = url

	return b
}

// WithDocumentID sets the source document ID.
func (b *CitationBuilder) WithDocumentID(docID string) *CitationBuilder {
	b.citation.DocumentID = docID

	return b
}

// WithChunkID sets the source chunk ID.
func (b *CitationBuilder) WithChunkID(chunkID string) *CitationBuilder {
	b.citation.ChunkID = chunkID

	return b
}

// WithExcerpt sets the content excerpt.
func (b *CitationBuilder) WithExcerpt(excerpt string) *CitationBuilder {
	b.citation.Excerpt = excerpt

	return b
}

// WithPage sets the page number.
func (b *CitationBuilder) WithPage(page int) *CitationBuilder {
	b.citation.Page = page

	return b
}

// WithLines sets the line range.
func (b *CitationBuilder) WithLines(start, end int) *CitationBuilder {
	b.citation.StartLine = start
	b.citation.EndLine = end

	return b
}

// WithOffset sets the character offset range.
func (b *CitationBuilder) WithOffset(start, end int) *CitationBuilder {
	b.citation.StartOffset = start
	b.citation.EndOffset = end

	return b
}

// WithScore sets the relevance score.
func (b *CitationBuilder) WithScore(score float64) *CitationBuilder {
	b.citation.Score = score

	return b
}

// WithConfidence sets the confidence level.
func (b *CitationBuilder) WithConfidence(confidence float64) *CitationBuilder {
	b.citation.Confidence = confidence

	return b
}

// WithMetadata adds metadata.
func (b *CitationBuilder) WithMetadata(key string, value any) *CitationBuilder {
	b.citation.Metadata[key] = value

	return b
}

// Build returns the completed citation.
func (b *CitationBuilder) Build() *Citation {
	return b.citation
}

// --- Citation Manager ---

// CitationManager manages citations for a response.
type CitationManager struct {
	citations     []Citation
	citationIndex map[string]int
	nextID        int
}

// NewCitationManager creates a new citation manager.
func NewCitationManager() *CitationManager {
	return &CitationManager{
		citations:     make([]Citation, 0),
		citationIndex: make(map[string]int),
		nextID:        1,
	}
}

// AddCitation adds a citation and returns its reference number.
func (m *CitationManager) AddCitation(citation *Citation) int {
	// Check if citation already exists (by document + chunk ID)
	key := fmt.Sprintf("%s:%s", citation.DocumentID, citation.ChunkID)
	if idx, exists := m.citationIndex[key]; exists {
		return idx + 1 // 1-based index
	}

	if citation.ID == "" {
		citation.ID = fmt.Sprintf("cite_%d", m.nextID)
	}

	m.citations = append(m.citations, *citation)
	idx := len(m.citations) - 1
	m.citationIndex[key] = idx
	m.nextID++

	return idx + 1 // 1-based index
}

// GetCitations returns all citations.
func (m *CitationManager) GetCitations() []Citation {
	return m.citations
}

// GetCitation returns a citation by its reference number.
func (m *CitationManager) GetCitation(refNum int) *Citation {
	idx := refNum - 1
	if idx < 0 || idx >= len(m.citations) {
		return nil
	}

	return &m.citations[idx]
}

// CreateCitedContent creates content with inline citation markers.
func (m *CitationManager) CreateCitedContent(content string) *CitedContent {
	return &CitedContent{
		RawContent:  content,
		Content:     content,
		Citations:   m.citations,
		CitationMap: m.createCitationMap(),
	}
}

func (m *CitationManager) createCitationMap() map[string]string {
	result := make(map[string]string)

	for i, citation := range m.citations {
		marker := fmt.Sprintf("[%d]", i+1)
		result[marker] = citation.ID
	}

	return result
}

// --- Citation Extractor ---

// CitationExtractor extracts citations from RAG results.
type CitationExtractor struct {
	minScore        float64
	maxExcerptLen   int
	includeMetadata bool
}

// NewCitationExtractor creates a new citation extractor.
func NewCitationExtractor() *CitationExtractor {
	return &CitationExtractor{
		minScore:        0.5,
		maxExcerptLen:   200,
		includeMetadata: true,
	}
}

// WithMinScore sets the minimum relevance score threshold.
func (e *CitationExtractor) WithMinScore(score float64) *CitationExtractor {
	e.minScore = score

	return e
}

// WithMaxExcerptLen sets the maximum excerpt length.
func (e *CitationExtractor) WithMaxExcerptLen(length int) *CitationExtractor {
	e.maxExcerptLen = length

	return e
}

// WithMetadata sets whether to include metadata.
func (e *CitationExtractor) WithMetadata(include bool) *CitationExtractor {
	e.includeMetadata = include

	return e
}

// FromRetrievalResult extracts citations from RAG retrieval results.
func (e *CitationExtractor) FromRetrievalResult(result *RetrievalResult) []Citation {
	citations := make([]Citation, 0)

	for _, doc := range result.Documents {
		if doc.Score < e.minScore {
			continue
		}

		citation := e.createCitationFromDoc(doc)
		citations = append(citations, *citation)
	}

	return citations
}

// createCitationFromDoc creates a citation from a retrieved document.
func (e *CitationExtractor) createCitationFromDoc(doc RetrievedDocument) *Citation {
	builder := NewCitation(CitationTypeRAG).
		WithChunkID(doc.Document.ID).
		WithScore(doc.Score)

	// Extract excerpt
	excerpt := doc.Document.Content
	if len(excerpt) > e.maxExcerptLen {
		excerpt = excerpt[:e.maxExcerptLen] + "..."
	}

	builder.WithExcerpt(excerpt)

	// Extract metadata if available
	if e.includeMetadata && doc.Document.Metadata != nil {
		// Document ID
		if docID, ok := doc.Document.Metadata["doc_id"].(string); ok {
			builder.WithDocumentID(docID)
		}

		// Title
		if title, ok := doc.Document.Metadata["title"].(string); ok {
			builder.WithTitle(title)
		}

		// URL
		if url, ok := doc.Document.Metadata["url"].(string); ok {
			builder.WithURL(url)
		}

		// Author
		if author, ok := doc.Document.Metadata["author"].(string); ok {
			builder.WithAuthor(author)
		}

		// Page
		if page, ok := doc.Document.Metadata["page"].(int); ok {
			builder.WithPage(page)
		}

		// Copy all metadata
		for k, v := range doc.Document.Metadata {
			builder.WithMetadata(k, v)
		}
	}

	return builder.Build()
}

// --- Citation Formatter ---

// CitationFormatter formats citations in various styles.
type CitationFormatter struct {
	style CitationStyle
}

// CitationStyle specifies the citation format.
type CitationStyle string

const (
	CitationStyleNumeric    CitationStyle = "numeric"     // [1], [2], [3]
	CitationStyleAuthorDate CitationStyle = "author_date" // (Author, 2024)
	CitationStyleFootnote   CitationStyle = "footnote"    // ¹, ², ³
	CitationStyleInline     CitationStyle = "inline"      // (source: Title)
	CitationStyleMarkdown   CitationStyle = "markdown"    // [^1], [^2]
)

// NewCitationFormatter creates a new citation formatter.
func NewCitationFormatter(style CitationStyle) *CitationFormatter {
	return &CitationFormatter{style: style}
}

// FormatReference formats a citation reference for inline use.
func (f *CitationFormatter) FormatReference(citation *Citation, refNum int) string {
	switch f.style {
	case CitationStyleNumeric:
		return fmt.Sprintf("[%d]", refNum)

	case CitationStyleAuthorDate:
		author := citation.Author
		if author == "" {
			author = "Unknown"
		}

		year := citation.CreatedAt.Year()

		return fmt.Sprintf("(%s, %d)", author, year)

	case CitationStyleFootnote:
		return superscript(refNum)

	case CitationStyleInline:
		if citation.Title != "" {
			return fmt.Sprintf("(source: %s)", citation.Title)
		}

		return fmt.Sprintf("(source: %s)", citation.ID)

	case CitationStyleMarkdown:
		return fmt.Sprintf("[^%d]", refNum)

	default:
		return fmt.Sprintf("[%d]", refNum)
	}
}

// FormatBibliography formats a citation for bibliography/references section.
func (f *CitationFormatter) FormatBibliography(citation *Citation, refNum int) string {
	var parts []string

	// Reference number
	switch f.style {
	case CitationStyleFootnote:
		parts = append(parts, superscript(refNum))
	case CitationStyleMarkdown:
		parts = append(parts, fmt.Sprintf("[^%d]:", refNum))
	default:
		parts = append(parts, fmt.Sprintf("[%d]", refNum))
	}

	// Author
	if citation.Author != "" {
		parts = append(parts, citation.Author+".")
	}

	// Title
	if citation.Title != "" {
		parts = append(parts, fmt.Sprintf("\"%s\".", citation.Title))
	}

	// URL
	if citation.URL != "" {
		parts = append(parts, fmt.Sprintf("<%s>", citation.URL))
	}

	// Score/relevance
	if citation.Score > 0 {
		parts = append(parts, fmt.Sprintf("(relevance: %.1f%%)", citation.Score*100))
	}

	return strings.Join(parts, " ")
}

// FormatCitedContent adds citation references to content.
func (f *CitationFormatter) FormatCitedContent(content string, citations []Citation) *CitedContent {
	manager := NewCitationManager()

	// Add all citations
	for i := range citations {
		manager.AddCitation(&citations[i])
	}

	// Build bibliography
	var bibliography strings.Builder
	bibliography.WriteString("\n\n---\n**References:**\n")

	for i, citation := range citations {
		bibliography.WriteString(f.FormatBibliography(&citation, i+1))
		bibliography.WriteString("\n")
	}

	return &CitedContent{
		RawContent:  content,
		Content:     content + bibliography.String(),
		Citations:   citations,
		CitationMap: manager.createCitationMap(),
	}
}

// --- Content with Citations Injector ---

// CitationInjector injects citation markers into content.
type CitationInjector struct {
	formatter *CitationFormatter
	manager   *CitationManager
}

// NewCitationInjector creates a new citation injector.
func NewCitationInjector(style CitationStyle) *CitationInjector {
	return &CitationInjector{
		formatter: NewCitationFormatter(style),
		manager:   NewCitationManager(),
	}
}

// AddCitation adds a citation and returns its marker.
func (i *CitationInjector) AddCitation(citation *Citation) string {
	refNum := i.manager.AddCitation(citation)

	return i.formatter.FormatReference(citation, refNum)
}

// InjectCitation injects a citation at a specific position in content.
func (i *CitationInjector) InjectCitation(content string, position int, citation *Citation) string {
	marker := i.AddCitation(citation)
	if position < 0 || position > len(content) {
		return content + marker
	}

	return content[:position] + marker + content[position:]
}

// GetCitedContent returns the content with all citations.
func (i *CitationInjector) GetCitedContent(content string) *CitedContent {
	return i.formatter.FormatCitedContent(content, i.manager.GetCitations())
}

// --- Helper functions ---

func generateCitationID() string {
	return fmt.Sprintf("cite_%d", time.Now().UnixNano())
}

func superscript(n int) string {
	superscripts := []rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}

	if n < 0 || n > 9 {
		return fmt.Sprintf("(%d)", n)
	}

	return string(superscripts[n])
}

// --- Citation Pattern Detector ---

// CitationPatternDetector detects citation patterns in text.
type CitationPatternDetector struct {
	patterns []*regexp.Regexp
}

// NewCitationPatternDetector creates a new citation pattern detector.
func NewCitationPatternDetector() *CitationPatternDetector {
	return &CitationPatternDetector{
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`\[(\d+)\]`),              // [1], [2]
			regexp.MustCompile(`\[\^(\d+)\]`),            // [^1], [^2]
			regexp.MustCompile(`\(([^,]+),\s*(\d{4})\)`), // (Author, 2024)
			regexp.MustCompile(`<source:\s*([^>]+)>`),    // <source: Document>
		},
	}
}

// FindCitations finds citation markers in text.
func (d *CitationPatternDetector) FindCitations(text string) []CitationMatch {
	matches := make([]CitationMatch, 0)

	for _, pattern := range d.patterns {
		found := pattern.FindAllStringSubmatchIndex(text, -1)
		for _, match := range found {
			matches = append(matches, CitationMatch{
				Start: match[0],
				End:   match[1],
				Text:  text[match[0]:match[1]],
			})
		}
	}

	return matches
}

// CitationMatch represents a found citation marker.
type CitationMatch struct {
	Start int
	End   int
	Text  string
}

// --- Streaming Citation Support ---

// CitationStreamer handles streaming citations as they're detected in text.
type CitationStreamer struct {
	mu sync.RWMutex

	// Manager for collecting citations
	manager *CitationManager

	// Detector for finding citations
	detector *CitationPatternDetector

	// Formatter for citation references
	formatter *CitationFormatter

	// Buffer for accumulating text
	buffer strings.Builder

	// Track detected citations in current stream
	detectedCitations []InlineCitation

	// Callbacks
	onCitationDetected func(citation InlineCitation)
	onEvent            func(llm.ClientStreamEvent) error

	// Stream context
	executionID string
	logger      forge.Logger
	metrics     forge.Metrics

	// Citation index for the current stream
	citationIndex int
}

// CitationStreamerConfig holds configuration for CitationStreamer.
type CitationStreamerConfig struct {
	Style       CitationStyle
	ExecutionID string
	OnEvent     func(llm.ClientStreamEvent) error
	OnCitation  func(citation InlineCitation)
	Logger      forge.Logger
	Metrics     forge.Metrics
}

// NewCitationStreamer creates a new citation streamer.
func NewCitationStreamer(config CitationStreamerConfig) *CitationStreamer {
	return &CitationStreamer{
		manager:            NewCitationManager(),
		detector:           NewCitationPatternDetector(),
		formatter:          NewCitationFormatter(config.Style),
		detectedCitations:  make([]InlineCitation, 0),
		onCitationDetected: config.OnCitation,
		onEvent:            config.OnEvent,
		executionID:        config.ExecutionID,
		logger:             config.Logger,
		metrics:            config.Metrics,
	}
}

// ProcessDelta processes a streaming text delta and detects citations.
func (s *CitationStreamer) ProcessDelta(delta string) (string, []InlineCitation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Append to buffer
	s.buffer.WriteString(delta)
	currentText := s.buffer.String()

	// Find all citations in the buffered text
	matches := s.detector.FindCitations(currentText)

	// Track new citations
	newCitations := make([]InlineCitation, 0)

	for _, match := range matches {
		// Check if this is a new citation
		isNew := true

		for _, existing := range s.detectedCitations {
			if existing.Position == match.Start && existing.Length == match.End-match.Start {
				isNew = false

				break
			}
		}

		if isNew {
			s.citationIndex++
			inlineCitation := InlineCitation{
				ID:       fmt.Sprintf("icite_%d_%d", time.Now().UnixNano(), s.citationIndex),
				Number:   s.citationIndex,
				Position: match.Start,
				Length:   match.End - match.Start,
			}

			s.detectedCitations = append(s.detectedCitations, inlineCitation)
			newCitations = append(newCitations, inlineCitation)

			// Callback
			if s.onCitationDetected != nil {
				s.onCitationDetected(inlineCitation)
			}
		}
	}

	return currentText, newCitations
}

// AddCitation adds a citation and returns its inline reference.
func (s *CitationStreamer) AddCitation(citation *Citation) InlineCitation {
	s.mu.Lock()
	defer s.mu.Unlock()

	refNum := s.manager.AddCitation(citation)
	position := s.buffer.Len()

	inlineCitation := InlineCitation{
		ID:       citation.ID,
		Number:   refNum,
		Position: position,
		Length:   0,
		Source: &Source{
			Title:      citation.Title,
			URL:        citation.URL,
			Author:     citation.Author,
			Type:       string(citation.Type),
			Excerpt:    citation.Excerpt,
			Confidence: citation.Confidence,
		},
	}

	s.detectedCitations = append(s.detectedCitations, inlineCitation)

	return inlineCitation
}

// StreamCitation streams a citation event.
func (s *CitationStreamer) StreamCitation(ctx context.Context, citation InlineCitation) error {
	if s.onEvent == nil {
		return nil
	}

	// Create citation event
	event := llm.NewUIPartStartEvent(s.executionID, citation.ID, string(PartTypeInlineCitation))
	event.PartData = citation

	return s.onEvent(event)
}

// GetDetectedCitations returns all detected citations.
func (s *CitationStreamer) GetDetectedCitations() []InlineCitation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]InlineCitation, len(s.detectedCitations))
	copy(result, s.detectedCitations)

	return result
}

// GetCitations returns all managed citations.
func (s *CitationStreamer) GetCitations() []Citation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.manager.GetCitations()
}

// GetBufferedText returns the accumulated text.
func (s *CitationStreamer) GetBufferedText() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.buffer.String()
}

// CreateInlineCitationPart creates an InlineCitationPart from the streamed data.
func (s *CitationStreamer) CreateInlineCitationPart() *InlineCitationPart {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &InlineCitationPart{
		PartType:    PartTypeInlineCitation,
		Text:        s.buffer.String(),
		Citations:   s.detectedCitations,
		ShowNumbers: true,
		Clickable:   true,
	}
}

// Reset resets the streamer state.
func (s *CitationStreamer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffer.Reset()
	s.detectedCitations = make([]InlineCitation, 0)
	s.citationIndex = 0
}

// --- Citation Stream Handler ---

// CitationStreamHandler integrates citation detection with the streaming system.
type CitationStreamHandler struct {
	streamer *CitationStreamer

	// Track state
	inCitation     bool
	citationBuffer strings.Builder
}

// NewCitationStreamHandler creates a new citation stream handler.
func NewCitationStreamHandler(config CitationStreamerConfig) *CitationStreamHandler {
	return &CitationStreamHandler{
		streamer: NewCitationStreamer(config),
	}
}

// HandleContentDelta processes content delta and detects citations.
func (h *CitationStreamHandler) HandleContentDelta(delta string) (processedDelta string, citations []InlineCitation) {
	return h.streamer.ProcessDelta(delta)
}

// AddSourceCitation adds a citation from a source.
func (h *CitationStreamHandler) AddSourceCitation(source *Source) InlineCitation {
	citation := NewCitation(CitationTypeDocument).
		WithTitle(source.Title).
		WithURL(source.URL).
		WithAuthor(source.Author).
		WithExcerpt(source.Excerpt).
		WithConfidence(source.Confidence).
		Build()

	return h.streamer.AddCitation(citation)
}

// GetInlineCitationPart returns the complete inline citation part.
func (h *CitationStreamHandler) GetInlineCitationPart() *InlineCitationPart {
	return h.streamer.CreateInlineCitationPart()
}

// GetCitations returns all citations.
func (h *CitationStreamHandler) GetCitations() []Citation {
	return h.streamer.GetCitations()
}

// --- Citation-aware Content Builder ---

// CitedContentBuilder builds content with inline citations.
type CitedContentBuilder struct {
	content    strings.Builder
	citations  []InlineCitation
	sources    []*Source
	formatter  *CitationFormatter
	nextNumber int
}

// NewCitedContentBuilder creates a new cited content builder.
func NewCitedContentBuilder(style CitationStyle) *CitedContentBuilder {
	return &CitedContentBuilder{
		citations:  make([]InlineCitation, 0),
		sources:    make([]*Source, 0),
		formatter:  NewCitationFormatter(style),
		nextNumber: 1,
	}
}

// WriteText writes text without citations.
func (b *CitedContentBuilder) WriteText(text string) *CitedContentBuilder {
	b.content.WriteString(text)

	return b
}

// WriteTextWithCitation writes text and attaches a citation.
func (b *CitedContentBuilder) WriteTextWithCitation(text string, source *Source) *CitedContentBuilder {
	position := b.content.Len()
	b.content.WriteString(text)

	citation := InlineCitation{
		ID:       fmt.Sprintf("cite_%d", time.Now().UnixNano()),
		Number:   b.nextNumber,
		Position: position,
		Length:   len(text),
		Source:   source,
	}

	b.citations = append(b.citations, citation)
	b.sources = append(b.sources, source)
	b.nextNumber++

	return b
}

// AddCitationMarker adds a citation marker at current position.
func (b *CitedContentBuilder) AddCitationMarker(source *Source) *CitedContentBuilder {
	position := b.content.Len()
	marker := fmt.Sprintf("[%d]", b.nextNumber)
	b.content.WriteString(marker)

	citation := InlineCitation{
		ID:       fmt.Sprintf("cite_%d", time.Now().UnixNano()),
		Number:   b.nextNumber,
		Position: position,
		Length:   len(marker),
		Source:   source,
	}

	b.citations = append(b.citations, citation)
	b.sources = append(b.sources, source)
	b.nextNumber++

	return b
}

// Build creates the final InlineCitationPart.
func (b *CitedContentBuilder) Build() *InlineCitationPart {
	return &InlineCitationPart{
		PartType:    PartTypeInlineCitation,
		Text:        b.content.String(),
		Citations:   b.citations,
		ShowNumbers: true,
		Clickable:   true,
	}
}

// GetText returns the current text content.
func (b *CitedContentBuilder) GetText() string {
	return b.content.String()
}

// GetCitations returns all inline citations.
func (b *CitedContentBuilder) GetCitations() []InlineCitation {
	return b.citations
}

// --- Stream Citation from RAG ---

// StreamCitationsFromRAG creates inline citations from RAG results and streams them.
func StreamCitationsFromRAG(
	ctx context.Context,
	executionID string,
	content string,
	ragResults *RetrievalResult,
	onEvent func(llm.ClientStreamEvent) error,
) (*InlineCitationPart, error) {
	builder := NewCitedContentBuilder(CitationStyleNumeric)

	// Write the content
	builder.WriteText(content)

	// Add citations from RAG results
	if ragResults != nil {
		for _, doc := range ragResults.Documents {
			source := &Source{
				Title:      getStringFromMetadata(doc.Document.Metadata, "title"),
				URL:        getStringFromMetadata(doc.Document.Metadata, "url"),
				Author:     getStringFromMetadata(doc.Document.Metadata, "author"),
				Type:       "document",
				Excerpt:    truncateString(doc.Document.Content, 200),
				Confidence: doc.Score,
			}
			builder.AddCitationMarker(source)
		}
	}

	part := builder.Build()

	// Stream the citation part
	if onEvent != nil {
		event := llm.NewUIPartStartEvent(executionID, "citations", string(PartTypeInlineCitation))

		event.PartData = part
		if err := onEvent(event); err != nil {
			return nil, err
		}

		endEvent := llm.NewUIPartEndEvent(executionID, "citations")
		if err := onEvent(endEvent); err != nil {
			return nil, err
		}
	}

	return part, nil
}

// Helper functions

func getStringFromMetadata(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}

	if val, ok := metadata[key].(string); ok {
		return val
	}

	return ""
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}
