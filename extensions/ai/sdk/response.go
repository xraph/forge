package sdk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// StructuredResponse represents a response with rich, typed content parts
// that can be rendered differently by frontend applications.
type StructuredResponse struct {
	// ID is a unique identifier for this response
	ID string `json:"id"`

	// Parts contains the ordered content parts
	Parts []ContentPart `json:"parts"`

	// Artifacts contains named, exportable content
	Artifacts []Artifact `json:"artifacts,omitempty"`

	// Citations contains source references
	Citations []Citation `json:"citations,omitempty"`

	// Suggestions contains follow-up actions
	Suggestions []Suggestion `json:"suggestions,omitempty"`

	// Metadata contains additional response metadata
	Metadata ResponseMetadata `json:"metadata"`
}

// ResponseMetadata contains metadata about the response.
type ResponseMetadata struct {
	Model        string        `json:"model,omitempty"`
	Provider     string        `json:"provider,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	InputTokens  int           `json:"input_tokens,omitempty"`
	OutputTokens int           `json:"output_tokens,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

// ContentPartType identifies the type of content part.
type ContentPartType string

const (
	PartTypeText        ContentPartType = "text"
	PartTypeCode        ContentPartType = "code"
	PartTypeTable       ContentPartType = "table"
	PartTypeCard        ContentPartType = "card"
	PartTypeList        ContentPartType = "list"
	PartTypeImage       ContentPartType = "image"
	PartTypeCollapsible ContentPartType = "collapsible"
	PartTypeThinking    ContentPartType = "thinking"
	PartTypeQuote       ContentPartType = "quote"
	PartTypeDivider     ContentPartType = "divider"
	PartTypeAlert       ContentPartType = "alert"
	PartTypeProgress    ContentPartType = "progress"
	PartTypeChart       ContentPartType = "chart"
	PartTypeJSON        ContentPartType = "json"
	PartTypeMarkdown    ContentPartType = "markdown"
)

// ContentPart is the interface for all content part types.
type ContentPart interface {
	Type() ContentPartType
	ToJSON() ([]byte, error)
}

// --- Text Part ---

// TextPart represents plain or formatted text.
type TextPart struct {
	PartType ContentPartType `json:"type"`
	Text     string          `json:"text"`
	Format   TextFormat      `json:"format,omitempty"`
	Style    *TextStyle      `json:"style,omitempty"`
}

// TextFormat specifies the text format.
type TextFormat string

const (
	TextFormatPlain    TextFormat = "plain"
	TextFormatMarkdown TextFormat = "markdown"
	TextFormatHTML     TextFormat = "html"
)

// TextStyle contains styling options for text.
type TextStyle struct {
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Color     string `json:"color,omitempty"`
	Size      string `json:"size,omitempty"`      // sm, md, lg, xl
	Alignment string `json:"alignment,omitempty"` // left, center, right
}

func (t *TextPart) Type() ContentPartType { return PartTypeText }
func (t *TextPart) ToJSON() ([]byte, error) {
	t.PartType = PartTypeText

	return json.Marshal(t)
}

// --- Code Part ---

// CodePart represents a code block with syntax highlighting.
type CodePart struct {
	PartType    ContentPartType `json:"type"`
	Code        string          `json:"code"`
	Language    string          `json:"language,omitempty"`
	Title       string          `json:"title,omitempty"`
	LineNumbers bool            `json:"line_numbers,omitempty"`
	Highlight   []int           `json:"highlight,omitempty"` // Line numbers to highlight
	Copyable    bool            `json:"copyable,omitempty"`
	Runnable    bool            `json:"runnable,omitempty"` // If the code can be executed
	Collapsed   bool            `json:"collapsed,omitempty"`
}

func (c *CodePart) Type() ContentPartType { return PartTypeCode }
func (c *CodePart) ToJSON() ([]byte, error) {
	c.PartType = PartTypeCode

	return json.Marshal(c)
}

// --- Table Part ---

// TablePart represents tabular data.
type TablePart struct {
	PartType   ContentPartType `json:"type"`
	Title      string          `json:"title,omitempty"`
	Caption    string          `json:"caption,omitempty"`
	Headers    []TableHeader   `json:"headers"`
	Rows       [][]TableCell   `json:"rows"`
	Footer     []TableCell     `json:"footer,omitempty"`
	Sortable   bool            `json:"sortable,omitempty"`
	Searchable bool            `json:"searchable,omitempty"`
	Paginated  bool            `json:"paginated,omitempty"`
	PageSize   int             `json:"page_size,omitempty"`
	Style      *TableStyle     `json:"style,omitempty"`
}

// TableHeader defines a table column header.
type TableHeader struct {
	Label     string `json:"label"`
	Key       string `json:"key,omitempty"`
	Alignment string `json:"alignment,omitempty"` // left, center, right
	Width     string `json:"width,omitempty"`
	Sortable  bool   `json:"sortable,omitempty"`
}

// TableCell represents a table cell.
type TableCell struct {
	Value   any    `json:"value"`
	Display string `json:"display,omitempty"` // Formatted display value
	Link    string `json:"link,omitempty"`
	Style   string `json:"style,omitempty"` // CSS class or style
}

// TableStyle contains styling options for tables.
type TableStyle struct {
	Striped  bool   `json:"striped,omitempty"`
	Bordered bool   `json:"bordered,omitempty"`
	Hover    bool   `json:"hover,omitempty"`
	Compact  bool   `json:"compact,omitempty"`
	Theme    string `json:"theme,omitempty"` // light, dark, auto
}

func (t *TablePart) Type() ContentPartType { return PartTypeTable }
func (t *TablePart) ToJSON() ([]byte, error) {
	t.PartType = PartTypeTable

	return json.Marshal(t)
}

// --- Card Part ---

// CardPart represents a card with title, content, and actions.
type CardPart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Subtitle    string          `json:"subtitle,omitempty"`
	Description string          `json:"description,omitempty"`
	Image       *CardImage      `json:"image,omitempty"`
	Icon        string          `json:"icon,omitempty"`
	Badge       *CardBadge      `json:"badge,omitempty"`
	Actions     []CardAction    `json:"actions,omitempty"`
	Metadata    []CardMetadata  `json:"metadata,omitempty"`
	Footer      string          `json:"footer,omitempty"`
	Link        string          `json:"link,omitempty"`
	Variant     string          `json:"variant,omitempty"` // default, outlined, elevated
}

