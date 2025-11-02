// v2/cmd/forge/config/types.go
package config

import "time"

// ForgeConfig represents the complete .forge.yaml configuration
type ForgeConfig struct {
	Project    ProjectConfig          `yaml:"project"`
	Dev        DevConfig              `yaml:"dev"`
	Database   DatabaseConfig         `yaml:"database"`
	Build      BuildConfig            `yaml:"build"`
	Deploy     DeployConfig           `yaml:"deploy"`
	Generate   GenerateConfig         `yaml:"generate"`
	Extensions map[string]interface{} `yaml:"extensions"`
	Test       TestConfig             `yaml:"test"`

	// Internal fields
	RootDir    string `yaml:"-"` // Directory containing .forge.yaml
	ConfigPath string `yaml:"-"` // Full path to .forge.yaml
}

// ProjectConfig defines project metadata and structure
type ProjectConfig struct {
	Name        string          `yaml:"name"`
	Version     string          `yaml:"version"`
	Description string          `yaml:"description"`
	Type        string          `yaml:"type"`      // monorepo, app, service, extension, cli
	Layout      string          `yaml:"layout"`    // single-module, multi-module
	Module      string          `yaml:"module"`    // For single-module layout
	Workspace   WorkspaceConfig `yaml:"workspace"` // For multi-module layout
	Structure   StructureConfig `yaml:"structure"` // For single-module layout
}

// WorkspaceConfig for multi-module layout
type WorkspaceConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Apps       string `yaml:"apps"`       // Glob pattern: "./apps/*"
	Services   string `yaml:"services"`   // Glob pattern: "./services/*"
	Extensions string `yaml:"extensions"` // Glob pattern: "./extensions/*"
	Pkg        string `yaml:"pkg"`        // Path: "./pkg"
}

// StructureConfig for single-module layout
type StructureConfig struct {
	Cmd         string `yaml:"cmd"`         // Path to cmd/
	Apps        string `yaml:"apps"`        // Path to apps/
	Pkg         string `yaml:"pkg"`         // Path to pkg/
	Internal    string `yaml:"internal"`    // Path to internal/
	Extensions  string `yaml:"extensions"`  // Path to extensions/
	Database    string `yaml:"database"`    // Path to database/
	Config      string `yaml:"config"`      // Path to config/
	Deployments string `yaml:"deployments"` // Path to deployments/
}

// DevConfig defines development server configuration
type DevConfig struct {
	AutoDiscover    bool            `yaml:"auto_discover"`
	DiscoverPattern string          `yaml:"discover_pattern"`
	DefaultApp      string          `yaml:"default_app"`
	Watch           WatchConfig     `yaml:"watch"`
	HotReload       HotReloadConfig `yaml:"hot_reload"`
}

// WatchConfig defines file watching configuration
type WatchConfig struct {
	Enabled bool     `yaml:"enabled"`
	Paths   []string `yaml:"paths"`
	Exclude []string `yaml:"exclude"`
}

// HotReloadConfig defines hot reload configuration
type HotReloadConfig struct {
	Enabled bool          `yaml:"enabled"`
	Delay   time.Duration `yaml:"delay"`
}

// DatabaseConfig defines database configuration
type DatabaseConfig struct {
	Driver         string                      `yaml:"driver"` // postgres, mysql, sqlite
	MigrationsPath string                      `yaml:"migrations_path"`
	SeedsPath      string                      `yaml:"seeds_path"`
	ModelsOutput   string                      `yaml:"models_output"`
	Codegen        DatabaseCodegenConfig       `yaml:"codegen"`
	Connections    map[string]ConnectionConfig `yaml:"connections"`
}

// DatabaseCodegenConfig for database model generation
type DatabaseCodegenConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Output    string `yaml:"output"`
	Templates string `yaml:"templates"`
}

// ConnectionConfig defines database connection settings
type ConnectionConfig struct {
	URL            string `yaml:"url"`
	MaxConnections int    `yaml:"max_connections"`
	MaxIdle        int    `yaml:"max_idle"`
}

// BuildConfig defines build configuration
type BuildConfig struct {
	OutputDir    string     `yaml:"output_dir"`
	CmdDir       string     `yaml:"cmd_dir"`       // Single-module only
	AutoDiscover bool       `yaml:"auto_discover"` // Multi-module only
	Apps         []BuildApp `yaml:"apps"`
	Platforms    []Platform `yaml:"platforms"`
	LDFlags      string     `yaml:"ldflags"`
	Tags         []string   `yaml:"tags"`
}

