package sdk

import (
	"testing"
)

func TestUIOutputParser_ParseFencedBlocks(t *testing.T) {
	parser := NewUIOutputParser()

	content := `Here is your data:

` + "```ui:table" + `
{
  "title": "Users",
  "headers": [{"label": "Name", "key": "name"}, {"label": "Email", "key": "email"}],
  "rows": [["Alice", "alice@example.com"], ["Bob", "bob@example.com"]]
}
` + "```" + `

The data shows all active users.`

	result := parser.Parse(content)

	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block, got %d", len(result.UIBlocks))
	}

	block := result.UIBlocks[0]
	if block.Type != PartTypeTable {
		t.Errorf("expected type 'table', got '%s'", block.Type)
	}

	if block.Part == nil {
		t.Error("expected part to be parsed")
	}

	// Check clean content doesn't have UI block
	if parser.HasUIBlocks(result.CleanContent) {
		t.Error("clean content should not have UI blocks")
	}
}

func TestUIOutputParser_ParseInlineTags(t *testing.T) {
	parser := NewUIOutputParser()

	content := `Here is a chart: <ui:chart>{"type": "bar", "title": "Sales", "labels": ["Q1", "Q2"], "datasets": [{"label": "Revenue", "data": [100, 200]}]}</ui:chart> and some text after.`

	result := parser.Parse(content)

	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block, got %d", len(result.UIBlocks))
	}

	block := result.UIBlocks[0]
	if block.Type != PartTypeChart {
		t.Errorf("expected type 'chart', got '%s'", block.Type)
	}
}

func TestUIOutputParser_MultipleBlocks(t *testing.T) {
	parser := NewUIOutputParser()

	content := `First table:

` + "```ui:table" + `
{"title": "Table 1", "headers": [{"label": "A", "key": "a"}], "rows": [["1"]]}
` + "```" + `

And now a chart:

` + "```ui:chart" + `
{"type": "pie", "title": "Distribution", "labels": ["A", "B"], "datasets": [{"data": [50, 50]}]}
` + "```" + `

And some metrics: <ui:metrics>{"metrics": [{"label": "Total", "value": 100}]}</ui:metrics>

Done.`

	result := parser.Parse(content)

	if len(result.UIBlocks) != 3 {
		t.Fatalf("expected 3 UI blocks, got %d", len(result.UIBlocks))
	}

	expectedTypes := []ContentPartType{PartTypeTable, PartTypeChart, PartTypeMetric}
	for i, block := range result.UIBlocks {
		if block.Type != expectedTypes[i] {
			t.Errorf("block %d: expected type '%s', got '%s'", i, expectedTypes[i], block.Type)
		}
	}

	// Verify text parts exist
	if len(result.TextParts) == 0 {
		t.Error("expected text parts to be extracted")
	}
}

func TestUIOutputParser_NoBlocks(t *testing.T) {
	parser := NewUIOutputParser()

	content := "This is just plain text with no UI blocks."

	result := parser.Parse(content)

	if len(result.UIBlocks) != 0 {
		t.Errorf("expected 0 UI blocks, got %d", len(result.UIBlocks))
	}

	if result.CleanContent != content {
		t.Error("clean content should be unchanged")
	}
}

func TestUIOutputParser_HasUIBlocks(t *testing.T) {
	parser := NewUIOutputParser()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "with fenced block",
			content:  "```ui:table\n{}\n```",
			expected: true,
		},
		{
			name:     "with inline tag",
			content:  "<ui:chart>{}</ui:chart>",
			expected: true,
		},
		{
			name:     "plain text",
			content:  "No UI blocks here",
			expected: false,
		},
		{
			name:     "regular code block",
			content:  "```python\nprint('hello')\n```",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.HasUIBlocks(tt.content)
			if result != tt.expected {
				t.Errorf("HasUIBlocks() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestUIOutputParser_TypeNormalization(t *testing.T) {
	parser := NewUIOutputParser()

	tests := []struct {
		input    string
		expected ContentPartType
	}{
		{"<ui:table>{}</ui:table>", PartTypeTable},
		{"<ui:chart>{}</ui:chart>", PartTypeChart},
		{"<ui:graph>{}</ui:graph>", PartTypeChart}, // graph normalizes to chart
		{"<ui:metric>{}</ui:metric>", PartTypeMetric},
		{"<ui:metrics>{}</ui:metrics>", PartTypeMetric},
		{"<ui:kpi>{}</ui:kpi>", PartTypeMetric}, // kpi normalizes to metric
		{"<ui:buttons>{}</ui:buttons>", PartTypeButtonGroup},
		{"<ui:button>{}</ui:button>", PartTypeButtonGroup},
		{"<ui:buttongroup>{}</ui:buttongroup>", PartTypeButtonGroup},
		{"<ui:kanban>{}</ui:kanban>", PartTypeKanban},
		{"<ui:board>{}</ui:board>", PartTypeKanban}, // board normalizes to kanban
		{"<ui:alert>{}</ui:alert>", PartTypeAlert},
		{"<ui:notification>{}</ui:notification>", PartTypeAlert},
	}

	for _, tt := range tests {
		result := parser.Parse(tt.input)
		if len(result.UIBlocks) == 0 {
			t.Errorf("expected UI block for %s", tt.input)

			continue
		}

		if result.UIBlocks[0].Type != tt.expected {
			t.Errorf("input %s: expected type %s, got %s", tt.input, tt.expected, result.UIBlocks[0].Type)
		}
	}
}

func TestUIOutputParser_DisableType(t *testing.T) {
	parser := NewUIOutputParser()
	parser.DisableType(PartTypeTable)

	content := `<ui:table>{"headers": [], "rows": []}</ui:table>
<ui:chart>{"type": "bar", "labels": [], "datasets": []}</ui:chart>`

	result := parser.Parse(content)

	// Should only have chart, not table
	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block (chart), got %d", len(result.UIBlocks))
	}

	if result.UIBlocks[0].Type != PartTypeChart {
		t.Errorf("expected chart block, got %s", result.UIBlocks[0].Type)
	}
}

func TestUIOutputParser_ParseTableBlock(t *testing.T) {
	parser := NewUIOutputParser()

	content := "```ui:table\n" + `{
  "title": "Sales Data",
  "headers": [
    {"label": "Month", "key": "month", "sortable": true},
    {"label": "Revenue", "key": "revenue"}
  ],
  "rows": [
    ["January", "$10,000"],
    ["February", "$12,000"],
    ["March", "$15,000"]
  ]
}` + "\n```"

	result := parser.Parse(content)

	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block, got %d", len(result.UIBlocks))
	}

	tablePart, ok := result.UIBlocks[0].Part.(*TablePart)
	if !ok {
		t.Fatal("expected TablePart")
	}

	if tablePart.Title != "Sales Data" {
		t.Errorf("expected title 'Sales Data', got '%s'", tablePart.Title)
	}

	if len(tablePart.Headers) != 2 {
		t.Errorf("expected 2 headers, got %d", len(tablePart.Headers))
	}

	if len(tablePart.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(tablePart.Rows))
	}
}

