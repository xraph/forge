package golang

import (
	"slices"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// TestOperationKeysFallBackAndStayUnique mirrors the TypeScript generator's
// own opkeys_test.go: the same fallback chain (ID, then OperationID, then a
// path-derived name) has to hold here too, since the Go capability surface's
// per-operation requirement table is keyed the same way the TypeScript one
// is, and a spec is free to repeat an operationId.
func TestOperationKeysFallBackAndStayUnique(t *testing.T) {
	endpoints := []client.Endpoint{
		{ID: "explicit", Method: "GET", Path: "/a"},
		{OperationID: "orders.list", Method: "GET", Path: "/orders"},
		{Method: "GET", Path: "/health"},
		// Two operations collapsing onto one key would drop an entry from the
		// operationRequirements map and emit a duplicate OperationName const.
		{OperationID: "orders.list", Method: "POST", Path: "/orders"},
		{OperationID: "orders.list", Method: "DELETE", Path: "/orders"},
	}

	got := operationKeys(endpoints)
	want := []string{"explicit", "orders.list", "get.health", "orders.list2", "orders.list3"}

	if !slices.Equal(got, want) {
		t.Fatalf("operationKeys = %v, want %v", got, want)
	}
}
