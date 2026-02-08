// v2/cmd/forge/config/types.go
package config

import "time"

// ForgeConfig represents the complete .forge.yaml configuration.
type ForgeConfig struct {
	Project    ProjectConfig  `yaml:"project"`
	Dev        DevConfig      `yaml:"dev,omitempty"`
	Database   DatabaseConfig `yaml:"database,omitempty"`
	Build      BuildConfig    `yaml:"build,omitempty"`
	Deploy     DeployConfig   `yaml:"deploy,omitempty"`
	Generate   GenerateConfig `yaml:"generate,omitempty"`
	Extensions map[string]any `yaml:"extensions,omitempty"`
	Test       TestConfig     `yaml:"test,omitempty"`

	// Internal fields
	RootDir    string `yaml:"-"` // Directory containing .forge.yaml
	ConfigPath string `yaml:"-"` // Full path to .forge.yaml
}

// ProjectConfig defines project metadata and structure.
type ProjectConfig struct {
	Name        string           `yaml:"name"`
	Version     string           `yaml:"version,omitempty"`
	Description string           `yaml:"description,omitempty"`
	Type        string           `yaml:"type,omitempty"`      // Default: "monorepo"
	Layout      string           `yaml:"layout,omitempty"`    // Default: "single-module"
	Module      string           `yaml:"module,omitempty"`    // Required for single-module
	Workspace   WorkspaceConfig  `yaml:"workspace,omitempty"` // For multi-module layout
	Structure   *StructureConfig `yaml:"structure,omitempty"` // For single-module layout (optional, uses conventions)
}

// WorkspaceConfig for multi-module layout.
type WorkspaceConfig struct {
	Enabled    bool   `yaml:"enabled,omitempty"`
	Apps       string `yaml:"apps,omitempty"`       // Glob pattern: "./apps/*"
	Services   string `yaml:"services,omitempty"`   // Glob pattern: "./services/*"
	Extensions string `yaml:"extensions,omitempty"` // Glob pattern: "./extensions/*"
	Pkg        string `yaml:"pkg,omitempty"`        // Path: "./pkg"
}

// StructureConfig for single-module layout.
type StructureConfig struct {
	Cmd         string `yaml:"cmd,omitempty"`         // Path to cmd/ (default: ./cmd)
	Apps        string `yaml:"apps,omitempty"`        // Path to apps/ (default: ./apps)
	Pkg         string `yaml:"pkg,omitempty"`         // Path to pkg/ (default: ./pkg)
	Internal    string `yaml:"internal,omitempty"`    // Path to internal/ (default: ./internal)
	Extensions  string `yaml:"extensions,omitempty"`  // Path to extensions/ (default: ./extensions)
	Database    string `yaml:"database,omitempty"`    // Path to database/ (default: ./database)
	Config      string `yaml:"config,omitempty"`      // Path to config/ (default: ./config)
	Deployments string `yaml:"deployments,omitempty"` // Path to deployments/ (default: ./deployments)
}

// DevConfig defines development server configuration.
type DevConfig struct {
	AutoDiscover    bool            `yaml:"auto_discover,omitempty"`
	DiscoverPattern string          `yaml:"discover_pattern,omitempty"`
	DefaultApp      string          `yaml:"default_app,omitempty"`
	Watch           WatchConfig     `yaml:"watch,omitempty"`
	HotReload       HotReloadConfig `yaml:"hot_reload,omitempty"`
}

// WatchConfig defines file watching configuration.
type WatchConfig struct {
	Enabled bool     `yaml:"enabled,omitempty"`
	Paths   []string `yaml:"paths,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

// HotReloadConfig defines hot reload configuration.
type HotReloadConfig struct {
	Enabled bool          `yaml:"enabled,omitempty"`
	Delay   time.Duration `yaml:"delay,omitempty"`
}

// DatabaseConfig defines database configuration.
type DatabaseConfig struct {
	Driver         string                      `yaml:"driver,omitempty"`          // postgres, mysql, sqlite
	MigrationsPath string                      `yaml:"migrations_path,omitempty"` // Default: ./database/migrations
	SeedsPath      string                      `yaml:"seeds_path,omitempty"`      // Default: ./database/seeds
	ModelsOutput   string                      `yaml:"models_output,omitempty"`
	Connections    map[string]ConnectionConfig `yaml:"connections,omitempty"`
}

// ConnectionConfig defines database connection settings.
type ConnectionConfig struct {
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections,omitempty"`
	MaxIdle        int    `yaml:"max_idle,omitempty"`
}

// BuildConfig defines build configuration.
type BuildConfig struct {
	OutputDir    string     `yaml:"output_dir,omitempty"`    // Default: ./bin
	CmdDir       string     `yaml:"cmd_dir,omitempty"`       // Default: ./cmd
	AutoDiscover bool       `yaml:"auto_discover,omitempty"` // Default: true
	Apps         []BuildApp `yaml:"apps,omitempty"`          // Optional override
	Platforms    []Platform `yaml:"platforms,omitempty"`     // For future use
	LDFlags      string     `yaml:"ldflags,omitempty"`
	Tags         []string   `yaml:"tags,omitempty"`
}

