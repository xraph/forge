// cmd/forge/plugins/dashboard.go
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
)

// DashboardPlugin scaffolds a standalone dashboard shell for deployments that
// build their own UI and serve it themselves, as an alternative to the
// prebuilt shell Forge embeds and serves at {BasePath}/ui by default.
//
// This is a separate namespace from `forge contributor`, which scaffolds a
// Go-side dashboard *contributor* (a backend extension that reports pages,
// widgets and settings into the embedded shell). `forge dashboard` scaffolds
// the shell itself, for the WithShellSource(ShellExternal) case where nobody
// on the Forge side is building it for you.
type DashboardPlugin struct {
	config *config.ForgeConfig
}

// NewDashboardPlugin creates a new dashboard plugin.
func NewDashboardPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &DashboardPlugin{config: cfg}
}

func (p *DashboardPlugin) Name() string    { return "dashboard" }
func (p *DashboardPlugin) Version() string { return "1.0.0" }
func (p *DashboardPlugin) Description() string {
	return "Standalone dashboard shell scaffolding (for WithShellSource(ShellExternal))"
}
func (p *DashboardPlugin) Dependencies() []string { return nil }
func (p *DashboardPlugin) Initialize() error      { return nil }

func (p *DashboardPlugin) Commands() []cli.Command {
	dashboardCmd := cli.NewCommand(
		"dashboard",
		"Standalone dashboard shell tools (for WithShellSource(ShellExternal))",
		nil, // No handler, requires subcommand
	)

	dashboardCmd.AddSubcommand(cli.NewCommand(
		"new",
		"Scaffold a standalone dashboard shell (Vite or Next.js)",
		p.newDashboard,
		cli.WithFlag(cli.NewStringFlag("name", "n", "Package name (defaults to the target directory's name)", "")),
		cli.WithFlag(cli.NewStringFlag("target", "t", "Scaffold target: vite or next", "vite")),
	))

	return []cli.Command{dashboardCmd}
}

// newDashboard is the `forge dashboard new` command handler. It only parses
// arguments and reports progress; the actual file generation lives in
// scaffoldDashboard so it can be tested directly without a CommandContext.
func (p *DashboardPlugin) newDashboard(ctx cli.CommandContext) error {
	targetArg := ctx.Arg(0)
	if targetArg == "" {
		var err error
		targetArg, err = ctx.Prompt("Target directory:")
		if err != nil {
			return err
		}
	}
	if targetArg == "" {
		return errors.New("target directory is required")
	}

	targetDir, err := filepath.Abs(targetArg)
	if err != nil {
		return err
	}

	name := ctx.String("name")
	if name == "" {
		name = filepath.Base(targetDir)
	}

	target := ctx.String("target")

	spinner := ctx.Spinner(fmt.Sprintf("Scaffolding dashboard shell in %s...", targetDir))

	if err := p.scaffoldDashboard(targetDir, name, target); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Dashboard shell scaffolded!"))

	cwd, _ := os.Getwd()

	ctx.Println("")
	ctx.Success("Next steps:")
	ctx.Println(fmt.Sprintf("  1. cd %s", relPath(cwd, targetDir)))
	if target == "next" {
		ctx.Println("  2. pnpm install")
		ctx.Println("  3. Add plugins to the `plugins` array in app/admin/[[...slug]]/page.tsx")
		ctx.Println("  4. Set FORGE_URL to your Forge server's origin")
		ctx.Println("  5. pnpm dev")
	} else {
		ctx.Println("  2. pnpm install")
		ctx.Println("  3. pnpm add <your plugin package>, then list it in the `plugins` array in src/App.tsx")
		ctx.Println("  4. pnpm build")
		ctx.Println("  5. Serve dist/ yourself and pass WithShellSource(dashboard.ShellExternal) to dashboard.NewExtension")
	}
	ctx.Println("")
	// Said here and not only in the source comments, because the person who
	// hits it is standing at step 2 with a registry error and no idea whether
	// they typed something wrong. The @forge-go packages are not published yet.
	ctx.Println("Note: the @forge-go/dashboard-* packages this depends on are not")
	ctx.Println("published to npm yet, so step 2 will fail to resolve them until they are.")
	if target != "next" {
		ctx.Println("")
		ctx.Println("See README.md in the scaffolded directory for details.")
	}

	return nil
}

// dashboardScaffoldData is the template data for every file scaffoldDashboard
// writes.
type dashboardScaffoldData struct {
	// Name is the sanitized npm package name written into package.json.
	Name string
	// DisplayName is a human-readable title, used in index.html and README.md.
	DisplayName string
}

