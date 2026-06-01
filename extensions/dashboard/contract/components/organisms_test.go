package components

import "testing"

func TestDataGrid_basic(t *testing.T) {
	node := DataGrid().
		Data(Query("members.list")).
		Columns(
			GridColumn{ID: "name", Field: "name", Header: "Name", Sortable: true},
			GridColumn{ID: "email", Field: "email", Header: "Email", Filterable: true},
			GridColumn{ID: "role", Field: "role", Header: "Role", Type: ColBadge},
		).
		Selection(SelectionMultiCfg()).
		PaginationFromBuilder(ServerPagination().PageSize(50)).
		Density(DensityCompact).
		StickyHeader(true).
		Toolbar(&ToolbarConfig{Search: true, ColumnPicker: true, DensityToggle: true}).
		Build()

	if node.Intent != "organism.data-grid" {
		t.Fatalf("intent: %q", node.Intent)
	}
	cols, _ := node.Props["columns"].([]any)
	if len(cols) != 3 {
		t.Errorf("columns: want 3, got %d", len(cols))
	}
	sel, _ := node.Props["selection"].(map[string]any)
	if sel["mode"] != "multi" {
		t.Errorf("selection.mode: %v", sel["mode"])
	}
	pag, _ := node.Props["pagination"].(map[string]any)
	if pag["mode"] != "server" {
		t.Errorf("pagination.mode: %v", pag["mode"])
	}
	if pag["pageSize"].(float64) != 50 {
		t.Errorf("pageSize: %v", pag["pageSize"])
	}
	if node.Data == nil || node.Data.QueryRef != "members.list" {
		t.Errorf("data binding: %+v", node.Data)
	}
}

func TestChart_seriesTokens(t *testing.T) {
	node := Chart(ChartLine).
		XAxis("timestamp").
		Series(
			ChartSeries{Key: "cpu", Label: "CPU", ColorToken: "chart-1"},
			ChartSeries{Key: "mem", Label: "Memory", ColorToken: "chart-2"},
		).
		Height(240).
		Build()

	series, _ := node.Props["series"].([]any)
	if len(series) != 2 {
		t.Fatalf("series: %v", series)
	}
	first, _ := series[0].(map[string]any)
	if first["colorToken"] != "chart-1" {
		t.Errorf("color token: %v", first["colorToken"])
	}
}

func TestKanban_compose(t *testing.T) {
	node := Kanban().
		StatusField("status").
		CardIntent("molecule.list-item").
		Columns(
			KanbanColumn{ID: "todo", Title: "To do", Accept: []string{"todo"}},
			KanbanColumn{ID: "doing", Title: "In progress", Accept: []string{"doing"}, Limit: 5},
			KanbanColumn{ID: "done", Title: "Done", Accept: []string{"done"}},
		).
		OnMove(RunCommand("tasks.move")).
		Build()

	cols, _ := node.Props["columns"].([]any)
	if len(cols) != 3 {
		t.Errorf("columns: %v", cols)
	}
	if node.Props["statusField"] != "status" {
		t.Errorf("statusField: %v", node.Props["statusField"])
	}
}

func TestStepper_panels(t *testing.T) {
	node := Stepper().
		Steps(
			StepperStep{ID: "info", Title: "Account info"},
			StepperStep{ID: "billing", Title: "Billing", Optional: true},
			StepperStep{ID: "review", Title: "Review"},
		).
		Initial("info").
		Panel("info", Text("info panel").Build()).
		Panel("billing", Text("billing panel").Build()).
		Build()

	if node.Props["initial"] != "info" {
		t.Errorf("initial: %v", node.Props["initial"])
	}
	if _, ok := node.Slots["info"]; !ok {
		t.Errorf("info slot missing")
	}
	if _, ok := node.Slots["billing"]; !ok {
		t.Errorf("billing slot missing")
	}
}
