package sdk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// UIOutputParser detects and parses structured UI output from AI responses.
// It supports two formats:
// 1. Fenced code blocks: ```ui:table {...} ```
// 2. Inline tags: <ui:chart>{...}</ui:chart>
type UIOutputParser struct {
	// Patterns for detecting UI blocks
	fencedBlockPattern *regexp.Regexp
	inlineTagPattern   *regexp.Regexp

	// Enabled UI types
	enabledTypes map[ContentPartType]bool
}

// UIBlock represents a detected UI block in the content.
type UIBlock struct {
	Type       ContentPartType `json:"type"`
	RawJSON    string          `json:"rawJson"`
	StartIndex int             `json:"startIndex"`
	EndIndex   int             `json:"endIndex"`
	Part       ContentPart     `json:"part,omitempty"`
}

// UIParseResult contains the result of parsing content for UI blocks.
type UIParseResult struct {
	// CleanContent is the content with UI blocks removed
	CleanContent string `json:"cleanContent"`

	// TextParts contains text segments between UI blocks
	TextParts []string `json:"textParts"`

	// UIBlocks contains all detected and parsed UI blocks
	UIBlocks []UIBlock `json:"uiBlocks"`

	// Parts contains all content parts in order (text + UI)
	Parts []ContentPart `json:"parts"`

	// Errors contains any parsing errors encountered
	Errors []error `json:"errors,omitempty"`
}

// NewUIOutputParser creates a new UI output parser.
func NewUIOutputParser() *UIOutputParser {
	return &UIOutputParser{
		// Match ```ui:type ... ``` blocks
		fencedBlockPattern: regexp.MustCompile(`(?s)` + "```" + `ui:(\w+)\s*\n(.+?)\n` + "```"),

		// Match <ui:type>...</ui:type> inline tags
		// Note: Go regex doesn't support backreferences, so we match generically
		// and validate the closing tag in code
		inlineTagPattern: regexp.MustCompile(`(?s)<ui:(\w+)>(.+?)</ui:(\w+)>`),

		// All types enabled by default
		enabledTypes: map[ContentPartType]bool{
			PartTypeTable:       true,
			PartTypeChart:       true,
			PartTypeCard:        true,
			PartTypeMetric:      true,
			PartTypeTimeline:    true,
			PartTypeKanban:      true,
			PartTypeButtonGroup: true,
			PartTypeForm:        true,
			PartTypeStats:       true,
			PartTypeGallery:     true,
			PartTypeAlert:       true,
			PartTypeProgress:    true,
			PartTypeList:        true,
			PartTypeCode:        true,
			PartTypeJSON:        true,
		},
	}
}

// EnableType enables parsing of a specific UI type.
func (p *UIOutputParser) EnableType(partType ContentPartType) *UIOutputParser {
	p.enabledTypes[partType] = true
	return p
}

// DisableType disables parsing of a specific UI type.
func (p *UIOutputParser) DisableType(partType ContentPartType) *UIOutputParser {
	p.enabledTypes[partType] = false
	return p
}

// EnableAllTypes enables all UI types.
func (p *UIOutputParser) EnableAllTypes() *UIOutputParser {
	for t := range p.enabledTypes {
		p.enabledTypes[t] = true
	}
	return p
}