// CardImage defines an image for the card.
type CardImage struct {
	URL    string `json:"url"`
	Alt    string `json:"alt,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// CardBadge defines a badge/tag for the card.
type CardBadge struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// CardAction defines an action button for the card.
type CardAction struct {
	Label   string `json:"label"`
	Action  string `json:"action"` // URL or action identifier
	Primary bool   `json:"primary,omitempty"`
	Icon    string `json:"icon,omitempty"`
}

// CardMetadata defines key-value metadata for the card.
type CardMetadata struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Icon  string `json:"icon,omitempty"`
}

func (c *CardPart) Type() ContentPartType { return PartTypeCard }
func (c *CardPart) ToJSON() ([]byte, error) {
	c.PartType = PartTypeCard

	return json.Marshal(c)
}

// --- List Part ---

// ListPart represents an ordered or unordered list.
type ListPart struct {
	PartType ContentPartType `json:"type"`
	Title    string          `json:"title,omitempty"`
	Items    []ListItem      `json:"items"`
	Ordered  bool            `json:"ordered,omitempty"`
	Style    string          `json:"style,omitempty"` // bullet, number, checkbox, custom
}

// ListItem represents a single list item.
type ListItem struct {
	Text       string     `json:"text"`
	Icon       string     `json:"icon,omitempty"`
	Checked    *bool      `json:"checked,omitempty"` // For checkbox lists
	Link       string     `json:"link,omitempty"`
	SubItems   []ListItem `json:"sub_items,omitempty"`
	Metadata   string     `json:"metadata,omitempty"`   // Secondary text
	Importance string     `json:"importance,omitempty"` // low, medium, high
}

func (l *ListPart) Type() ContentPartType { return PartTypeList }
func (l *ListPart) ToJSON() ([]byte, error) {
	l.PartType = PartTypeList

	return json.Marshal(l)
}

// --- Image Part ---

// ImagePart represents an image with optional caption.
type ImagePart struct {
	PartType     ContentPartType `json:"type"`
	URL          string          `json:"url"`
	Alt          string          `json:"alt,omitempty"`
	Caption      string          `json:"caption,omitempty"`
	Width        int             `json:"width,omitempty"`
	Height       int             `json:"height,omitempty"`
	Thumbnail    string          `json:"thumbnail,omitempty"`
	Zoomable     bool            `json:"zoomable,omitempty"`
	Downloadable bool            `json:"downloadable,omitempty"`
}

func (i *ImagePart) Type() ContentPartType { return PartTypeImage }
func (i *ImagePart) ToJSON() ([]byte, error) {
	i.PartType = PartTypeImage

	return json.Marshal(i)
}

// --- Collapsible Part ---

// CollapsiblePart represents expandable/collapsible content.
type CollapsiblePart struct {
	PartType    ContentPartType `json:"type"`
	Summary     string          `json:"summary"`
	Content     []ContentPart   `json:"content"`
	DefaultOpen bool            `json:"default_open,omitempty"`
	Icon        string          `json:"icon,omitempty"`
}

func (c *CollapsiblePart) Type() ContentPartType { return PartTypeCollapsible }
func (c *CollapsiblePart) ToJSON() ([]byte, error) {
	c.PartType = PartTypeCollapsible
	// Need to handle nested content parts
	type alias CollapsiblePart

	return json.Marshal((*alias)(c))
}

// --- Thinking Part ---

// ThinkingPart represents the model's reasoning/thought process.
type ThinkingPart struct {
	PartType    ContentPartType `json:"type"`
	Content     string          `json:"content"`
	Steps       []ThinkingStep  `json:"steps,omitempty"`
	DefaultOpen bool            `json:"default_open,omitempty"`
}

// ThinkingStep represents a single reasoning step.
type ThinkingStep struct {
	Label   string `json:"label,omitempty"`
	Content string `json:"content"`
	Status  string `json:"status,omitempty"` // pending, complete, error
}

func (t *ThinkingPart) Type() ContentPartType { return PartTypeThinking }
func (t *ThinkingPart) ToJSON() ([]byte, error) {
	t.PartType = PartTypeThinking

	return json.Marshal(t)
}

// --- Quote Part ---

// QuotePart represents a quotation or callout.
type QuotePart struct {
	PartType   ContentPartType `json:"type"`
	Text       string          `json:"text"`
	Source     string          `json:"source,omitempty"`
	SourceURL  string          `json:"source_url,omitempty"`
	CitationID string          `json:"citation_id,omitempty"` // Reference to Citation
}

func (q *QuotePart) Type() ContentPartType { return PartTypeQuote }
func (q *QuotePart) ToJSON() ([]byte, error) {
	q.PartType = PartTypeQuote

	return json.Marshal(q)
}

// --- Divider Part ---

// DividerPart represents a visual separator.
type DividerPart struct {
	PartType ContentPartType `json:"type"`
	Style    string          `json:"style,omitempty"` // solid, dashed, dotted
	Label    string          `json:"label,omitempty"` // Optional label in the middle
}

func (d *DividerPart) Type() ContentPartType { return PartTypeDivider }
func (d *DividerPart) ToJSON() ([]byte, error) {
	d.PartType = PartTypeDivider

	return json.Marshal(d)
}

// --- Alert Part ---

// AlertPart represents an alert/notification message.
type AlertPart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Message     string          `json:"message"`
	Severity    AlertSeverity   `json:"severity"`
	Icon        string          `json:"icon,omitempty"`
	Dismissible bool            `json:"dismissible,omitempty"`
	Actions     []CardAction    `json:"actions,omitempty"`
}

// AlertSeverity indicates the alert type.
type AlertSeverity string

const (
	AlertInfo    AlertSeverity = "info"
	AlertSuccess AlertSeverity = "success"
	AlertWarning AlertSeverity = "warning"
	AlertError   AlertSeverity = "error"
)

func (a *AlertPart) Type() ContentPartType { return PartTypeAlert }
func (a *AlertPart) ToJSON() ([]byte, error) {
	a.PartType = PartTypeAlert

	return json.Marshal(a)
}

// --- Progress Part ---

// ProgressPart represents a progress indicator.
type ProgressPart struct {
	PartType    ContentPartType `json:"type"`
	Label       string          `json:"label,omitempty"`
	Value       float64         `json:"value"` // 0-100
	Max         float64         `json:"max,omitempty"`
	ShowPercent bool            `json:"show_percent,omitempty"`
	Status      string          `json:"status,omitempty"` // pending, active, complete, error
	Steps       []ProgressStep  `json:"steps,omitempty"`  // For multi-step progress
}

// ProgressStep represents a step in multi-step progress.
type ProgressStep struct {
	Label  string `json:"label"`
	Status string `json:"status"` // pending, active, complete, error
}

func (p *ProgressPart) Type() ContentPartType { return PartTypeProgress }
func (p *ProgressPart) ToJSON() ([]byte, error) {
	p.PartType = PartTypeProgress

	return json.Marshal(p)
}

// --- Chart Part ---

// ChartPart represents a chart/visualization.
type ChartPart struct {
	PartType  ContentPartType `json:"type"`
	ChartType ChartType       `json:"chart_type"`
	Title     string          `json:"title,omitempty"`
	Data      ChartData       `json:"data"`
	Options   *ChartOptions   `json:"options,omitempty"`
}

// ChartType specifies the type of chart.
type ChartType string

const (
	ChartLine     ChartType = "line"
	ChartBar      ChartType = "bar"
	ChartPie      ChartType = "pie"
	ChartDoughnut ChartType = "doughnut"
	ChartArea     ChartType = "area"
	ChartScatter  ChartType = "scatter"
)

// ChartData contains the data for a chart.
type ChartData struct {
	Labels   []string       `json:"labels,omitempty"`
	Datasets []ChartDataset `json:"datasets"`
}

// ChartDataset represents a single dataset in a chart.
type ChartDataset struct {
	Label           string    `json:"label,omitempty"`
	Data            []float64 `json:"data"`
	BackgroundColor string    `json:"background_color,omitempty"`
	BorderColor     string    `json:"border_color,omitempty"`
}

// ChartOptions contains chart configuration options.
type ChartOptions struct {
	ShowLegend bool   `json:"show_legend,omitempty"`
	ShowGrid   bool   `json:"show_grid,omitempty"`
	Responsive bool   `json:"responsive,omitempty"`
	Theme      string `json:"theme,omitempty"`
}

func (c *ChartPart) Type() ContentPartType { return PartTypeChart }
func (c *ChartPart) ToJSON() ([]byte, error) {
	c.PartType = PartTypeChart

	return json.Marshal(c)
}

// --- JSON Part ---

// JSONPart represents structured JSON data.
type JSONPart struct {
	PartType    ContentPartType `json:"type"`
	Data        any             `json:"data"`
	Title       string          `json:"title,omitempty"`
	Collapsible bool            `json:"collapsible,omitempty"`
	Copyable    bool            `json:"copyable,omitempty"`
}

func (j *JSONPart) Type() ContentPartType { return PartTypeJSON }
func (j *JSONPart) ToJSON() ([]byte, error) {
	j.PartType = PartTypeJSON

	return json.Marshal(j)
}

// --- Markdown Part ---

// MarkdownPart represents markdown content.
type MarkdownPart struct {
	PartType ContentPartType `json:"type"`
	Content  string          `json:"content"`
}

func (m *MarkdownPart) Type() ContentPartType { return PartTypeMarkdown }
func (m *MarkdownPart) ToJSON() ([]byte, error) {
	m.PartType = PartTypeMarkdown

	return json.Marshal(m)
}

// --- Response Builder ---

// ResponseBuilder provides a fluent API for building structured responses.
type ResponseBuilder struct {
	response *StructuredResponse
}

// NewResponseBuilder creates a new response builder.
func NewResponseBuilder() *ResponseBuilder {
	return &ResponseBuilder{
		response: &StructuredResponse{
			ID:    generateResponseID(),
			Parts: make([]ContentPart, 0),
			Metadata: ResponseMetadata{
				Timestamp: time.Now(),
			},
		},
	}
}

// WithID sets the response ID.
func (b *ResponseBuilder) WithID(id string) *ResponseBuilder {
	b.response.ID = id

	return b
}

// WithMetadata sets response metadata.
func (b *ResponseBuilder) WithMetadata(model, provider string, duration time.Duration) *ResponseBuilder {
	b.response.Metadata.Model = model
	b.response.Metadata.Provider = provider
	b.response.Metadata.Duration = duration

	return b
}

// WithTokenUsage sets token usage.
func (b *ResponseBuilder) WithTokenUsage(input, output int) *ResponseBuilder {
	b.response.Metadata.InputTokens = input
	b.response.Metadata.OutputTokens = output

	return b
}

// AddPart adds a content part to the response.
func (b *ResponseBuilder) AddPart(part ContentPart) *ResponseBuilder {
	b.response.Parts = append(b.response.Parts, part)

	return b
}

// AddText adds a text part.
func (b *ResponseBuilder) AddText(text string) *ResponseBuilder {
	return b.AddPart(&TextPart{Text: text, Format: TextFormatPlain})
}

// AddMarkdown adds a markdown part.
func (b *ResponseBuilder) AddMarkdown(content string) *ResponseBuilder {
	return b.AddPart(&MarkdownPart{Content: content})
}

// AddCode adds a code block.
func (b *ResponseBuilder) AddCode(code, language string) *ResponseBuilder {
	return b.AddPart(&CodePart{
		Code:     code,
		Language: language,
		Copyable: true,
	})
}

// AddTable adds a table.
func (b *ResponseBuilder) AddTable(headers []string, rows [][]any) *ResponseBuilder {
	tableHeaders := make([]TableHeader, len(headers))
	for i, h := range headers {
		tableHeaders[i] = TableHeader{Label: h, Key: h}
	}

	tableRows := make([][]TableCell, len(rows))
	for i, row := range rows {
		tableRows[i] = make([]TableCell, len(row))
		for j, cell := range row {
			tableRows[i][j] = TableCell{Value: cell, Display: fmt.Sprint(cell)}
		}
	}

	return b.AddPart(&TablePart{
		Headers: tableHeaders,
		Rows:    tableRows,
		Style:   &TableStyle{Striped: true, Hover: true},
	})
}

// AddCard adds a card.
func (b *ResponseBuilder) AddCard(title, description string) *ResponseBuilder {
	return b.AddPart(&CardPart{
		Title:       title,
		Description: description,
	})
}

// AddList adds a list.
func (b *ResponseBuilder) AddList(items []string, ordered bool) *ResponseBuilder {
	listItems := make([]ListItem, len(items))
	for i, item := range items {
		listItems[i] = ListItem{Text: item}
	}

	return b.AddPart(&ListPart{
		Items:   listItems,
		Ordered: ordered,
	})
}

// AddAlert adds an alert.
func (b *ResponseBuilder) AddAlert(message string, severity AlertSeverity) *ResponseBuilder {
	return b.AddPart(&AlertPart{
		Message:  message,
		Severity: severity,
	})
}

// AddThinking adds thinking/reasoning content.
func (b *ResponseBuilder) AddThinking(content string) *ResponseBuilder {
	return b.AddPart(&ThinkingPart{
		Content:     content,
		DefaultOpen: false,
	})
}

// AddDivider adds a visual divider.
func (b *ResponseBuilder) AddDivider() *ResponseBuilder {
	return b.AddPart(&DividerPart{Style: "solid"})
}

// AddArtifact adds an artifact to the response.
func (b *ResponseBuilder) AddArtifact(artifact Artifact) *ResponseBuilder {
	b.response.Artifacts = append(b.response.Artifacts, artifact)

	return b
}

// AddCitation adds a citation to the response.
func (b *ResponseBuilder) AddCitation(citation Citation) *ResponseBuilder {
	b.response.Citations = append(b.response.Citations, citation)

	return b
}

// AddSuggestion adds a suggestion to the response.
func (b *ResponseBuilder) AddSuggestion(suggestion Suggestion) *ResponseBuilder {
	b.response.Suggestions = append(b.response.Suggestions, suggestion)

	return b
}

// Build returns the completed structured response.
func (b *ResponseBuilder) Build() *StructuredResponse {
	return b.response
}

// --- Response Parser ---

// ResponseParser parses raw LLM output into structured parts.
type ResponseParser struct {
	// Patterns for detecting content types
	codeBlockRegex *regexp.Regexp
	tableRegex     *regexp.Regexp
	listRegex      *regexp.Regexp
	thinkingRegex  *regexp.Regexp
	headingRegex   *regexp.Regexp

	// UI block patterns
	uiFencedBlockRegex *regexp.Regexp
	uiInlineTagRegex   *regexp.Regexp
	enableUIBlocks     bool
}

// NewResponseParser creates a new response parser.
func NewResponseParser() *ResponseParser {
	return &ResponseParser{
		codeBlockRegex:     regexp.MustCompile("```(\\w+)?\\n([\\s\\S]*?)```"),
		tableRegex:         regexp.MustCompile(`\|(.+)\|[\r\n]+\|[-:|]+\|[\r\n]+((?:\|.+\|[\r\n]*)+)`),
		listRegex:          regexp.MustCompile(`(?m)^[\s]*[-*+][\s]+(.+)$`),
		thinkingRegex:      regexp.MustCompile(`<thinking>([\s\S]*?)</thinking>`),
		headingRegex:       regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`),
		uiFencedBlockRegex: regexp.MustCompile(`(?s)` + "```" + `ui:(\w+)\s*\n(.+?)\n` + "```"),
		uiInlineTagRegex:   regexp.MustCompile(`(?s)<ui:(\w+)>(.+?)</ui:(\w+)>`),
		enableUIBlocks:     false, // Disabled by default
	}
}

