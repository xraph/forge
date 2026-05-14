package pilot

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// AuditProvider is the slice of contract.AuditStore the audit handlers need.
// Defined here (not as a type alias) so tests can supply a stub without
// importing the full contract package surface.
type AuditProvider interface {
	List(filter contract.AuditFilter) []contract.AuditRecord
	Subscribe() (<-chan contract.AuditRecord, func())
}

// AuditRecordDTO is the wire shape for one audit record. Timestamps are
// RFC3339Nano strings and the payload field is omitted (slice (k) keeps the
// store free of payload data; per-intent redaction is a slice (l) concern).
type AuditRecordDTO struct {
	Time          string `json:"time"`
	Contributor   string `json:"contributor"`
	Intent        string `json:"intent"`
	IntentVersion int    `json:"intentVersion,omitempty"`
	Subject       string `json:"subject,omitempty"`
	User          string `json:"user,omitempty"`
	Result        string `json:"result"`
	LatencyMs     int64  `json:"latencyMs"`
	CorrelationID string `json:"correlationID,omitempty"`
}

func projectRecord(r contract.AuditRecord) AuditRecordDTO {
	return AuditRecordDTO{
		Time:          r.Time.UTC().Format(time.RFC3339Nano),
		Contributor:   r.Contributor,
		Intent:        r.Intent,
		IntentVersion: r.IntentVersion,
		Subject:       r.Subject,
		User:          r.User,
		Result:        r.Result,
		LatencyMs:     r.LatencyMs,
		CorrelationID: r.CorrelationID,
	}
}

// AuditListInput is the wire input for the audit.list query.
type AuditListInput struct {
	Limit       int    `json:"limit,omitempty"`
	Contributor string `json:"contributor,omitempty"`
	Intent      string `json:"intent,omitempty"`
	User        string `json:"user,omitempty"`
	Result      string `json:"result,omitempty"`
}

// AuditListResponse is the wire output of audit.list.
type AuditListResponse struct {
	Records []AuditRecordDTO `json:"records"`
	Total   int              `json:"total"`
}

func auditListHandler(p AuditProvider) func(ctx context.Context, in AuditListInput, _ contract.Principal) (AuditListResponse, error) {
	return func(_ context.Context, in AuditListInput, _ contract.Principal) (AuditListResponse, error) {
		if p == nil {
			return AuditListResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "audit store not configured"}
		}
		recs := p.List(contract.AuditFilter{
			Limit:       in.Limit,
			Contributor: in.Contributor,
			Intent:      in.Intent,
			User:        in.User,
			Result:      in.Result,
		})
		out := make([]AuditRecordDTO, 0, len(recs))
		for _, r := range recs {
			out = append(out, projectRecord(r))
		}
		return AuditListResponse{Records: out, Total: len(out)}, nil
	}
}

// auditTailSub returns a subscription handler that streams every Append from
// the store as an append-mode StreamEvent. Cancellation tears down the
// store-side subscriber. nil provider yields CodeUnavailable on subscribe.
func auditTailSub(p AuditProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan AuditRecordDTO, func(), error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan AuditRecordDTO, func(), error) {
		if p == nil {
			return nil, nil, &contract.Error{Code: contract.CodeUnavailable, Message: "audit store not configured"}
		}
		src, cancel := p.Subscribe()
		out := make(chan AuditRecordDTO, 16)
		stop := func() {
			cancel()
		}
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case rec, ok := <-src:
					if !ok {
						return
					}
					select {
					case out <- projectRecord(rec):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out, stop, nil
	}
}
