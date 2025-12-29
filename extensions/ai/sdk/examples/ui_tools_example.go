// Package examples demonstrates the UI Tools Streaming SDK usage.
//
// This file contains examples for:
// - Creating UI tools that render as tables, charts, metrics
// - Progressive streaming of UI components
// - Using the StreamBuilder with UI tools
// - Inline citations with streaming
package examples

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk"
)

// =============================================================================
// Example 1: Creating a Table UI Tool for Database Queries
// =============================================================================

// DataQueryTool executes SQL queries and renders results as streaming tables.
type DataQueryTool struct {
	db *sql.DB
}

// NewDataQueryTool creates a new database query tool.
func NewDataQueryTool(db *sql.DB) *sdk.TableUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		query, ok := params["query"].(string)
		if !ok {
			return nil, fmt.Errorf("query parameter is required")
		}

		// Execute query (simplified example)
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		// Get column names
		columns, _ := rows.Columns()

		// Collect results
		var results []map[string]any
		for rows.Next() {
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, err
			}

			row := make(map[string]any)
			for i, col := range columns {
				row[col] = values[i]
			}
			results = append(results, row)
		}

		return results, nil
	}

	return sdk.NewTableUITool("sql_query", "Execute SQL queries and display results", handler, sdk.TableUIConfig{
		BatchSize:  20, // Stream 20 rows at a time
		Sortable:   true,
		Searchable: true,
		Paginated:  true,
		PageSize:   50,
	})
}

// =============================================================================
// Example 2: Creating a Metrics Dashboard Tool
// =============================================================================

// DashboardMetrics represents system metrics.
type DashboardMetrics struct {
	ActiveUsers    int64
	Revenue        float64
	OrdersToday    int64
	ConversionRate float64
}

// NewMetricsDashboardTool creates a tool that displays system metrics.
func NewMetricsDashboardTool(getMetrics func(ctx context.Context) (*DashboardMetrics, error)) *sdk.MetricsUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return getMetrics(ctx)
	}

	metricsBuilder := func(result any) []sdk.Metric {
		m, ok := result.(*DashboardMetrics)
		if !ok {
			return nil
		}

		return []sdk.Metric{
			{
				ID:             "active_users",
				Label:          "Active Users",
				Value:          m.ActiveUsers,
				FormattedValue: fmt.Sprintf("%d", m.ActiveUsers),
				Icon:           "users",
				Color:          "#3B82F6",
				Trend: &sdk.MetricTrend{
					Direction:  sdk.TrendUp,
					Value:      125,
					Percentage: 12.5,
					Period:     "vs last week",
				},
				Status: sdk.MetricStatusGood,
			},
			{
				ID:             "revenue",
				Label:          "Revenue",
				Value:          m.Revenue,
				FormattedValue: fmt.Sprintf("$%.2f", m.Revenue),
				Unit:           "$",
				Icon:           "dollar-sign",
				Color:          "#10B981",
				Trend: &sdk.MetricTrend{
					Direction:  sdk.TrendUp,
					Value:      5000,
					Percentage: 8.3,
					Period:     "vs last month",
				},
				Status: sdk.MetricStatusGood,
			},
			{
				ID:             "orders_today",
				Label:          "Orders Today",
				Value:          m.OrdersToday,
				FormattedValue: fmt.Sprintf("%d", m.OrdersToday),
				Icon:           "shopping-cart",
				Color:          "#F59E0B",
			},
			{
				ID:             "conversion_rate",
				Label:          "Conversion Rate",
				Value:          m.ConversionRate,
				FormattedValue: fmt.Sprintf("%.1f%%", m.ConversionRate),
				Unit:           "%",
				Icon:           "trending-up",
				Color:          "#8B5CF6",
				Target: &sdk.MetricTarget{
					Value:    5.0,
					Label:    "Target",
					Progress: (m.ConversionRate / 5.0) * 100,
					Achieved: m.ConversionRate >= 5.0,
				},
			},
		}
	}

	return sdk.NewMetricsUITool("system_metrics", "Display system metrics dashboard", handler, metricsBuilder)
}

