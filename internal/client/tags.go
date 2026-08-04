package client

import (
	"sort"
	"strings"
)

// DeriveTags computes an operation's invalidation contract from its method and
// the entity it touches.
//
// Every non-GET invalidates the collection, PATCH included. A patch only
// changes list membership when it touches a filtered field, and the server
// cannot know which lists a browser has mounted. Over-refetching is a
// performance defect a profiler finds; under-refetching is a stale row a user
// reports three weeks later. The default is correct and the escape is explicit.
func DeriveTags(method string, entity *EntityRef, isList bool) TagSet {
	if entity == nil {
		return TagSet{}
	}

	item := entity.Type + ":{" + entity.IDField + "}"
	collection := entity.Type + "[]"

	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		provides := []string{item}
		if isList {
			provides = append(provides, collection)
		}

		return TagSet{Provides: provides}

	case "DELETE":
		return TagSet{Invalidates: []string{collection}}

	default:
		return TagSet{Provides: []string{item}, Invalidates: []string{collection}}
	}
}

// ApplyTagOverrides folds route-declared additions and suppressions into a
// derived contract. Output is sorted and deduplicated so generated files do not
// churn between runs.
func ApplyTagOverrides(base TagSet, extra, suppressed []string) TagSet {
	drop := make(map[string]bool, len(suppressed))
	for _, s := range suppressed {
		drop[s] = true
	}

	return TagSet{
		Provides:    normalizeTags(base.Provides, nil, drop),
		Invalidates: normalizeTags(base.Invalidates, extra, drop),
	}
}

// normalizeTags merges, removes suppressed entries, deduplicates and sorts.
// Returns nil rather than an empty slice so reflect.DeepEqual against a zero
// TagSet behaves as a reader expects.
func normalizeTags(base, extra []string, drop map[string]bool) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))

	for _, tag := range append(append([]string{}, base...), extra...) {
		if drop[tag] || seen[tag] {
			continue
		}

		seen[tag] = true

		out = append(out, tag)
	}

	if len(out) == 0 {
		return nil
	}

	sort.Strings(out)

	return out
}