// scaffoldFile pairs a destination path (relative to targetDir, which may
// include subdirectories the caller must create) with the template that
// fills it.
type scaffoldFile struct {
	rel  string
	tmpl string
}

// dashboardViteFiles is the file set scaffoldDashboard has always written.
// Every path here is a single top-level segment or one level under src/, so
// the loop's MkdirAll(filepath.Dir(dest)) is a no-op past that -- unlike
// dashboardNextFiles below, whose App Router paths nest several levels deep.
var dashboardViteFiles = []scaffoldFile{
	{"package.json", dashboardPackageJSONTemplate},
	{"vite.config.ts", dashboardViteConfigTemplate},
	{"tsconfig.json", dashboardTSConfigTemplate},
	{"index.html", dashboardIndexHTMLTemplate},
	{"README.md", dashboardReadmeTemplate},
	{".gitignore", dashboardGitignoreTemplate},
	{filepath.Join("src", "main.tsx"), dashboardMainTSXTemplate},
	{filepath.Join("src", "App.tsx"), dashboardAppTSXTemplate},
}

// dashboardNextFiles scaffolds a dashboard mounted inside an existing Next.js
// App Router app, instead of a standalone Vite shell: a client page under a
// catch-all route, and an API route that proxies the dashboard's data
// contract to a real Forge server. See dashboard_templates.go for why the
// page must be a client component and why the route proxies rather than
// pointing the browser straight at Forge.
var dashboardNextFiles = []scaffoldFile{
	{"package.json", dashboardNextPackageJSONTemplate},
	{filepath.Join("app", "admin", "[[...slug]]", "page.tsx"), dashboardNextPageTemplate},
	{filepath.Join("app", "api", "forge", "[...path]", "route.ts"), dashboardNextRouteTemplate},
}

// filesForTarget resolves a --target value to the file set scaffoldDashboard
// writes. "vite" is the default and must stay so -- it is what every
// scaffold produced before --target existed, and the `dashboard new` flag
// itself defaults to it.
func filesForTarget(target string) ([]scaffoldFile, error) {
	switch target {
	case "", "vite":
		return dashboardViteFiles, nil
	case "next":
		return dashboardNextFiles, nil
	default:
		return nil, fmt.Errorf("unknown dashboard scaffold target %q: want vite or next", target)
	}
}

// scaffoldDashboard writes a standalone dashboard shell into targetDir, in
// the shape target selects. The "vite" target (the default) depends on all
// three @forge-go packages the standalone dashboard front end is split into:
// dashboard-plugin, dashboard-kit, and dashboard-runtime (the last supplies
// ForgeDashboardProvider and PluginErrorBoundary -- a third-party plugin's
// throw must not blank the whole dashboard, and that containment matters
// more in a custom build than in the first-party shell, not less). The
// "next" target instead mounts the dashboard inside an existing Next.js app
// via dashboard-host and dashboard-next; see dashboard_templates.go.
//
// It does not run `pnpm install`, `pnpm build`, or anything else that needs a
// registry -- those packages are not published yet, so a scaffolded project
// cannot install today. That is expected until they are released; it is not
// a bug in the scaffold.
func (p *DashboardPlugin) scaffoldDashboard(targetDir, rawName, target string) error {
	if strings.TrimSpace(rawName) == "" {
		rawName = "dashboard"
	}

	files, err := filesForTarget(target)
	if err != nil {
		return err
	}

	data := dashboardScaffoldData{
		Name:        npmPackageName(rawName),
		DisplayName: toDisplayName(strings.ReplaceAll(rawName, "-", "_")),
	}

	for _, f := range files {
		dest := filepath.Join(targetDir, f.rel)
		// Next's App Router paths nest ("app/admin/[[...slug]]/page.tsx"), so
		// unlike the flat Vite file set this needs an explicit MkdirAll per
		// file rather than one fixed "src" directory up front.
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := writeTemplate(dest, f.tmpl, data); err != nil {
			return err
		}
	}

	return nil
}

// npmPackageNamePattern matches characters legal in an unscoped npm package
// name: lowercase letters, digits, hyphens, underscores and dots.
var npmPackageNamePattern = regexp.MustCompile(`[^a-z0-9._-]+`)

// npmPackageName sanitizes an arbitrary directory or flag value into a name
// npm's package.json will accept: lowercased, invalid characters collapsed to
// a hyphen, and leading dots/underscores (which npm also rejects) stripped.
// Falls back to "dashboard" if that leaves nothing usable.
func npmPackageName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = npmPackageNamePattern.ReplaceAllString(s, "-")
	s = strings.TrimLeft(s, "._-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "dashboard"
	}
	return s
}