// =============================================================================
// Example 3: Creating a Chart Tool for Sales Analytics
// =============================================================================

// SalesData represents monthly sales data.
type SalesData struct {
	Months  []string
	Revenue []float64
	Orders  []float64
}

// NewSalesChartTool creates a tool that displays sales charts.
func NewSalesChartTool(getSales func(ctx context.Context, year int) (*SalesData, error)) *sdk.ChartUITool {
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		year := int(time.Now().Year())
		if y, ok := params["year"].(float64); ok {
			year = int(y)
		}
		return getSales(ctx, year)
	}

	dataBuilder := func(result any) sdk.ChartData {
		data, ok := result.(*SalesData)
		if !ok {
			return sdk.ChartData{}
		}

		return sdk.ChartData{
			Labels: data.Months,
			Datasets: []sdk.ChartDataset{
				{
					Label:           "Revenue ($)",
					Data:            data.Revenue,
					BackgroundColor: "#3B82F6",
					BorderColor:     "#2563EB",
				},
				{
					Label:           "Orders",
					Data:            data.Orders,
					BackgroundColor: "#10B981",
					BorderColor:     "#059669",
				},
			},
		}
	}

	return sdk.NewChartUITool("sales_chart", "Display sales analytics chart", sdk.ChartLine, handler, dataBuilder)
}

// =============================================================================
// Example 4: Custom UI Tool with Button Actions
// =============================================================================

// NewTaskManagerTool creates a tool that manages tasks with interactive buttons.
func NewTaskManagerTool() *sdk.BaseUITool {
	renderFunc := func(ctx context.Context, result any, streamer *sdk.UIPartStreamer) error {
		tasks, ok := result.([]Task)
		if !ok {
			return fmt.Errorf("invalid result type")
		}

		// Start streaming
		if err := streamer.Start(); err != nil {
			return err
		}

		// Stream title
		if err := streamer.StreamSection("title", "Task Manager"); err != nil {
			return err
		}

		// Convert tasks to kanban columns
		columns := []sdk.KanbanColumn{
			{ID: "todo", Title: "To Do", Color: "#EF4444", Cards: []sdk.KanbanCard{}},
			{ID: "in_progress", Title: "In Progress", Color: "#F59E0B", Cards: []sdk.KanbanCard{}},
			{ID: "done", Title: "Done", Color: "#10B981", Cards: []sdk.KanbanCard{}},
		}

		for _, task := range tasks {
			card := sdk.KanbanCard{
				ID:          task.ID,
				Title:       task.Title,
				Description: task.Description,
				Priority:    task.Priority,
				Actions: []sdk.Button{
					sdk.NewButton("edit_"+task.ID, "Edit").
						WithIcon("edit").
						WithVariant(sdk.ButtonOutline).
						WithToolAction("edit_task", map[string]any{"task_id": task.ID}).
						Build(),
					sdk.NewButton("delete_"+task.ID, "Delete").
						WithIcon("trash").
						WithVariant(sdk.ButtonDanger).
						WithToolAction("delete_task", map[string]any{"task_id": task.ID}).
						WithConfirm("Delete Task", "Are you sure you want to delete this task?").
						Build(),
				},
			}

			switch task.Status {
			case "todo":
				columns[0].Cards = append(columns[0].Cards, card)
			case "in_progress":
				columns[1].Cards = append(columns[1].Cards, card)
			case "done":
				columns[2].Cards = append(columns[2].Cards, card)
			}
		}

		// Stream columns progressively
		for _, col := range columns {
			if err := streamer.StreamColumns(col); err != nil {
				return err
			}
		}

		// Stream action buttons
		actions := []sdk.Button{
			sdk.NewButton("add_task", "Add Task").
				WithIcon("plus").
				WithVariant(sdk.ButtonPrimary).
				WithToolAction("create_task", nil).
				Build(),
			sdk.NewButton("refresh", "Refresh").
				WithIcon("refresh").
				WithVariant(sdk.ButtonSecondary).
				WithToolAction("list_tasks", nil).
				Build(),
		}
		if err := streamer.StreamActions(actions); err != nil {
			return err
		}

		return streamer.End()
	}

	return sdk.NewBaseUITool(sdk.BaseUIToolConfig{
		Name:        "task_manager",
		Description: "Interactive task management board",
		Hints: sdk.UIToolHints{
			PreferredPartType: sdk.PartTypeKanban,
			StreamingEnabled:  true,
			ActionHandlers: []sdk.ActionHandler{
				{ID: "create_task", Name: "Create Task", ToolName: "create_task"},
				{ID: "edit_task", Name: "Edit Task", ToolName: "edit_task"},
				{ID: "delete_task", Name: "Delete Task", ToolName: "delete_task", RequiresConfirmation: true},
			},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			// Return mock tasks
			return []Task{
				{ID: "1", Title: "Design UI", Status: "done", Priority: "high"},
				{ID: "2", Title: "Implement API", Status: "in_progress", Priority: "high"},
				{ID: "3", Title: "Write tests", Status: "todo", Priority: "medium"},
			}, nil
		},
		RenderFunc: renderFunc,
	})
}

