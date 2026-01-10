package sdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

func TestUIPartStreamer_BasicFlow(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeTable,
		ExecutionID: "test-exec-1",
		OnEvent:     onEvent,
		Context:     context.Background(),
	})

	// Start streaming
	if err := streamer.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stream sections
	if err := streamer.StreamHeader([]TableHeader{
		{Label: "Name", Key: "name"},
		{Label: "Value", Key: "value"},
	}); err != nil {
		t.Fatalf("StreamHeader failed: %v", err)
	}

	if err := streamer.StreamRows([][]TableCell{
		{{Value: "Row1", Display: "Row1"}, {Value: 100, Display: "100"}},
	}); err != nil {
		t.Fatalf("StreamRows failed: %v", err)
	}

	if err := streamer.StreamFooter(map[string]any{"total": 100}); err != nil {
		t.Fatalf("StreamFooter failed: %v", err)
	}

	// End streaming
	if err := streamer.End(); err != nil {
		t.Fatalf("End failed: %v", err)
	}

	// Verify events
	mu.Lock()
	defer mu.Unlock()

	if len(events) < 4 {
		t.Fatalf("Expected at least 4 events, got %d", len(events))
	}

	// First event should be start
	if events[0].Type != llm.EventUIPartStart {
		t.Errorf("Expected first event to be ui_part_start, got %s", events[0].Type)
	}

	// Last event should be end
	if events[len(events)-1].Type != llm.EventUIPartEnd {
		t.Errorf("Expected last event to be ui_part_end, got %s", events[len(events)-1].Type)
	}

	// Check part type
	if events[0].PartType != string(PartTypeTable) {
		t.Errorf("Expected part type table, got %s", events[0].PartType)
	}
}

func TestUIPartStreamer_AutoStart(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeMetric,
		ExecutionID: "test-exec-2",
		OnEvent:     onEvent,
		Context:     context.Background(),
	})

	// Stream without explicit start - should auto-start
	if err := streamer.StreamSection("metrics", Metric{
		Label: "Test",
		Value: 42,
	}); err != nil {
		t.Fatalf("StreamSection failed: %v", err)
	}

	if err := streamer.End(); err != nil {
		t.Fatalf("End failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have start, delta, and end
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	if events[0].Type != llm.EventUIPartStart {
		t.Errorf("Expected auto-start event")
	}
}

func TestUIPartStreamer_DataAccumulation(t *testing.T) {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeTable,
		ExecutionID: "test-exec-3",
		Context:     context.Background(),
	})

	_ = streamer.Start()

	// Stream multiple row batches
	_ = streamer.StreamRows([]int{1, 2, 3})
	_ = streamer.StreamRows([]int{4, 5, 6})
	_ = streamer.StreamRows([]int{7, 8, 9})

	_ = streamer.End()

	data := streamer.GetAccumulatedData()

	rows, ok := data["rows"].([]any)
	if !ok {
		t.Fatal("Expected rows to be accumulated")
	}

	if len(rows) != 3 {
		t.Errorf("Expected 3 row batches, got %d", len(rows))
	}
}

func TestUIPartStreamer_ErrorHandling(t *testing.T) {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeChart,
		ExecutionID: "test-exec-4",
		Context:     context.Background(),
	})

	// End before start should fail
	if err := streamer.End(); err == nil {
		t.Error("Expected error when ending before start")
	}

	// Start
	_ = streamer.Start()

	// Double start should fail
	if err := streamer.Start(); err == nil {
		t.Error("Expected error on double start")
	}

	// End
	_ = streamer.End()

	// Double end should fail
	if err := streamer.End(); err == nil {
		t.Error("Expected error on double end")
	}

	// Stream after end should fail
	if err := streamer.StreamContent("test"); err == nil {
		t.Error("Expected error when streaming after end")
	}
}

func TestUIPartStreamer_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeForm,
		ExecutionID: "test-exec-5",
		Context:     ctx,
	})

	_ = streamer.Start()

	// Cancel context
	cancel()

	// Stream should fail due to cancelled context
	if err := streamer.StreamContent("test"); err == nil {
		t.Error("Expected error due to cancelled context")
	}
}

func TestUIPartStreamManager(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	manager := NewUIPartStreamManager(onEvent, nil, nil)

	// Create multiple streamers
	streamer1 := manager.CreateStreamer(context.Background(), "exec-1", PartTypeTable, nil)
	streamer2 := manager.CreateStreamer(context.Background(), "exec-1", PartTypeChart, nil)

	// Stream on both
	_ = streamer1.Start()
	_ = streamer1.StreamHeader([]string{"col1", "col2"})

	_ = streamer2.Start()
	_ = streamer2.StreamContent(map[string]any{"type": "line"})

	// Get all streamers
	streamers := manager.GetAllStreamers()
	if len(streamers) != 2 {
		t.Errorf("Expected 2 streamers, got %d", len(streamers))
	}

	// End all
	manager.EndAll()

	// Verify both ended
	if !streamer1.IsCompleted() || !streamer2.IsCompleted() {
		t.Error("Expected all streamers to be completed")
	}
}

