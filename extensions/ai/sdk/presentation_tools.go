package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// PresentationToolName constants for built-in presentation tools.
const (
	ToolRenderTable    = "render_table"
	ToolRenderChart    = "render_chart"
	ToolRenderMetrics  = "render_metrics"
	ToolRenderTimeline = "render_timeline"
	ToolRenderKanban   = "render_kanban"
	ToolRenderButtons  = "render_buttons"
	ToolRenderForm     = "render_form"
	ToolRenderCard     = "render_card"
	ToolRenderStats    = "render_stats"
	ToolRenderGallery  = "render_gallery"
	ToolRenderAlert    = "render_alert"
	ToolRenderProgress = "render_progress"
)

// PresentationTools returns all built-in presentation tools that AI can call.
func PresentationTools() []UITool {
	return []UITool{
		newRenderTableTool(),
		newRenderChartTool(),
		newRenderMetricsTool(),
		newRenderTimelineTool(),
		newRenderKanbanTool(),
		newRenderButtonsTool(),
		newRenderFormTool(),
		newRenderCardTool(),
		newRenderStatsTool(),
		newRenderGalleryTool(),
		newRenderAlertTool(),
		newRenderProgressTool(),
	}
}

// GetPresentationToolSchemas returns LLM-ready tool definitions for all presentation tools.
func GetPresentationToolSchemas() []llm.Tool {
	tools := PresentationTools()
	schemas := make([]llm.Tool, len(tools))

	for i, tool := range tools {
		schemas[i] = llm.Tool{
			Type: "function",
			Function: &llm.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  toolParamsToMap(tool.GetParameters()),
			},
		}
	}

	return schemas
}

// toolParamsToMap converts ToolParameterSchema to map[string]any for LLM.
func toolParamsToMap(schema ToolParameterSchema) map[string]any {
	props := make(map[string]any)

	for name, prop := range schema.Properties {
		propMap := map[string]any{
			"type":        prop.Type,
			"description": prop.Description,
		}
		if len(prop.Enum) > 0 {
			propMap["enum"] = prop.Enum
		}

		if prop.Default != nil {
			propMap["default"] = prop.Default
		}

		props[name] = propMap
	}

	return map[string]any{
		"type":       schema.Type,
		"properties": props,
		"required":   schema.Required,
	}
}

// =============================================================================
// render_table - Display data as an interactive table
// =============================================================================

func newRenderTableTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderTable,
		Description: "Display data as an interactive table. Use when showing tabular data, lists of items, database records, comparisons, or any structured data with rows and columns.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Title of the table",
				},
				"headers": {
					Type:        "array",
					Description: "Array of column headers. Each header should have 'label' (display name) and 'key' (data key). Optional: 'sortable' (boolean), 'width' (string)",
				},
				"rows": {
					Type:        "array",
					Description: "Array of rows. Each row is an array of cell values matching header order. Values can be strings, numbers, or objects with 'value' and 'display' keys",
				},
				"options": {
					Type:        "object",
					Description: "Optional table options: 'sortable' (boolean), 'searchable' (boolean), 'paginated' (boolean), 'pageSize' (number)",
				},
			},
			Required: []string{"headers", "rows"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeTable,
			StreamingEnabled:  true,
		},
		Handler:    handleRenderTable,
		RenderFunc: renderTableUI,
	})
}

func handleRenderTable(ctx context.Context, params map[string]any) (any, error) {
	// Pass through the params as the result - the render function will handle it
	return params, nil
}

func renderTableUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_table")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream headers
	if headersRaw, ok := params["headers"]; ok {
		headers := parseTableHeaders(headersRaw)
		if err := streamer.StreamHeader(headers); err != nil {
			return err
		}
	}

	// Stream rows
	if rowsRaw, ok := params["rows"].([]any); ok {
		rows := parseTableRows(rowsRaw)
		// Stream in batches of 10
		batchSize := 10
		for i := 0; i < len(rows); i += batchSize {
			end := i + batchSize
			if end > len(rows) {
				end = len(rows)
			}

			if err := streamer.StreamRows(rows[i:end]); err != nil {
				return err
			}
		}
	}

	// Stream options as metadata
	if options, ok := params["options"].(map[string]any); ok {
		if err := streamer.StreamMetadata(options); err != nil {
			return err
		}
	}

	return streamer.End()
}

