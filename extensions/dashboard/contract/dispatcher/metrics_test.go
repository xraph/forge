package dispatcher

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type recordingMetrics struct {
	mu      sync.Mutex
	records []recordedDispatch
}

type recordedDispatch struct {
	Contributor, Intent string
	Version             int
	Kind                contract.Kind
	ErrCode             contract.ErrorCode
}

func (r *recordingMetrics) RecordDispatch(_ context.Context, c, i string, v int, k contract.Kind, _ time.Duration, errCode contract.ErrorCode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, recordedDispatch{c, i, v, k, errCode})
}

func TestDispatcher_EmitsMetrics_Success(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rm.records))
	}
	r := rm.records[0]
	if r.ErrCode != "" {
		t.Errorf("expected empty errCode for success, got %q", r.ErrCode)
	}
	if r.Kind != contract.KindQuery {
		t.Errorf("kind = %v", r.Kind)
	}
}

func TestDispatcher_EmitsMetrics_Error(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 || rm.records[0].ErrCode != contract.CodeConflict {
		t.Errorf("expected conflict record, got %+v", rm.records)
	}
}

func TestDispatcher_EmitsMetrics_NotFound(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "x", Intent: "y", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 || rm.records[0].ErrCode != contract.CodeNotFound {
		t.Errorf("expected not-found record, got %+v", rm.records)
	}
}