// Task represents a task item.
type Task struct {
	ID          string
	Title       string
	Description string
	Status      string
	Priority    string
}

// =============================================================================
// Example 5: Using StreamBuilder with UI Tools
// =============================================================================

// ExampleStreamBuilderWithUITools demonstrates using StreamBuilder with UI tools.
func ExampleStreamBuilderWithUITools(
	ctx context.Context,
	aiExt interface {
		Stream(ctx context.Context) *sdk.StreamBuilder
	},
	onSSE func(event llm.ClientStreamEvent) error,
) error {
	// Create UI tool registry
	registry := sdk.NewUIToolRegistry(nil, nil)

	// Register tools
	_ = registry.RegisterUITool(NewTaskManagerTool())

	// Use StreamBuilder with UI tools
	result, err := aiExt.Stream(ctx).
		WithPrompt("Show me my tasks").
		WithUIToolRegistry(registry).
		WithUIToolRendering(true).
		OnUIPartStart(func(partID, partType string) {
			fmt.Printf("UI Part started: %s (%s)\n", partID, partType)
		}).
		OnUIPartDelta(func(partID, section string, data any) {
			fmt.Printf("UI Part delta: %s.%s\n", partID, section)
		}).
		OnUIPartEnd(func(partID string, part sdk.ContentPart) {
			fmt.Printf("UI Part completed: %s\n", partID)
		}).
		OnStreamEvent(func(event llm.ClientStreamEvent) {
			onSSE(event) // Forward to SSE
		}).
		Stream()

	if err != nil {
		return err
	}

	fmt.Printf("Streamed %d UI parts\n", len(result.UIParts))
	return nil
}

// =============================================================================
// Example 6: Progressive Table Streaming
// =============================================================================

// ExampleProgressiveTableStreaming demonstrates streaming a large table progressively.
func ExampleProgressiveTableStreaming(
	ctx context.Context,
	executionID string,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	// Define headers
	headers := []sdk.TableHeader{
		{Label: "ID", Key: "id", Sortable: true},
		{Label: "Name", Key: "name", Sortable: true},
		{Label: "Email", Key: "email"},
		{Label: "Status", Key: "status"},
		{Label: "Created", Key: "created_at", Sortable: true},
	}

	// Generate large dataset
	rows := make([][]sdk.TableCell, 100)
	for i := 0; i < 100; i++ {
		rows[i] = []sdk.TableCell{
			{Value: i + 1, Display: fmt.Sprintf("%d", i+1)},
			{Value: fmt.Sprintf("User %d", i+1), Display: fmt.Sprintf("User %d", i+1)},
			{Value: fmt.Sprintf("user%d@example.com", i+1), Display: fmt.Sprintf("user%d@example.com", i+1)},
			{Value: "active", Display: "Active"},
			{Value: time.Now().Add(-time.Duration(i) * time.Hour), Display: time.Now().Add(-time.Duration(i) * time.Hour).Format("2006-01-02")},
		}
	}

	// Stream the table progressively
	return sdk.StreamTable(ctx, executionID, "User List", headers, rows, onEvent)
}