// WithUIBlocks enables parsing of ui:type blocks.
func (p *ResponseParser) WithUIBlocks(enabled bool) *ResponseParser {
	p.enableUIBlocks = enabled

	return p
}

// Parse converts raw text into structured content parts.
func (p *ResponseParser) Parse(content string) []ContentPart {
	parts := make([]ContentPart, 0)
	remaining := content

	// Extract thinking blocks first
	thinkingMatches := p.thinkingRegex.FindAllStringSubmatchIndex(remaining, -1)
	for i := len(thinkingMatches) - 1; i >= 0; i-- {
		match := thinkingMatches[i]
		thinkingContent := remaining[match[2]:match[3]]
		parts = append(parts, &ThinkingPart{
			Content:     strings.TrimSpace(thinkingContent),
			DefaultOpen: false,
		})
		remaining = remaining[:match[0]] + remaining[match[1]:]
	}

	// Extract UI blocks if enabled
	if p.enableUIBlocks {
		remaining, parts = p.extractUIBlocks(remaining, parts)
	}

	// Extract code blocks
	codeMatches := p.codeBlockRegex.FindAllStringSubmatchIndex(remaining, -1)
	lastEnd := 0

	for _, match := range codeMatches {
		// Add text before code block
		if match[0] > lastEnd {
			textBefore := strings.TrimSpace(remaining[lastEnd:match[0]])
			if textBefore != "" {
				parts = append(parts, &TextPart{Text: textBefore, Format: TextFormatMarkdown})
			}
		}

		// Extract language and code
		var language string
		if match[2] != -1 && match[3] != -1 {
			language = remaining[match[2]:match[3]]
		}

		code := remaining[match[4]:match[5]]

		parts = append(parts, &CodePart{
			Code:     strings.TrimSpace(code),
			Language: language,
			Copyable: true,
		})

		lastEnd = match[1]
	}

	// Add remaining text
	if lastEnd < len(remaining) {
		remainingText := strings.TrimSpace(remaining[lastEnd:])
		if remainingText != "" {
			// Check if remaining text contains tables
			parts = append(parts, p.parseRemainingText(remainingText)...)
		}
	}

	return parts
}