// Parse parses content and extracts UI blocks.
func (p *UIOutputParser) Parse(content string) *UIParseResult {
	result := &UIParseResult{
		CleanContent: content,
		TextParts:    make([]string, 0),
		UIBlocks:     make([]UIBlock, 0),
		Parts:        make([]ContentPart, 0),
		Errors:       make([]error, 0),
	}

	// Collect all matches from both patterns
	var matches []uiMatch

	// Find fenced blocks ```ui:type ... ```
	fencedMatches := p.fencedBlockPattern.FindAllStringSubmatchIndex(content, -1)
	for _, m := range fencedMatches {
		if len(m) >= 6 {
			matches = append(matches, uiMatch{
				uiType:     content[m[2]:m[3]],
				jsonData:   content[m[4]:m[5]],
				startIndex: m[0],
				endIndex:   m[1],
				fullMatch:  content[m[0]:m[1]],
			})
		}
	}

	// Find inline tags <ui:type>...</ui:type>
	inlineMatches := p.inlineTagPattern.FindAllStringSubmatchIndex(content, -1)
	for _, m := range inlineMatches {
		// Now has 4 groups: full match, opening tag, content, closing tag
		if len(m) >= 8 {
			openTag := content[m[2]:m[3]]
			closeTag := content[m[6]:m[7]]
			// Only include if opening and closing tags match
			if openTag == closeTag {
				matches = append(matches, uiMatch{
					uiType:     openTag,
					jsonData:   content[m[4]:m[5]],
					startIndex: m[0],
					endIndex:   m[1],
					fullMatch:  content[m[0]:m[1]],
				})
			}
		}
	}

	// Sort matches by start index
	sortMatchesByIndex(matches)

	// Process matches and build result
	lastEnd := 0
	for _, m := range matches {
		partType := p.normalizeUIType(m.uiType)

		// Check if type is enabled
		if !p.enabledTypes[partType] {
			continue
		}

		// Add text before this block
		if m.startIndex > lastEnd {
			textBefore := content[lastEnd:m.startIndex]
			if trimmed := strings.TrimSpace(textBefore); trimmed != "" {
				result.TextParts = append(result.TextParts, textBefore)
				result.Parts = append(result.Parts, &TextPart{
					PartType: PartTypeText,
					Text:     textBefore,
					Format:   TextFormatMarkdown,
				})
			}
		}

		// Parse the UI block
		uiBlock := UIBlock{
			Type:       partType,
			RawJSON:    strings.TrimSpace(m.jsonData),
			StartIndex: m.startIndex,
			EndIndex:   m.endIndex,
		}

		// Try to parse JSON into ContentPart
		part, err := p.parseUIBlock(partType, m.jsonData)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to parse ui:%s block: %w", m.uiType, err))
			// Keep the raw block as fallback
			uiBlock.Part = &JSONPart{
				PartType: PartTypeJSON,
				Data:     m.jsonData,
				Title:    fmt.Sprintf("UI: %s (parse error)", m.uiType),
			}
		} else {
			uiBlock.Part = part
		}

		result.UIBlocks = append(result.UIBlocks, uiBlock)
		result.Parts = append(result.Parts, uiBlock.Part)

		lastEnd = m.endIndex
	}

	// Add remaining text after last block
	if lastEnd < len(content) {
		textAfter := content[lastEnd:]
		if trimmed := strings.TrimSpace(textAfter); trimmed != "" {
			result.TextParts = append(result.TextParts, textAfter)
			result.Parts = append(result.Parts, &TextPart{
				PartType: PartTypeText,
				Text:     textAfter,
				Format:   TextFormatMarkdown,
			})
		}
	}

	// Build clean content (with UI blocks removed)
	result.CleanContent = p.buildCleanContent(content, matches)

	return result
}

// HasUIBlocks checks if content contains any UI blocks.
func (p *UIOutputParser) HasUIBlocks(content string) bool {
	return p.fencedBlockPattern.MatchString(content) || p.inlineTagPattern.MatchString(content)
}

// ExtractUIBlocks extracts only the UI blocks without full parsing.
func (p *UIOutputParser) ExtractUIBlocks(content string) []UIBlock {
	result := p.Parse(content)
	return result.UIBlocks
}

// normalizeUIType converts UI type string to ContentPartType.
func (p *UIOutputParser) normalizeUIType(uiType string) ContentPartType {
	switch strings.ToLower(uiType) {
	case "table":
		return PartTypeTable
	case "chart", "graph":
		return PartTypeChart
	case "card":
		return PartTypeCard
	case "metric", "metrics", "kpi":
		return PartTypeMetric
	case "timeline":
		return PartTypeTimeline
	case "kanban", "board":
		return PartTypeKanban
	case "buttons", "button", "buttongroup", "button_group":
		return PartTypeButtonGroup
	case "form":
		return PartTypeForm
	case "stats", "statistics":
		return PartTypeStats
	case "gallery", "images":
		return PartTypeGallery
	case "alert", "notification":
		return PartTypeAlert
	case "progress", "progressbar":
		return PartTypeProgress
	case "list":
		return PartTypeList
	case "code":
		return PartTypeCode
	case "json":
		return PartTypeJSON
	default:
		return ContentPartType(uiType)
	}
}

// parseUIBlock parses JSON data into a ContentPart.
func (p *UIOutputParser) parseUIBlock(partType ContentPartType, jsonData string) (ContentPart, error) {
	jsonData = strings.TrimSpace(jsonData)

	switch partType {
	case PartTypeTable:
		return p.parseTableBlock(jsonData)
	case PartTypeChart:
		return p.parseChartBlock(jsonData)
	case PartTypeCard:
		return p.parseCardBlock(jsonData)
	case PartTypeMetric:
		return p.parseMetricBlock(jsonData)
	case PartTypeTimeline:
		return p.parseTimelineBlock(jsonData)
	case PartTypeKanban:
		return p.parseKanbanBlock(jsonData)
	case PartTypeButtonGroup:
		return p.parseButtonGroupBlock(jsonData)
	case PartTypeForm:
		return p.parseFormBlock(jsonData)
	case PartTypeStats:
		return p.parseStatsBlock(jsonData)
	case PartTypeGallery:
		return p.parseGalleryBlock(jsonData)
	case PartTypeAlert:
		return p.parseAlertBlock(jsonData)
	case PartTypeProgress:
		return p.parseProgressBlock(jsonData)
	case PartTypeList:
		return p.parseListBlock(jsonData)
	case PartTypeCode:
		return p.parseCodeBlock(jsonData)
	default:
		// Return as JSON part for unknown types
		var data any
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			return nil, err
		}
		return &JSONPart{
			PartType: PartTypeJSON,
			Data:     data,
		}, nil
	}
}

