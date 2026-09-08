// cmd/forge/plugins/dashboard_test.go
package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scaffoldDashboardPackageJSON is the shape this test cares about. It leaves
// everything else in package.json (scripts, devDependencies, ...) alone: the
// contract under test is "the right three @forge-go packages are named",
// nothing more.
type scaffoldDashboardPackageJSON struct {
	Name         string            `json:"name"`
	Dependencies map[string]string `json:"dependencies"`
}

// TestScaffoldDashboardWritesExpectedFiles pins the full file set a fresh
// scaffold produces, so an accidental extra or missing file shows up as a
// diff here instead of silently in someone's checkout.
func TestScaffoldDashboardWritesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "my-dashboard", "vite")
	require.NoError(t, err)

	want := []string{
		"package.json",
		"vite.config.ts",
		"tsconfig.json",
		"index.html",
		"README.md",
		".gitignore",
		filepath.Join("src", "main.tsx"),
		filepath.Join("src", "App.tsx"),
	}

	for _, rel := range want {
		path := filepath.Join(dir, rel)
		info, err := os.Stat(path)
		require.NoErrorf(t, err, "expected scaffold to write %s", rel)
		assert.Greaterf(t, info.Size(), int64(0), "%s should not be empty", rel)
	}
}

// TestScaffoldDashboardPackageJSONNamesPublishedPackages pins the scaffold's
// dependency set: package.json must name all three @forge-go packages the
// dashboard front end is split into. dashboard-runtime is easy to leave out
// and was, once -- it is the one that supplies ForgeDashboardProvider and
// PluginErrorBoundary, so App.tsx cannot render or contain a plugin without
// it, and a scaffold missing it fails only at the user's first build.
func TestScaffoldDashboardPackageJSONNamesPublishedPackages(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "my-dashboard", "vite")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)

	var pkg scaffoldDashboardPackageJSON
	require.NoErrorf(t, json.Unmarshal(raw, &pkg), "package.json must parse as JSON:\n%s", raw)

	assert.Equal(t, "my-dashboard", pkg.Name)

	for _, name := range []string{
		"@forge-go/dashboard-plugin",
		"@forge-go/dashboard-kit",
		"@forge-go/dashboard-runtime",
	} {
		_, has := pkg.Dependencies[name]
		assert.Truef(t, has, "package.json dependencies must name %s, got: %v", name, pkg.Dependencies)
	}
}

// pluginErrorBoundaryWrapsSetup and pluginErrorBoundaryWrapsRoute anchor the
// boundary check to structure, not to a bare occurrence count.
//
// A counting assertion (`strings.Count(src, "<PluginErrorBoundary") >= 2`)
// came first. It discriminated against the historical shell regression --
// one boundary, or none -- but could not tell two correct placements apart
// from two boundaries stacked on the *same* element (both wrapping the route,
// say, with the setup panel left bare). That is exactly the shape the
// regression had, reproduced with a passing count. These two patterns instead
// require a PluginErrorBoundary's opening tag to be the immediate parent of
// <Setup and, separately, of <PluginProvider (which itself wraps the routed
// <Page/>) -- so a boundary that wraps the wrong element, or wraps neither,
// fails the corresponding pattern regardless of how many boundaries exist
// elsewhere in the file.
var (
	pluginErrorBoundaryWrapsSetup = regexp.MustCompile(`<PluginErrorBoundary[^>]*>\s*<Setup\b`)
	pluginErrorBoundaryWrapsRoute = regexp.MustCompile(`<PluginErrorBoundary[^>]*>\s*<PluginProvider\b`)
)