// extractUIBlocks extracts and parses ui:type blocks from content.
func (p *ResponseParser) extractUIBlocks(content string, existingParts []ContentPart) (string, []ContentPart) {
	parts := existingParts
	remaining := content

	// Create a UI output parser to handle the actual parsing
	parser := NewUIOutputParser()
	result := parser.Parse(content)

	// If UI blocks were found, add them to parts and use clean content
	if len(result.UIBlocks) > 0 {
		for _, block := range result.UIBlocks {
			if block.Part != nil {
				parts = append(parts, block.Part)
			}
		}

		remaining = result.CleanContent
	}

	return remaining, parts
}

// parseRemainingText parses remaining text for tables and lists.
func (p *ResponseParser) parseRemainingText(text string) []ContentPart {
	parts := make([]ContentPart, 0)

	// Check for markdown tables
	tableMatches := p.tableRegex.FindAllStringSubmatchIndex(text, -1)
	if len(tableMatches) > 0 {
		lastEnd := 0
		for _, match := range tableMatches {
			// Text before table
			if match[0] > lastEnd {
				textBefore := strings.TrimSpace(text[lastEnd:match[0]])
				if textBefore != "" {
					parts = append(parts, &TextPart{Text: textBefore, Format: TextFormatMarkdown})
				}
			}

			// Parse table
			tablePart := p.parseMarkdownTable(text[match[0]:match[1]])
			if tablePart != nil {
				parts = append(parts, tablePart)
			}

			lastEnd = match[1]
		}

		// Remaining text after tables
		if lastEnd < len(text) {
			remainingText := strings.TrimSpace(text[lastEnd:])
			if remainingText != "" {
				parts = append(parts, &TextPart{Text: remainingText, Format: TextFormatMarkdown})
			}
		}
	} else {
		// No tables, just add as markdown text
		parts = append(parts, &TextPart{Text: text, Format: TextFormatMarkdown})
	}

	return parts
}

