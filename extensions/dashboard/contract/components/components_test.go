package components

import (
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestRow_basic(t *testing.T) {
	node := Row(
		Text("hello").Build(),
		Text("world").Build(),
	).Gap(SpacingMd).Align(AlignCenter).Build()

	if node.Intent != "layout.row" {
		t.Fatalf("intent: want layout.row, got %q", node.Intent)
	}
	if got, ok := node.Props["gap"]; !ok || got != "4" {
		t.Errorf("props.gap: want \"4\", got %v", got)
	}
	if got, ok := node.Props["align"]; !ok || got != "center" {
		t.Errorf("props.align: want \"center\", got %v", got)
	}
	children, ok := node.Slots["children"]
	if !ok {
		t.Fatalf("slots.children missing")
	}
	if len(children) != 2 {
		t.Errorf("children: want 2, got %d", len(children))
	}
}

func TestCommonFluent(t *testing.T) {
	pred := All("user.admin")
	node := Row().
		Title("Header").
		VisibleWhen(pred).
		Op("users.delete").
		Build()

	if node.Title != "Header" {
		t.Errorf("title: want Header, got %q", node.Title)
	}
	if node.VisibleWhen == nil || node.VisibleWhen.All[0] != "user.admin" {
		t.Errorf("visibleWhen not propagated: %+v", node.VisibleWhen)
	}
	if node.Op != "users.delete" {
		t.Errorf("op: want users.delete, got %q", node.Op)
	}
}

func TestCard_slots(t *testing.T) {
	node := Card(Text("body").Build()).
		Title("Header text").
		Description("Subtitle").
		Header(Badge("New").Build()).
		Footer(Button("Save").Build()).
		Build()

	if got := node.Title; got != "Header text" {
		t.Errorf("node.title: %q", got)
	}
	// Content should be the children slot routed to "content" for card.
	if _, ok := node.Slots["content"]; !ok {
		t.Errorf("content slot missing: %v", node.Slots)
	}
	if _, ok := node.Slots["header"]; !ok {
		t.Errorf("header slot missing")
	}
	if _, ok := node.Slots["footer"]; !ok {
		t.Errorf("footer slot missing")
	}
}

func TestPage_full(t *testing.T) {
	node := Page("Members").
		Description("Manage team membership").
		Crumb("Home", "/").
		Crumb("Members", "/members").
		Actions(Button("Invite").Build()).
		Children(Text("rows").Build()).
		Build()

	if node.Intent != "layout.page" {
		t.Fatalf("intent: %q", node.Intent)
	}
	if node.Title != "Members" {
		t.Errorf("title: %q", node.Title)
	}
	crumbs, _ := node.Props["breadcrumbs"].([]any)
	if len(crumbs) != 2 {
		t.Errorf("breadcrumbs: %v", crumbs)
	}
	if _, ok := node.Slots["actions"]; !ok {
		t.Errorf("actions slot missing")
	}
	if _, ok := node.Slots["content"]; !ok {
		t.Errorf("content slot missing (children should route to content)")
	}
}

func TestButton_variant(t *testing.T) {
	node := Button("Delete").
		Variant(VariantDestructive).
		Size(SizeLg).
		LeadingIcon("trash").
		Confirm("Are you sure?").
		Op("users.delete").
		Build()

	if node.Op != "users.delete" {
		t.Errorf("op: %q", node.Op)
	}
	if got := node.Props["variant"]; got != "destructive" {
		t.Errorf("variant: %v", got)
	}
	if got := node.Props["size"]; got != "lg" {
		t.Errorf("size: %v", got)
	}
	if got := node.Props["leadingIcon"]; got != "trash" {
		t.Errorf("leadingIcon: %v", got)
	}
}

func TestAction_serialization(t *testing.T) {
	a := Navigate("/users").WithContributor("auth")
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["kind"] != "navigate" {
		t.Errorf("kind: %v", back["kind"])
	}
	if back["href"] != "/users" {
		t.Errorf("href: %v", back["href"])
	}
}

func TestDataBinding_helpers(t *testing.T) {
	d := Intent("users.list", map[string]contract.ParamSource{
		"orgId": FromRoute("orgId"),
		"limit": Param(25),
	})

	if d.Intent != "users.list" {
		t.Errorf("intent: %q", d.Intent)
	}
	if d.Params["orgId"].From != "route.orgId" {
		t.Errorf("orgId param: %+v", d.Params["orgId"])
	}
	if d.Params["limit"].Value != 25 {
		t.Errorf("limit param value: %+v", d.Params["limit"])
	}
}

func TestGrid_responsive(t *testing.T) {
	node := Grid(
		Badge("a").Build(),
		Badge("b").Build(),
	).Responsive(ResponsiveCols{Base: 1, Md: 2, Lg: 4}).Gap(SpacingMd).Build()

	cols, ok := node.Props["cols"].(map[string]any)
	if !ok {
		t.Fatalf("cols not a map: %T", node.Props["cols"])
	}
	if cols["base"].(float64) != 1 || cols["md"].(float64) != 2 || cols["lg"].(float64) != 4 {
		t.Errorf("cols: %+v", cols)
	}
}

func TestTabs_panels(t *testing.T) {
	node := Tabs().
		Item("general", "General").
		Item("members", "Members").
		Panel("general", Text("g").Build()).
		Panel("members", Text("m").Build()).
		Default("general").
		Build()

	items, _ := node.Props["items"].([]any)
	if len(items) != 2 {
		t.Errorf("items: %v", items)
	}
	if _, ok := node.Slots["general"]; !ok {
		t.Errorf("general slot missing")
	}
	if _, ok := node.Slots["members"]; !ok {
		t.Errorf("members slot missing")
	}
}

func TestNoEmptyPropsMap(t *testing.T) {
	// A builder with no fields set should not emit an empty props map.
	node := Spinner().Build()
	if node.Props != nil && len(node.Props) == 0 {
		t.Errorf("expected nil/omitempty props, got empty map: %v", node.Props)
	}
}

func TestCheckbox_pointerSemantics(t *testing.T) {
	// Pointer-bool fields should round-trip false correctly (not be dropped
	// by omitempty).
	node := Checkbox("agree").Checked(false).Build()
	raw, _ := json.Marshal(node)
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	props, _ := back["props"].(map[string]any)
	if got, ok := props["checked"]; !ok || got != false {
		t.Errorf("checked=false should round-trip; got %v ok=%v", got, ok)
	}
}
