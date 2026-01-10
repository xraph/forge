package sdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentPartTypes(t *testing.T) {
	t.Run("TextPart", func(t *testing.T) {
		part := &TextPart{
			Text:   "Hello, world!",
			Format: TextFormatPlain,
		}

		if part.Type() != PartTypeText {
			t.Errorf("expected type %s, got %s", PartTypeText, part.Type())
		}

		data, err := part.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		if !strings.Contains(string(data), `"text"`) {
			t.Error("JSON should contain type field")
		}
	})

	t.Run("CodePart", func(t *testing.T) {
		part := &CodePart{
			Code:     "func main() {}",
			Language: "go",
			Copyable: true,
		}

		if part.Type() != PartTypeCode {
			t.Errorf("expected type %s, got %s", PartTypeCode, part.Type())
		}

		data, err := part.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		if !strings.Contains(string(data), `"code"`) {
			t.Error("JSON should contain code type")
		}
	})

	t.Run("TablePart", func(t *testing.T) {
		part := &TablePart{
			Headers: []TableHeader{
				{Label: "Name", Key: "name"},
				{Label: "Age", Key: "age"},
			},
			Rows: [][]TableCell{
				{{Value: "Alice", Display: "Alice"}, {Value: 30, Display: "30"}},
				{{Value: "Bob", Display: "Bob"}, {Value: 25, Display: "25"}},
			},
		}

		if part.Type() != PartTypeTable {
			t.Errorf("expected type %s, got %s", PartTypeTable, part.Type())
		}
	})

	t.Run("CardPart", func(t *testing.T) {
		part := &CardPart{
			Title:       "My Card",
			Description: "A test card",
			Actions: []CardAction{
				{Label: "Click me", Action: "click"},
			},
		}

		if part.Type() != PartTypeCard {
			t.Errorf("expected type %s, got %s", PartTypeCard, part.Type())
		}
	})

	t.Run("ListPart", func(t *testing.T) {
		part := &ListPart{
			Items: []ListItem{
				{Text: "Item 1"},
				{Text: "Item 2"},
			},
			Ordered: true,
		}

		if part.Type() != PartTypeList {
			t.Errorf("expected type %s, got %s", PartTypeList, part.Type())
		}
	})

	t.Run("AlertPart", func(t *testing.T) {
		part := &AlertPart{
			Message:  "Warning!",
			Severity: AlertWarning,
		}

		if part.Type() != PartTypeAlert {
			t.Errorf("expected type %s, got %s", PartTypeAlert, part.Type())
		}
	})

	t.Run("ThinkingPart", func(t *testing.T) {
		part := &ThinkingPart{
			Content: "Let me think about this...",
		}

		if part.Type() != PartTypeThinking {
			t.Errorf("expected type %s, got %s", PartTypeThinking, part.Type())
		}
	})
}

func TestResponseBuilder(t *testing.T) {
	t.Run("build simple response", func(t *testing.T) {
		builder := NewResponseBuilder()
		response := builder.
			AddText("Hello").
			AddCode("print('hi')", "python").
			AddDivider().
			Build()

		if len(response.Parts) != 3 {
			t.Errorf("expected 3 parts, got %d", len(response.Parts))
		}

		if response.ID == "" {
			t.Error("response should have an ID")
		}
	})

	t.Run("build response with table", func(t *testing.T) {
		builder := NewResponseBuilder()
		response := builder.
			AddTable(
				[]string{"Name", "Age"},
				[][]any{{"Alice", 30}, {"Bob", 25}},
			).
			Build()

		if len(response.Parts) != 1 {
			t.Errorf("expected 1 part, got %d", len(response.Parts))
		}

		tablePart, ok := response.Parts[0].(*TablePart)
		if !ok {
			t.Fatal("expected TablePart")
		}

		if len(tablePart.Headers) != 2 {
			t.Errorf("expected 2 headers, got %d", len(tablePart.Headers))
		}

		if len(tablePart.Rows) != 2 {
			t.Errorf("expected 2 rows, got %d", len(tablePart.Rows))
		}
	})

	t.Run("build response with list", func(t *testing.T) {
		builder := NewResponseBuilder()
		response := builder.
			AddList([]string{"First", "Second", "Third"}, true).
			Build()

		listPart, ok := response.Parts[0].(*ListPart)
		if !ok {
			t.Fatal("expected ListPart")
		}

		if !listPart.Ordered {
			t.Error("list should be ordered")
		}

		if len(listPart.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(listPart.Items))
		}
	})

	t.Run("build response with metadata", func(t *testing.T) {
		builder := NewResponseBuilder()
		response := builder.
			WithMetadata("gpt-4", "openai", 1000).
			WithTokenUsage(100, 50).
			AddText("Test").
			Build()

		if response.Metadata.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %s", response.Metadata.Model)
		}

		if response.Metadata.Provider != "openai" {
			t.Errorf("expected provider openai, got %s", response.Metadata.Provider)
		}

		if response.Metadata.InputTokens != 100 {
			t.Errorf("expected 100 input tokens, got %d", response.Metadata.InputTokens)
		}
	})
}