func parseTableHeaders(raw any) []TableHeader {
	headers := make([]TableHeader, 0)

	switch v := raw.(type) {
	case []any:
		for _, h := range v {
			switch hv := h.(type) {
			case string:
				headers = append(headers, TableHeader{Label: hv, Key: hv})
			case map[string]any:
				header := TableHeader{}
				if label, ok := hv["label"].(string); ok {
					header.Label = label
				}

				if key, ok := hv["key"].(string); ok {
					header.Key = key
				} else {
					header.Key = header.Label
				}

				if sortable, ok := hv["sortable"].(bool); ok {
					header.Sortable = sortable
				}

				if width, ok := hv["width"].(string); ok {
					header.Width = width
				}

				headers = append(headers, header)
			}
		}
	}

	return headers
}

func parseTableRows(raw []any) [][]TableCell {
	rows := make([][]TableCell, 0, len(raw))

	for _, r := range raw {
		if rowArr, ok := r.([]any); ok {
			cells := make([]TableCell, 0, len(rowArr))
			for _, c := range rowArr {
				cell := TableCell{}

				switch cv := c.(type) {
				case string:
					cell.Value = cv
					cell.Display = cv
				case float64:
					cell.Value = cv
					cell.Display = fmt.Sprintf("%v", cv)
				case int:
					cell.Value = cv
					cell.Display = strconv.Itoa(cv)
				case map[string]any:
					if val, ok := cv["value"]; ok {
						cell.Value = val
					}

					if disp, ok := cv["display"].(string); ok {
						cell.Display = disp
					} else {
						cell.Display = fmt.Sprintf("%v", cell.Value)
					}

					if style, ok := cv["style"].(string); ok {
						cell.Style = style
					}

					if link, ok := cv["link"].(string); ok {
						cell.Link = link
					}
				default:
					cell.Value = cv
					cell.Display = fmt.Sprintf("%v", cv)
				}

				cells = append(cells, cell)
			}

			rows = append(rows, cells)
		}
	}

	return rows
}

// =============================================================================
// render_chart - Display data as a chart
// =============================================================================

func newRenderChartTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderChart,
		Description: "Display data as an interactive chart. Use for visualizing trends, comparisons, distributions, or any data that benefits from graphical representation. Supports line, bar, pie, doughnut, area, and scatter charts.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Title of the chart",
				},
				"type": {
					Type:        "string",
					Description: "Chart type",
					Enum:        []string{"line", "bar", "pie", "doughnut", "area", "scatter"},
				},
				"labels": {
					Type:        "array",
					Description: "X-axis labels or category names",
				},
				"datasets": {
					Type:        "array",
					Description: "Array of datasets. Each dataset has 'label' (string), 'data' (array of numbers), optional 'backgroundColor' and 'borderColor'",
				},
				"options": {
					Type:        "object",
					Description: "Optional chart options: 'showLegend' (boolean), 'showGrid' (boolean), 'stacked' (boolean)",
				},
			},
			Required: []string{"type", "labels", "datasets"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeChart,
			StreamingEnabled:  false, // Charts render all at once
		},
		Handler:    handleRenderChart,
		RenderFunc: renderChartUI,
	})
}

func handleRenderChart(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderChartUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_chart")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream chart type
	if chartType, ok := params["type"].(string); ok {
		if err := streamer.StreamSection("chartType", chartType); err != nil {
			return err
		}
	}

	// Build and stream chart data
	chartData := ChartData{}

	if labels, ok := params["labels"].([]any); ok {
		for _, l := range labels {
			if s, ok := l.(string); ok {
				chartData.Labels = append(chartData.Labels, s)
			}
		}
	}

	if datasets, ok := params["datasets"].([]any); ok {
		for _, ds := range datasets {
			if dsMap, ok := ds.(map[string]any); ok {
				dataset := ChartDataset{}
				if label, ok := dsMap["label"].(string); ok {
					dataset.Label = label
				}

				if data, ok := dsMap["data"].([]any); ok {
					for _, d := range data {
						switch v := d.(type) {
						case float64:
							dataset.Data = append(dataset.Data, v)
						case int:
							dataset.Data = append(dataset.Data, float64(v))
						}
					}
				}

				if bg, ok := dsMap["backgroundColor"].(string); ok {
					dataset.BackgroundColor = bg
				}

				if bc, ok := dsMap["borderColor"].(string); ok {
					dataset.BorderColor = bc
				}

				chartData.Datasets = append(chartData.Datasets, dataset)
			}
		}
	}

	if err := streamer.StreamSection("data", chartData); err != nil {
		return err
	}

	// Stream options
	if options, ok := params["options"].(map[string]any); ok {
		if err := streamer.StreamMetadata(options); err != nil {
			return err
		}
	}

	return streamer.End()
}

// =============================================================================
// render_metrics - Display KPIs and metrics
// =============================================================================

func newRenderMetricsTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderMetrics,
		Description: "Display key performance indicators (KPIs) and metrics. Use for dashboards, stats summaries, or highlighting important numbers with optional trends and sparklines.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Dashboard or section title",
				},
				"metrics": {
					Type:        "array",
					Description: "Array of metrics. Each metric has 'label' (string), 'value' (number/string), optional 'unit', 'icon', 'trend' (object with 'direction': up/down/stable, 'percentage'), 'status' (good/warning/bad)",
				},
				"layout": {
					Type:        "string",
					Description: "Layout style",
					Enum:        []string{"grid", "list", "compact", "cards"},
				},
				"columns": {
					Type:        "integer",
					Description: "Number of columns for grid layout (default: 3)",
				},
			},
			Required: []string{"metrics"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeMetric,
			StreamingEnabled:  true,
		},
		Handler:    handleRenderMetrics,
		RenderFunc: renderMetricsUI,
	})
}

func handleRenderMetrics(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderMetricsUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_metrics")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream metrics one by one
	if metricsRaw, ok := params["metrics"].([]any); ok {
		for _, m := range metricsRaw {
			if mMap, ok := m.(map[string]any); ok {
				metric := parseMetric(mMap)
				if err := streamer.StreamSection("metrics", metric); err != nil {
					return err
				}
			}
		}
	}

	// Stream layout options
	metadata := make(map[string]any)
	if layout, ok := params["layout"].(string); ok {
		metadata["layout"] = layout
	}

	if columns, ok := params["columns"].(float64); ok {
		metadata["columns"] = int(columns)
	}

	if len(metadata) > 0 {
		if err := streamer.StreamMetadata(metadata); err != nil {
			return err
		}
	}

	return streamer.End()
}

func parseMetric(m map[string]any) Metric {
	metric := Metric{}

	if label, ok := m["label"].(string); ok {
		metric.Label = label
	}

	if value, ok := m["value"]; ok {
		metric.Value = value
		metric.FormattedValue = fmt.Sprintf("%v", value)
	}

	if formatted, ok := m["formattedValue"].(string); ok {
		metric.FormattedValue = formatted
	}

	if unit, ok := m["unit"].(string); ok {
		metric.Unit = unit
	}

	if icon, ok := m["icon"].(string); ok {
		metric.Icon = icon
	}

	if color, ok := m["color"].(string); ok {
		metric.Color = color
	}

	if status, ok := m["status"].(string); ok {
		metric.Status = MetricStatus(status)
	}

	// Parse trend
	if trendMap, ok := m["trend"].(map[string]any); ok {
		trend := &MetricTrend{}
		if dir, ok := trendMap["direction"].(string); ok {
			trend.Direction = TrendDirection(dir)
		}

		if pct, ok := trendMap["percentage"].(float64); ok {
			trend.Percentage = pct
		}

		if period, ok := trendMap["period"].(string); ok {
			trend.Period = period
		}

		metric.Trend = trend
	}

	return metric
}

// =============================================================================
// render_timeline - Display chronological events
// =============================================================================

func newRenderTimelineTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderTimeline,
		Description: "Display a timeline of events. Use for showing chronological data, history, project milestones, activity logs, or any sequential events.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Timeline title",
				},
				"events": {
					Type:        "array",
					Description: "Array of events. Each event has 'title' (string), 'description' (optional string), 'timestamp' (ISO date string), optional 'icon', 'color', 'status' (pending/in_progress/completed/cancelled/error)",
				},
				"orientation": {
					Type:        "string",
					Description: "Timeline orientation",
					Enum:        []string{"vertical", "horizontal"},
				},
			},
			Required: []string{"events"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeTimeline,
			StreamingEnabled:  true,
		},
		Handler:    handleRenderTimeline,
		RenderFunc: renderTimelineUI,
	})
}

func handleRenderTimeline(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderTimelineUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_timeline")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream events
	if eventsRaw, ok := params["events"].([]any); ok {
		for _, e := range eventsRaw {
			if eMap, ok := e.(map[string]any); ok {
				event := parseTimelineEvent(eMap)
				if err := streamer.StreamSection("events", event); err != nil {
					return err
				}
			}
		}
	}

	// Stream orientation
	if orientation, ok := params["orientation"].(string); ok {
		if err := streamer.StreamMetadata(map[string]any{"orientation": orientation}); err != nil {
			return err
		}
	}

	return streamer.End()
}

