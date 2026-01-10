package sdk

import (
	"context"
	"testing"
)

func TestSuggestionBuilder(t *testing.T) {
	t.Run("build prompt suggestion", func(t *testing.T) {
		suggestion := NewSuggestion("Tell me more", SuggestionTypePrompt).
			WithDescription("Get more details").
			WithAction("Tell me more about this topic").
			WithPriority(8).
			WithConfidence(0.9).
			Primary().
			Build()

		if suggestion.Label != "Tell me more" {
			t.Errorf("expected label 'Tell me more', got '%s'", suggestion.Label)
		}

		if suggestion.Type != SuggestionTypePrompt {
			t.Errorf("expected type prompt, got %s", suggestion.Type)
		}

		if suggestion.Priority != 8 {
			t.Errorf("expected priority 8, got %d", suggestion.Priority)
		}

		if suggestion.Confidence != 0.9 {
			t.Errorf("expected confidence 0.9, got %f", suggestion.Confidence)
		}

		if !suggestion.Primary {
			t.Error("suggestion should be primary")
		}
	})

	t.Run("build action suggestion", func(t *testing.T) {
		suggestion := NewSuggestion("Run Code", SuggestionTypeAction).
			WithAction("run_code").
			WithPayloadValue("code_id", "123").
			WithIcon("play").
			Build()

		if suggestion.Type != SuggestionTypeAction {
			t.Errorf("expected type action, got %s", suggestion.Type)
		}

		if suggestion.Payload["code_id"] != "123" {
			t.Error("payload should contain code_id")
		}

		if suggestion.Icon != "play" {
			t.Errorf("expected icon 'play', got '%s'", suggestion.Icon)
		}
	})

	t.Run("build link suggestion", func(t *testing.T) {
		suggestion := NewSuggestion("Learn More", SuggestionTypeLink).
			WithAction("https://example.com").
			WithCategory("external").
			WithTags("documentation", "help").
			Build()

		if suggestion.Type != SuggestionTypeLink {
			t.Errorf("expected type link, got %s", suggestion.Type)
		}

		if suggestion.Category != "external" {
			t.Errorf("expected category 'external', got '%s'", suggestion.Category)
		}

		if len(suggestion.Tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(suggestion.Tags))
		}
	})

	t.Run("priority bounds", func(t *testing.T) {
		low := NewSuggestion("Low", SuggestionTypePrompt).
			WithPriority(-5).
			Build()

		if low.Priority != 1 {
			t.Errorf("priority should be clamped to 1, got %d", low.Priority)
		}

		high := NewSuggestion("High", SuggestionTypePrompt).
			WithPriority(100).
			Build()

		if high.Priority != 10 {
			t.Errorf("priority should be clamped to 10, got %d", high.Priority)
		}
	})

	t.Run("confidence bounds", func(t *testing.T) {
		low := NewSuggestion("Low", SuggestionTypePrompt).
			WithConfidence(-0.5).
			Build()

		if low.Confidence != 0 {
			t.Errorf("confidence should be clamped to 0, got %f", low.Confidence)
		}

		high := NewSuggestion("High", SuggestionTypePrompt).
			WithConfidence(1.5).
			Build()

		if high.Confidence != 1 {
			t.Errorf("confidence should be clamped to 1, got %f", high.Confidence)
		}
	})
}

func TestFollowUpGenerator(t *testing.T) {
	generator := NewFollowUpGenerator().
		WithMaxSuggestions(3)

	t.Run("generate from topics", func(t *testing.T) {
		input := SuggestionInput{
			Content: "Go is a programming language developed by Google.",
			Query:   "What is Go?",
			Topics:  []string{"Go", "Google", "programming"},
		}

		suggestions := generator.Generate(context.Background(), input)

		if len(suggestions) > 3 {
			t.Errorf("should respect max suggestions, got %d", len(suggestions))
		}

		// Should generate topic expansion suggestions
		found := false

		for _, s := range suggestions {
			if s.Type == SuggestionTypeExpand {
				found = true

				break
			}
		}

		if !found {
			t.Error("should generate expand suggestions for topics")
		}
	})

	t.Run("generate from entities", func(t *testing.T) {
		input := SuggestionInput{
			Content:  "The function processData handles data transformation.",
			Query:    "How does processData work?",
			Entities: []string{"processData", "transformation"},
		}

		suggestions := generator.Generate(context.Background(), input)

		// Should generate related suggestions for entities
		hasRelated := false

		for _, s := range suggestions {
			if s.Type == SuggestionTypeRelated {
				hasRelated = true

				break
			}
		}

		if !hasRelated {
			t.Error("should generate related suggestions for entities")
		}
	})

	t.Run("add custom template", func(t *testing.T) {
		customGen := NewFollowUpGenerator().
			WithMaxSuggestions(10). // Increase max to ensure custom template is included
			AddTemplate(FollowUpTemplate{
				Pattern:     "custom",
				Label:       "Custom follow-up",
				Description: "A custom suggestion",
				Priority:    10,
			})

		input := SuggestionInput{
			Content: "Some content",
		}

		suggestions := customGen.Generate(context.Background(), input)

		// Should include custom template
		found := false

		for _, s := range suggestions {
			if s.Label == "Custom follow-up" {
				found = true

				break
			}
		}

		if !found {
			t.Error("should include custom template suggestion")
		}
	})
}

