package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// UIPartSection represents a streamable section of a UI part.
type UIPartSection struct {
	Name   string `json:"name"`   // header, content, rows, actions, footer, etc.
	Data   any    `json:"data"`   // section-specific data
	Status string `json:"status"` // pending, streaming, complete
}

// UIPartSectionStatus represents the status of a section.
type UIPartSectionStatus string

const (
	SectionStatusPending   UIPartSectionStatus = "pending"
	SectionStatusStreaming UIPartSectionStatus = "streaming"
	SectionStatusComplete  UIPartSectionStatus = "complete"
	SectionStatusError     UIPartSectionStatus = "error"
)

// UIPartStreamer handles progressive streaming of UI components.
// It enables streaming different sections of a UI component (header, content, footer, etc.)
// independently, allowing frontends to render progressively as data arrives.
type UIPartStreamer struct {
	// partID uniquely identifies this UI part
	partID string

	// partType is the type of UI component being streamed
	partType ContentPartType

	// executionID links this to the parent stream session
	executionID string

	// sections tracks all streamable sections
	sections []UIPartSection

	// sectionIndex is a monotonic counter for section deltas
	sectionIndex int64

	// mu protects concurrent access
	mu sync.RWMutex

	// started tracks if streaming has begun
	started bool

	// completed tracks if streaming is done
	completed bool

	// onEvent is called for each streaming event
	onEvent func(llm.ClientStreamEvent) error

	// logger for debug output
	logger forge.Logger

	// metrics for observability
	metrics forge.Metrics

	// ctx for cancellation
	ctx context.Context

	// metadata stores additional part metadata
	metadata map[string]any

	// accumulator collects streamed data for final ContentPart
	accumulator map[string]any

	// finalPart stores the completed ContentPart
	finalPart ContentPart
}

// UIPartStreamerConfig holds configuration for creating a UIPartStreamer.
type UIPartStreamerConfig struct {
	PartType    ContentPartType
	ExecutionID string
	OnEvent     func(llm.ClientStreamEvent) error
	Logger      forge.Logger
	Metrics     forge.Metrics
	Context     context.Context
	Metadata    map[string]any
}

// NewUIPartStreamer creates a new UIPartStreamer for progressive component rendering.
func NewUIPartStreamer(config UIPartStreamerConfig) *UIPartStreamer {
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	partID := "part_" + uuid.New().String()[:8]

	return &UIPartStreamer{
		partID:      partID,
		partType:    config.PartType,
		executionID: config.ExecutionID,
		sections:    make([]UIPartSection, 0),
		onEvent:     config.OnEvent,
		logger:      config.Logger,
		metrics:     config.Metrics,
		ctx:         ctx,
		metadata:    config.Metadata,
		accumulator: make(map[string]any),
	}
}

// GetPartID returns the unique identifier for this UI part.
func (s *UIPartStreamer) GetPartID() string {
	return s.partID
}

// GetPartType returns the UI part type.
func (s *UIPartStreamer) GetPartType() ContentPartType {
	return s.partType
}

// GetExecutionID returns the parent execution ID.
func (s *UIPartStreamer) GetExecutionID() string {
	return s.executionID
}

// IsStarted returns whether streaming has begun.
func (s *UIPartStreamer) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.started
}

// IsCompleted returns whether streaming is finished.
func (s *UIPartStreamer) IsCompleted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.completed
}

// Start begins streaming the UI part. This sends the ui_part_start event.
func (s *UIPartStreamer) Start() error {
	s.mu.Lock()

	if s.started {
		s.mu.Unlock()

		return errors.New("UI part streamer already started")
	}

	s.started = true
	s.mu.Unlock()

	// Check context
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	// Send start event
	event := llm.NewUIPartStartEvent(s.executionID, s.partID, string(s.partType))
	if s.metadata != nil {
		event = event.WithPartData(s.metadata)
	}

	if err := s.sendEvent(event); err != nil {
		return fmt.Errorf("failed to send ui_part_start: %w", err)
	}

	if s.logger != nil {
		s.logger.Debug("UI part streaming started",
			F("part_id", s.partID),
			F("part_type", s.partType),
			F("execution_id", s.executionID),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.sdk.ui_stream.started",
			"part_type", string(s.partType),
		).Inc()
	}

	return nil
}