// BuildApp defines a buildable application.
type BuildApp struct {
	Name       string `yaml:"name"`
	Module     string `yaml:"module,omitempty"` // Multi-module: path to module
	Cmd        string `yaml:"cmd,omitempty"`    // Path to main.go
	Output     string `yaml:"output,omitempty"` // Binary name
	Dockerfile string `yaml:"dockerfile,omitempty"`
}

// Platform defines target build platform.
type Platform struct {
	OS   string `yaml:"os"`
	Arch string `yaml:"arch"`
}

// DeployConfig defines deployment configuration.
type DeployConfig struct {
	Registry     string              `yaml:"registry,omitempty"`     // e.g., ghcr.io/myorg
	Environments []EnvironmentConfig `yaml:"environments,omitempty"` // Main config used

	// Platform-specific configs (optional)
	Docker       *DockerConfig       `yaml:"docker,omitempty"`
	Kubernetes   *KubernetesConfig   `yaml:"kubernetes,omitempty"`
	DigitalOcean *DigitalOceanConfig `yaml:"digitalocean,omitempty"`
	Render       *RenderConfig       `yaml:"render,omitempty"`
}

// DockerConfig for Docker deployment.
type DockerConfig struct {
	BuildContext string            `yaml:"build_context,omitempty"`
	Dockerfile   string            `yaml:"dockerfile,omitempty"`
	ComposeFile  string            `yaml:"compose_file,omitempty"`
	Network      string            `yaml:"network,omitempty"`
	Volumes      map[string]string `yaml:"volumes,omitempty"`
}

