// cmd/forge/plugins/dashboard_test.go
package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

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

// TestScaffoldDashboardPackageJSONNamesPublishedPackages is the load-bearing
// assertion for this task: the scaffold's package.json must depend on all
// three package names Task 2 published under @forge-go. dashboard-runtime
// joined dashboard-plugin and dashboard-kit in fix round 1, when review
// caught that App.tsx used no error boundary -- ForgeDashboardProvider and
// PluginErrorBoundary both live in dashboard-runtime, so the dependency
// followed the fix. This is also the discriminator target -- see the task
// report for the break/confirm-fail/restore/paste-real-output cycle run
// against this test.
func TestScaffoldDashboardPackageJSONNamesPublishedPackages(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

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

// TestScaffoldDashboardAppUsesPluginErrorBoundary is the fix-round-1
// discriminator target: the scaffold must wrap both places apps/shell's own
// PluginHost wraps in PluginErrorBoundary -- a plugin's setup panel and its
// route element -- so a third-party plugin's throw is contained to its own
// box instead of blanking the whole dashboard. Two usage sites, not one: a
// boundary around only the route (or only the setup panel) is the same class
// of gap W2 shipped and a review caught.
func TestScaffoldDashboardAppUsesPluginErrorBoundary(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

	raw, err := os.ReadFile(filepath.Join(dir, "src", "App.tsx"))
	require.NoError(t, err)
	src := string(raw)

	assert.Contains(t, src, "@forge-go/dashboard-runtime", "App.tsx must import from @forge-go/dashboard-runtime")

	usages := strings.Count(src, "<PluginErrorBoundary")
	assert.GreaterOrEqualf(t, usages, 2,
		"App.tsx must wrap both the plugin setup panel and the route element in <PluginErrorBoundary>, found %d usage(s):\n%s",
		usages, src)
}

// TestScaffoldDashboardPackageJSONNameIsSanitized covers the directory-name
// -> npm-package-name path: a target directory whose basename is not a legal
// unscoped npm package name (uppercase, spaces, leading dots) still produces
// a package.json that parses and has a usable name.
func TestScaffoldDashboardPackageJSONNameIsSanitized(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	require.NoError(t, p.scaffoldDashboard(dir, "My Cool Dashboard!!"))

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
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

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
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	readme := string(raw)

	assert.Contains(t, readme, "ShellExternal")
	assert.Contains(t, readme, "pnpm add")
	assert.Contains(t, readme, "pnpm build")
	assert.Contains(t, readme, "404")
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