// parseMarkdownTable parses a markdown table into a TablePart.
func (p *ResponseParser) parseMarkdownTable(tableText string) *TablePart {
	lines := strings.Split(strings.TrimSpace(tableText), "\n")
	if len(lines) < 3 {
		return nil
	}

	// Parse header
	headerLine := strings.Trim(lines[0], "|")
	headerCells := strings.Split(headerLine, "|")

	headers := make([]TableHeader, len(headerCells))
	for i, cell := range headerCells {
		headers[i] = TableHeader{
			Label: strings.TrimSpace(cell),
			Key:   strings.TrimSpace(cell),
		}
	}

	// Skip separator line (lines[1])

	// Parse rows
	rows := make([][]TableCell, 0)

	for i := 2; i < len(lines); i++ {
		rowLine := strings.Trim(lines[i], "|")
		if rowLine == "" {
			continue
		}

		cells := strings.Split(rowLine, "|")

		row := make([]TableCell, len(cells))
		for j, cell := range cells {
			cellValue := strings.TrimSpace(cell)
			row[j] = TableCell{Value: cellValue, Display: cellValue}
		}

		rows = append(rows, row)
	}

	return &TablePart{
		Headers: headers,
		Rows:    rows,
		Style:   &TableStyle{Striped: true, Hover: true},
	}
}