func TestUIOutputParser_ParseChartBlock(t *testing.T) {
	parser := NewUIOutputParser()

	content := "```ui:chart\n" + `{
  "title": "Revenue Trend",
  "type": "line",
  "labels": ["Jan", "Feb", "Mar", "Apr"],
  "datasets": [
    {
      "label": "2024",
      "data": [100, 150, 200, 250],
      "borderColor": "#4CAF50"
    }
  ]
}` + "\n```"

	result := parser.Parse(content)

	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block, got %d", len(result.UIBlocks))
	}

	chartPart, ok := result.UIBlocks[0].Part.(*ChartPart)
	if !ok {
		t.Fatal("expected ChartPart")
	}

	if chartPart.Title != "Revenue Trend" {
		t.Errorf("expected title 'Revenue Trend', got '%s'", chartPart.Title)
	}

	if chartPart.ChartType != ChartLine {
		t.Errorf("expected type 'line', got '%s'", chartPart.ChartType)
	}

	if len(chartPart.Data.Labels) != 4 {
		t.Errorf("expected 4 labels, got %d", len(chartPart.Data.Labels))
	}
}

func TestUIOutputParser_ParseAlertBlock(t *testing.T) {
	parser := NewUIOutputParser()

	content := `<ui:alert>{"title": "Warning", "message": "Please check your input", "severity": "warning", "dismissible": true}</ui:alert>`

	result := parser.Parse(content)

	if len(result.UIBlocks) != 1 {
		t.Fatalf("expected 1 UI block, got %d", len(result.UIBlocks))
	}

	alertPart, ok := result.UIBlocks[0].Part.(*AlertPart)
	if !ok {
		t.Fatal("expected AlertPart")
	}

	if alertPart.Title != "Warning" {
		t.Errorf("expected title 'Warning', got '%s'", alertPart.Title)
	}

	if alertPart.Severity != AlertWarning {
		t.Errorf("expected severity 'warning', got '%s'", alertPart.Severity)
	}

	if !alertPart.Dismissible {
		t.Error("expected dismissible to be true")
	}
}

func TestUIOutputParser_InvalidJSON(t *testing.T) {
	parser := NewUIOutputParser()

	// Invalid JSON should result in an error but not panic
	content := "```ui:table\n{invalid json}\n```"

	result := parser.Parse(content)

	// Should have one block with error
	if len(result.Errors) == 0 {
		t.Error("expected parsing error for invalid JSON")
	}
}

func TestUIOutputParser_ExtractUIBlocks(t *testing.T) {
	parser := NewUIOutputParser()

	content := `Text before <ui:metric>{"metrics": [{"label": "Score", "value": 95}]}</ui:metric> text after`

	blocks := parser.ExtractUIBlocks(content)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	if blocks[0].Type != PartTypeMetric {
		t.Errorf("expected metric type, got %s", blocks[0].Type)
	}
}

func TestUIOutputParser_CleanContent(t *testing.T) {
	parser := NewUIOutputParser()

	content := `Before
<ui:table>{"headers": [], "rows": []}</ui:table>
After`

	result := parser.Parse(content)

	// Clean content should have Before and After but not the UI block
	if parser.HasUIBlocks(result.CleanContent) {
		t.Error("clean content should not contain UI blocks")
	}

	if result.CleanContent == "" {
		t.Error("clean content should not be empty")
	}
}

func TestConvertToStructuredResponse(t *testing.T) {
	parser := NewUIOutputParser()

	content := `<ui:table>{"title": "Test", "headers": [], "rows": []}</ui:table>`

	result := parser.Parse(content)
	response := ConvertToStructuredResponse(result)

	if response == nil {
		t.Fatal("expected structured response")
	}

	if response.ID == "" {
		t.Error("expected response ID")
	}

	if len(response.Parts) == 0 {
		t.Error("expected parts in response")
	}
}