func TestUIPartStreamer_SectionConvenience(t *testing.T) {
	var (
		sections []string
		mu       sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		if event.Type == llm.EventUIPartDelta {
			sections = append(sections, event.Section)
		}

		mu.Unlock()

		return nil
	}

	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeKanban,
		ExecutionID: "test-exec-6",
		OnEvent:     onEvent,
		Context:     context.Background(),
	})

	_ = streamer.Start()
	_ = streamer.StreamHeader("Header")
	_ = streamer.StreamContent("Content")
	_ = streamer.StreamColumns([]string{"col1", "col2"})
	_ = streamer.StreamItems([]string{"item1", "item2"})
	_ = streamer.StreamActions([]string{"action1"})
	_ = streamer.StreamFooter("Footer")
	_ = streamer.StreamMetadata(map[string]any{"key": "value"})
	_ = streamer.End()

	mu.Lock()
	defer mu.Unlock()

	expectedSections := []string{"header", "content", "columns", "items", "actions", "footer", "metadata"}
	if len(sections) != len(expectedSections) {
		t.Errorf("Expected %d sections, got %d", len(expectedSections), len(sections))
	}

	for i, expected := range expectedSections {
		if i < len(sections) && sections[i] != expected {
			t.Errorf("Section %d: expected %s, got %s", i, expected, sections[i])
		}
	}
}