// Parse methods for each UI type

func (p *UIOutputParser) parseTableBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title   string          `json:"title"`
		Headers json.RawMessage `json:"headers"`
		Rows    json.RawMessage `json:"rows"`
		Options map[string]any  `json:"options"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	// Parse headers
	headers := parseTableHeaders(unmarshalToAny(data.Headers))

	// Parse rows
	var rowsAny []any
	json.Unmarshal(data.Rows, &rowsAny)
	rows := parseTableRows(rowsAny)

	return &TablePart{
		PartType: PartTypeTable,
		Title:    data.Title,
		Headers:  headers,
		Rows:     rows,
	}, nil
}

func (p *UIOutputParser) parseChartBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title    string            `json:"title"`
		Type     string            `json:"type"`
		Labels   []string          `json:"labels"`
		Datasets []json.RawMessage `json:"datasets"`
		Options  map[string]any    `json:"options"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	chartData := ChartData{
		Labels:   data.Labels,
		Datasets: make([]ChartDataset, 0),
	}

	for _, ds := range data.Datasets {
		var dataset ChartDataset
		if err := json.Unmarshal(ds, &dataset); err == nil {
			chartData.Datasets = append(chartData.Datasets, dataset)
		}
	}

	return &ChartPart{
		PartType:  PartTypeChart,
		Title:     data.Title,
		ChartType: ChartType(data.Type),
		Data:      chartData,
	}, nil
}

func (p *UIOutputParser) parseCardBlock(jsonData string) (ContentPart, error) {
	var card CardPart
	if err := json.Unmarshal([]byte(jsonData), &card); err != nil {
		return nil, err
	}
	card.PartType = PartTypeCard
	return &card, nil
}

func (p *UIOutputParser) parseMetricBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title   string            `json:"title"`
		Metrics []json.RawMessage `json:"metrics"`
		Layout  string            `json:"layout"`
		Columns int               `json:"columns"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	metrics := make([]Metric, 0)
	for _, m := range data.Metrics {
		var metric Metric
		if err := json.Unmarshal(m, &metric); err == nil {
			metrics = append(metrics, metric)
		}
	}

	return &MetricPart{
		PartType: PartTypeMetric,
		Title:    data.Title,
		Metrics:  metrics,
		Layout:   MetricLayout(data.Layout),
		Columns:  data.Columns,
	}, nil
}

func (p *UIOutputParser) parseTimelineBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title       string            `json:"title"`
		Events      []json.RawMessage `json:"events"`
		Orientation string            `json:"orientation"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	events := make([]TimelineEvent, 0)
	for _, e := range data.Events {
		var event TimelineEvent
		if err := json.Unmarshal(e, &event); err == nil {
			events = append(events, event)
		}
	}

	return &TimelinePart{
		PartType:    PartTypeTimeline,
		Title:       data.Title,
		Events:      events,
		Orientation: data.Orientation,
	}, nil
}

func (p *UIOutputParser) parseKanbanBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title     string            `json:"title"`
		Columns   []json.RawMessage `json:"columns"`
		Draggable bool              `json:"draggable"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	columns := make([]KanbanColumn, 0)
	for _, c := range data.Columns {
		var col KanbanColumn
		if err := json.Unmarshal(c, &col); err == nil {
			columns = append(columns, col)
		}
	}

	return &KanbanPart{
		PartType:  PartTypeKanban,
		Title:     data.Title,
		Columns:   columns,
		Draggable: data.Draggable,
	}, nil
}

func (p *UIOutputParser) parseButtonGroupBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title   string            `json:"title"`
		Buttons []json.RawMessage `json:"buttons"`
		Layout  string            `json:"layout"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	buttons := make([]Button, 0)
	for _, b := range data.Buttons {
		var btn Button
		if err := json.Unmarshal(b, &btn); err == nil {
			buttons = append(buttons, btn)
		}
	}

	return &ButtonGroupPart{
		PartType: PartTypeButtonGroup,
		Title:    data.Title,
		Buttons:  buttons,
		Layout:   ButtonLayout(data.Layout),
	}, nil
}

func (p *UIOutputParser) parseFormBlock(jsonData string) (ContentPart, error) {
	var form FormPart
	if err := json.Unmarshal([]byte(jsonData), &form); err != nil {
		return nil, err
	}
	form.PartType = PartTypeForm
	return &form, nil
}

