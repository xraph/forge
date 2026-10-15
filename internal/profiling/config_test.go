package profiling

import (
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func hasWarning(s Settings, substr string) bool {
	for _, w := range s.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}

	return false
}

func TestResolveNothingSetRunsNothing(t *testing.T) {
	s := Resolve(Inputs{AppName: "orders", TracingEnabled: true})

	if s.Push || s.SpanLabels {
		t.Fatalf("Push=%v SpanLabels=%v, want both off", s.Push, s.SpanLabels)
	}

	if len(s.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", s.Warnings)
	}
}

func TestResolvePrecedence(t *testing.T) {
	getenv := env(map[string]string{
		"PYROSCOPE_SERVER_ADDRESS":      "http://env:4040",
		"PYROSCOPE_APPLICATION_NAME":    "env-app",
		"PYROSCOPE_BASIC_AUTH_USER":     "env-user",
		"PYROSCOPE_BASIC_AUTH_PASSWORD": "env-pass",
		"PYROSCOPE_TENANT_ID":           "env-tenant",
	})

	cases := []struct {
		name       string
		code, file shared.ProfilingConfig
		want       string // ServerAddress
		wantApp    string
		wantUser   string
		wantTenant string
	}{
		{"env only", shared.ProfilingConfig{}, shared.ProfilingConfig{}, "http://env:4040", "env-app", "env-user", "env-tenant"},
		{
			"file beats env",
			shared.ProfilingConfig{},
			shared.ProfilingConfig{ServerAddress: "http://file:4040", ApplicationName: "file-app", BasicAuthUser: "file-user", TenantID: "file-tenant"},
			"http://file:4040", "file-app", "file-user", "file-tenant",
		},
		{
			"code beats file",
			shared.ProfilingConfig{ServerAddress: "http://code:4040", ApplicationName: "code-app", BasicAuthUser: "code-user", TenantID: "code-tenant"},
			shared.ProfilingConfig{ServerAddress: "http://file:4040", ApplicationName: "file-app", BasicAuthUser: "file-user", TenantID: "file-tenant"},
			"http://code:4040", "code-app", "code-user", "code-tenant",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Resolve(Inputs{Code: tc.code, File: tc.file, AppName: "orders", TracingEnabled: true, Getenv: getenv})

			if s.ServerAddress != tc.want || s.ApplicationName != tc.wantApp || s.BasicAuthUser != tc.wantUser || s.TenantID != tc.wantTenant {
				t.Fatalf("got %q %q %q %q", s.ServerAddress, s.ApplicationName, s.BasicAuthUser, s.TenantID)
			}

			if !s.Push || !s.SpanLabels {
				t.Fatalf("Push=%v SpanLabels=%v, want both on", s.Push, s.SpanLabels)
			}
		})
	}
}

func TestResolveSpanLabels(t *testing.T) {
	cases := []struct {
		name        string
		code        shared.ProfilingConfig
		tracing     bool
		want        bool
		wantWarning bool
	}{
		{"pull user turns labels on alone", shared.ProfilingConfig{SpanLabels: new(true)}, true, true, false},
		{"push user turns labels off", shared.ProfilingConfig{ServerAddress: "http://p:4040", SpanLabels: new(false)}, true, false, false},
		{"explicit labels with tracing off", shared.ProfilingConfig{SpanLabels: new(true)}, false, false, true},
		{"push with tracing off", shared.ProfilingConfig{ServerAddress: "http://p:4040"}, false, false, true},
		{"nothing with tracing off", shared.ProfilingConfig{}, false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Resolve(Inputs{Code: tc.code, AppName: "orders", TracingEnabled: tc.tracing})

			if s.SpanLabels != tc.want {
				t.Fatalf("SpanLabels = %v, want %v", s.SpanLabels, tc.want)
			}

			if got := hasWarning(s, "tracing"); got != tc.wantWarning {
				t.Fatalf("tracing warning = %v, want %v (warnings %v)", got, tc.wantWarning, s.Warnings)
			}
		})
	}
}