func parseTimelineEvent(e map[string]any) TimelineEvent {
	event := TimelineEvent{
		ID: fmt.Sprintf("evt_%d", time.Now().UnixNano()),
	}

	if title, ok := e["title"].(string); ok {
		event.Title = title
	}

	if desc, ok := e["description"].(string); ok {
		event.Description = desc
	}

	if ts, ok := e["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			event.Timestamp = t
		} else {
			event.Timestamp = time.Now()
		}
	}

	if icon, ok := e["icon"].(string); ok {
		event.Icon = icon
	}

	if color, ok := e["color"].(string); ok {
		event.Color = color
	}

	if status, ok := e["status"].(string); ok {
		event.Status = TimelineStatus(status)
	}

	return event
}

// =============================================================================
// render_kanban - Display a kanban board
// =============================================================================

func newRenderKanbanTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderKanban,
		Description: "Display a kanban board with columns and cards. Use for task management, workflow visualization, project status boards, or any columnar organization of items.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Board title",
				},
				"columns": {
					Type:        "array",
					Description: "Array of columns. Each column has 'title' (string), 'id' (string), optional 'color', and 'cards' array. Each card has 'title', 'description', optional 'labels', 'priority' (low/medium/high/urgent)",
				},
				"draggable": {
					Type:        "boolean",
					Description: "Allow drag and drop (default: true)",
				},
			},
			Required: []string{"columns"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeKanban,
			StreamingEnabled:  true,
		},
		Handler:    handleRenderKanban,
		RenderFunc: renderKanbanUI,
	})
}

func handleRenderKanban(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderKanbanUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_kanban")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream columns
	if columnsRaw, ok := params["columns"].([]any); ok {
		for _, c := range columnsRaw {
			if cMap, ok := c.(map[string]any); ok {
				column := parseKanbanColumn(cMap)
				if err := streamer.StreamColumns(column); err != nil {
					return err
				}
			}
		}
	}

	// Stream options
	metadata := make(map[string]any)
	if draggable, ok := params["draggable"].(bool); ok {
		metadata["draggable"] = draggable
	}

	if len(metadata) > 0 {
		if err := streamer.StreamMetadata(metadata); err != nil {
			return err
		}
	}

	return streamer.End()
}

func parseKanbanColumn(c map[string]any) KanbanColumn {
	column := KanbanColumn{
		ID:    fmt.Sprintf("col_%d", time.Now().UnixNano()),
		Cards: make([]KanbanCard, 0),
	}

	if id, ok := c["id"].(string); ok {
		column.ID = id
	}

	if title, ok := c["title"].(string); ok {
		column.Title = title
	}

	if color, ok := c["color"].(string); ok {
		column.Color = color
	}

	if cardsRaw, ok := c["cards"].([]any); ok {
		for _, card := range cardsRaw {
			if cardMap, ok := card.(map[string]any); ok {
				column.Cards = append(column.Cards, parseKanbanCard(cardMap))
			}
		}
	}

	return column
}

func parseKanbanCard(c map[string]any) KanbanCard {
	card := KanbanCard{
		ID: fmt.Sprintf("card_%d", time.Now().UnixNano()),
	}

	if id, ok := c["id"].(string); ok {
		card.ID = id
	}

	if title, ok := c["title"].(string); ok {
		card.Title = title
	}

	if desc, ok := c["description"].(string); ok {
		card.Description = desc
	}

	if priority, ok := c["priority"].(string); ok {
		card.Priority = priority
	}

	if labelsRaw, ok := c["labels"].([]any); ok {
		for _, l := range labelsRaw {
			switch lv := l.(type) {
			case string:
				card.Labels = append(card.Labels, KanbanLabel{Text: lv})
			case map[string]any:
				label := KanbanLabel{}
				if text, ok := lv["text"].(string); ok {
					label.Text = text
				}

				if color, ok := lv["color"].(string); ok {
					label.Color = color
				}

				card.Labels = append(card.Labels, label)
			}
		}
	}

	return card
}

// =============================================================================
// render_buttons - Display interactive buttons
// =============================================================================

func newRenderButtonsTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderButtons,
		Description: "Display a group of interactive buttons. Use for presenting action choices, navigation options, quick replies, or any interactive options the user can select.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Optional title above buttons",
				},
				"buttons": {
					Type:        "array",
					Description: "Array of buttons. Each button has 'label' (string), 'id' (string), optional 'icon', 'variant' (primary/secondary/outline/danger), 'action' object with 'type' (link/tool/callback/copy) and 'value'",
				},
				"layout": {
					Type:        "string",
					Description: "Button layout",
					Enum:        []string{"horizontal", "vertical", "grid", "wrap"},
				},
			},
			Required: []string{"buttons"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeButtonGroup,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderButtons,
		RenderFunc: renderButtonsUI,
	})
}

