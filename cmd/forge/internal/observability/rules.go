package observability

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"text/template"
	"time"
)

var (
	// ErrInvalidSLO reports an availability objective outside (0, 100).
	ErrInvalidSLO = errors.New("observability: slo must be greater than 0 and less than 100")
	// ErrInvalidP95 reports a latency threshold that is not positive.
	ErrInvalidP95 = errors.New("observability: p95 threshold must be positive")
)

// RulesOptions sets the objective and threshold the alert rules use.
type RulesOptions struct {
	// SLO is the availability objective in percent, for example 99.9.
	SLO float64
	// P95Threshold is the latency above which ForgeHighLatencyP95 fires.
	P95Threshold time.Duration
}

// DefaultRulesOptions is a 99.9% objective and a one second p95.
func DefaultRulesOptions() RulesOptions {
	return RulesOptions{SLO: 99.9, P95Threshold: time.Second}
}

// RenderRules renders the Prometheus rule file for o.
func RenderRules(o RulesOptions) ([]byte, error) {
	if math.IsNaN(o.SLO) || o.SLO <= 0 || o.SLO >= 100 {
		return nil, ErrInvalidSLO
	}

	if o.P95Threshold <= 0 {
		return nil, ErrInvalidP95
	}

	raw, err := assets.ReadFile("assets/forge-rules.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("observability: read rules template: %w", err)
	}

	tmpl, err := template.New("rules").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("observability: parse rules template: %w", err)
	}

	budget := 1 - o.SLO/100
	data := struct {
		SLO, Fast, Slow, P95 string
		Windows              []string
	}{
		SLO:     strconv.FormatFloat(o.SLO, 'f', -1, 64),
		Fast:    number(14.4 * budget),
		Slow:    number(6 * budget),
		P95:     number(o.P95Threshold.Seconds()),
		Windows: []string{"5m", "30m", "1h", "6h"},
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("observability: render rules: %w", err)
	}

	return out.Bytes(), nil
}

// number prints a threshold with six significant digits, so float noise such
// as 0.014399999999999 comes out as 0.0144.
func number(v float64) string {
	return strconv.FormatFloat(v, 'g', 6, 64)
}
