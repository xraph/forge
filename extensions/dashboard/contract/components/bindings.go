package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// Query returns a DataBinding referencing a named query declared in the
// contributor's manifest.
//
//	Query("members.list")
func Query(name string) *contract.DataBinding {
	return &contract.DataBinding{QueryRef: name}
}

// Intent returns an inline DataBinding for the named intent with optional
// parameters.
//
//	Intent("members.list", map[string]contract.ParamSource{
//	    "orgId": FromRoute("orgId"),
//	})
func Intent(name string, params ...map[string]contract.ParamSource) *contract.DataBinding {
	d := &contract.DataBinding{Intent: name}
	if len(params) > 0 {
		d.Params = params[0]
	}
	return d
}

// Param returns a ParamSource carrying a literal value.
func Param(v any) contract.ParamSource {
	return contract.ParamSource{Value: v}
}

// FromRoute returns a ParamSource that pulls its value from the named
// route parameter at dispatch time (e.g., FromRoute("id") for "/users/:id").
func FromRoute(name string) contract.ParamSource {
	return contract.ParamSource{From: "route." + name}
}

// FromParent returns a ParamSource that pulls its value from the parent
// node's data context (e.g., the row in a list).
func FromParent(field string) contract.ParamSource {
	return contract.ParamSource{From: "parent." + field}
}

// FromSession returns a ParamSource that pulls its value from the current
// session (e.g., FromSession("user.id")).
func FromSession(field string) contract.ParamSource {
	return contract.ParamSource{From: "session." + field}
}

// FromState returns a ParamSource that pulls its value from the page state.
func FromState(key string) contract.ParamSource {
	return contract.ParamSource{From: "state." + key}
}

// Predicate helpers — these mirror the contract.Predicate shape with a
// readable Go API.

// All returns a Predicate that requires every named check to pass.
func All(checks ...string) *contract.Predicate {
	return &contract.Predicate{All: checks}
}

// Any returns a Predicate that requires at least one check to pass.
func Any(checks ...string) *contract.Predicate {
	return &contract.Predicate{Any: checks}
}

// Not returns a Predicate that requires none of the checks to pass.
func Not(checks ...string) *contract.Predicate {
	return &contract.Predicate{Not: checks}
}

// Warden returns a Predicate that delegates the access decision to the
// named Warden hook.
func Warden(name string) *contract.Predicate {
	return &contract.Predicate{Warden: name}
}