func handleRenderButtons(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderButtonsUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_buttons")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream title
	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	// Stream buttons
	if buttonsRaw, ok := params["buttons"].([]any); ok {
		buttons := make([]Button, 0, len(buttonsRaw))
		for _, b := range buttonsRaw {
			if bMap, ok := b.(map[string]any); ok {
				buttons = append(buttons, parseButton(bMap))
			}
		}

		if err := streamer.StreamSection("buttons", buttons); err != nil {
			return err
		}
	}

	// Stream layout
	if layout, ok := params["layout"].(string); ok {
		if err := streamer.StreamMetadata(map[string]any{"layout": layout}); err != nil {
			return err
		}
	}

	return streamer.End()
}

func parseButton(b map[string]any) Button {
	button := Button{
		ID:      fmt.Sprintf("btn_%d", time.Now().UnixNano()),
		Variant: ButtonPrimary,
	}

	if id, ok := b["id"].(string); ok {
		button.ID = id
	}

	if label, ok := b["label"].(string); ok {
		button.Label = label
	}

	if icon, ok := b["icon"].(string); ok {
		button.Icon = icon
	}

	if variant, ok := b["variant"].(string); ok {
		button.Variant = ButtonVariant(variant)
	}

	if disabled, ok := b["disabled"].(bool); ok {
		button.Disabled = disabled
	}

	// Parse action
	if actionMap, ok := b["action"].(map[string]any); ok {
		action := ButtonAction{}
		if actionType, ok := actionMap["type"].(string); ok {
			action.Type = ButtonActionType(actionType)
		}

		if value, ok := actionMap["value"].(string); ok {
			action.Value = value
		}

		if payload, ok := actionMap["payload"].(map[string]any); ok {
			action.Payload = payload
		}

		button.Action = action
	}

	return button
}

// =============================================================================
// render_form - Display an interactive form
// =============================================================================

func newRenderFormTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderForm,
		Description: "Display an interactive form for collecting user input. Use for data entry, settings, search filters, or any structured input collection.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"id": {
					Type:        "string",
					Description: "Unique form identifier",
				},
				"title": {
					Type:        "string",
					Description: "Form title",
				},
				"description": {
					Type:        "string",
					Description: "Form description or instructions",
				},
				"fields": {
					Type:        "array",
					Description: "Array of form fields. Each field has 'name' (string), 'label' (string), 'type' (text/email/password/number/select/checkbox/textarea/date), optional 'placeholder', 'required' (boolean), 'options' (for select)",
				},
				"submitLabel": {
					Type:        "string",
					Description: "Submit button label (default: 'Submit')",
				},
			},
			Required: []string{"fields"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeForm,
			StreamingEnabled:  true,
		},
		Handler:    handleRenderForm,
		RenderFunc: renderFormUI,
	})
}

