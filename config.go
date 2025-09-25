package forge

// CMDAppConfig represents configuration for a specific command
type CMDAppConfig struct {
	Host  string      `json:"host" yaml:"host" env:"HOST" default:"localhost" validate:"required" description:"Host to listen on" xml:"host"`
	Port  int         `json:"port" yaml:"port" env:"PORT" default:"8080" validate:"required" description:"Port to listen on" xml:"port"`
	Debug bool        `json:"debug" yaml:"debug" env:"DEBUG" default:"false" description:"Enable debug mode" xml:"debug"`
	Build BuildConfig `json:"build" yaml:"build" description:"Build configuration for this command"`
}

// BuildConfig represents build-specific configuration
type BuildConfig struct {
	// Output configuration
	OutputDir  string `json:"output_dir" yaml:"output_dir" description:"Output directory for built binaries"`
	OutputName string `json:"output_name" yaml:"output_name" description:"Output filename (without extension)"`
	OutputPath string `json:"output_path" yaml:"output_path" description:"Full output path (overrides dir and name)"`

	// Build options
	Tags       []string `json:"tags" yaml:"tags" description:"Build tags"`
	Ldflags    string   `json:"ldflags" yaml:"ldflags" description:"Linker flags"`
	Gcflags    string   `json:"gcflags" yaml:"gcflags" description:"Compiler flags"`
	Race       bool     `json:"race" yaml:"race" description:"Enable race detector"`
	Msan       bool     `json:"msan" yaml:"msan" description:"Enable memory sanitizer"`
	Asan       bool     `json:"asan" yaml:"asan" description:"Enable address sanitizer"`
	Trimpath   bool     `json:"trimpath" yaml:"trimpath" default:"true" description:"Remove file system paths from binary"`
	Buildmode  string   `json:"buildmode" yaml:"buildmode" description:"Build mode"`
	Compiler   string   `json:"compiler" yaml:"compiler" description:"Compiler to use"`
	Gccgoflags string   `json:"gccgoflags" yaml:"gccgoflags" description:"Flags for gccgo"`
	Verbose    bool     `json:"verbose" yaml:"verbose" description:"Enable verbose build output"`

	// Cross-compilation
	TargetOS   string `json:"target_os" yaml:"target_os" description:"Target operating system"`
	TargetArch string `json:"target_arch" yaml:"target_arch" description:"Target architecture"`

	// Advanced options
	CrossCompile bool `json:"cross_compile" yaml:"cross_compile" description:"Enable cross-compilation"`
	StaticLink   bool `json:"static_link" yaml:"static_link" description:"Enable static linking"`
	StripDebug   bool `json:"strip_debug" yaml:"strip_debug" description:"Strip debug information"`
	Compress     bool `json:"compress" yaml:"compress" description:"Compress binary with UPX"`
}

// GlobalConfig represents the root configuration structure
type GlobalConfig struct {
	// Environment settings
	Environment string `json:"environment" yaml:"environment" default:"development" description:"Application environment"`

	// Global build configuration (applies to all commands unless overridden)
	Build BuildConfig `json:"build" yaml:"build" description:"Global build configuration"`

	// Command-specific configurations
	Cmds map[string]CMDAppConfig `json:"cmds" yaml:"cmds" description:"Command-specific configurations"`

	// Logging configuration
	Logging LoggingConfig `json:"logging" yaml:"logging" description:"Logging configuration"`

	// Development configuration
	Development DevelopmentConfig `json:"development" yaml:"development" description:"Development server configuration"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `json:"level" yaml:"level" default:"info" description:"Log level"`
	Format string `json:"format" yaml:"format" default:"text" description:"Log format (text, json)"`
	Output string `json:"output" yaml:"output" default:"stdout" description:"Log output (stdout, stderr, file)"`
	File   string `json:"file" yaml:"file" description:"Log file path (when output is file)"`
}

// DevelopmentConfig represents development server configuration
type DevelopmentConfig struct {
	Port       int    `json:"port" yaml:"port" default:"8080" description:"Development server port"`
	Host       string `json:"host" yaml:"host" default:"localhost" description:"Development server host"`
	Watch      bool   `json:"watch" yaml:"watch" default:"true" description:"Enable file watching"`
	Reload     bool   `json:"reload" yaml:"reload" default:"true" description:"Enable hot reload"`
	Debug      bool   `json:"debug" yaml:"debug" default:"true" description:"Enable debug mode"`
	BuildDelay int    `json:"build_delay" yaml:"build_delay" default:"500" description:"Delay before rebuild (ms)"`
}
