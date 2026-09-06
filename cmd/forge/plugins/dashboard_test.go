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
// contract under test is "the right two @forge-go packages are named",
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
// assertion for this task: the scaffold's package.json must depend on
// exactly the two package names Task 2 published under @forge-go. This is
// also the discriminator target -- see the task report for the
// break/confirm-fail/restore/paste-real-output cycle run against this test.
func TestScaffoldDashboardPackageJSONNamesPublishedPackages(t *testing.T) {
	dir := t.TempDir()
	p := &DashboardPlugin{}
	require.NoError(t, p.scaffoldDashboard(dir, "my-dashboard"))

	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)

	var pkg scaffoldDashboardPackageJSON
	require.NoErrorf(t, json.Unmarshal(raw, &pkg), "package.json must parse as JSON:\n%s", raw)

	assert.Equal(t, "my-dashboard", pkg.Name)

	_, hasPlugin := pkg.Dependencies["@forge-go/dashboard-plugin"]
	assert.True(t, hasPlugin, "package.json dependencies must name @forge-go/dashboard-plugin, got: %v", pkg.Dependencies)

	_, hasKit := pkg.Dependencies["@forge-go/dashboard-kit"]
	assert.True(t, hasKit, "package.json dependencies must name @forge-go/dashboard-kit, got: %v", pkg.Dependencies)
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
