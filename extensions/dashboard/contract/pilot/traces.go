package pilot

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// TracesProvider is the slice of TraceStore traces.go reads.
type TracesProvider interface {
	ListTraces(filter collector.TraceFilter) ([]collector.TraceSummary, collector.TraceStats)
	GetTrace(traceID string) *collector.TraceDetail
}

func spanStatusString(s collector.SpanStatus) string {
	switch s {
	case collector.SpanStatusOK:
		return "ok"
	case collector.SpanStatusError:
		return "error"
	default:
		return "unset"
	}
}

func tracesListHandler(p TracesProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (TracesList, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (TracesList, error) {
		if p == nil {
			return TracesList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "traces provider not configured"}
		}
		summaries, stats := p.ListTraces(collector.TraceFilter{Limit: 200})
		out := make([]TraceSummaryDTO, 0, len(summaries))
		for _, s := range summaries {
			out = append(out, TraceSummaryDTO{
				TraceID:      s.TraceID,
				RootSpanName: s.RootSpanName,
				SpanCount:    s.SpanCount,
				DurationMs:   s.Duration.Milliseconds(),
				Status:       spanStatusString(s.Status),
				StartTime:    s.StartTime.UTC().Format(time.RFC3339Nano),
				Protocol:     s.Protocol,
			})
		}
		return TracesList{Traces: out, Total: stats.TotalTraces}, nil
	}
}

func traceDetailHandler(p TracesProvider) func(ctx context.Context, in TraceDetailInput, _ contract.Principal) (*TraceDetailResponse, error) {
	return func(_ context.Context, in TraceDetailInput, _ contract.Principal) (*TraceDetailResponse, error) {
		if in.ID == "" {
			return nil, &contract.Error{Code: contract.CodeBadRequest, Message: "id is required"}
		}
		if p == nil {
			return nil, &contract.Error{Code: contract.CodeUnavailable, Message: "traces provider not configured"}
		}
		detail := p.GetTrace(in.ID)
		if detail == nil {
			return nil, &contract.Error{Code: contract.CodeNotFound, Message: "trace " + in.ID + " not found"}
		}
		return detail, nil
	}
}
