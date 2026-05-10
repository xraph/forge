package dispatcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestIntentRef_StringForm(t *testing.T) {
	r := IntentRef{Intent: "users.list", Version: 1}
	if got := r.String(); got != "users.list@1" {
		t.Errorf("String() = %q", got)
	}
}

func TestResult_HoldsData(t *testing.T) {
	r := &Result{Data: json.RawMessage(`{"ok":true}`), ExtraInvalidates: []string{"x"}}
	if string(r.Data) != `{"ok":true}` {
		t.Errorf("data lost")
	}
	if r.ExtraInvalidates[0] != "x" {
		t.Errorf("invalidates lost")
	}
}

// Compile-time check: a value-conformant function compiles as Handler.
func TestHandlerSignature_Compiles(t *testing.T) {
	var h Handler = func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	}
	_ = h
}