func TestResolveApplicationName(t *testing.T) {
	cases := []struct {
		appName, configured, want string
		warn                      bool
	}{
		{"orders", "", "orders", false},
		{"orders", "billing.v2/api-1", "billing.v2/api-1", false},
		{"My App", "", "My_App", true},
		{"café", "", "caf_", true},
		{"", "", "forge", false},
		{"orders", "   ", "___", true},
	}

	for _, tc := range cases {
		s := Resolve(Inputs{Code: shared.ProfilingConfig{ApplicationName: tc.configured}, AppName: tc.appName, TracingEnabled: true})

		if s.ApplicationName != tc.want {
			t.Errorf("app %q, configured %q: name = %q, want %q", tc.appName, tc.configured, s.ApplicationName, tc.want)
		}

		if got := hasWarning(s, "application name"); got != tc.warn {
			t.Errorf("app %q, configured %q: warning = %v, want %v", tc.appName, tc.configured, got, tc.warn)
		}
	}
}

func TestResolveTags(t *testing.T) {
	s := Resolve(Inputs{
		Code: shared.ProfilingConfig{Tags: map[string]string{
			"environment": "blue", // user beats forge
			"region":      "eu",   // kept
			"bad key":     "x",    // dropped
			"__name__":    "x",    // reserved, dropped
		}},
		AppName: "orders", AppVersion: "1.2.3", Environment: "production", TracingEnabled: true,
	})

	want := map[string]string{"environment": "blue", "region": "eu", "service_version": "1.2.3"}
	if len(s.Tags) != len(want) {
		t.Fatalf("tags = %v, want %v", s.Tags, want)
	}

	for k, v := range want {
		if s.Tags[k] != v {
			t.Fatalf("tags = %v, want %v", s.Tags, want)
		}
	}

	if !hasWarning(s, `"bad key"`) || !hasWarning(s, `"__name__"`) {
		t.Fatalf("warnings = %v, want one for each dropped key", s.Warnings)
	}
}

func TestResolveTagValues(t *testing.T) {
	cases := []struct {
		name        string
		tags        map[string]string
		version     string
		environment string
		key, want   string
		warn        bool
	}{
		{"clean user value", map[string]string{"region": "eu-west.1"}, "", "", "region", "eu-west.1", false},
		{"comma in user value", map[string]string{"team": "a,b"}, "", "", "team", "a_b", true},
		{"braces and equals in user value", map[string]string{"query": "{x=y}"}, "", "", "query", "_x_y_", true},
		{"comma in app version", nil, "1.0,rc1", "", "service_version", "1.0_rc1", true},
		{"brace in environment", nil, "", "prod{eu}", "environment", "prod_eu_", true},
		{"user replaces a bad environment", map[string]string{"environment": "blue"}, "", "prod=eu", "environment", "blue", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Resolve(Inputs{
				Code:    shared.ProfilingConfig{Tags: tc.tags},
				AppName: "orders", AppVersion: tc.version, Environment: tc.environment, TracingEnabled: true,
			})

			if got := s.Tags[tc.key]; got != tc.want {
				t.Fatalf("tag %q = %q, want %q (tags %v)", tc.key, got, tc.want, s.Tags)
			}

			if got := hasWarning(s, "tag \""+tc.key+"\" value"); got != tc.warn {
				t.Fatalf("value warning = %v, want %v (warnings %v)", got, tc.warn, s.Warnings)
			}
		})
	}
}

func TestResolveTagsOmitEmptyVersionAndEnvironment(t *testing.T) {
	s := Resolve(Inputs{AppName: "orders", TracingEnabled: true})

	if len(s.Tags) != 0 {
		t.Fatalf("tags = %v, want none", s.Tags)
	}
}

func TestResolveProfileTypes(t *testing.T) {
	if s := Resolve(Inputs{AppName: "orders"}); !slices.Equal(s.ProfileTypes, DefaultProfileTypes) {
		t.Fatalf("default types = %v", s.ProfileTypes)
	}

	s := Resolve(Inputs{
		File:    shared.ProfilingConfig{ProfileTypes: []string{"cpu", "mutex_count", "goroutine_leak", "heap"}},
		AppName: "orders",
	})

	if !slices.Equal(s.ProfileTypes, []string{"cpu", "mutex_count"}) {
		t.Fatalf("types = %v, want [cpu mutex_count]", s.ProfileTypes)
	}

	if !hasWarning(s, `"goroutine_leak"`) || !hasWarning(s, `"heap"`) {
		t.Fatalf("warnings = %v, want one per unknown type", s.Warnings)
	}

	s = Resolve(Inputs{File: shared.ProfilingConfig{ProfileTypes: []string{"heap"}}, AppName: "orders"})
	if !slices.Equal(s.ProfileTypes, DefaultProfileTypes) {
		t.Fatalf("all-unknown list = %v, want the defaults", s.ProfileTypes)
	}
}
