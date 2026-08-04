package typescript

import (
	"slices"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestOperationKeysFallBackAndStayUnique(t *testing.T) {
	endpoints := []client.Endpoint{
		{ID: "explicit", Method: "GET", Path: "/a"},
		{OperationID: "orders.list", Method: "GET", Path: "/orders"},
		{Method: "GET", Path: "/health"},
		// A spec is free to repeat an operationId. Two operations collapsing
		// onto one key would drop an entry from a `const` object and emit a
		// duplicate `export const`.
		{OperationID: "orders.list", Method: "POST", Path: "/orders"},
		{OperationID: "orders.list", Method: "DELETE", Path: "/orders"},
	}

	got := operationKeys(endpoints)
	want := []string{"explicit", "orders.list", "get.health", "orders.list2", "orders.list3"}

	if !slices.Equal(got, want) {
		t.Fatalf("operationKeys = %v, want %v", got, want)
	}
}

func TestHookNamesStayUniqueAcrossSeparatorVariants(t *testing.T) {
	// Distinct object keys, one PascalCase result: toPascal collapses `-` and
	// `_` alike, so without a second uniqueness pass hooks.ts would declare
	// `useListOrders` twice.
	got := hookNames([]string{"list-orders", "list_orders", "listOrders"})
	want := []string{"useListOrders", "useListOrders2", "useListOrders3"}

	if !slices.Equal(got, want) {
		t.Fatalf("hookNames = %v, want %v", got, want)
	}
}

func TestTSMemberUsesBracketsForNonIdentifiers(t *testing.T) {
	cases := map[string]string{
		"orderList":         "ops.orderList",
		"orders.list":       "ops['orders.list']",
		"list-orders":       "ops['list-orders']",
		"":                  "ops['']",
		"2legit":            "ops['2legit']",
		`quote'in\the.name`: `ops['quote\'in\\the.name']`,
	}

	for key, want := range cases {
		if got := tsMember("ops", key); got != want {
			t.Errorf("tsMember(ops, %q) = %s, want %s", key, got, want)
		}
	}
}
