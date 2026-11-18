package plugins

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/errors"
	"gopkg.in/yaml.v3"
)

// ClientConfig represents the .forge-client.yml configuration file.
type ClientConfig struct {
	// Source configuration
	Source SourceConfig `yaml:"source"`

	// Default generation settings
	Defaults GenerationDefaults `yaml:"defaults"`

	// Streaming extension configuration
	Streaming StreamingExtConfig `yaml:"streaming,omitempty"`

	// Multiple client configurations
	Clients []ClientGenConfig `yaml:"clients,omitempty"`
}

// StreamingExtConfig defines streaming extension features configuration.
type StreamingExtConfig struct {
	// Enable room management client
	Rooms bool `yaml:"rooms"`

	// Enable presence tracking client
	Presence bool `yaml:"presence"`

	// Enable typing indicator client
	Typing bool `yaml:"typing"`

	// Enable pub/sub channel client
	Channels bool `yaml:"channels"`

	// Enable message history support
	History bool `yaml:"history"`
}

// SourceEntry is one specification document to read.
type SourceEntry struct {
	// Type: "file" or "url".
	Type string `yaml:"type"`

	Path string `yaml:"path,omitempty"`
	URL  string `yaml:"url,omitempty"`
}

// SourceConfig defines where to get the API specification.
//
// Sources is one ordered list rather than parallel path and url arrays,
// because merge precedence depends on order and parallel arrays leave the
// relative order of a file source and a URL source undefined.
type SourceConfig struct {
	// Type: "file", "url", "auto"
	Type string `yaml:"type"`

	// Path to spec file (when type=file). Read as a one-element Sources list
	// when Sources is empty, so an existing .forge-client.yml keeps working.
	Path string `yaml:"path,omitempty"`

	// URL to fetch spec (when type=url). Same one-element handling as Path.
	URL string `yaml:"url,omitempty"`

	// Sources lists several documents to parse and merge. When set it wins
	// over the scalar Path and URL keys above.
	Sources []SourceEntry `yaml:"sources,omitempty"`

	// Auto-discovery paths (when type=auto)
	AutoDiscoverPaths []string `yaml:"auto_discover_paths,omitempty"`

	// AllowInsecureHTTP permits fetching a spec over plain http from a host
	// other than localhost. Off by default; the --allow-insecure-http flag
	// does the same for one run.
	AllowInsecureHTTP bool `yaml:"allow_insecure_http,omitempty"`
}

// Entries normalises this configuration into the list of documents to read.
func (s SourceConfig) Entries() []SourceEntry {
	if len(s.Sources) > 0 {
		return s.Sources
	}

	switch {
	case s.Path != "":
		return []SourceEntry{{Type: "file", Path: s.Path}}
	case s.URL != "":
		return []SourceEntry{{Type: "url", URL: s.URL}}
	default:
		return nil
	}
}

// GenerationDefaults defines default settings for client generation.
type GenerationDefaults struct {
	Language string `yaml:"language"`
	Output   string `yaml:"output"`
	Package  string `yaml:"package"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Module   string `yaml:"module,omitempty"`

	// Hooks emits the operation manifest (ops.ts) and typed hook facades
	// (hooks.ts) delegating to @forge-go/client-core over the generated client.
	Hooks bool `yaml:"hooks"`

	// ReactQuery is the former name of Hooks, still read so an existing
	// .forge-client.yml keeps working; either key enables the layer, and using
	// this one prints a deprecation notice. See resolveHooks in client.go.
	//
	// Deprecated: use hooks. The generated code has not been TanStack Query
	// since the hook facades replaced it.
	ReactQuery bool `yaml:"react_query"`

	// Include keeps only endpoints whose path matches a pattern; Exclude drops
	// matches and is applied second. Both accept a path prefix, a glob, or a
	// trailing "/**". A specification usually describes more than any one
	// consumer talks to, and these are how a client binds only its own surface.
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`

	// Feature flags
	Auth      bool `yaml:"auth"`
	Streaming bool `yaml:"streaming"`

	// Streaming features
	Reconnection    bool `yaml:"reconnection"`
	Heartbeat       bool `yaml:"heartbeat"`
	StateManagement bool `yaml:"state_management"`

	// Enhanced features
	UseFetch        bool `yaml:"use_fetch"`
	DualPackage     bool `yaml:"dual_package"`
	GenerateTests   bool `yaml:"generate_tests"`
	GenerateLinting bool `yaml:"generate_linting"`
	GenerateCI      bool `yaml:"generate_ci"`
	ErrorTaxonomy   bool `yaml:"error_taxonomy"`
	Interceptors    bool `yaml:"interceptors"`
	Pagination      bool `yaml:"pagination"`

	// Output control
	ClientOnly bool `yaml:"client_only"` // Generate only client source files

	// FieldNaming selects the client-side identifier style for schema
	// properties: "camel", "pascal", "snake", or "preserve". Empty means
	// unset -- the generator's own per-language default applies (camel for
	// typescript and dart, preserve otherwise; see client.GeneratorConfig.FieldNaming's
	// doc comment). Mirrors --field-naming; the CLI flag, when passed, wins
	// over this.
	FieldNaming string `yaml:"field_naming,omitempty"`

	// StripPrefix removes a leading service prefix from schema names, operation
	// ids, entity typenames and cache tags. Mirrors --strip-prefix; the flag,
	// when passed, wins. A clients: entry's own strip_prefix wins over both,
	// which is the usual case -- each client strips its own service's prefix.
	StripPrefix string `yaml:"strip_prefix,omitempty"`

	// StripPrefixes are additional prefixes stripped from every client, on top
	// of whichever prefix that client owns.
	//
	// Rarely needed with a clients: block, which already tells the generator
	// every prefix in play -- see clientStripPrefixes. This is for the prefix
	// no client declares: a service the gateway fronts and describes but that
	// nobody generates a client for, whose types still turn up inside the
	// documents of the services that talk to it.
	StripPrefixes []string `yaml:"strip_prefixes,omitempty"`

	// FieldOverrides maps a wire name (or "SchemaName.wire_name" for a
	// schema-scoped override) to an explicit client-side name, exactly like
	// client.GeneratorConfig.FieldOverrides. Mirrors --field-overrides; the
	// CLI flag, when passed, wins over this.
	FieldOverrides map[string]string `yaml:"field_overrides,omitempty"`

	// Int64 is the Dart int64 representation: "string" (the default) or
	// "int". Other languages ignore it.
	Int64 string `yaml:"int64,omitempty"`
}