func (p *UIOutputParser) parseStatsBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title  string            `json:"title"`
		Stats  []json.RawMessage `json:"stats"`
		Layout string            `json:"layout"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	stats := make([]Stat, 0)
	for _, s := range data.Stats {
		var stat Stat
		if err := json.Unmarshal(s, &stat); err == nil {
			stats = append(stats, stat)
		}
	}

	return &StatsPart{
		PartType: PartTypeStats,
		Title:    data.Title,
		Stats:    stats,
		Layout:   data.Layout,
	}, nil
}

func (p *UIOutputParser) parseGalleryBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title   string            `json:"title"`
		Items   []json.RawMessage `json:"items"`
		Layout  string            `json:"layout"`
		Columns int               `json:"columns"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	items := make([]GalleryItem, 0)
	for _, i := range data.Items {
		var item GalleryItem
		if err := json.Unmarshal(i, &item); err == nil {
			items = append(items, item)
		}
	}

	return &GalleryPart{
		PartType: PartTypeGallery,
		Title:    data.Title,
		Items:    items,
		Layout:   data.Layout,
		Columns:  data.Columns,
	}, nil
}

func (p *UIOutputParser) parseAlertBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title       string `json:"title"`
		Message     string `json:"message"`
		Type        string `json:"type"`
		Severity    string `json:"severity"`
		Dismissible bool   `json:"dismissible"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	severity := data.Severity
	if severity == "" {
		severity = data.Type // Fallback to type field
	}
	if severity == "" {
		severity = "info"
	}

	return &AlertPart{
		PartType:    PartTypeAlert,
		Title:       data.Title,
		Message:     data.Message,
		Severity:    AlertSeverity(severity),
		Dismissible: data.Dismissible,
	}, nil
}

func (p *UIOutputParser) parseProgressBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Label   string  `json:"label"`
		Value   float64 `json:"value"`
		Max     float64 `json:"max"`
		Variant string  `json:"variant"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	if data.Max == 0 {
		data.Max = 100
	}

	return &ProgressPart{
		PartType: PartTypeProgress,
		Label:    data.Label,
		Value:    data.Value,
		Max:      data.Max,
	}, nil
}

func (p *UIOutputParser) parseListBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Title   string     `json:"title"`
		Items   []ListItem `json:"items"`
		Ordered bool       `json:"ordered"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	return &ListPart{
		PartType: PartTypeList,
		Title:    data.Title,
		Items:    data.Items,
		Ordered:  data.Ordered,
	}, nil
}

func (p *UIOutputParser) parseCodeBlock(jsonData string) (ContentPart, error) {
	var data struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Title    string `json:"title"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	return &CodePart{
		PartType: PartTypeCode,
		Code:     data.Code,
		Language: data.Language,
		Title:    data.Title,
	}, nil
}

// buildCleanContent removes UI blocks from content.
func (p *UIOutputParser) buildCleanContent(content string, matches []uiMatch) string {
	if len(matches) == 0 {
		return content
	}

	var builder strings.Builder
	lastEnd := 0

	for _, m := range matches {
		if m.startIndex > lastEnd {
			builder.WriteString(content[lastEnd:m.startIndex])
		}
		lastEnd = m.endIndex
	}

	if lastEnd < len(content) {
		builder.WriteString(content[lastEnd:])
	}

	return strings.TrimSpace(builder.String())
}

// uiMatch represents a matched UI block in content.
type uiMatch struct {
	uiType     string
	jsonData   string
	startIndex int
	endIndex   int
	fullMatch  string
}

// sortMatchesByIndex sorts matches by start index.
func sortMatchesByIndex(matches []uiMatch) {
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].startIndex < matches[i].startIndex {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

// unmarshalToAny unmarshals JSON to any type.
func unmarshalToAny(raw json.RawMessage) any {
	var result any
	json.Unmarshal(raw, &result)
	return result
}

// =============================================================================
// Integration helpers
// =============================================================================

// ParseAndStreamUI parses content for UI blocks and streams them.
func ParseAndStreamUI(
	content string,
	executionID string,
	onUIBlock func(block UIBlock) error,
) (*UIParseResult, error) {
	parser := NewUIOutputParser()
	result := parser.Parse(content)

	for _, block := range result.UIBlocks {
		if err := onUIBlock(block); err != nil {
			return result, err
		}
	}

	return result, nil
}

// ConvertToStructuredResponse converts parsed UI blocks to a StructuredResponse.
func ConvertToStructuredResponse(result *UIParseResult) *StructuredResponse {
	return &StructuredResponse{
		ID:    fmt.Sprintf("resp_%d", time.Now().UnixNano()),
		Parts: result.Parts,
		Metadata: ResponseMetadata{
			Timestamp: time.Now(),
		},
	}
}
