package pages

import (
	"fmt"
	"time"

	"github.com/xraph/forgeui/components/badge"

	"github.com/xraph/forge/extensions/dashboard/collector"
)

// truncateTraceID returns a shortened trace ID for display.
func truncateTraceID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}

// spanKindString converts SpanKind to a display string.
func spanKindString(k collector.SpanKind) string {
	switch k {
	case collector.SpanKindInternal:
		return "Internal"
	case collector.SpanKindServer:
		return "Server"
	case collector.SpanKindClient:
		return "Client"
	case collector.SpanKindProducer:
		return "Producer"
	case collector.SpanKindConsumer:
		return "Consumer"
	default:
		return "Unspecified"
	}
}

// spanStatusString converts SpanStatus to a display string.
func spanStatusString(s collector.SpanStatus) string {
	switch s {
	case collector.SpanStatusOK:
		return "OK"
	case collector.SpanStatusError:
		return "Error"
	default:
		return "Unset"
	}
}

// traceStatusBadgeVariant returns a badge variant for trace status.
func traceStatusBadgeVariant(s collector.SpanStatus) badge.Variant {
	switch s {
	case collector.SpanStatusOK:
		return badge.VariantDefault
	case collector.SpanStatusError:
		return badge.VariantDestructive
	default:
		return badge.VariantSecondary
	}
}

// protocolBadgeVariant returns a badge variant for protocol type.
func protocolBadgeVariant(protocol string) badge.Variant {
	switch protocol {
	case "REST":
		return badge.VariantDefault
	case "WS":
		return badge.VariantSecondary
	case "SSE":
		return badge.VariantOutline
	case "Event":
		return badge.VariantSecondary
	default:
		return badge.VariantOutline
	}
}

// spanBarColorClass returns Tailwind classes for waterfall bar color.
func spanBarColorClass(s collector.SpanStatus) string {
	switch s {
	case collector.SpanStatusOK:
		return "bg-green-500"
	case collector.SpanStatusError:
		return "bg-red-500"
	default:
		return "bg-blue-500"
	}
}

// formatDurationShort formats a duration for compact display.
func formatDurationShort(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fus", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// formatTimeShort formats a time for compact display (HH:MM:SS.mmm).
func formatTimeShort(t time.Time) string {
	return t.Format("15:04:05.000")
}
