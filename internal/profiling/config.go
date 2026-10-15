// Package profiling resolves forge's continuous profiling config and runs the
// Pyroscope push profiler. The span labels that link a trace to its CPU
// profile live in internal/telemetry, next to the server span.
package profiling

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// DefaultProfileTypes is what the Pyroscope SDK collects by default.
var DefaultProfileTypes = []string{"cpu", "alloc_objects", "alloc_space", "inuse_objects", "inuse_space"}

var extraProfileTypes = []string{"goroutines", "mutex_count", "mutex_duration", "block_count", "block_duration"}

// Inputs is everything Resolve reads.
type Inputs struct {
	Code           shared.ProfilingConfig // from WithAppProfilingConfig
	File           shared.ProfilingConfig // from the forge config "profiling" key
	AppName        string
	AppVersion     string
	Environment    string
	TracingEnabled bool
	Getenv         func(string) string
}

// Settings is the resolved profiling configuration.
type Settings struct {
	Push              bool // a server address is set
	SpanLabels        bool
	ServerAddress     string
	ApplicationName   string
	BasicAuthUser     string
	BasicAuthPassword string
	TenantID          string
	Tags              map[string]string
	ProfileTypes      []string
	Warnings          []string
}

// Resolve applies the precedence code, forge config, PYROSCOPE_* env, default.
func Resolve(in Inputs) Settings {
	getenv := in.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	s := Settings{
		ServerAddress:     first(in.Code.ServerAddress, in.File.ServerAddress, getenv("PYROSCOPE_SERVER_ADDRESS")),
		BasicAuthUser:     first(in.Code.BasicAuthUser, in.File.BasicAuthUser, getenv("PYROSCOPE_BASIC_AUTH_USER")),
		BasicAuthPassword: first(in.Code.BasicAuthPassword, in.File.BasicAuthPassword, getenv("PYROSCOPE_BASIC_AUTH_PASSWORD")),
		TenantID:          first(in.Code.TenantID, in.File.TenantID, getenv("PYROSCOPE_TENANT_ID")),
	}

	s.Push = s.ServerAddress != ""
	s.SpanLabels = resolveSpanLabels(in, s.Push, &s.Warnings)
	s.ApplicationName = resolveApplicationName(
		first(in.Code.ApplicationName, in.File.ApplicationName, getenv("PYROSCOPE_APPLICATION_NAME")), in.AppName, &s.Warnings)
	s.Tags = resolveTags(in, &s.Warnings)
	s.ProfileTypes = resolveProfileTypes(in, &s.Warnings)

	return s
}

func resolveSpanLabels(in Inputs, push bool, warnings *[]string) bool {
	want := push

	switch {
	case in.Code.SpanLabels != nil:
		want = *in.Code.SpanLabels
	case in.File.SpanLabels != nil:
		want = *in.File.SpanLabels
	}

	if want && !in.TracingEnabled {
		*warnings = append(*warnings, "profiling: span labels need tracing, which is off, so they stay off")

		return false
	}

	return want
}

// resolveApplicationName returns a name Pyroscope accepts. It refuses runes
// outside [A-Za-z0-9_./-], and an app that fails to start profiling over its
// name is worse than one that profiles under a slightly different one.
func resolveApplicationName(configured, appName string, warnings *[]string) string {
	name := configured
	if name == "" {
		name = appName
	}

	if name == "" {
		return "forge"
	}

	clean := strings.Map(func(r rune) rune {
		if r == '-' || r == '/' || isLabelRune(r) {
			return r
		}

		return '_'
	}, name)

	if clean != name {
		*warnings = append(*warnings, fmt.Sprintf(
			"profiling: application name %q has characters Pyroscope refuses, using %q", name, clean))
	}

	return clean
}

func resolveTags(in Inputs, warnings *[]string) map[string]string {
	tags := make(map[string]string)

	if in.AppVersion != "" {
		tags["service_version"] = in.AppVersion
	}

	if in.Environment != "" {
		tags["environment"] = in.Environment
	}

	user := in.Code.Tags
	if len(user) == 0 {
		user = in.File.Tags
	}

	for _, k := range slices.Sorted(maps.Keys(user)) {
		if !validTagKey(k) {
			*warnings = append(*warnings, fmt.Sprintf("profiling: tag %q is not a valid Pyroscope label name, dropping it", k))

			continue
		}

		tags[k] = user[k]
	}

	if len(tags) == 0 {
		return nil
	}

	return tags
}

func resolveProfileTypes(in Inputs, warnings *[]string) []string {
	requested := in.Code.ProfileTypes
	if len(requested) == 0 {
		requested = in.File.ProfileTypes
	}

	if len(requested) == 0 {
		return slices.Clone(DefaultProfileTypes)
	}

	var out []string

	for _, t := range requested {
		if !slices.Contains(DefaultProfileTypes, t) && !slices.Contains(extraProfileTypes, t) {
			*warnings = append(*warnings, fmt.Sprintf("profiling: unknown profile type %q, skipping it", t))

			continue
		}

		out = append(out, t)
	}

	if len(out) == 0 {
		return slices.Clone(DefaultProfileTypes)
	}

	return out
}

func validTagKey(k string) bool {
	if k == "" || k == "__name__" {
		return false
	}

	for _, r := range k {
		if !isLabelRune(r) {
			return false
		}
	}

	return true
}

func isLabelRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.'
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}