func handleRenderForm(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderFormUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_form")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream form metadata
	if id, ok := params["id"].(string); ok {
		if err := streamer.StreamSection("id", id); err != nil {
			return err
		}
	}

	if title, ok := params["title"].(string); ok {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	if desc, ok := params["description"].(string); ok {
		if err := streamer.StreamSection("description", desc); err != nil {
			return err
		}
	}

	// Stream fields
	if fieldsRaw, ok := params["fields"].([]any); ok {
		for _, f := range fieldsRaw {
			if fMap, ok := f.(map[string]any); ok {
				field := parseFormField(fMap)
				if err := streamer.StreamSection("fields", field); err != nil {
					return err
				}
			}
		}
	}

	// Stream submit button
	submitLabel := "Submit"
	if label, ok := params["submitLabel"].(string); ok {
		submitLabel = label
	}

	submitButton := Button{
		ID:      "submit",
		Label:   submitLabel,
		Variant: ButtonPrimary,
		Action:  ButtonAction{Type: ActionTypeSubmit},
	}
	if err := streamer.StreamActions([]Button{submitButton}); err != nil {
		return err
	}

	return streamer.End()
}

func parseFormField(f map[string]any) FormField {
	field := FormField{
		ID:   fmt.Sprintf("field_%d", time.Now().UnixNano()),
		Type: FieldTypeText,
	}

	if id, ok := f["id"].(string); ok {
		field.ID = id
	}

	if name, ok := f["name"].(string); ok {
		field.Name = name
		if field.ID == "" {
			field.ID = name
		}
	}

	if label, ok := f["label"].(string); ok {
		field.Label = label
	}

	if fieldType, ok := f["type"].(string); ok {
		field.Type = FormFieldType(fieldType)
	}

	if placeholder, ok := f["placeholder"].(string); ok {
		field.Placeholder = placeholder
	}

	if required, ok := f["required"].(bool); ok {
		field.Required = required
	}

	if defaultVal, ok := f["defaultValue"]; ok {
		field.DefaultValue = defaultVal
	}

	// Parse options for select fields
	if optionsRaw, ok := f["options"].([]any); ok {
		for _, o := range optionsRaw {
			switch ov := o.(type) {
			case string:
				field.Options = append(field.Options, FormFieldOption{Value: ov, Label: ov})
			case map[string]any:
				opt := FormFieldOption{}
				if val, ok := ov["value"].(string); ok {
					opt.Value = val
				}

				if label, ok := ov["label"].(string); ok {
					opt.Label = label
				}

				field.Options = append(field.Options, opt)
			}
		}
	}

	return field
}

// =============================================================================
// render_card - Display a content card
// =============================================================================

func newRenderCardTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderCard,
		Description: "Display content in a card format. Use for highlighting important information, summaries, profiles, or any content that benefits from visual containment.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Card title",
				},
				"subtitle": {
					Type:        "string",
					Description: "Card subtitle",
				},
				"content": {
					Type:        "string",
					Description: "Main card content (supports markdown)",
				},
				"icon": {
					Type:        "string",
					Description: "Icon name to display",
				},
				"image": {
					Type:        "string",
					Description: "Image URL for card header",
				},
				"footer": {
					Type:        "string",
					Description: "Footer text",
				},
				"actions": {
					Type:        "array",
					Description: "Array of action buttons",
				},
			},
			Required: []string{"title"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeCard,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderCard,
		RenderFunc: renderCardUI,
	})
}

func handleRenderCard(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderCardUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_card")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream card fields
	if title, ok := params["title"].(string); ok {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	if subtitle, ok := params["subtitle"].(string); ok {
		if err := streamer.StreamSection("subtitle", subtitle); err != nil {
			return err
		}
	}

	if content, ok := params["content"].(string); ok {
		if err := streamer.StreamContent(content); err != nil {
			return err
		}
	}

	if icon, ok := params["icon"].(string); ok {
		if err := streamer.StreamSection("icon", icon); err != nil {
			return err
		}
	}

	if image, ok := params["image"].(string); ok {
		if err := streamer.StreamSection("image", image); err != nil {
			return err
		}
	}

	if footer, ok := params["footer"].(string); ok {
		if err := streamer.StreamFooter(footer); err != nil {
			return err
		}
	}

	// Stream actions
	if actionsRaw, ok := params["actions"].([]any); ok {
		actions := make([]Button, 0)

		for _, a := range actionsRaw {
			if aMap, ok := a.(map[string]any); ok {
				actions = append(actions, parseButton(aMap))
			}
		}

		if len(actions) > 0 {
			if err := streamer.StreamActions(actions); err != nil {
				return err
			}
		}
	}

	return streamer.End()
}

// =============================================================================
// render_stats - Display compact statistics
// =============================================================================

func newRenderStatsTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderStats,
		Description: "Display compact statistics in a row or grid. Use for quick overviews, summaries, or key numbers without detailed formatting.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Stats section title",
				},
				"stats": {
					Type:        "array",
					Description: "Array of stats. Each stat has 'label' (string), 'value' (number/string), optional 'unit', 'change' (e.g., '+5%'), 'changeColor' (green/red)",
				},
				"layout": {
					Type:        "string",
					Description: "Layout style",
					Enum:        []string{"row", "grid", "column"},
				},
			},
			Required: []string{"stats"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeStats,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderStats,
		RenderFunc: renderStatsUI,
	})
}

func handleRenderStats(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderStatsUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_stats")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	if statsRaw, ok := params["stats"].([]any); ok {
		stats := make([]Stat, 0, len(statsRaw))
		for _, s := range statsRaw {
			if sMap, ok := s.(map[string]any); ok {
				stat := Stat{}
				if label, ok := sMap["label"].(string); ok {
					stat.Label = label
				}

				if value, ok := sMap["value"]; ok {
					stat.Value = value
				}

				if unit, ok := sMap["unit"].(string); ok {
					stat.Unit = unit
				}

				if change, ok := sMap["change"].(string); ok {
					stat.Change = change
				}

				if changeColor, ok := sMap["changeColor"].(string); ok {
					stat.ChangeColor = changeColor
				}

				stats = append(stats, stat)
			}
		}

		if err := streamer.StreamSection("stats", stats); err != nil {
			return err
		}
	}

	if layout, ok := params["layout"].(string); ok {
		if err := streamer.StreamMetadata(map[string]any{"layout": layout}); err != nil {
			return err
		}
	}

	return streamer.End()
}