func TestStreamTable(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	headers := []TableHeader{
		{Label: "ID", Key: "id"},
		{Label: "Name", Key: "name"},
	}

	rows := [][]TableCell{
		{{Value: 1, Display: "1"}, {Value: "Alice", Display: "Alice"}},
		{{Value: 2, Display: "2"}, {Value: "Bob", Display: "Bob"}},
	}

	err := StreamTable(context.Background(), "exec-1", "Test Table", headers, rows, onEvent)
	if err != nil {
		t.Fatalf("StreamTable failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have: start, title, header, rows, end
	if len(events) < 4 {
		t.Errorf("Expected at least 4 events, got %d", len(events))
	}

	// First should be start
	if events[0].Type != llm.EventUIPartStart {
		t.Errorf("Expected start event, got %s", events[0].Type)
	}

	// Last should be end
	if events[len(events)-1].Type != llm.EventUIPartEnd {
		t.Errorf("Expected end event, got %s", events[len(events)-1].Type)
	}
}

func TestStreamMetrics(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	metrics := []Metric{
		{Label: "Users", Value: 1000},
		{Label: "Revenue", Value: 50000, Unit: "$"},
		{Label: "Growth", Value: 15.5, Unit: "%"},
	}

	err := StreamMetrics(context.Background(), "exec-1", "Dashboard", metrics, onEvent)
	if err != nil {
		t.Fatalf("StreamMetrics failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have: start, title, 3 metrics, end = 6 events
	if len(events) != 6 {
		t.Errorf("Expected 6 events, got %d", len(events))
	}
}

func TestStreamTimeline(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	timelineEvents := []TimelineEvent{
		{ID: "1", Title: "Event 1", Timestamp: time.Now()},
		{ID: "2", Title: "Event 2", Timestamp: time.Now().Add(time.Hour)},
	}

	err := StreamTimeline(context.Background(), "exec-1", "History", timelineEvents, onEvent)
	if err != nil {
		t.Fatalf("StreamTimeline failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have: start, title, 2 events, end = 5 events
	if len(events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events))
	}
}

func TestStreamButtons(t *testing.T) {
	var (
		events []llm.ClientStreamEvent
		mu     sync.Mutex
	)

	onEvent := func(event llm.ClientStreamEvent) error {
		mu.Lock()

		events = append(events, event)

		mu.Unlock()

		return nil
	}

	buttons := []Button{
		NewButton("btn1", "Save").WithVariant(ButtonPrimary).Build(),
		NewButton("btn2", "Cancel").WithVariant(ButtonSecondary).Build(),
	}

	err := StreamButtons(context.Background(), "exec-1", "Actions", buttons, onEvent)
	if err != nil {
		t.Fatalf("StreamButtons failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have: start, title, buttons, end = 4 events
	if len(events) != 4 {
		t.Errorf("Expected 4 events, got %d", len(events))
	}
}

func TestUIPartStreamer_BuildFinalPart(t *testing.T) {
	tests := []struct {
		name     string
		partType ContentPartType
		sections map[string]any
		wantType ContentPartType
	}{
		{
			name:     "Table",
			partType: PartTypeTable,
			sections: map[string]any{
				"title":  "Test Table",
				"header": []TableHeader{{Label: "Col1", Key: "col1"}},
				"rows":   []any{[]TableCell{{Value: "test"}}},
			},
			wantType: PartTypeTable,
		},
		{
			name:     "Card",
			partType: PartTypeCard,
			sections: map[string]any{
				"title":       "Test Card",
				"description": "Description",
			},
			wantType: PartTypeCard,
		},
		{
			name:     "Metric",
			partType: PartTypeMetric,
			sections: map[string]any{
				"title":   "Dashboard",
				"metrics": []any{Metric{Label: "Test", Value: 100}},
			},
			wantType: PartTypeMetric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := NewUIPartStreamer(UIPartStreamerConfig{
				PartType:    tt.partType,
				ExecutionID: "test-exec",
				Context:     context.Background(),
			})

			_ = streamer.Start()
			for section, data := range tt.sections {
				_ = streamer.StreamSection(section, data)
			}

			_ = streamer.End()

			part, err := streamer.BuildFinalPart()
			if err != nil {
				t.Fatalf("BuildFinalPart failed: %v", err)
			}

			if part.Type() != tt.wantType {
				t.Errorf("Expected part type %s, got %s", tt.wantType, part.Type())
			}
		})
	}
}

func TestButtonBuilder(t *testing.T) {
	btn := NewButton("test-btn", "Click Me").
		WithIcon("plus").
		WithVariant(ButtonPrimary).
		WithSize("lg").
		WithTooltip("Click to add").
		WithToolAction("add_item", map[string]any{"type": "new"}).
		WithConfirm("Confirm", "Are you sure?").
		Build()

	if btn.ID != "test-btn" {
		t.Errorf("Expected ID test-btn, got %s", btn.ID)
	}

	if btn.Label != "Click Me" {
		t.Errorf("Expected Label Click Me, got %s", btn.Label)
	}

	if btn.Icon != "plus" {
		t.Errorf("Expected Icon plus, got %s", btn.Icon)
	}

	if btn.Variant != ButtonPrimary {
		t.Errorf("Expected Variant primary, got %s", btn.Variant)
	}

	if btn.Action.Type != ActionTypeTool {
		t.Errorf("Expected Action Type tool, got %s", btn.Action.Type)
	}

	if btn.Action.Confirm == nil {
		t.Error("Expected Confirm to be set")
	}
}

func TestUIPartTypes_JSON(t *testing.T) {
	tests := []ContentPart{
		NewButtonGroupPart(
			NewButton("btn1", "Button 1").Build(),
		),
		NewTimelinePart(
			TimelineEvent{ID: "e1", Title: "Event 1", Timestamp: time.Now()},
		),
		NewKanbanPart(
			KanbanColumn{ID: "col1", Title: "To Do"},
		),
		NewMetricPart(
			Metric{Label: "Test", Value: 100},
		),
		NewFormPart("form1",
			FormField{ID: "field1", Name: "name", Label: "Name", Type: FieldTypeText},
		),
		NewTabsPart(
			Tab{ID: "tab1", Label: "Tab 1"},
		),
		NewAccordionPart(
			AccordionItem{ID: "item1", Title: "Item 1"},
		),
		NewInlineCitationPart("Some text [1]",
			InlineCitation{ID: "cite1", Number: 1, Position: 10, Length: 3},
		),
		NewStatsPart(
			Stat{Label: "Users", Value: 1000},
		),
		NewCarouselPart(
			CarouselItem{ID: "slide1", Title: "Slide 1"},
		),
		NewGalleryPart(
			GalleryItem{ID: "img1", URL: "http://example.com/img.jpg"},
		),
	}

	for _, part := range tests {
		t.Run(string(part.Type()), func(t *testing.T) {
			data, err := part.ToJSON()
			if err != nil {
				t.Fatalf("ToJSON failed: %v", err)
			}

			// Verify it's valid JSON
			var result map[string]any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Invalid JSON output: %v", err)
			}

			// Verify type field
			if result["type"] != string(part.Type()) {
				t.Errorf("Expected type %s, got %v", part.Type(), result["type"])
			}
		})
	}
}

func TestUIPartEvents_Constructors(t *testing.T) {
	// Test NewUIPartStartEvent
	startEvent := llm.NewUIPartStartEvent("exec-1", "part-1", "table")
	if startEvent.Type != llm.EventUIPartStart {
		t.Errorf("Expected type ui_part_start, got %s", startEvent.Type)
	}

	if startEvent.PartID != "part-1" {
		t.Errorf("Expected PartID part-1, got %s", startEvent.PartID)
	}

	if startEvent.PartType != "table" {
		t.Errorf("Expected PartType table, got %s", startEvent.PartType)
	}

	// Test NewUIPartDeltaEvent
	deltaEvent := llm.NewUIPartDeltaEvent("exec-1", "part-1", "rows", []int{1, 2, 3}, 5)
	if deltaEvent.Type != llm.EventUIPartDelta {
		t.Errorf("Expected type ui_part_delta, got %s", deltaEvent.Type)
	}

	if deltaEvent.Section != "rows" {
		t.Errorf("Expected Section rows, got %s", deltaEvent.Section)
	}

	if deltaEvent.Index != 5 {
		t.Errorf("Expected Index 5, got %d", deltaEvent.Index)
	}

	// Test NewUIPartEndEvent
	endEvent := llm.NewUIPartEndEvent("exec-1", "part-1")
	if endEvent.Type != llm.EventUIPartEnd {
		t.Errorf("Expected type ui_part_end, got %s", endEvent.Type)
	}
}