func TestResponseParser(t *testing.T) {
	parser := NewResponseParser()

	t.Run("parse plain text", func(t *testing.T) {
		parts := parser.Parse("Hello, world!")

		if len(parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(parts))
		}

		textPart, ok := parts[0].(*TextPart)
		if !ok {
			t.Fatal("expected TextPart")
		}

		if textPart.Text != "Hello, world!" {
			t.Errorf("expected 'Hello, world!', got '%s'", textPart.Text)
		}
	})

	t.Run("parse code block", func(t *testing.T) {
		content := "Here's some code:\n\n```python\nprint('hello')\n```\n\nThat's it."
		parts := parser.Parse(content)

		if len(parts) < 2 {
			t.Fatalf("expected at least 2 parts, got %d", len(parts))
		}

		// Find the code part
		var codePart *CodePart

		for _, part := range parts {
			if cp, ok := part.(*CodePart); ok {
				codePart = cp

				break
			}
		}

		if codePart == nil {
			t.Fatal("expected to find a CodePart")
		}

		if codePart.Language != "python" {
			t.Errorf("expected language 'python', got '%s'", codePart.Language)
		}

		if !strings.Contains(codePart.Code, "print") {
			t.Error("code should contain 'print'")
		}
	})

	t.Run("parse thinking block", func(t *testing.T) {
		content := "<thinking>Let me think about this...</thinking>\n\nHere's my answer."
		parts := parser.Parse(content)

		// Find the thinking part
		var thinkingPart *ThinkingPart

		for _, part := range parts {
			if tp, ok := part.(*ThinkingPart); ok {
				thinkingPart = tp

				break
			}
		}

		if thinkingPart == nil {
			t.Fatal("expected to find a ThinkingPart")
		}

		if !strings.Contains(thinkingPart.Content, "think about") {
			t.Error("thinking content should contain 'think about'")
		}
	})

	t.Run("parse markdown table", func(t *testing.T) {
		content := `Here's a table:

| Name | Age |
|------|-----|
| Alice | 30 |
| Bob | 25 |

Done.`
		parts := parser.Parse(content)

		// Find the table part
		var tablePart *TablePart

		for _, part := range parts {
			if tp, ok := part.(*TablePart); ok {
				tablePart = tp

				break
			}
		}

		if tablePart == nil {
			t.Fatal("expected to find a TablePart")
		}

		if len(tablePart.Headers) != 2 {
			t.Errorf("expected 2 headers, got %d", len(tablePart.Headers))
		}

		if len(tablePart.Rows) != 2 {
			t.Errorf("expected 2 rows, got %d", len(tablePart.Rows))
		}
	})
}

func TestStructuredResponseSerialization(t *testing.T) {
	response := NewResponseBuilder().
		WithID("test-123").
		AddText("Hello").
		AddCode("print('hi')", "python").
		Build()

	data, err := response.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if decoded["id"] != "test-123" {
		t.Errorf("expected id 'test-123', got %v", decoded["id"])
	}

	parts, ok := decoded["parts"].([]any)
	if !ok {
		t.Fatal("parts should be an array")
	}

	if len(parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(parts))
	}
}

func TestStructuredResponseToPlainText(t *testing.T) {
	response := NewResponseBuilder().
		AddText("Hello, world!").
		AddCode("print('hi')", "python").
		AddList([]string{"Item 1", "Item 2"}, true).
		AddAlert("Warning!", AlertWarning).
		Build()

	plainText := response.ToPlainText()

	if !strings.Contains(plainText, "Hello, world!") {
		t.Error("plain text should contain text content")
	}

	if !strings.Contains(plainText, "```python") {
		t.Error("plain text should contain code block")
	}

	if !strings.Contains(plainText, "1. Item 1") {
		t.Error("plain text should contain numbered list")
	}

	if !strings.Contains(plainText, "[warning]") {
		t.Error("plain text should contain alert")
	}
}