// StreamSection progressively streams a section of the UI part.
// Sections can be streamed in any order, and the same section can be
// streamed multiple times (e.g., streaming table rows in batches).
func (s *UIPartStreamer) StreamSection(section string, data any) error {
	s.mu.Lock()

	if !s.started {
		s.mu.Unlock()
		// Auto-start if not started
		if err := s.Start(); err != nil {
			return err
		}

		s.mu.Lock()
	}

	if s.completed {
		s.mu.Unlock()

		return errors.New("UI part streamer already completed")
	}

	// Track section
	s.sections = append(s.sections, UIPartSection{
		Name:   section,
		Data:   data,
		Status: string(SectionStatusComplete),
	})

	// Accumulate data for final part
	s.accumulateSection(section, data)

	s.mu.Unlock()

	// Check context
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	// Get next index
	index := atomic.AddInt64(&s.sectionIndex, 1) - 1

	// Send delta event
	event := llm.NewUIPartDeltaEvent(s.executionID, s.partID, section, data, index)
	if err := s.sendEvent(event); err != nil {
		return fmt.Errorf("failed to send ui_part_delta: %w", err)
	}

	if s.logger != nil {
		s.logger.Debug("UI part section streamed",
			F("part_id", s.partID),
			F("section", section),
			F("index", index),
		)
	}

	return nil
}

// StreamSectionBatch streams multiple sections in a batch.
func (s *UIPartStreamer) StreamSectionBatch(sections map[string]any) error {
	for section, data := range sections {
		if err := s.StreamSection(section, data); err != nil {
			return err
		}
	}

	return nil
}

// StreamHeader is a convenience method for streaming the header section.
func (s *UIPartStreamer) StreamHeader(data any) error {
	return s.StreamSection("header", data)
}

// StreamContent is a convenience method for streaming the content section.
func (s *UIPartStreamer) StreamContent(data any) error {
	return s.StreamSection("content", data)
}

// StreamRows is a convenience method for streaming table/list rows.
func (s *UIPartStreamer) StreamRows(rows any) error {
	return s.StreamSection("rows", rows)
}

// StreamColumns is a convenience method for streaming kanban columns.
func (s *UIPartStreamer) StreamColumns(columns any) error {
	return s.StreamSection("columns", columns)
}

// StreamItems is a convenience method for streaming list items.
func (s *UIPartStreamer) StreamItems(items any) error {
	return s.StreamSection("items", items)
}

// StreamActions is a convenience method for streaming action buttons.
func (s *UIPartStreamer) StreamActions(actions any) error {
	return s.StreamSection("actions", actions)
}

// StreamFooter is a convenience method for streaming the footer section.
func (s *UIPartStreamer) StreamFooter(data any) error {
	return s.StreamSection("footer", data)
}

// StreamMetadata is a convenience method for streaming metadata.
func (s *UIPartStreamer) StreamMetadata(metadata map[string]any) error {
	return s.StreamSection("metadata", metadata)
}

// End completes streaming the UI part. This sends the ui_part_end event.
func (s *UIPartStreamer) End() error {
	s.mu.Lock()

	if !s.started {
		s.mu.Unlock()

		return errors.New("UI part streamer not started")
	}

	if s.completed {
		s.mu.Unlock()

		return errors.New("UI part streamer already completed")
	}

	s.completed = true
	s.mu.Unlock()

	// Check context
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	// Send end event
	event := llm.NewUIPartEndEvent(s.executionID, s.partID)
	if err := s.sendEvent(event); err != nil {
		return fmt.Errorf("failed to send ui_part_end: %w", err)
	}

	if s.logger != nil {
		s.logger.Debug("UI part streaming completed",
			F("part_id", s.partID),
			F("part_type", s.partType),
			F("sections_streamed", len(s.sections)),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.sdk.ui_stream.completed",
			"part_type", string(s.partType),
		).Inc()
		s.metrics.Histogram("forge.ai.sdk.ui_stream.sections",
			"part_type", string(s.partType),
		).Observe(float64(len(s.sections)))
	}

	return nil
}