// BuildApp defines a buildable application
type BuildApp struct {
	Name       string `yaml:"name"`
	Module     string `yaml:"module"` // Multi-module: path to module
	Cmd        string `yaml:"cmd"`    // Path to main.go
	Output     string `yaml:"output"` // Binary name
	Dockerfile string `yaml:"dockerfile"`
}

// Platform defines target build platform
type Platform struct {
	OS   string `yaml:"os"`
	Arch string `yaml:"arch"`
}

// DeployConfig defines deployment configuration
type DeployConfig struct {
	Registry     string              `yaml:"registry"`
	Docker       DockerConfig        `yaml:"docker"`
	Kubernetes   KubernetesConfig    `yaml:"kubernetes"`
	DigitalOcean DigitalOceanConfig  `yaml:"digitalocean"`
	Render       RenderConfig        `yaml:"render"`
	Environments []EnvironmentConfig `yaml:"environments"`
}

// DockerConfig for Docker deployment
type DockerConfig struct {
	BuildContext string            `yaml:"build_context"`
	Dockerfile   string            `yaml:"dockerfile"`
	ComposeFile  string            `yaml:"compose_file"`
	Network      string            `yaml:"network"`
	Volumes      map[string]string `yaml:"volumes"`
}

// KubernetesConfig for Kubernetes deployment
type KubernetesConfig struct {
	Manifests string `yaml:"manifests"`
	Namespace string `yaml:"namespace"`
	Context   string `yaml:"context"`
	Registry  string `yaml:"registry"`
}

// DigitalOceanConfig for Digital Ocean deployment
type DigitalOceanConfig struct {
	Region        string `yaml:"region"`
	AppSpecFile   string `yaml:"app_spec_file"`
	ClusterName   string `yaml:"cluster_name"`
	GitRepo       string `yaml:"git_repo"`        // e.g., "myorg/myrepo"
	GitBranch     string `yaml:"git_branch"`      // e.g., "main"
	DeployOnPush  bool   `yaml:"deploy_on_push"`  // Auto-deploy on push
}

// RenderConfig for Render.com deployment
type RenderConfig struct {
	BlueprintFile string `yaml:"blueprint_file"`
	Region        string `yaml:"region"`
	GitRepo       string `yaml:"git_repo"`   // e.g., "myorg/myrepo"
	GitBranch     string `yaml:"git_branch"` // e.g., "main"
}

// EnvironmentConfig defines deployment environment
type EnvironmentConfig struct {
	Name      string            `yaml:"name"`
	Cluster   string            `yaml:"cluster"`
	Namespace string            `yaml:"namespace"`
	Region    string            `yaml:"region"`
	Variables map[string]string `yaml:"variables"`
}

// GenerateConfig defines code generation configuration
type GenerateConfig struct {
	TemplatesPath string                     `yaml:"templates_path"`
	Generators    map[string]GeneratorConfig `yaml:"generators"`
}

// GeneratorConfig defines a specific generator
type GeneratorConfig struct {
	Output       string `yaml:"output"`
	CmdPath      string `yaml:"cmd_path"`      // Single-module
	InternalPath string `yaml:"internal_path"` // Single-module
	CreateModule bool   `yaml:"create_module"` // Multi-module
	ModulePath   string `yaml:"module_path"`   // Multi-module
}

// TestConfig defines testing configuration
type TestConfig struct {
	CoverageThreshold int           `yaml:"coverage_threshold"`
	RaceDetector      bool          `yaml:"race_detector"`
	Parallel          bool          `yaml:"parallel"`
	Timeout           time.Duration `yaml:"timeout"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *ForgeConfig {
	return &ForgeConfig{
		Project: ProjectConfig{
			Type:   "monorepo",
			Layout: "single-module",
			Structure: StructureConfig{
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

// IsSingleModule returns true if the project uses single-module layout
func (c *ForgeConfig) IsSingleModule() bool {
	return c.Project.Layout == "single-module" || c.Project.Layout == ""
}

// IsMultiModule returns true if the project uses multi-module layout
func (c *ForgeConfig) IsMultiModule() bool {
	return c.Project.Layout == "multi-module"
}