// TestScaffoldDashboardAppUsesPluginErrorBoundary requires the scaffold to
// wrap both of the places apps/shell's own PluginHost wraps in
// PluginErrorBoundary -- a plugin's setup panel and its route element -- so
// a third-party plugin's throw is contained to its own box instead of
// blanking the whole dashboard.
//
// Both sites, not just two occurrences of the tag. The regression this
// guards against stacks two boundaries on the route and leaves the setup
// panel bare, which a counting assertion reads as correct; see the comment
// on the two patterns above.
func TestScaffoldDashboardAppUsesPluginErrorBoundary(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "my-dashboard", "vite")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "src", "App.tsx"))
	require.NoError(t, err)
	src := string(raw)

	assert.Contains(t, src, "@forge-go/dashboard-runtime", "App.tsx must import from @forge-go/dashboard-runtime")

	assert.Truef(t, pluginErrorBoundaryWrapsSetup.MatchString(src),
		"App.tsx must wrap the plugin setup panel (<Setup .../>) directly in <PluginErrorBoundary>, source:\n%s", src)

	assert.Truef(t, pluginErrorBoundaryWrapsRoute.MatchString(src),
		"App.tsx must wrap the route's <PluginProvider>...<Page/></PluginProvider> directly in <PluginErrorBoundary>, source:\n%s", src)
}

// TestScaffoldDashboardPackageJSONNameIsSanitized covers the directory-name
// -> npm-package-name path: a target directory whose basename is not a legal
// unscoped npm package name (uppercase, spaces, leading dots) still produces
// a package.json that parses and has a usable name.
func TestScaffoldDashboardPackageJSONNameIsSanitized(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "My Cool Dashboard!!", "vite")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)

	var pkg scaffoldDashboardPackageJSON
	require.NoError(t, json.Unmarshal(raw, &pkg))

	assert.Equal(t, "my-cool-dashboard", pkg.Name)
	assert.NotContains(t, pkg.Name, " ")
	assert.NotContains(t, pkg.Name, "!")
	assert.Equal(t, strings.ToLower(pkg.Name), pkg.Name)
}

// TestScaffoldDashboardViteConfigSetsRelativeBase pins the one line the task
// brief calls "not optional and not cosmetic": an absolute Vite base bakes a
// specific mount into both index.html's asset URLs and the preload resolver
// for lazy chunks, breaking any deployment under a different BasePath.
func TestScaffoldDashboardViteConfigSetsRelativeBase(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "my-dashboard", "vite")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "vite.config.ts"))
	require.NoError(t, err)

	assert.Contains(t, string(raw), `base: "./"`)
}

// TestScaffoldDashboardReadmeCoversShellExternal guards against a README that
// implies Forge serves the build for the user -- it does not, once
// WithShellSource(ShellExternal) is set.
func TestScaffoldDashboardReadmeCoversShellExternal(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	_, err := p.scaffoldDashboard(dir, "my-dashboard", "vite")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	readme := string(raw)

	assert.Contains(t, readme, "ShellExternal")
	assert.Contains(t, readme, "pnpm add")
	assert.Contains(t, readme, "pnpm build")
	assert.Contains(t, readme, "404")
}

// TestScaffoldDashboardNextTarget covers --target=next: the App Router mount
// and proxy route land at their nested paths, no vite.config.ts is emitted,
// and the page starts with "use client" -- required because the host it
// renders (ForgeDashboard, from @forge-go/dashboard-host) mounts a
// BrowserRouter, which needs the browser.
func TestScaffoldDashboardNextTarget(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}

	_, err := p.scaffoldDashboard(dir, "my-dash", "next")
	require.NoError(t, err)

	for _, want := range []string{
		"app/admin/[[...slug]]/page.tsx",
		"app/api/forge/[...path]/route.ts",
		"package.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "vite.config.ts")); err == nil {
		t.Error("next target emitted vite.config.ts")
	}

	page, err := os.ReadFile(filepath.Join(dir, "app/admin/[[...slug]]/page.tsx"))
	require.NoError(t, err)
	// BrowserRouter needs the browser, so the mount must be a client component.
	if !strings.HasPrefix(string(page), `"use client"`) {
		t.Error("page.tsx does not start with the use client directive")
	}
}