// --- Helper functions ---

func generateResponseID() string {
	return fmt.Sprintf("resp_%d", time.Now().UnixNano())
}

// ToJSON serializes the structured response to JSON.
func (r *StructuredResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ToPlainText converts the structured response to plain text.
func (r *StructuredResponse) ToPlainText() string {
	var sb strings.Builder

	for _, part := range r.Parts {
		switch p := part.(type) {
		case *TextPart:
			sb.WriteString(p.Text)
			sb.WriteString("\n\n")
		case *MarkdownPart:
			sb.WriteString(p.Content)
			sb.WriteString("\n\n")
		case *CodePart:
			sb.WriteString("```")
			sb.WriteString(p.Language)
			sb.WriteString("\n")
			sb.WriteString(p.Code)
			sb.WriteString("\n```\n\n")
		case *TablePart:
			// Simple text representation
			for _, h := range p.Headers {
				sb.WriteString(h.Label)
				sb.WriteString("\t")
			}

			sb.WriteString("\n")

			for _, row := range p.Rows {
				for _, cell := range row {
					sb.WriteString(cell.Display)
					sb.WriteString("\t")
				}

				sb.WriteString("\n")
			}

			sb.WriteString("\n")
		case *ListPart:
			for i, item := range p.Items {
				if p.Ordered {
					sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Text))
				} else {
					sb.WriteString(fmt.Sprintf("- %s\n", item.Text))
				}
			}

			sb.WriteString("\n")
		case *AlertPart:
			sb.WriteString(fmt.Sprintf("[%s] %s\n\n", p.Severity, p.Message))
		case *ThinkingPart:
			sb.WriteString("<thinking>\n")
			sb.WriteString(p.Content)
			sb.WriteString("\n</thinking>\n\n")
		}
	}

	return sb.String()
}