// EndWithError completes streaming with an error.
func (s *UIPartStreamer) EndWithError(err error) error {
	s.mu.Lock()
	s.completed = true
	s.mu.Unlock()

	// Send error event
	event := llm.NewErrorEvent(s.executionID, llm.ErrCodeInternalError, err.Error())

	event.PartID = s.partID
	if sendErr := s.sendEvent(event); sendErr != nil {
		return fmt.Errorf("failed to send error event: %w", sendErr)
	}

	if s.logger != nil {
		s.logger.Error("UI part streaming failed",
			F("part_id", s.partID),
			F("error", err.Error()),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.sdk.ui_stream.errors",
			"part_type", string(s.partType),
		).Inc()
	}

	return nil
}

// GetSections returns all streamed sections.
func (s *UIPartStreamer) GetSections() []UIPartSection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sections := make([]UIPartSection, len(s.sections))
	copy(sections, s.sections)

	return sections
}

// GetAccumulatedData returns all accumulated section data.
func (s *UIPartStreamer) GetAccumulatedData() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := make(map[string]any)
	maps.Copy(data, s.accumulator)

	return data
}

// SetFinalPart sets the final completed ContentPart.
func (s *UIPartStreamer) SetFinalPart(part ContentPart) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.finalPart = part
}

// GetFinalPart returns the final completed ContentPart.
func (s *UIPartStreamer) GetFinalPart() ContentPart {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.finalPart
}

// BuildFinalPart attempts to build the final ContentPart from accumulated data.
func (s *UIPartStreamer) BuildFinalPart() (ContentPart, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try to build the appropriate part type from accumulated data
	switch s.partType {
	case PartTypeTable:
		return s.buildTablePart()
	case PartTypeCard:
		return s.buildCardPart()
	case PartTypeButtonGroup:
		return s.buildButtonGroupPart()
	case PartTypeMetric:
		return s.buildMetricPart()
	case PartTypeTimeline:
		return s.buildTimelinePart()
	case PartTypeKanban:
		return s.buildKanbanPart()
	case PartTypeForm:
		return s.buildFormPart()
	case PartTypeChart:
		return s.buildChartPart()
	default:
		// Return generic JSON part for unsupported types
		return &JSONPart{
			PartType: PartTypeJSON,
			Data:     s.accumulator,
			Title:    string(s.partType),
		}, nil
	}
}

// accumulateSection accumulates data for a section.
func (s *UIPartStreamer) accumulateSection(section string, data any) {
	// Handle array sections (rows, items, columns, etc.)
	switch section {
	case "rows", "items", "columns", "cards", "events", "metrics", "buttons", "fields":
		// Append to existing array
		existing, ok := s.accumulator[section]
		if !ok {
			s.accumulator[section] = []any{data}
		} else if arr, ok := existing.([]any); ok {
			s.accumulator[section] = append(arr, data)
		} else {
			s.accumulator[section] = []any{existing, data}
		}
	default:
		// Replace single value sections
		s.accumulator[section] = data
	}
}

// sendEvent sends an event through the callback.
func (s *UIPartStreamer) sendEvent(event llm.ClientStreamEvent) error {
	if s.onEvent != nil {
		return s.onEvent(event)
	}

	return nil
}

// Build methods for specific part types