// =============================================================================
// Example 7: Timeline with Events
// =============================================================================

// ExampleTimelineStreaming demonstrates streaming a timeline of events.
func ExampleTimelineStreaming(
	ctx context.Context,
	executionID string,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	events := []sdk.TimelineEvent{
		{
			ID:          "1",
			Title:       "Project Started",
			Description: "Initial project kickoff meeting",
			Timestamp:   time.Now().Add(-30 * 24 * time.Hour),
			Icon:        "rocket",
			Color:       "#3B82F6",
			Status:      sdk.TimelineStatusCompleted,
		},
		{
			ID:          "2",
			Title:       "Design Phase",
			Description: "UI/UX design completed",
			Timestamp:   time.Now().Add(-20 * 24 * time.Hour),
			Icon:        "pencil",
			Color:       "#8B5CF6",
			Status:      sdk.TimelineStatusCompleted,
		},
		{
			ID:          "3",
			Title:       "Development",
			Description: "Core features implementation",
			Timestamp:   time.Now().Add(-10 * 24 * time.Hour),
			Icon:        "code",
			Color:       "#F59E0B",
			Status:      sdk.TimelineStatusInProgress,
		},
		{
			ID:          "4",
			Title:       "Testing",
			Description: "QA and bug fixes",
			Timestamp:   time.Now().Add(5 * 24 * time.Hour),
			Icon:        "check-circle",
			Color:       "#10B981",
			Status:      sdk.TimelineStatusPending,
		},
		{
			ID:          "5",
			Title:       "Launch",
			Description: "Product launch",
			Timestamp:   time.Now().Add(15 * 24 * time.Hour),
			Icon:        "flag",
			Color:       "#EF4444",
			Status:      sdk.TimelineStatusPending,
		},
	}

	return sdk.StreamTimeline(ctx, executionID, "Project Timeline", events, onEvent)
}

// =============================================================================
// Example 8: Metrics Dashboard
// =============================================================================

// ExampleMetricsStreaming demonstrates streaming a metrics dashboard.
func ExampleMetricsStreaming(
	ctx context.Context,
	executionID string,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	metrics := []sdk.Metric{
		{
			ID:             "total_users",
			Label:          "Total Users",
			Value:          125430,
			FormattedValue: "125.4K",
			Icon:           "users",
			Color:          "#3B82F6",
			Trend: &sdk.MetricTrend{
				Direction:  sdk.TrendUp,
				Percentage: 12.5,
				Period:     "vs last month",
			},
			Sparkline: []float64{100, 105, 108, 112, 118, 125},
		},
		{
			ID:             "revenue",
			Label:          "Monthly Revenue",
			Value:          89420.50,
			FormattedValue: "$89.4K",
			Unit:           "$",
			Icon:           "dollar-sign",
			Color:          "#10B981",
			Trend: &sdk.MetricTrend{
				Direction:  sdk.TrendUp,
				Percentage: 8.3,
				Period:     "vs last month",
			},
		},
		{
			ID:             "active_sessions",
			Label:          "Active Sessions",
			Value:          3842,
			FormattedValue: "3,842",
			Icon:           "activity",
			Color:          "#F59E0B",
		},
		{
			ID:             "error_rate",
			Label:          "Error Rate",
			Value:          0.12,
			FormattedValue: "0.12%",
			Unit:           "%",
			Icon:           "alert-circle",
			Color:          "#EF4444",
			Status:         sdk.MetricStatusGood,
			Target: &sdk.MetricTarget{
				Value:    1.0,
				Label:    "SLA Target",
				Progress: 88,
				Achieved: true,
			},
		},
	}

	return sdk.StreamMetrics(ctx, executionID, "System Dashboard", metrics, onEvent)
}