// =============================================================================
// render_gallery - Display an image/media gallery
// =============================================================================

func newRenderGalleryTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderGallery,
		Description: "Display an image or media gallery. Use for photo collections, product images, portfolios, or any visual content collection.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Gallery title",
				},
				"items": {
					Type:        "array",
					Description: "Array of gallery items. Each item has 'url' (image URL), optional 'thumbnail', 'title', 'description'",
				},
				"layout": {
					Type:        "string",
					Description: "Gallery layout",
					Enum:        []string{"grid", "masonry", "list"},
				},
				"columns": {
					Type:        "integer",
					Description: "Number of columns (default: 3)",
				},
			},
			Required: []string{"items"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeGallery,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderGallery,
		RenderFunc: renderGalleryUI,
	})
}

func handleRenderGallery(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderGalleryUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_gallery")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	if title, ok := params["title"].(string); ok && title != "" {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	if itemsRaw, ok := params["items"].([]any); ok {
		items := make([]GalleryItem, 0, len(itemsRaw))
		for i, item := range itemsRaw {
			if itemMap, ok := item.(map[string]any); ok {
				gi := GalleryItem{
					ID: fmt.Sprintf("img_%d", i),
				}
				if url, ok := itemMap["url"].(string); ok {
					gi.URL = url
				}

				if thumb, ok := itemMap["thumbnail"].(string); ok {
					gi.Thumbnail = thumb
				}

				if title, ok := itemMap["title"].(string); ok {
					gi.Title = title
				}

				if desc, ok := itemMap["description"].(string); ok {
					gi.Description = desc
				}

				items = append(items, gi)
			}
		}

		if err := streamer.StreamItems(items); err != nil {
			return err
		}
	}

	metadata := make(map[string]any)
	if layout, ok := params["layout"].(string); ok {
		metadata["layout"] = layout
	}

	if columns, ok := params["columns"].(float64); ok {
		metadata["columns"] = int(columns)
	}

	if len(metadata) > 0 {
		if err := streamer.StreamMetadata(metadata); err != nil {
			return err
		}
	}

	return streamer.End()
}

// =============================================================================
// render_alert - Display an alert/notification
// =============================================================================

func newRenderAlertTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderAlert,
		Description: "Display an alert or notification message. Use for warnings, errors, success messages, or important notices that need user attention.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"title": {
					Type:        "string",
					Description: "Alert title",
				},
				"message": {
					Type:        "string",
					Description: "Alert message content",
				},
				"type": {
					Type:        "string",
					Description: "Alert type/severity",
					Enum:        []string{"info", "success", "warning", "error"},
				},
				"dismissible": {
					Type:        "boolean",
					Description: "Whether the alert can be dismissed",
				},
				"actions": {
					Type:        "array",
					Description: "Optional action buttons",
				},
			},
			Required: []string{"message", "type"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeAlert,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderAlert,
		RenderFunc: renderAlertUI,
	})
}

func handleRenderAlert(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderAlertUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_alert")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	if title, ok := params["title"].(string); ok {
		if err := streamer.StreamSection("title", title); err != nil {
			return err
		}
	}

	if message, ok := params["message"].(string); ok {
		if err := streamer.StreamContent(message); err != nil {
			return err
		}
	}

	if alertType, ok := params["type"].(string); ok {
		if err := streamer.StreamSection("alertType", alertType); err != nil {
			return err
		}
	}

	metadata := make(map[string]any)
	if dismissible, ok := params["dismissible"].(bool); ok {
		metadata["dismissible"] = dismissible
	}

	if len(metadata) > 0 {
		if err := streamer.StreamMetadata(metadata); err != nil {
			return err
		}
	}

	if actionsRaw, ok := params["actions"].([]any); ok {
		actions := make([]Button, 0)

		for _, a := range actionsRaw {
			if aMap, ok := a.(map[string]any); ok {
				actions = append(actions, parseButton(aMap))
			}
		}

		if len(actions) > 0 {
			if err := streamer.StreamActions(actions); err != nil {
				return err
			}
		}
	}

	return streamer.End()
}

// =============================================================================
// render_progress - Display a progress indicator
// =============================================================================

