package mcp_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/mcp"
)

// The server keeps tools, resources and prompts in maps keyed by name. Go
// randomises map iteration, so anything that walks one of those maps into a
// slice hands back a different order on every call unless it sorts.
//
// Twelve entries rather than two or three: a map small enough to live in one
// bucket (<= 8 entries) only rotates its iteration order, so a smaller case
// lands on the right answer often enough to pass by luck.
const determinismRuns = 64

func names(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("item-%02d", (i*7)%n))
	}

	return out
}

func newTestServer(t *testing.T) *mcp.Server {
	t.Helper()

	return mcp.NewServer(mcp.DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
}

func TestListTools_Deterministic(t *testing.T) {
	server := newTestServer(t)

	for _, name := range names(12) {
		if err := server.RegisterTool(&mcp.Tool{Name: name, Description: "d"}); err != nil {
			t.Fatalf("RegisterTool(%q): %v", name, err)
		}
	}

	got := func() []string {
		out := make([]string, 0, 12)
		for _, tool := range server.ListTools() {
			out = append(out, tool.Name)
		}

		return out
	}

	want := got()
	if len(want) != 12 {
		t.Fatalf("got %d tools, want 12", len(want))
	}

	if !slices.IsSorted(want) {
		t.Errorf("ListTools is not sorted by name: %v", want)
	}

	for run := range determinismRuns {
		if !slices.Equal(got(), want) {
			t.Fatalf("run %d: ListTools order is not stable\n got: %v\nwant: %v", run, got(), want)
		}
	}
}

func TestListPrompts_Deterministic(t *testing.T) {
	server := newTestServer(t)

	for _, name := range names(12) {
		if err := server.RegisterPrompt(&mcp.Prompt{Name: name, Description: "d"}); err != nil {
			t.Fatalf("RegisterPrompt(%q): %v", name, err)
		}
	}

	got := func() []string {
		out := make([]string, 0, 12)
		for _, p := range server.ListPrompts() {
			out = append(out, p.Name)
		}

		return out
	}

	want := got()

	if !slices.IsSorted(want) {
		t.Errorf("ListPrompts is not sorted by name: %v", want)
	}

	for run := range determinismRuns {
		if !slices.Equal(got(), want) {
			t.Fatalf("run %d: ListPrompts order is not stable\n got: %v\nwant: %v", run, got(), want)
		}
	}
}

func TestListResources_Deterministic(t *testing.T) {
	server := newTestServer(t)

	for _, name := range names(12) {
		res := &mcp.Resource{URI: "file:///" + name, Name: name}
		if err := server.RegisterResource(res); err != nil {
			t.Fatalf("RegisterResource(%q): %v", name, err)
		}
	}

	got := func() []string {
		out := make([]string, 0, 12)
		for _, r := range server.ListResources() {
			out = append(out, r.URI)
		}

		return out
	}

	want := got()

	if !slices.IsSorted(want) {
		t.Errorf("ListResources is not sorted by URI: %v", want)
	}

	for run := range determinismRuns {
		if !slices.Equal(got(), want) {
			t.Fatalf("run %d: ListResources order is not stable\n got: %v\nwant: %v", run, got(), want)
		}
	}
}

// The query string is part of the request URL that ExecuteTool builds. An
// order that changes per call produces two cache entries for one logical
// request, and breaks any upstream that signs the raw query.
func TestExecuteTool_QueryOrderDeterministic(t *testing.T) {
	server := newTestServer(t)

	route := forge.RouteInfo{Method: "GET", Path: "/items", Name: "items-list", Summary: "List"}

	tool, err := server.GenerateToolFromRoute(route)
	if err != nil {
		t.Fatalf("GenerateToolFromRoute: %v", err)
	}

	if err := server.RegisterTool(tool); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	query := map[string]any{}
	for i := range 12 {
		query[fmt.Sprintf("p%02d", (i*7)%12)] = i
	}

	args := map[string]any{"query": query}

	run1, err := server.ExecuteTool(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	_, rawQuery, found := strings.Cut(run1, "?")
	if !found {
		t.Fatalf("no query string in result: %s", run1)
	}

	if !slices.IsSorted(strings.Split(rawQuery, "&")) {
		t.Errorf("query parameters are not in sorted order: %s", rawQuery)
	}

	for run := range determinismRuns {
		got, err := server.ExecuteTool(context.Background(), tool, args)
		if err != nil {
			t.Fatalf("ExecuteTool: %v", err)
		}

		if got != run1 {
			t.Fatalf("run %d: query string is not stable\n got: %s\nwant: %s", run, got, run1)
		}
	}
}

// The generated prompt text goes to a model. A reordering changes the output
// and misses the prompt cache, so identical arguments must render identically.
func TestGeneratePrompt_ArgOrderDeterministic(t *testing.T) {
	server := newTestServer(t)

	prompt := &mcp.Prompt{Name: "summarize", Description: "Summarize things"}
	if err := server.RegisterPrompt(prompt); err != nil {
		t.Fatalf("RegisterPrompt: %v", err)
	}

	args := map[string]any{}
	for i := range 12 {
		args[fmt.Sprintf("a%02d", (i*7)%12)] = i
	}

	text := func() string {
		msgs, err := server.GeneratePrompt(context.Background(), prompt, args)
		if err != nil {
			t.Fatalf("GeneratePrompt: %v", err)
		}

		if len(msgs) == 0 || len(msgs[0].Content) == 0 {
			t.Fatal("GeneratePrompt returned no content")
		}

		return msgs[0].Content[0].Text
	}

	want := text()

	for run := range determinismRuns {
		if got := text(); got != want {
			t.Fatalf("run %d: prompt text is not stable\n got: %s\nwant: %s", run, got, want)
		}
	}
}