func (s *UIPartStreamer) buildTablePart() (ContentPart, error) {
	part := &TablePart{
		PartType: PartTypeTable,
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if headers, ok := s.accumulator["header"].([]TableHeader); ok {
		part.Headers = headers
	} else if headerData, ok := s.accumulator["header"]; ok {
		// Try to convert from JSON
		if jsonBytes, err := json.Marshal(headerData); err == nil {
			var headers []TableHeader
			if json.Unmarshal(jsonBytes, &headers) == nil {
				part.Headers = headers
			}
		}
	}

	if rows, ok := s.accumulator["rows"].([]any); ok {
		for _, rowData := range rows {
			if row, ok := rowData.([]TableCell); ok {
				part.Rows = append(part.Rows, row)
			} else if jsonBytes, err := json.Marshal(rowData); err == nil {
				var row []TableCell
				if json.Unmarshal(jsonBytes, &row) == nil {
					part.Rows = append(part.Rows, row)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildCardPart() (ContentPart, error) {
	part := &CardPart{
		PartType: PartTypeCard,
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if subtitle, ok := s.accumulator["subtitle"].(string); ok {
		part.Subtitle = subtitle
	}

	if description, ok := s.accumulator["description"].(string); ok {
		part.Description = description
	}

	if icon, ok := s.accumulator["icon"].(string); ok {
		part.Icon = icon
	}

	if footer, ok := s.accumulator["footer"].(string); ok {
		part.Footer = footer
	}

	return part, nil
}

func (s *UIPartStreamer) buildButtonGroupPart() (ContentPart, error) {
	part := &ButtonGroupPart{
		PartType: PartTypeButtonGroup,
		Buttons:  make([]Button, 0),
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if buttons, ok := s.accumulator["buttons"].([]any); ok {
		for _, btnData := range buttons {
			if jsonBytes, err := json.Marshal(btnData); err == nil {
				var btn Button
				if json.Unmarshal(jsonBytes, &btn) == nil {
					part.Buttons = append(part.Buttons, btn)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildMetricPart() (ContentPart, error) {
	part := &MetricPart{
		PartType: PartTypeMetric,
		Metrics:  make([]Metric, 0),
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if metrics, ok := s.accumulator["metrics"].([]any); ok {
		for _, metricData := range metrics {
			if jsonBytes, err := json.Marshal(metricData); err == nil {
				var metric Metric
				if json.Unmarshal(jsonBytes, &metric) == nil {
					part.Metrics = append(part.Metrics, metric)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildTimelinePart() (ContentPart, error) {
	part := &TimelinePart{
		PartType: PartTypeTimeline,
		Events:   make([]TimelineEvent, 0),
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if events, ok := s.accumulator["events"].([]any); ok {
		for _, eventData := range events {
			if jsonBytes, err := json.Marshal(eventData); err == nil {
				var event TimelineEvent
				if json.Unmarshal(jsonBytes, &event) == nil {
					part.Events = append(part.Events, event)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildKanbanPart() (ContentPart, error) {
	part := &KanbanPart{
		PartType: PartTypeKanban,
		Columns:  make([]KanbanColumn, 0),
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if columns, ok := s.accumulator["columns"].([]any); ok {
		for _, colData := range columns {
			if jsonBytes, err := json.Marshal(colData); err == nil {
				var col KanbanColumn
				if json.Unmarshal(jsonBytes, &col) == nil {
					part.Columns = append(part.Columns, col)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildFormPart() (ContentPart, error) {
	part := &FormPart{
		PartType: PartTypeForm,
		Fields:   make([]FormField, 0),
	}

	if id, ok := s.accumulator["id"].(string); ok {
		part.ID = id
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if description, ok := s.accumulator["description"].(string); ok {
		part.Description = description
	}

	if fields, ok := s.accumulator["fields"].([]any); ok {
		for _, fieldData := range fields {
			if jsonBytes, err := json.Marshal(fieldData); err == nil {
				var field FormField
				if json.Unmarshal(jsonBytes, &field) == nil {
					part.Fields = append(part.Fields, field)
				}
			}
		}
	}

	return part, nil
}

func (s *UIPartStreamer) buildChartPart() (ContentPart, error) {
	part := &ChartPart{
		PartType: PartTypeChart,
	}

	if title, ok := s.accumulator["title"].(string); ok {
		part.Title = title
	}

	if chartType, ok := s.accumulator["chartType"].(string); ok {
		part.ChartType = ChartType(chartType)
	}

	if data, ok := s.accumulator["data"]; ok {
		if jsonBytes, err := json.Marshal(data); err == nil {
			var chartData ChartData
			if json.Unmarshal(jsonBytes, &chartData) == nil {
				part.Data = chartData
			}
		}
	}

	return part, nil
}

// --- UIPartStreamManager manages multiple concurrent UI part streams ---

// UIPartStreamManager manages multiple concurrent UI part streamers.
type UIPartStreamManager struct {
	mu        sync.RWMutex
	streamers map[string]*UIPartStreamer
	onEvent   func(llm.ClientStreamEvent) error
	logger    forge.Logger
	metrics   forge.Metrics
}

// NewUIPartStreamManager creates a new UI part stream manager.
func NewUIPartStreamManager(
	onEvent func(llm.ClientStreamEvent) error,
	logger forge.Logger,
	metrics forge.Metrics,
) *UIPartStreamManager {
	return &UIPartStreamManager{
		streamers: make(map[string]*UIPartStreamer),
		onEvent:   onEvent,
		logger:    logger,
		metrics:   metrics,
	}
}

// CreateStreamer creates a new UI part streamer and registers it.
func (m *UIPartStreamManager) CreateStreamer(
	ctx context.Context,
	executionID string,
	partType ContentPartType,
	metadata map[string]any,
) *UIPartStreamer {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    partType,
		ExecutionID: executionID,
		OnEvent:     m.onEvent,
		Logger:      m.logger,
		Metrics:     m.metrics,
		Context:     ctx,
		Metadata:    metadata,
	})

	m.mu.Lock()
	m.streamers[streamer.partID] = streamer
	m.mu.Unlock()

	return streamer
}

// GetStreamer retrieves a streamer by part ID.
func (m *UIPartStreamManager) GetStreamer(partID string) (*UIPartStreamer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.streamers[partID]

	return s, ok
}

// RemoveStreamer removes a streamer from management.
func (m *UIPartStreamManager) RemoveStreamer(partID string) {
	m.mu.Lock()
	delete(m.streamers, partID)
	m.mu.Unlock()
}

// GetAllStreamers returns all active streamers.
func (m *UIPartStreamManager) GetAllStreamers() []*UIPartStreamer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streamers := make([]*UIPartStreamer, 0, len(m.streamers))
	for _, s := range m.streamers {
		streamers = append(streamers, s)
	}

	return streamers
}

// EndAll ends all active streamers.
func (m *UIPartStreamManager) EndAll() {
	m.mu.RLock()

	streamers := make([]*UIPartStreamer, 0, len(m.streamers))
	for _, s := range m.streamers {
		streamers = append(streamers, s)
	}

	m.mu.RUnlock()

	for _, s := range streamers {
		if !s.IsCompleted() {
			_ = s.End()
		}
	}
}

// --- Helper functions for streaming common UI parts ---

// StreamTable creates and streams a complete table part.
func StreamTable(
	ctx context.Context,
	executionID string,
	title string,
	headers []TableHeader,
	rows [][]TableCell,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeTable,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
	})

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream headers
	if err := streamer.StreamHeader(headers); err != nil {
		return err
	}

	// Stream rows in batches
	batchSize := 10
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}

		batch := rows[i:end]
		if err := streamer.StreamRows(batch); err != nil {
			return err
		}
	}

	return streamer.End()
}

// StreamMetrics creates and streams a metrics part.
func StreamMetrics(
	ctx context.Context,
	executionID string,
	title string,
	metrics []Metric,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeMetric,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
	})

	if err := streamer.Start(); err != nil {
		return err
	}

	if title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream each metric individually for progressive rendering
	for _, metric := range metrics {
		if err := streamer.StreamSection("metrics", metric); err != nil {
			return err
		}
	}

	return streamer.End()
}

// StreamTimeline creates and streams a timeline part.
func StreamTimeline(
	ctx context.Context,
	executionID string,
	title string,
	events []TimelineEvent,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeTimeline,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
	})

	if err := streamer.Start(); err != nil {
		return err
	}

	if title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream events progressively
	for _, event := range events {
		if err := streamer.StreamSection("events", event); err != nil {
			return err
		}
		// Small delay for visual effect (optional)
		time.Sleep(50 * time.Millisecond)
	}

	return streamer.End()
}

// StreamButtons creates and streams a button group.
func StreamButtons(
	ctx context.Context,
	executionID string,
	title string,
	buttons []Button,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    PartTypeButtonGroup,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
	})

	if err := streamer.Start(); err != nil {
		return err
	}

	if title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream all buttons at once (usually small)
	if err := streamer.StreamSection("buttons", buttons); err != nil {
		return err
	}

	return streamer.End()
}