// ClientGenConfig defines configuration for generating a specific client.
type ClientGenConfig struct {
	Name     string `yaml:"name"`
	Language string `yaml:"language"`
	Output   string `yaml:"output"`
	Package  string `yaml:"package,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Module   string `yaml:"module,omitempty"`

	// Include and Exclude carve this client's surface out of the shared
	// specification, with the same matching rules as defaults.include -- a path
	// prefix, a glob, or a trailing "/**" -- and Exclude applied second.
	//
	// These are why a clients: block is worth having rather than N invocations
	// of the CLI. One gateway fronting several services publishes one merged
	// document, and each consumer wants the routes of one service: without a
	// per-client filter every generated client carries every service's surface
	// under a different package name.
	//
	// Unset means this client inherits defaults.include/defaults.exclude. A
	// client that sets either one replaces the corresponding default outright
	// rather than appending to it -- an inherited include is a floor a client
	// could not get below, and "this client sees only /studio" has to be
	// expressible next to a default that names something else.
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`

	// Hooks overrides defaults.hooks for this client. A pointer because false
	// and unset differ here: the whole point is to let one client opt out of a
	// layer the defaults turned on.
	Hooks *bool `yaml:"hooks,omitempty"`

	// StripPrefix names the prefix this client's OWN service was merged under.
	//
	// Per client rather than shared, because a defaults.strip_prefix of
	// "Studio_" applied to the portal client would strip nothing from it and
	// leave the stutter in place.
	//
	// Declaring it does two jobs. It strips this client's own typenames, and it
	// tells every SIBLING client that this prefix is a service prefix, so the
	// foreign copies of these types that turn up in their documents strip too.
	// See clientStripPrefixes in client_multi.go.
	StripPrefix string `yaml:"strip_prefix,omitempty"`

	// StripPrefixes overrides the prefix set for this client outright, for the
	// case where the derived set is wrong: two services whose types genuinely
	// collide once stripped, where the fix is to strip fewer of them here.
	//
	// See clientStripPrefixes for how this composes with the derived set.
	StripPrefixes []string `yaml:"strip_prefixes,omitempty"`

	// Override feature flags
	Auth      *bool `yaml:"auth,omitempty"`
	Streaming *bool `yaml:"streaming,omitempty"`

	// ClientOnly overrides defaults.client_only for this client. A pointer so
	// an absent key inherits. A TypeScript client that is client-only skips
	// package.json and tsconfig; a Dart one keeps pubspec.yaml and skips only
	// the README, because a Dart package without a pubspec cannot be used.
	ClientOnly *bool `yaml:"client_only,omitempty"`

	// Int64 overrides defaults.int64 for this client.
	Int64 string `yaml:"int64,omitempty"`
}

