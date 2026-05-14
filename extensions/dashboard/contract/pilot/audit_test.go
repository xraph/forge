package pilot

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type fakeAuditProvider struct {
	listed []contract.AuditRecord
	subs   []chan contract.AuditRecord
}

func (f *fakeAuditProvider) List(_ contract.AuditFilter) []contract.AuditRecord {
	return f.listed
}
func (f *fakeAuditProvider) Subscribe() (<-chan contract.AuditRecord, func()) {
	ch := make(chan contract.AuditRecord, 8)
	f.subs = append(f.subs, ch)
	return ch, func() { close(ch) }
}

func TestAuditListHandler_Projects(t *testing.T) {
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	p := &fakeAuditProvider{listed: []contract.AuditRecord{
		{Time: now, Contributor: "core-contract", Intent: "x.do", Result: "ok", LatencyMs: 12},
	}}
	h := auditListHandler(p)
	got, err := h(context.Background(), AuditListInput{Limit: 50}, contract.Principal{})
	if err != nil {
		t.Fatalf("audit.list: %v", err)
	}
	if got.Total != 1 || len(got.Records) != 1 {
		t.Fatalf("expected 1 record, got %+v", got)
	}
	if got.Records[0].Time != now.Format(time.RFC3339Nano) {
		t.Errorf("time projection wrong: %q", got.Records[0].Time)
	}
	if got.Records[0].Intent != "x.do" || got.Records[0].LatencyMs != 12 {
		t.Errorf("record projection wrong: %+v", got.Records[0])
	}
}

func TestAuditListHandler_NilProviderUnavailable(t *testing.T) {
	_, err := auditListHandler(nil)(context.Background(), AuditListInput{}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

func TestAuditTailSub_StreamsAppends(t *testing.T) {
	store := contract.NewInMemoryAuditStore(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, stop, err := auditTailSub(adaptStore(store))(ctx, struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	store.Append(contract.AuditRecord{Time: time.Now(), Intent: "live"})
	select {
	case ev := <-out:
		if ev.Intent != "live" {
			t.Errorf("intent = %q", ev.Intent)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestAuditTailSub_NilProviderUnavailable(t *testing.T) {
	_, _, err := auditTailSub(nil)(context.Background(), struct{}{}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

// adaptStore exposes the contract.AuditStore as the AuditProvider the pilot
// handler expects. The two interfaces share a method set; this is a
// compile-time check.
func adaptStore(s contract.AuditStore) AuditProvider { return s }
