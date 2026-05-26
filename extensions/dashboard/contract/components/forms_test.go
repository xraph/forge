package components

import "testing"

func TestStatCard_basic(t *testing.T) {
	node := StatCard("Total members").
		Value("128").
		Trend(TrendUp).
		Delta(12.5).
		DeltaFormat("percent").
		Build()

	if node.Intent != "molecule.stat-card" {
		t.Fatalf("intent: %q", node.Intent)
	}
	if node.Props["value"] != "128" {
		t.Errorf("value: %v", node.Props["value"])
	}
	if node.Props["trend"] != "up" {
		t.Errorf("trend: %v", node.Props["trend"])
	}
	if node.Props["delta"].(float64) != 12.5 {
		t.Errorf("delta: %v", node.Props["delta"])
	}
}

func TestField_withControl(t *testing.T) {
	node := Field("Email").
		Required(true).
		Description("Used for password recovery").
		Control(Input("email").Type(InputEmail).Build()).
		Build()

	if node.Props["label"] != "Email" {
		t.Errorf("label: %v", node.Props["label"])
	}
	control, ok := node.Slots["control"]
	if !ok || len(control) != 1 {
		t.Fatalf("control slot missing: %+v", node.Slots)
	}
	if control[0].Intent != "atom.input" {
		t.Errorf("control intent: %q", control[0].Intent)
	}
}

func TestDynamicForm_compose(t *testing.T) {
	form := DynamicForm().
		Op("users.create").
		Layout(FormLayoutTwoColumn).
		SubmitLabel("Create user").
		Fields(
			EmailField("email").Label("Email").Required(true).Email().Build(),
			TextField("name").Label("Name").Required(true).MinLength(2).Build(),
			SelectField("role").
				Label("Role").
				Options(
					Option{Label: "Admin", Value: "admin"},
					Option{Label: "Member", Value: "member"},
				).
				Build(),
			SwitchField("active").Label("Active").Default(true).Build(),
		).
		OnSuccess(Navigate("/users")).
		Build()

	if form.Intent != "organism.dynamic-form" {
		t.Fatalf("intent: %q", form.Intent)
	}
	if form.Op != "users.create" {
		t.Errorf("op: %q", form.Op)
	}
	fields, _ := form.Props["fields"].([]any)
	if len(fields) != 4 {
		t.Fatalf("fields: want 4, got %d", len(fields))
	}
	first, _ := fields[0].(map[string]any)
	if first["type"] != "email" {
		t.Errorf("first field type: %v", first["type"])
	}
	rules, _ := first["validate"].([]any)
	hasEmail := false
	for _, r := range rules {
		m, _ := r.(map[string]any)
		if m["kind"] == "email" {
			hasEmail = true
		}
	}
	if !hasEmail {
		t.Errorf("email rule missing: %+v", rules)
	}
}

func TestFieldSpec_visibilityPredicate(t *testing.T) {
	spec := TextField("companyName").
		Label("Company Name").
		Visible(All("form.accountType==business")).
		Build()
	if spec.Visible == nil {
		t.Fatalf("visible predicate not set")
	}
	if spec.Visible.All[0] != "form.accountType==business" {
		t.Errorf("predicate: %+v", spec.Visible)
	}
}

func TestPagination_defaults(t *testing.T) {
	node := Pagination().Total(500).PageSizeOptions(10, 25, 50, 100).ShowFirstLast(true).Build()
	if node.Props["page"].(float64) != 1 {
		t.Errorf("page default: %v", node.Props["page"])
	}
	if node.Props["pageSize"].(float64) != 25 {
		t.Errorf("pageSize default: %v", node.Props["pageSize"])
	}
	opts, _ := node.Props["pageSizeOptions"].([]any)
	if len(opts) != 4 {
		t.Errorf("pageSizeOptions: %v", opts)
	}
}

func TestDropdownMenu_items(t *testing.T) {
	node := DropdownMenu().
		TriggerLabel("Actions").
		Items(
			MenuItem{Label: "Edit", Icon: "pencil", Action: Navigate("/edit")},
			MenuItem{Separator: true},
			MenuItem{Label: "Delete", Icon: "trash", Variant: VariantDestructive, Action: RunCommand("users.delete")},
		).
		Build()
	items, _ := node.Props["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items: want 3, got %d", len(items))
	}
	delete, _ := items[2].(map[string]any)
	if delete["variant"] != "destructive" {
		t.Errorf("delete variant: %v", delete["variant"])
	}
}