// LoadClientConfig loads .forge-client.yml from the current directory or parent.
func LoadClientConfig(startDir string) (*ClientConfig, error) {
	// Try current directory first
	configPath := filepath.Join(startDir, ".forge-client.yml")
	if _, err := os.Stat(configPath); err == nil {
		return loadClientConfigFile(configPath)
	}

	// Try .forge-client.yaml
	configPath = filepath.Join(startDir, ".forge-client.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return loadClientConfigFile(configPath)
	}

	// Try parent directories
	dir := startDir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}

		dir = parent

		configPath = filepath.Join(dir, ".forge-client.yml")
		if _, err := os.Stat(configPath); err == nil {
			return loadClientConfigFile(configPath)
		}

		configPath = filepath.Join(dir, ".forge-client.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return loadClientConfigFile(configPath)
		}
	}

	return nil, errors.New(".forge-client.yml not found")
}

// loadClientConfigFile loads and parses a client config file.
func loadClientConfigFile(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config ClientConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return &config, nil
}

// DefaultClientConfig returns a default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Source: SourceConfig{
			Type: "auto",
			AutoDiscoverPaths: []string{
				"./openapi.json",
				"./openapi.yaml",
				"./api/openapi.json",
				"./api/openapi.yaml",
				"./docs/openapi.json",
				"./docs/openapi.yaml",
			},
		},
		Defaults: GenerationDefaults{
			Language:        "go",
			Output:          "./client",
			Package:         "client",
			Auth:            true,
			Streaming:       true,
			Reconnection:    true,
			Heartbeat:       true,
			StateManagement: true,
			UseFetch:        true,
			DualPackage:     true,
			GenerateTests:   true,
			GenerateLinting: true,
			GenerateCI:      true,
			ErrorTaxonomy:   true,
			Interceptors:    true,
			Pagination:      true,
		},
	}
}

// SaveClientConfig saves a client configuration to a file.
func SaveClientConfig(config *ClientConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// maxSpecBytes caps how much of a fetched spec is read. A real OpenAPI or
// AsyncAPI document is a few megabytes at most, and without a cap a
// misbehaving or hostile server could stream until the CLI runs out of
// memory. A variable rather than a constant so tests can lower it.
var maxSpecBytes int64 = 64 << 20

// insecureHTTPOptIn names both ways to allow a plain http spec URL, for the
// error that refuses one.
const insecureHTTPOptIn = "pass --allow-insecure-http or set source.allow_insecure_http: true in .forge-client.yml"

// checkSpecURL refuses a plain http spec URL unless it points at this machine
// or the caller opted in. A spec fetched over the network in cleartext can be
// swapped in transit, and the generated client is code that ships, so a
// silent downgrade is not acceptable. Loopback stays allowed because a local
// dev server is the common case and its traffic never leaves the host.
func checkSpecURL(u *url.URL, allowInsecureHTTP bool) error {
	if !strings.EqualFold(u.Scheme, "http") || allowInsecureHTTP || isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("refusing to fetch spec over plain http from %s: use https, or %s", u.Host, insecureHTTPOptIn)
}

// isLoopbackHost reports whether host is localhost or a loopback IP
// (127.0.0.0/8 or ::1). Other names are not resolved: a name that resolves to
// loopback today may not tomorrow, and resolving here would not bind the
// request to the address that was checked.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// fetchSpecFromURL fetches an OpenAPI/AsyncAPI spec from a URL.
//
// ctx is the caller's cancellation context, not context.Background(): a
// watch's Ctrl+C has to reach an in-flight fetch, or the process outlives the
// signal that was supposed to stop it.
//
// Plain http is refused for anything but loopback unless allowInsecureHTTP is
// set (see checkSpecURL), including on a redirect, and the body is capped at
// maxSpecBytes.
func fetchSpecFromURL(ctx context.Context, rawURL string, timeout time.Duration, allowInsecureHTTP bool) ([]byte, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := checkSpecURL(req.URL, allowInsecureHTTP); err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	// An https URL that redirects to plain http would otherwise bypass the
	// check above, so every hop is checked the same way. The hop limit matches
	// net/http's default policy, which a custom CheckRedirect replaces.
	httpClient := &http.Client{
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}

			return checkSpecURL(next.URL, allowInsecureHTTP)
		},
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch spec failed with status: %d", resp.StatusCode)
	}

	// One byte past the cap is read so a body of exactly maxSpecBytes is
	// accepted and anything longer is reported rather than silently truncated.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if int64(len(data)) > maxSpecBytes {
		return nil, fmt.Errorf("spec is larger than the %d MB limit", maxSpecBytes>>20)
	}

	return data, nil
}

// autoDiscoverSpec tries to find a spec file in common locations.
func autoDiscoverSpec(rootDir string, paths []string) (string, error) {
	if len(paths) == 0 {
		paths = DefaultClientConfig().Source.AutoDiscoverPaths
	}

	for _, path := range paths {
		// Make path absolute if relative
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(rootDir, path)
		}

		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", errors.New("no spec file found in auto-discover paths")
}