// TestScaffoldDashboardNextDoesNotOverwriteExistingFiles is the regression
// test for the critical finding in the final review: dashboardNextFiles
// includes package.json, writeTemplate is a bare os.WriteFile (which
// truncates), and the "next" target is documented as writing into an
// *existing* Next app -- whose package.json holds the app's real name,
// dependencies, scripts and package manager config. Without a guard,
// scaffolding into a real app would have silently replaced all of that with
// the 10-dependency Next stub, unrecoverable outside git. Pre-seeding a
// distinctive package.json and asserting it is byte-for-byte unchanged is
// the only way to catch this: every prior test scaffolds into an empty
// t.TempDir(), so none of them could have seen it.
func TestScaffoldDashboardNextDoesNotOverwriteExistingFiles(t *testing.T) {
	dir := t.TempDir()
	existing := []byte(`{"name":"my-real-nextjs-app","version":"3.4.1","dependencies":{"next":"15.0.0"}}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), existing, 0644))

	p := &DashboardPlugin{}
	skipped, err := p.scaffoldDashboard(dir, "my-dash", "next")
	require.NoError(t, err)

	assert.Contains(t, skipped, "package.json", "scaffoldDashboard must report package.json as skipped")

	raw, readErr := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, readErr)
	assert.Equal(t, existing, raw, "an existing package.json must survive scaffoldDashboard byte-for-byte")

	// The two files that genuinely did not exist yet must still be written --
	// the guard protects existing files, it must not turn into a blanket
	// refusal that makes the command useless against a real app.
	for _, want := range []string{
		"app/admin/[[...slug]]/page.tsx",
		"app/api/forge/[...path]/route.ts",
	} {
		if _, statErr := os.Stat(filepath.Join(dir, want)); statErr != nil {
			t.Errorf("missing %s: %v", want, statErr)
		}
	}
}

// TestScaffoldDashboardNextPageContractBase pins the one value in the Next
// scaffold that is not derivable from either repo alone, and was wrong in an
// earlier draft of this template: contractBase must resolve, once proxied,
// to wherever Forge actually mounts the dashboard contract.
//
// extension.go mounts the contract at BasePath + "/api/dashboard/v1"
// (BasePath defaults to "/dashboard"). The generated route captures
// everything after "/api/forge/" and forwards it to FORGE_URL verbatim, so
// contractBase's suffix after "/api/forge" has to spell out
// "api/dashboard/v1" itself -- "/api/forge/dashboard/v1" (missing the
// "api/" segment) silently 404s instead. With the documented
// FORGE_URL=http://localhost:8080/dashboard, a browser request to
// contractBase resolves to http://localhost:8080/dashboard/api/dashboard/v1,
// matching where Forge actually serves it.
func TestScaffoldDashboardNextPageContractBase(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}

	_, err := p.scaffoldDashboard(dir, "my-dash", "next")
	require.NoError(t, err)

	page, err := os.ReadFile(filepath.Join(dir, "app/admin/[[...slug]]/page.tsx"))
	require.NoError(t, err)

	assert.Contains(t, string(page), `contractBase: "/api/forge/api/dashboard/v1"`,
		"contractBase must proxy to Forge's actual mount, {BasePath}/api/dashboard/v1")
}

// dashboardNextDynamicImportPattern requires ForgeDashboard to be loaded
// through next/dynamic with ssr:false, not a direct named import. A loose
// strings.Contains(page, "ForgeDashboard") check would pass against the
// broken direct-import version too, since the identifier still appears in
// the JSX at the bottom of the page -- this anchors on the actual dynamic()
// call shape instead: the dashboard-host import as dynamic()'s loader,
// immediately paired with { ssr: false }.
var dashboardNextDynamicImportPattern = regexp.MustCompile(
	`const ForgeDashboard = dynamic\(\s*\(\)\s*=>\s*import\("@forge-go/dashboard-host"\)\.then\(\(mod\)\s*=>\s*mod\.ForgeDashboard\),\s*\{\s*ssr:\s*false\s*\}`,
)

// TestScaffoldDashboardNextPageLoadsForgeDashboardDynamically guards against
// the exact crash Task 8 hit against a live server: the App Router still
// server-renders "use client" pages on first load, and ForgeDashboard
// composes a BrowserRouter and base-ui portal components that touch
// `document` during render, not just in effects, so a direct import of it
// throws "document is not defined" on that server pass. Loading it through
// next/dynamic with ssr:false skips the server pass.
func TestScaffoldDashboardNextPageLoadsForgeDashboardDynamically(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}

	_, err := p.scaffoldDashboard(dir, "my-dash", "next")
	require.NoError(t, err)

	page, err := os.ReadFile(filepath.Join(dir, "app/admin/[[...slug]]/page.tsx"))
	require.NoError(t, err)
	src := string(page)

	assert.NotContains(t, src, `import { ForgeDashboard } from "@forge-go/dashboard-host"`,
		"ForgeDashboard must not be imported directly -- that is the exact form that crashes with \"document is not defined\" on the server-rendered first load")

	// dashboardNextDynamicImportPattern alone would still pass on a page
	// missing this import line entirely -- the dynamic() call shape it
	// matches never mentions "next/dynamic" by name -- so a template that
	// dropped this import would compile the regexp check clean while
	// shipping a page that fails to build. Assert the import exists too.
	assert.Contains(t, src, `import dynamic from "next/dynamic"`,
		"page.tsx must import dynamic from next/dynamic")

	assert.Truef(t, dashboardNextDynamicImportPattern.MatchString(src),
		"page.tsx must load ForgeDashboard via next/dynamic with ssr:false, source:\n%s", src)
}

// TestScaffoldDashboardNextRouteDocumentsForgeURL pins the FORGE_URL
// explanation inside the generated route.ts itself, not just in a Go source
// comment or transient console output. That it must be the dashboard's base
// URL (including BasePath), not the bare server origin, is exactly the
// value class the contractBase fix above addressed: not derivable from
// either repo in isolation, so it has to survive as part of the artifact a
// developer actually opens, not just in git history.
func TestScaffoldDashboardNextRouteDocumentsForgeURL(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}

	_, err := p.scaffoldDashboard(dir, "my-dash", "next")
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "app/api/forge/[...path]/route.ts"))
	require.NoError(t, err)
	route := string(raw)

	assert.Contains(t, route, "FORGE_URL",
		"route.ts must mention FORGE_URL directly, not only in a Go source comment")
	assert.Contains(t, route, "BasePath",
		"route.ts must explain that FORGE_URL needs Forge's BasePath included, not just the server origin")
	assert.Contains(t, route, "http://localhost:8080/dashboard",
		"route.ts must give a concrete example FORGE_URL value")
}

// TestScaffoldDashboardUnknownTarget guards filesForTarget's default case: an
// unrecognized --target must fail loudly rather than silently falling back
// to the Vite scaffold.
func TestScaffoldDashboardUnknownTarget(t *testing.T) {
	p := &DashboardPlugin{}
	if _, err := p.scaffoldDashboard(t.TempDir(), "my-dash", "svelte"); err == nil {
		t.Error("expected an error for an unknown target")
	}
}

// TestNpmPackageNameSanitization exercises the sanitizer directly, since it
// is the one piece of scaffold logic with real branching (empty input,
// leading punctuation, mixed case, invalid characters).
func TestNpmPackageNameSanitization(t *testing.T) {
	cases := map[string]string{
		"my-dashboard":        "my-dashboard",
		"My Cool Dashboard!!": "my-cool-dashboard",
		"":                    "dashboard",
		"   ":                 "dashboard",
		"...":                 "dashboard",
		"_leading":            "leading",
		"Already-Fine_123":    "already-fine_123",
	}

	for input, want := range cases {
		assert.Equalf(t, want, npmPackageName(input), "npmPackageName(%q)", input)
	}
}
