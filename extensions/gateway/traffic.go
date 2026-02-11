package gateway

import (
	"math/rand"
	"net/http"
	"strings"
)

// TrafficSplitter evaluates traffic rules to determine target selection.
type TrafficSplitter struct {
	enabled bool
}

// NewTrafficSplitter creates a new traffic splitter.
func NewTrafficSplitter(enabled bool) *TrafficSplitter {
	return &TrafficSplitter{enabled: enabled}
}

// FilterTargets filters targets based on the route's traffic policy and request.
// Returns the filtered targets, or the original targets if no policy applies.
func (ts *TrafficSplitter) FilterTargets(r *http.Request, route *Route) []*Target {
	if !ts.enabled || route.TrafficPolicy == nil {
		return route.Targets
	}

	policy := route.TrafficPolicy

	switch policy.Type {
	case TrafficCanary:
		return ts.applyCanary(r, route.Targets, policy)
	case TrafficBlueGreen:
		return ts.applyBlueGreen(r, route.Targets, policy)
	case TrafficABTest:
		return ts.applyABTest(r, route.Targets, policy)
	case TrafficWeighted:
		return ts.applyWeighted(r, route.Targets, policy)
	default:
		return route.Targets
	}
}

// ShouldMirror returns the mirror target URL if the request should be mirrored.
func (ts *TrafficSplitter) ShouldMirror(route *Route) string {
	if !ts.enabled || route.TrafficPolicy == nil {
		return ""
	}

	if route.TrafficPolicy.Type == TrafficMirror {
		return route.TrafficPolicy.MirrorTarget
	}

	return ""
}

func (ts *TrafficSplitter) applyCanary(_ *http.Request, targets []*Target, policy *TrafficPolicy) []*Target {
	for _, rule := range policy.Rules {
		if rule.Match.Type == MatchWeight {
			// Random roll to decide canary vs stable
			if rand.Intn(100) < rule.Weight {
				return filterByTags(targets, rule.TargetTags)
			}
		}
	}

	// Default: return non-canary targets (targets without "canary" tag)
	stable := make([]*Target, 0, len(targets))

	for _, t := range targets {
		if !hasTag(t.Tags, "canary") {
			stable = append(stable, t)
		}
	}

	if len(stable) == 0 {
		return targets
	}

	return stable
}

func (ts *TrafficSplitter) applyBlueGreen(_ *http.Request, targets []*Target, policy *TrafficPolicy) []*Target {
	// Blue-green uses the first rule to determine which group is active
	if len(policy.Rules) > 0 {
		rule := policy.Rules[0]

		return filterByTags(targets, rule.TargetTags)
	}

	return targets
}

func (ts *TrafficSplitter) applyABTest(r *http.Request, targets []*Target, policy *TrafficPolicy) []*Target {
	for _, rule := range policy.Rules {
		if matchTrafficRule(r, rule.Match) {
			filtered := filterByTags(targets, rule.TargetTags)
			if len(filtered) > 0 {
				return filtered
			}
		}
	}

	return targets
}

func (ts *TrafficSplitter) applyWeighted(_ *http.Request, targets []*Target, policy *TrafficPolicy) []*Target {
	// Build weighted selection based on rules
	totalWeight := 0

	for _, rule := range policy.Rules {
		totalWeight += rule.Weight
	}

	if totalWeight == 0 {
		return targets
	}

	roll := rand.Intn(totalWeight)

	for _, rule := range policy.Rules {
		roll -= rule.Weight
		if roll < 0 {
			filtered := filterByTags(targets, rule.TargetTags)
			if len(filtered) > 0 {
				return filtered
			}
		}
	}

	return targets
}

func matchTrafficRule(r *http.Request, match TrafficMatch) bool {
	var matched bool

	switch match.Type {
	case MatchHeader:
		val := r.Header.Get(match.Key)
		matched = val == match.Value

	case MatchCookie:
		cookie, err := r.Cookie(match.Key)
		if err == nil {
			matched = cookie.Value == match.Value
		}

	case MatchWeight:
		matched = rand.Intn(100) < 50 // Generic weight-based

	case MatchIPRange:
		host, _, _ := splitHostPort(r.RemoteAddr)
		matched = matchIPPattern(host, match.Value)
	}

	if match.Negate {
		return !matched
	}

	return matched
}

func filterByTags(targets []*Target, tags []string) []*Target {
	if len(tags) == 0 {
		return targets
	}

	filtered := make([]*Target, 0, len(targets))

	for _, t := range targets {
		if hasAnyTag(t.Tags, tags) {
			filtered = append(filtered, t)
		}
	}

	return filtered
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}

	return false
}

func hasAnyTag(tags, required []string) bool {
	for _, req := range required {
		if hasTag(tags, req) {
			return true
		}
	}

	return false
}