// =============================================================================
// Example 9: Interactive Form
// =============================================================================

// ExampleFormStreaming demonstrates streaming an interactive form.
func ExampleFormStreaming(
	ctx context.Context,
	executionID string,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	streamer := sdk.NewUIPartStreamer(sdk.UIPartStreamerConfig{
		PartType:    sdk.PartTypeForm,
		ExecutionID: executionID,
		OnEvent:     onEvent,
		Context:     ctx,
	})

	if err := streamer.Start(); err != nil {
		return err
	}

	// Stream form metadata
	if err := streamer.StreamSection("id", "user_registration"); err != nil {
		return err
	}
	if err := streamer.StreamSection("title", "User Registration"); err != nil {
		return err
	}
	if err := streamer.StreamSection("description", "Create a new user account"); err != nil {
		return err
	}

	// Stream fields progressively
	fields := []sdk.FormField{
		{
			ID:          "name",
			Name:        "name",
			Label:       "Full Name",
			Type:        sdk.FieldTypeText,
			Placeholder: "Enter your full name",
			Required:    true,
			Validation:  &sdk.FieldValidation{MinLength: intPtr(2), MaxLength: intPtr(100)},
		},
		{
			ID:          "email",
			Name:        "email",
			Label:       "Email Address",
			Type:        sdk.FieldTypeEmail,
			Placeholder: "you@example.com",
			Required:    true,
		},
		{
			ID:          "password",
			Name:        "password",
			Label:       "Password",
			Type:        sdk.FieldTypePassword,
			Placeholder: "Create a strong password",
			Required:    true,
			Validation:  &sdk.FieldValidation{MinLength: intPtr(8)},
		},
		{
			ID:       "role",
			Name:     "role",
			Label:    "Role",
			Type:     sdk.FieldTypeSelect,
			Required: true,
			Options: []sdk.FormFieldOption{
				{Value: "user", Label: "User"},
				{Value: "admin", Label: "Administrator"},
				{Value: "moderator", Label: "Moderator"},
			},
			DefaultValue: "user",
		},
		{
			ID:          "bio",
			Name:        "bio",
			Label:       "Bio",
			Type:        sdk.FieldTypeTextarea,
			Placeholder: "Tell us about yourself",
			Width:       "full",
		},
		{
			ID:    "notifications",
			Name:  "notifications",
			Label: "Email Notifications",
			Type:  sdk.FieldTypeToggle,
		},
	}

	for _, field := range fields {
		if err := streamer.StreamSection("fields", field); err != nil {
			return err
		}
	}

	// Stream action buttons
	actions := []sdk.Button{
		sdk.NewButton("submit", "Create Account").
			WithVariant(sdk.ButtonPrimary).
			WithIcon("user-plus").
			Build(),
		sdk.NewButton("cancel", "Cancel").
			WithVariant(sdk.ButtonSecondary).
			Build(),
	}
	if err := streamer.StreamActions(actions); err != nil {
		return err
	}

	return streamer.End()
}

func intPtr(i int) *int {
	return &i
}

// =============================================================================
// Example 10: Inline Citations with Streaming
// =============================================================================