func newRenderProgressTool() *BaseUITool {
	return NewBaseUITool(BaseUIToolConfig{
		Name:        ToolRenderProgress,
		Description: "Display a progress indicator. Use for showing completion status, loading progress, or step-by-step progress through a process.",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"label": {
					Type:        "string",
					Description: "Progress label",
				},
				"value": {
					Type:        "number",
					Description: "Current progress value (0-100)",
				},
				"max": {
					Type:        "number",
					Description: "Maximum value (default: 100)",
				},
				"showPercentage": {
					Type:        "boolean",
					Description: "Show percentage text",
				},
				"variant": {
					Type:        "string",
					Description: "Progress bar style",
					Enum:        []string{"default", "success", "warning", "error"},
				},
			},
			Required: []string{"value"},
		},
		Hints: UIToolHints{
			PreferredPartType: PartTypeProgress,
			StreamingEnabled:  false,
		},
		Handler:    handleRenderProgress,
		RenderFunc: renderProgressUI,
	})
}

func handleRenderProgress(ctx context.Context, params map[string]any) (any, error) {
	return params, nil
}

func renderProgressUI(ctx context.Context, result any, streamer *UIPartStreamer) error {
	params, ok := result.(map[string]any)
	if !ok {
		return errors.New("invalid result type for render_progress")
	}

	if err := streamer.Start(); err != nil {
		return err
	}

	if label, ok := params["label"].(string); ok {
		if err := streamer.StreamSection("label", label); err != nil {
			return err
		}
	}

	value := 0.0
	if v, ok := params["value"].(float64); ok {
		value = v
	}

	if err := streamer.StreamSection("value", value); err != nil {
		return err
	}

	metadata := make(map[string]any)
	if max, ok := params["max"].(float64); ok {
		metadata["max"] = max
	}

	if showPct, ok := params["showPercentage"].(bool); ok {
		metadata["showPercentage"] = showPct
	}

	if variant, ok := params["variant"].(string); ok {
		metadata["variant"] = variant
	}

	if len(metadata) > 0 {
		if err := streamer.StreamMetadata(metadata); err != nil {
			return err
		}
	}

	return streamer.End()
}

// =============================================================================
// Utility: Execute presentation tool by name
// =============================================================================

// ExecutePresentationTool executes a presentation tool by name with the given parameters.
func ExecutePresentationTool(
	ctx context.Context,
	toolName string,
	params map[string]any,
	onEvent func(llm.ClientStreamEvent) error,
	executionID string,
) (*UIToolExecutionResult, error) {
	// Find the tool
	var tool UITool

	for _, t := range PresentationTools() {
		if t.Name() == toolName {
			tool = t

			break
		}
	}

	if tool == nil {
		return nil, fmt.Errorf("presentation tool not found: %s", toolName)
	}

	startTime := time.Now()

	// Execute handler
	result, err := tool.Execute(ctx, params)
	if err != nil {
		return &UIToolExecutionResult{
			ToolName:  toolName,
			Success:   false,
			Error:     err,
			Duration:  time.Since(startTime),
			Timestamp: startTime,
		}, err
	}

	// Create streamer and render
	streamer := NewUIPartStreamer(UIPartStreamerConfig{
		PartType:    tool.GetUIHints().PreferredPartType,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
		Metadata: map[string]any{
			"tool_name": toolName,
		},
	})

	if err := tool.RenderUI(ctx, result, streamer); err != nil {
		return &UIToolExecutionResult{
			ToolName:  toolName,
			Success:   false,
			Result:    result,
			Error:     err,
			Duration:  time.Since(startTime),
			Timestamp: startTime,
		}, err
	}

	finalPart, _ := streamer.BuildFinalPart()

	return &UIToolExecutionResult{
		ToolName:  toolName,
		Success:   true,
		Result:    result,
		Part:      finalPart,
		PartID:    streamer.GetPartID(),
		Duration:  time.Since(startTime),
		Timestamp: startTime,
		Metadata:  streamer.GetAccumulatedData(),
	}, nil
}

// IsPresentationTool checks if a tool name is a built-in presentation tool.
func IsPresentationTool(toolName string) bool {
	for _, t := range PresentationTools() {
		if t.Name() == toolName {
			return true
		}
	}

	return false
}

// GetPresentationToolDescriptions returns a formatted string of all presentation tools
// and their descriptions for use in system prompts.
func GetPresentationToolDescriptions() string {
	var builder strings.Builder
	builder.WriteString("Available presentation tools for displaying data:\n\n")

	for _, tool := range PresentationTools() {
		builder.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name(), tool.Description()))
	}

	return builder.String()
}

// MarshalPresentationToolCall marshals tool call arguments to JSON.
func MarshalPresentationToolCall(params map[string]any) (string, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