// KubernetesConfig for Kubernetes deployment.
type KubernetesConfig struct {
	Manifests string `yaml:"manifests,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
	Context   string `yaml:"context,omitempty"`
	Registry  string `yaml:"registry,omitempty"`
}

// DigitalOceanConfig for Digital Ocean deployment.
type DigitalOceanConfig struct {
	Region       string `yaml:"region,omitempty"`
	AppSpecFile  string `yaml:"app_spec_file,omitempty"`
	ClusterName  string `yaml:"cluster_name,omitempty"`
	GitRepo      string `yaml:"git_repo,omitempty"`       // e.g., "myorg/myrepo"
	GitBranch    string `yaml:"git_branch,omitempty"`     // e.g., "main"
	DeployOnPush bool   `yaml:"deploy_on_push,omitempty"` // Auto-deploy on push
}

// RenderConfig for Render.com deployment.
type RenderConfig struct {
	BlueprintFile string `yaml:"blueprint_file,omitempty"`
	Region        string `yaml:"region,omitempty"`
	GitRepo       string `yaml:"git_repo,omitempty"`   // e.g., "myorg/myrepo"
	GitBranch     string `yaml:"git_branch,omitempty"` // e.g., "main"
}

// EnvironmentConfig defines deployment environment.
type EnvironmentConfig struct {
	Name      string            `yaml:"name"`
	Cluster   string            `yaml:"cluster,omitempty"`
	Namespace string            `yaml:"namespace,omitempty"` // K8s namespace
	Region    string            `yaml:"region,omitempty"`    // Cloud region
	Variables map[string]string `yaml:"variables,omitempty"` // Env vars
}

// GenerateConfig defines code generation configuration.
type GenerateConfig struct {
	TemplatesPath string                     `yaml:"templates_path,omitempty"` // Default: ./templates
	Generators    map[string]GeneratorConfig `yaml:"generators,omitempty"`     // Optional overrides
}

// GeneratorConfig defines a specific generator.
type GeneratorConfig struct {
	Output       string `yaml:"output,omitempty"`        // Template: ./{{.Type}}/{{.Name}}
	CmdPath      string `yaml:"cmd_path,omitempty"`      // Single-module (optional override)
	InternalPath string `yaml:"internal_path,omitempty"` // Single-module (optional override)
	CreateModule bool   `yaml:"create_module,omitempty"` // Multi-module
	ModulePath   string `yaml:"module_path,omitempty"`   // Multi-module
}

// TestConfig defines testing configuration.
type TestConfig struct {
	CoverageThreshold int           `yaml:"coverage_threshold,omitempty"`
	RaceDetector      bool          `yaml:"race_detector,omitempty"`
	Parallel          bool          `yaml:"parallel,omitempty"`
	Timeout           time.Duration `yaml:"timeout,omitempty"`
}

// DefaultConfig returns a default configuration with all fields populated.
func DefaultConfig() *ForgeConfig {
	return &ForgeConfig{
		Project: ProjectConfig{
			Type:   "monorepo",
			Layout: "single-module",
			Structure: &StructureConfig{
				Cmd:         "./cmd",
				Apps:        "./apps",
				Pkg:         "./pkg",
				Internal:    "./internal",
				Extensions:  "./extensions",
				Database:    "./database",
				Config:      "./config",
				Deployments: "./deployments",
			},
		},
		Dev: DevConfig{
			AutoDiscover:    true,
			DiscoverPattern: "./cmd/*",
			Watch: WatchConfig{
				Enabled: true,
				Paths: []string{
					"./apps/**/internal/**/*.go",
					"./pkg/**/*.go",
					"./internal/**/*.go",
				},
				Exclude: []string{
					"**/*_test.go",
					"**/testdata/**",
					"**/vendor/**",
				},
			},
			HotReload: HotReloadConfig{
				Enabled: true,
				Delay:   500 * time.Millisecond,
			},
		},
		Database: DatabaseConfig{
			Driver:         "postgres",
			MigrationsPath: "./database/migrations",
			SeedsPath:      "./database/seeds",
			ModelsOutput:   "./database/models",
		},
		Build: BuildConfig{
			OutputDir: "./bin",
			Platforms: []Platform{
				{OS: "linux", Arch: "amd64"},
				{OS: "darwin", Arch: "arm64"},
			},
		},
		Test: TestConfig{
			CoverageThreshold: 80,
			RaceDetector:      true,
			Parallel:          true,
			Timeout:           30 * time.Second,
		},
	}
}

// DefaultConfigMinimal returns minimal config with smart defaults.
func DefaultConfigMinimal() *ForgeConfig {
	return &ForgeConfig{
		Project: ProjectConfig{
			Type:   "monorepo",
			Layout: "single-module",
			// Structure omitted - uses conventions via GetStructure()
		},
		Dev: DevConfig{
			AutoDiscover: true,
			Watch: WatchConfig{
				Enabled: true,
				// Paths omitted - uses conventions
			},
		},
		Database: DatabaseConfig{
			Driver: "postgres",
			// Paths omitted - uses GetMigrationsPath(), GetSeedsPath()
		},
		Build: BuildConfig{
			AutoDiscover: true,
			// OutputDir omitted - uses GetOutputDir()
		},
	}
}

// IsSingleModule returns true if the project uses single-module layout.
func (c *ForgeConfig) IsSingleModule() bool {
	return c.Project.Layout == "single-module" || c.Project.Layout == ""
}

// IsMultiModule returns true if the project uses multi-module layout.
func (c *ForgeConfig) IsMultiModule() bool {
	return c.Project.Layout == "multi-module"
}

// =============================================================================
// GETTER METHODS WITH SMART DEFAULTS
// =============================================================================

// GetStructure returns the project structure configuration with Go convention defaults.
func (p *ProjectConfig) GetStructure() StructureConfig {
	if p.Structure != nil {
		return *p.Structure
	}
	// Return Go convention defaults
	return StructureConfig{
		Cmd:         "./cmd",
		Apps:        "./apps",
		Pkg:         "./pkg",
		Internal:    "./internal",
		Extensions:  "./extensions",
		Database:    "./database",
		Config:      "./config",
		Deployments: "./deployments",
	}
}

// GetMigrationsPath returns the migrations path with default fallback.
func (d *DatabaseConfig) GetMigrationsPath() string {
	if d.MigrationsPath != "" {
		return d.MigrationsPath
	}
	return "./database/migrations"
}

// GetSeedsPath returns the seeds path with default fallback.
func (d *DatabaseConfig) GetSeedsPath() string {
	if d.SeedsPath != "" {
		return d.SeedsPath
	}
	return "./database/seeds"
}

// GetModelsOutput returns the models output path with default fallback.
func (d *DatabaseConfig) GetModelsOutput() string {
	if d.ModelsOutput != "" {
		return d.ModelsOutput
	}
	return "./database/models"
}

// GetOutputDir returns the build output directory with default fallback.
func (b *BuildConfig) GetOutputDir() string {
	if b.OutputDir != "" {
		return b.OutputDir
	}
	return "./bin"
}

// GetCmdDir returns the cmd directory with default fallback.
func (b *BuildConfig) GetCmdDir() string {
	if b.CmdDir != "" {
		return b.CmdDir
	}
	return "./cmd"
}

// ShouldAutoDiscover returns true if build should auto-discover apps.
func (b *BuildConfig) ShouldAutoDiscover() bool {
	// Auto-discover if explicitly enabled OR if no apps are explicitly defined
	return b.AutoDiscover || len(b.Apps) == 0
}

// GetTemplatesPath returns the templates path with default fallback.
func (g *GenerateConfig) GetTemplatesPath() string {
	if g.TemplatesPath != "" {
		return g.TemplatesPath
	}
	return "./templates"
}