func TestActionSuggestionGenerator(t *testing.T) {
	generator := NewActionSuggestionGenerator()

	t.Run("generate for code content", func(t *testing.T) {
		input := SuggestionInput{
			Content: "Here's the code:\n```python\nprint('hello')\n```",
		}

		suggestions := generator.Generate(context.Background(), input)

		// Should suggest running code
		foundRunCode := false

		for _, s := range suggestions {
			if s.Action == "run_code" {
				foundRunCode = true

				break
			}
		}

		if !foundRunCode {
			t.Error("should suggest running code for content with code blocks")
		}
	})

	t.Run("generate for data content", func(t *testing.T) {
		input := SuggestionInput{
			Content: "Here's the data: {\"name\": \"test\", \"value\": 123}",
		}

		suggestions := generator.Generate(context.Background(), input)

		// Should suggest visualizing
		foundVisualize := false

		for _, s := range suggestions {
			if s.Action == "visualize" {
				foundVisualize = true

				break
			}
		}

		if !foundVisualize {
			t.Error("should suggest visualizing for content with data")
		}
	})

	t.Run("register custom action", func(t *testing.T) {
		generator.RegisterAction(ActionDefinition{
			ID:          "custom_action",
			Label:       "Custom Action",
			Description: "Do something custom",
			Handler:     "custom",
			Priority:    10,
		})

		input := SuggestionInput{Content: "Any content"}
		suggestions := generator.Generate(context.Background(), input)

		found := false

		for _, s := range suggestions {
			if s.Action == "custom" {
				found = true

				break
			}
		}

		if !found {
			t.Error("should include registered custom action")
		}
	})
}

func TestSuggestionManager(t *testing.T) {
	t.Run("combine generators", func(t *testing.T) {
		manager := NewSuggestionManager().
			AddGenerator(NewFollowUpGenerator()).
			AddGenerator(NewActionSuggestionGenerator()).
			WithMaxSuggestions(5)

		input := SuggestionInput{
			Content: "Here's some code:\n```go\nfmt.Println(\"hello\")\n```",
			Query:   "Show me Go code",
			Topics:  []string{"Go", "code"},
		}

		suggestions := manager.GenerateSuggestions(context.Background(), input)

		if len(suggestions) > 5 {
			t.Errorf("should respect max suggestions, got %d", len(suggestions))
		}

		// Should have both follow-up and action suggestions
		hasFollowUp := false
		hasAction := false

		for _, s := range suggestions {
			switch s.Type {
			case SuggestionTypeExpand, SuggestionTypeRelated, SuggestionTypePrompt:
				hasFollowUp = true
			case SuggestionTypeAction:
				hasAction = true
			}
		}

		if !hasFollowUp {
			t.Error("should include follow-up suggestions")
		}

		if !hasAction {
			t.Error("should include action suggestions")
		}
	})

	t.Run("sort by priority", func(t *testing.T) {
		manager := DefaultSuggestionManager().
			WithMaxSuggestions(10)

		input := SuggestionInput{
			Content: "```python\nprint('test')\n```",
			Topics:  []string{"Python", "testing"},
		}

		suggestions := manager.GenerateSuggestions(context.Background(), input)

		// Verify suggestions are sorted by priority (descending)
		for i := 1; i < len(suggestions); i++ {
			if suggestions[i].Priority > suggestions[i-1].Priority {
				t.Error("suggestions should be sorted by priority (descending)")

				break
			}
		}
	})
}

func TestQuickSuggestionHelpers(t *testing.T) {
	t.Run("NewPromptSuggestion", func(t *testing.T) {
		suggestion := NewPromptSuggestion("Tell me more")

		if suggestion.Type != SuggestionTypePrompt {
			t.Errorf("expected type prompt, got %s", suggestion.Type)
		}

		if suggestion.Label != "Tell me more" {
			t.Errorf("expected label 'Tell me more', got '%s'", suggestion.Label)
		}
	})

	t.Run("NewActionSuggestion", func(t *testing.T) {
		suggestion := NewActionSuggestion("Run", "execute")

		if suggestion.Type != SuggestionTypeAction {
			t.Errorf("expected type action, got %s", suggestion.Type)
		}

		if suggestion.Action != "execute" {
			t.Errorf("expected action 'execute', got '%s'", suggestion.Action)
		}
	})

	t.Run("NewLinkSuggestion", func(t *testing.T) {
		suggestion := NewLinkSuggestion("Visit", "https://example.com")

		if suggestion.Type != SuggestionTypeLink {
			t.Errorf("expected type link, got %s", suggestion.Type)
		}

		if suggestion.Action != "https://example.com" {
			t.Errorf("expected URL, got '%s'", suggestion.Action)
		}
	})

	t.Run("NewCopySuggestion", func(t *testing.T) {
		suggestion := NewCopySuggestion("content to copy")

		if suggestion.Type != SuggestionTypeCopy {
			t.Errorf("expected type copy, got %s", suggestion.Type)
		}

		if suggestion.Payload["content"] != "content to copy" {
			t.Error("payload should contain content")
		}
	})

	t.Run("NewExportSuggestion", func(t *testing.T) {
		suggestion := NewExportSuggestion("pdf")

		if suggestion.Type != SuggestionTypeExport {
			t.Errorf("expected type export, got %s", suggestion.Type)
		}

		if suggestion.Payload["format"] != "pdf" {
			t.Error("payload should contain format")
		}
	})
}
