// v2/cmd/forge/plugins/doctor.go
package plugins

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

// DoctorPlugin provides system diagnostics.
type DoctorPlugin struct {
	config *config.ForgeConfig
}

// NewDoctorPlugin creates a new doctor plugin.
func NewDoctorPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &DoctorPlugin{config: cfg}
}

func (p *DoctorPlugin) Name() string           { return "doctor" }
func (p *DoctorPlugin) Version() string        { return "1.0.0" }
func (p *DoctorPlugin) Description() string    { return "System diagnostics and health checks" }
func (p *DoctorPlugin) Dependencies() []string { return nil }
func (p *DoctorPlugin) Initialize() error      { return nil }

func (p *DoctorPlugin) Commands() []cli.Command {
	return []cli.Command{
		cli.NewCommand(
			"doctor",
			"Check system requirements and project health",
			p.runDoctor,
			cli.WithFlag(cli.NewBoolFlag("verbose", "v", "Show verbose output", false)),
		),
	}
}

func (p *DoctorPlugin) runDoctor(ctx cli.CommandContext) error {
	verbose := ctx.Bool("verbose")

	ctx.Info("Forge v2 System Diagnostics")
	ctx.Println("=" + strings.Repeat("=", 50))
	ctx.Println("")

	// System checks
	ctx.Info("System Information:")
	ctx.Println("  OS: " + runtime.GOOS)
	ctx.Println("  Arch: " + runtime.GOARCH)
	ctx.Println("  Go Version: " + runtime.Version())
	ctx.Println("")

	// Environment checks
	ctx.Info("Environment Checks:")
	table := ctx.Table()
	table.SetHeader([]string{"Check", "Status", "Version/Details"})

	// Go version
	goVersion, goOk := p.checkGo()
	if goOk {
		table.AppendRow([]string{"Go", cli.Green("✓ OK"), goVersion})
	} else {
		table.AppendRow([]string{"Go", cli.Red("✗ Failed"), "Not found or too old"})
	}

	// Git
	gitVersion, gitOk := p.checkGit()
	if gitOk {
		table.AppendRow([]string{"Git", cli.Green("✓ OK"), gitVersion})
	} else {
		table.AppendRow([]string{"Git", cli.Yellow("⚠ Optional"), "Not found"})
	}

	// Docker
	dockerVersion, dockerOk := p.checkDocker()
	if dockerOk {
		table.AppendRow([]string{"Docker", cli.Green("✓ OK"), dockerVersion})
	} else {
		table.AppendRow([]string{"Docker", cli.Yellow("⚠ Optional"), "Not found"})
	}

	// Docker Compose
	composeVersion, composeOk := p.checkDockerCompose()
	if composeOk {
		table.AppendRow([]string{"Docker Compose", cli.Green("✓ OK"), composeVersion})
	} else {
		table.AppendRow([]string{"Docker Compose", cli.Yellow("⚠ Optional"), "Not found"})
	}

	// Kubectl
	kubectlVersion, kubectlOk := p.checkKubectl()
	if kubectlOk {
		table.AppendRow([]string{"kubectl", cli.Green("✓ OK"), kubectlVersion})
	} else {
		table.AppendRow([]string{"kubectl", cli.Yellow("⚠ Optional"), "Not found"})
	}

	table.Render()
	ctx.Println("")

	// Project checks
	if p.config != nil {
		ctx.Info("Project Configuration:")
		ctx.Println("  Name: " + p.config.Project.Name)
		ctx.Println("  Layout: " + p.config.Project.Layout)
		ctx.Println("  Type: " + p.config.Project.Type)
		ctx.Println("  Config: " + p.config.ConfigPath)
		ctx.Println("")

		// Check project structure
		ctx.Info("Project Structure:")
		structureTable := ctx.Table()
		structureTable.SetHeader([]string{"Component", "Status", "Location"})

		if p.config.IsSingleModule() {
			p.checkDirectory(structureTable, "cmd", p.config.Project.Structure.Cmd)
			p.checkDirectory(structureTable, "apps", p.config.Project.Structure.Apps)
			p.checkDirectory(structureTable, "pkg", p.config.Project.Structure.Pkg)
			p.checkDirectory(structureTable, "internal", p.config.Project.Structure.Internal)
		} else {
			p.checkDirectory(structureTable, "apps", "./apps")
			p.checkDirectory(structureTable, "services", "./services")
			p.checkDirectory(structureTable, "pkg", "./pkg")
		}

		p.checkDirectory(structureTable, "database", p.config.Database.MigrationsPath)
		structureTable.Render()
		ctx.Println("")
	} else {
		ctx.Warning("No .forge.yaml found - some checks skipped")
		ctx.Info("Run 'forge init' to initialize a new project")
		ctx.Println("")
	}

	// Summary
	ctx.Info("Summary:")

	if goOk {
		ctx.Println(cli.Green("  ✓ Your system is ready for Forge development!"))
	} else {
		ctx.Println(cli.Red("  ✗ Please install Go 1.21 or later"))
	}

	if !dockerOk && verbose {
		ctx.Println(cli.Yellow("  ⚠ Docker is optional but recommended for deployment"))
	}

	ctx.Println("")
	ctx.Info("Next Steps:")

	if p.config == nil {
		ctx.Println("  1. Run: forge init")
		ctx.Println("  2. Run: forge generate:app --name=my-app")
		ctx.Println("  3. Run: forge dev")
	} else {
		ctx.Println("  1. Run: forge dev")
		ctx.Println("  2. Generate code: forge generate:*")
		ctx.Println("  3. Build: forge build")
	}

	return nil
}

func (p *DoctorPlugin) checkGo() (string, bool) {
	cmd := exec.Command("go", "version")

	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(output)), true
}

func (p *DoctorPlugin) checkGit() (string, bool) {
	cmd := exec.Command("git", "--version")

	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(output)), true
}

func (p *DoctorPlugin) checkDocker() (string, bool) {
	cmd := exec.Command("docker", "--version")

	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(output)), true
}

func (p *DoctorPlugin) checkDockerCompose() (string, bool) {
	cmd := exec.Command("docker", "compose", "version")

	output, err := cmd.Output()
	if err != nil {
		// Try old docker-compose
		cmd = exec.Command("docker-compose", "--version")

		output, err = cmd.Output()
		if err != nil {
			return "", false
		}
	}

	return strings.TrimSpace(string(output)), true
}

func (p *DoctorPlugin) checkKubectl() (string, bool) {
	cmd := exec.Command("kubectl", "version", "--client", "--short")

	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(output)), true
}

func (p *DoctorPlugin) checkDirectory(table cli.TableWriter, name, path string) {
	fullPath := p.config.RootDir + "/" + path
	if p.directoryExists(fullPath) {
		table.AppendRow([]string{name, cli.Green("✓ Exists"), path})
	} else {
		table.AppendRow([]string{name, cli.Yellow("○ Missing"), path})
	}
}

func (p *DoctorPlugin) directoryExists(path string) bool {
	cmd := exec.Command("test", "-d", path)

	return cmd.Run() == nil
}