// ExampleCitationStreaming demonstrates streaming content with inline citations.
func ExampleCitationStreaming(
	ctx context.Context,
	executionID string,
	onEvent func(llm.ClientStreamEvent) error,
) error {
	builder := sdk.NewCitedContentBuilder(sdk.CitationStyleNumeric)

	// Build content with citations
	builder.WriteText("According to recent research, ")
	builder.WriteTextWithCitation("AI systems are becoming increasingly capable of complex reasoning", &sdk.Source{
		Title:      "Advances in AI Reasoning",
		URL:        "https://example.com/ai-research",
		Author:     "Smith et al.",
		Type:       "article",
		Confidence: 0.95,
	})
	builder.WriteText(". Furthermore, ")
	builder.WriteTextWithCitation("large language models have shown remarkable abilities in code generation", &sdk.Source{
		Title:      "LLMs for Code Generation",
		URL:        "https://example.com/llm-code",
		Author:     "Johnson, A.",
		Type:       "paper",
		Confidence: 0.88,
	})
	builder.WriteText(".")

	part := builder.Build()

	// Stream the citation part
	event := llm.NewUIPartStartEvent(executionID, "citations", string(sdk.PartTypeInlineCitation))
	event.PartData = part
	if err := onEvent(event); err != nil {
		return err
	}

	endEvent := llm.NewUIPartEndEvent(executionID, "citations")
	return onEvent(endEvent)
}

// =============================================================================
// Example 11: HTTP Handler for SSE Streaming
// =============================================================================

// ExampleSSEHandler demonstrates an HTTP handler that streams UI parts via SSE.
func ExampleSSEHandler(logger forge.Logger) func(w interface{ Write([]byte) (int, error) }, ctx context.Context) {
	return func(w interface{ Write([]byte) (int, error) }, ctx context.Context) {
		executionID := fmt.Sprintf("exec_%d", time.Now().UnixNano())

		// SSE event sender
		sendSSE := func(event llm.ClientStreamEvent) error {
			data, _ := event.PartData.([]byte)
			if data == nil {
				data = []byte("{}")
			}
			_, err := w.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Type, data)))
			return err
		}

		// Stream metrics example
		_ = ExampleMetricsStreaming(ctx, executionID, sendSSE)
	}
}

// =============================================================================
// Example 12: Full Integration Example
// =============================================================================

// FullIntegrationExample shows a complete example with registry, tools, and streaming.
func FullIntegrationExample(ctx context.Context, logger forge.Logger, metrics forge.Metrics) error {
	// Create registries
	uiRegistry := sdk.NewUIToolRegistry(logger, metrics)

	// Register various UI tools
	_ = uiRegistry.RegisterUITool(NewTaskManagerTool())

	// Create a custom analytics tool
	analyticsTool := sdk.NewUITool("analytics", "Show analytics dashboard").
		WithPreferredPartType(sdk.PartTypeMetric).
		WithStreaming(true).
		WithHandler(func(ctx context.Context, params map[string]any) (any, error) {
			return []sdk.Metric{
				{Label: "Views", Value: 10000},
				{Label: "Clicks", Value: 500},
				{Label: "CTR", Value: 5.0, Unit: "%"},
			}, nil
		}).
		WithRenderFunc(func(ctx context.Context, result any, streamer *sdk.UIPartStreamer) error {
			metrics := result.([]sdk.Metric)
			_ = streamer.Start()
			for _, m := range metrics {
				_ = streamer.StreamSection("metrics", m)
			}
			return streamer.End()
		}).
		Build()

	_ = uiRegistry.RegisterUITool(analyticsTool)

	// Execute a UI tool with streaming
	result, err := uiRegistry.ExecuteUITool(
		ctx,
		"analytics",
		map[string]any{},
		func(event llm.ClientStreamEvent) error {
			logger.Debug("SSE Event",
				sdk.F("type", event.Type),
				sdk.F("part_id", event.PartID),
				sdk.F("section", event.Section),
			)
			return nil
		},
		"demo-execution",
	)

	if err != nil {
		return err
	}

	logger.Info("UI Tool execution completed",
		sdk.F("tool", result.ToolName),
		sdk.F("success", result.Success),
		sdk.F("duration", result.Duration),
		sdk.F("part_id", result.PartID),
	)

	return nil
}
