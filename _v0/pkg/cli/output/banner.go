package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Banner represents a configurable banner for terminal applications
type Banner struct {
	config *BannerConfig
	styles *BannerStyles
}

// BannerConfig contains configuration for banner display
type BannerConfig struct {
	// Application information
	AppName     string `yaml:"app_name" json:"app_name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description" json:"description"`
	Author      string `yaml:"author" json:"author"`
	Environment string `yaml:"environment" json:"environment"`
	BuildTime   string `yaml:"build_time" json:"build_time"`
	GitCommit   string `yaml:"git_commit" json:"git_commit"`

	// Display options
	Style         BannerStyle `yaml:"style" json:"style"`
	ColorOutput   bool        `yaml:"color_output" json:"color_output"`
	ShowVersion   bool        `yaml:"show_version" json:"show_version"`
	ShowEnv       bool        `yaml:"show_env" json:"show_env"`
	ShowBuildInfo bool        `yaml:"show_build_info" json:"show_build_info"`
	Width         int         `yaml:"width" json:"width"`
	Padding       int         `yaml:"padding" json:"padding"`
	Centered      bool        `yaml:"centered" json:"centered"`

	// Output
	Writer io.Writer `yaml:"-" json:"-"`

	// Custom content
	CustomLines []string `yaml:"custom_lines" json:"custom_lines"`
	Footer      string   `yaml:"footer" json:"footer"`
}

// BannerStyle defines different banner styles
type BannerStyle string

const (
	StyleSimple    BannerStyle = "simple"
	StyleBoxed     BannerStyle = "boxed"
	StyleGradient  BannerStyle = "gradient"
	StyleASCII     BannerStyle = "ascii"
	StyleMinimal   BannerStyle = "minimal"
	StyleClassic   BannerStyle = "classic"
	StyleModern    BannerStyle = "modern"
	StyleDeveloper BannerStyle = "developer"
)

// BannerStyles contains lipgloss styles for banner components
type BannerStyles struct {
	// Main styles
	ContainerStyle lipgloss.Style
	TitleStyle     lipgloss.Style
	VersionStyle   lipgloss.Style
	DescStyle      lipgloss.Style
	InfoStyle      lipgloss.Style
	FooterStyle    lipgloss.Style

	// Decorative styles
	BorderStyle    lipgloss.Style
	AccentStyle    lipgloss.Style
	MetadataStyle  lipgloss.Style
	SeparatorStyle lipgloss.Style

	// Environment-specific
	DevStyle  lipgloss.Style
	ProdStyle lipgloss.Style
	TestStyle lipgloss.Style
}

// NewBanner creates a new banner with the given configuration
func NewBanner(config BannerConfig) *Banner {
	if config.Writer == nil {
		config.Writer = os.Stdout
	}
	if config.Width == 0 {
		config.Width = 80
	}
	if config.Padding == 0 {
		config.Padding = 2
	}

	styles := createBannerStyles(config)

	return &Banner{
		config: &config,
		styles: styles,
	}
}

// NewForgeBanner creates a banner specifically for Forge applications
func NewForgeBanner(appName, version string) *Banner {
	config := BannerConfig{
		AppName:       appName,
		Version:       version,
		Description:   "Built with Forge Framework",
		Style:         StyleModern,
		ColorOutput:   true,
		ShowVersion:   true,
		ShowEnv:       true,
		ShowBuildInfo: false,
		Width:         70,
		Centered:      true,
		Writer:        os.Stdout,
	}
	return NewBanner(config)
}

// createBannerStyles creates beautiful lipgloss styles for banners
func createBannerStyles(config BannerConfig) *BannerStyles {
	styles := &BannerStyles{}

	if config.ColorOutput {
		// Modern color palette
		primaryColor := lipgloss.Color("#6366F1")   // Indigo
		secondaryColor := lipgloss.Color("#10B981") // Emerald
		accentColor := lipgloss.Color("#F59E0B")    // Amber
		textColor := lipgloss.Color("#F3F4F6")      // Gray-100
		mutedColor := lipgloss.Color("#9CA3AF")     // Gray-400

		// Title styling
		styles.TitleStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Align(lipgloss.Center)

		// Version styling
		styles.VersionStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

		// Description styling
		styles.DescStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Italic(true).
			Align(lipgloss.Center)

		// Info styling
		styles.InfoStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

		// Footer styling
		styles.FooterStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Align(lipgloss.Center).
			Faint(true)

		// Border styling
		styles.BorderStyle = lipgloss.NewStyle().
			BorderForeground(primaryColor)

		// Accent styling
		styles.AccentStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

		// Metadata styling
		styles.MetadataStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Faint(true)

		// Separator styling
		styles.SeparatorStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

		// Environment-specific styles
		styles.DevStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")). // Red
			Bold(true)

		styles.ProdStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")). // Green
			Bold(true)

		styles.TestStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")). // Amber
			Bold(true)

		// Container styling based on style
		switch config.Style {
		case StyleBoxed:
			styles.ContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(1, 2)

		case StyleGradient:
			styles.ContainerStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#6366F1")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#8B5CF6")).
				Padding(1, 2)

		case StyleClassic:
			styles.ContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(primaryColor).
				Padding(1, 2)

		case StyleModern:
			styles.ContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(1, 3).
				MarginTop(1).
				MarginBottom(1)

		default:
			styles.ContainerStyle = lipgloss.NewStyle().
				Padding(1, 2)
		}
	} else {
		// Monochrome styles
		styles.TitleStyle = lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
		styles.VersionStyle = lipgloss.NewStyle().Bold(true)
		styles.DescStyle = lipgloss.NewStyle().Align(lipgloss.Center)
		styles.InfoStyle = lipgloss.NewStyle()
		styles.FooterStyle = lipgloss.NewStyle().Align(lipgloss.Center)
		styles.BorderStyle = lipgloss.NewStyle()
		styles.AccentStyle = lipgloss.NewStyle().Bold(true)
		styles.MetadataStyle = lipgloss.NewStyle()
		styles.SeparatorStyle = lipgloss.NewStyle()
		styles.DevStyle = lipgloss.NewStyle().Bold(true)
		styles.ProdStyle = lipgloss.NewStyle().Bold(true)
		styles.TestStyle = lipgloss.NewStyle().Bold(true)

		if config.Style == StyleBoxed || config.Style == StyleClassic || config.Style == StyleModern {
			styles.ContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(1, 2)
		} else {
			styles.ContainerStyle = lipgloss.NewStyle().Padding(1, 2)
		}
	}

	return styles
}

// Render renders the banner to the configured writer
func (b *Banner) Render() error {
	content := b.buildContent()
	_, err := b.config.Writer.Write([]byte(content))
	return err
}

// String returns the banner as a string
func (b *Banner) String() string {
	return b.buildContent()
}

// buildContent builds the complete banner content
func (b *Banner) buildContent() string {
	var content strings.Builder

	switch b.config.Style {
	case StyleASCII:
		content.WriteString(b.buildASCIIBanner())
	case StyleMinimal:
		content.WriteString(b.buildMinimalBanner())
	case StyleDeveloper:
		content.WriteString(b.buildDeveloperBanner())
	default:
		content.WriteString(b.buildStandardBanner())
	}

	return content.String()
}

// buildStandardBanner builds a standard styled banner
func (b *Banner) buildStandardBanner() string {
	var sections []string

	// Header section with title and version
	headerSection := b.buildHeaderSection()
	sections = append(sections, headerSection)

	// Info section with structured data
	if b.hasMetadata() {
		infoSection := b.buildInfoSection()
		sections = append(sections, infoSection)
	}

	// Footer section
	if b.config.Footer != "" {
		footerSection := b.buildFooterSection()
		sections = append(sections, footerSection)
	}

	// Join sections with proper spacing
	content := strings.Join(sections, "\n\n")

	if b.config.Centered {
		content = b.centerContent(content)
	}

	return b.styles.ContainerStyle.Render(content) + "\n"
}

// buildHeaderSection creates a beautiful header with app name and version
func (b *Banner) buildHeaderSection() string {
	var lines []string

	// Main title with enhanced styling
	titleLine := b.styles.TitleStyle.Render("🚀 " + strings.ToUpper(b.config.AppName))
	if b.config.ShowVersion && b.config.Version != "" {
		versionBadge := b.styles.VersionStyle.Render(" v" + b.config.Version + " ")
		titleLine += " " + versionBadge
	}
	lines = append(lines, titleLine)

	// Description with better styling
	if b.config.Description != "" {
		descLine := b.styles.DescStyle.Render("✨ " + b.config.Description)
		lines = append(lines, descLine)
	}

	return strings.Join(lines, "\n")
}

// buildInfoSection creates a structured info section
func (b *Banner) buildInfoSection() string {
	var lines []string

	// Section header
	sectionHeader := b.styles.SeparatorStyle.Render("┌─ Configuration")
	lines = append(lines, sectionHeader)

	// Environment with icon and enhanced styling
	if b.config.ShowEnv && b.config.Environment != "" {
		envStyle := b.getEnvironmentStyle(b.config.Environment)
		envIcon := b.getEnvironmentIcon(b.config.Environment)
		envLine := b.styles.InfoStyle.Render("├─ Environment: ") +
			envIcon + " " + envStyle.Render(strings.ToTitle(b.config.Environment))
		lines = append(lines, envLine)
	}

	// Custom lines with tree structure
	for i, line := range b.config.CustomLines {
		prefix := "├─"
		if i == len(b.config.CustomLines)-1 && !b.config.ShowBuildInfo {
			prefix = "└─"
		}

		// Parse key-value pairs for better formatting
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			formattedLine := b.styles.InfoStyle.Render(prefix+" "+key+": ") +
				b.styles.AccentStyle.Render(value)
			lines = append(lines, formattedLine)
		} else {
			lines = append(lines, b.styles.InfoStyle.Render(prefix+" "+line))
		}
	}

	// Build information with icons
	if b.config.ShowBuildInfo {
		buildLines := b.buildBuildInfoLines()
		lines = append(lines, buildLines...)
	}

	// Author information
	if b.config.Author != "" {
		authorLine := b.styles.InfoStyle.Render("└─ Author: ") +
			b.styles.AccentStyle.Render("👤 "+b.config.Author)
		lines = append(lines, authorLine)
	} else if len(lines) > 1 {
		// Fix the last connector if no author
		lastLine := lines[len(lines)-1]
		if strings.Contains(lastLine, "├─") {
			lines[len(lines)-1] = strings.Replace(lastLine, "├─", "└─", 1)
		}
	}

	return strings.Join(lines, "\n")
}

// buildBuildInfoLines creates formatted build information lines
func (b *Banner) buildBuildInfoLines() []string {
	var lines []string

	if b.config.BuildTime != "" {
		buildLine := b.styles.MetadataStyle.Render("├─ Built: ") +
			b.styles.AccentStyle.Render("🔨 "+b.formatBuildTime(b.config.BuildTime))
		lines = append(lines, buildLine)
	}

	if b.config.GitCommit != "" {
		commit := b.config.GitCommit
		if len(commit) > 8 {
			commit = commit[:8]
		}
		commitLine := b.styles.MetadataStyle.Render("├─ Commit: ") +
			b.styles.AccentStyle.Render("📝 "+commit)
		lines = append(lines, commitLine)
	}

	return lines
}

// buildFooterSection creates a styled footer
func (b *Banner) buildFooterSection() string {
	footerContent := b.config.Footer

	// Add helpful icons to common footer messages
	if strings.Contains(strings.ToLower(footerContent), "ctrl+c") {
		footerContent = "⏹️  " + footerContent
	} else if strings.Contains(strings.ToLower(footerContent), "ready") {
		footerContent = "✅ " + footerContent
	} else if strings.Contains(strings.ToLower(footerContent), "starting") {
		footerContent = "🚀 " + footerContent
	}

	return b.styles.FooterStyle.Render(footerContent)
}

// getEnvironmentIcon returns appropriate icon for environment
func (b *Banner) getEnvironmentIcon(env string) string {
	switch strings.ToLower(env) {
	case "development", "dev":
		return "🔧"
	case "production", "prod":
		return "🌟"
	case "staging", "stage":
		return "🎭"
	case "test", "testing":
		return "🧪"
	default:
		return "⚙️"
	}
}

// formatBuildTime formats build time for better display
func (b *Banner) formatBuildTime(buildTime string) string {
	// Try to parse and reformat the time
	if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	return buildTime
}

// buildASCIIBanner builds an ASCII art banner
func (b *Banner) buildASCIIBanner() string {
	var content strings.Builder

	// Simple ASCII art for the app name
	asciiArt := b.generateASCIIArt(b.config.AppName)
	content.WriteString(b.styles.TitleStyle.Render(asciiArt))
	content.WriteString("\n")

	if b.config.ShowVersion && b.config.Version != "" {
		content.WriteString(b.styles.VersionStyle.Render("Version: " + b.config.Version))
		content.WriteString("\n")
	}

	if b.config.Description != "" {
		content.WriteString(b.styles.DescStyle.Render(b.config.Description))
		content.WriteString("\n")
	}

	return content.String()
}

// buildMinimalBanner builds a minimal banner
func (b *Banner) buildMinimalBanner() string {
	parts := []string{b.config.AppName}

	if b.config.ShowVersion && b.config.Version != "" {
		parts = append(parts, "v"+b.config.Version)
	}

	if b.config.ShowEnv && b.config.Environment != "" {
		envStyle := b.getEnvironmentStyle(b.config.Environment)
		parts = append(parts, envStyle.Render("["+b.config.Environment+"]"))
	}

	return b.styles.TitleStyle.Render(strings.Join(parts, " ")) + "\n"
}

// buildDeveloperBanner builds a developer-focused banner with technical details
func (b *Banner) buildDeveloperBanner() string {
	var lines []string

	// Header
	header := fmt.Sprintf("╭─ %s", b.config.AppName)
	if b.config.ShowVersion && b.config.Version != "" {
		header += " v" + b.config.Version
	}
	lines = append(lines, b.styles.TitleStyle.Render(header))

	// Technical details
	if b.config.ShowEnv && b.config.Environment != "" {
		envStyle := b.getEnvironmentStyle(b.config.Environment)
		lines = append(lines, b.styles.InfoStyle.Render("├─ Environment: ")+envStyle.Render(b.config.Environment))
	}

	if b.config.BuildTime != "" {
		lines = append(lines, b.styles.MetadataStyle.Render("├─ Build Time: "+b.config.BuildTime))
	}

	if b.config.GitCommit != "" {
		commit := b.config.GitCommit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		lines = append(lines, b.styles.MetadataStyle.Render("├─ Git Commit: "+commit))
	}

	// Footer
	lines = append(lines, b.styles.InfoStyle.Render("╰─ Ready"))

	return strings.Join(lines, "\n") + "\n"
}

// generateASCIIArt generates simple ASCII art for text
func (b *Banner) generateASCIIArt(text string) string {
	// Simple block letter representation
	if len(text) > 20 {
		return text // Too long for ASCII art
	}

	// Return stylized version with spacing
	spaced := strings.Join(strings.Split(strings.ToUpper(text), ""), " ")
	return fmt.Sprintf("╔═══ %s ═══╗", spaced)
}

// getEnvironmentStyle returns appropriate style for environment
func (b *Banner) getEnvironmentStyle(env string) lipgloss.Style {
	switch strings.ToLower(env) {
	case "development", "dev":
		return b.styles.DevStyle
	case "production", "prod":
		return b.styles.ProdStyle
	case "test", "testing":
		return b.styles.TestStyle
	default:
		return b.styles.InfoStyle
	}
}

// hasMetadata checks if there's any metadata to display
func (b *Banner) hasMetadata() bool {
	return (b.config.ShowEnv && b.config.Environment != "") ||
		(b.config.ShowBuildInfo && (b.config.BuildTime != "" || b.config.GitCommit != "")) ||
		b.config.Author != "" ||
		len(b.config.CustomLines) > 0
}

// centerContent centers content within the specified width
func (b *Banner) centerContent(content string) string {
	lines := strings.Split(content, "\n")
	var centeredLines []string

	for _, line := range lines {
		// Remove any existing styling to measure actual width
		cleanLine := stripANSI(line)
		if len(cleanLine) < b.config.Width {
			padding := (b.config.Width - len(cleanLine)) / 2
			centeredLines = append(centeredLines, strings.Repeat(" ", padding)+line)
		} else {
			centeredLines = append(centeredLines, line)
		}
	}

	return strings.Join(centeredLines, "\n")
}

// stripANSI removes ANSI codes for width calculation
func stripANSI(str string) string {
	// Simple ANSI stripping - in production you might want a more robust solution
	lines := strings.Split(str, "\033[")
	if len(lines) == 1 {
		return str
	}

	result := lines[0]
	for i := 1; i < len(lines); i++ {
		if idx := strings.Index(lines[i], "m"); idx != -1 {
			result += lines[i][idx+1:]
		}
	}
	return result
}

// Preset banner configurations

// CLIBannerConfig returns a banner config suitable for CLI applications
func CLIBannerConfig(appName, version string) BannerConfig {
	return BannerConfig{
		AppName:     appName,
		Version:     version,
		Style:       StyleMinimal,
		ColorOutput: true,
		ShowVersion: true,
		ShowEnv:     false,
		Width:       60,
		Centered:    false,
		Writer:      os.Stdout,
	}
}

// ServerBannerConfig returns a banner config suitable for server applications
func ServerBannerConfig(appName, version, environment string) BannerConfig {
	return BannerConfig{
		AppName:     appName,
		Version:     version,
		Environment: environment,
		Style:       StyleModern,
		ColorOutput: true,
		ShowVersion: true,
		ShowEnv:     true,
		Width:       70,
		Centered:    true,
		Writer:      os.Stdout,
	}
}

// DeveloperBannerConfig returns a banner config with technical details
func DeveloperBannerConfig(appName, version string) BannerConfig {
	return BannerConfig{
		AppName:       appName,
		Version:       version,
		Style:         StyleDeveloper,
		ColorOutput:   true,
		ShowVersion:   true,
		ShowEnv:       true,
		ShowBuildInfo: true,
		Width:         80,
		Centered:      false,
		Writer:        os.Stdout,
	}
}

// DisplayDevelopmentServerBanner displays a beautiful banner for development servers
func DisplayDevelopmentServerBanner(appName, version, environment, host string, port int, hotReload bool) {
	config := DeveloperBannerConfig(appName, version)
	config.Environment = environment
	config.Style = StyleModern
	config.Width = 70

	// Build custom lines with server info
	customLines := []string{
		fmt.Sprintf("Host: %s:%d", host, port),
	}

	if hotReload {
		customLines = append(customLines, "Hot reload: enabled")
	}

	config.CustomLines = customLines
	config.Footer = "Press Ctrl+C to stop development server"

	banner := NewBanner(config)
	banner.Render()
}

// DisplayCLIStartupBanner displays a startup banner for CLI applications
func DisplayCLIStartupBanner(appName, version string, options map[string]string) {
	config := CLIBannerConfig(appName, version)
	config.Style = StyleModern
	config.Width = 60

	var customLines []string
	for key, value := range options {
		customLines = append(customLines, fmt.Sprintf("%s: %s", key, value))
	}
	config.CustomLines = customLines

	banner := NewBanner(config)
	banner.Render()
}

// DisplayProductionBanner displays a production-ready banner
func DisplayProductionBanner(appName, version, environment string, buildInfo map[string]string) {
	config := ServerBannerConfig(appName, version, environment)
	config.Style = StyleClassic
	config.ShowBuildInfo = true
	config.Width = 80

	if buildTime, ok := buildInfo["build_time"]; ok {
		config.BuildTime = buildTime
	}
	if gitCommit, ok := buildInfo["git_commit"]; ok {
		config.GitCommit = gitCommit
	}
	if author, ok := buildInfo["author"]; ok {
		config.Author = author
	}

	banner := NewBanner(config)
	banner.Render()
}

// WithBuildInfo adds build information to a banner
func (b *Banner) WithBuildInfo(buildTime, gitCommit string) *Banner {
	b.config.BuildTime = buildTime
	b.config.GitCommit = gitCommit
	b.config.ShowBuildInfo = true
	return b
}

// WithEnvironment sets the environment for the banner
func (b *Banner) WithEnvironment(env string) *Banner {
	b.config.Environment = env
	b.config.ShowEnv = true
	return b
}

// WithCustomLines adds custom lines to the banner
func (b *Banner) WithCustomLines(lines ...string) *Banner {
	b.config.CustomLines = append(b.config.CustomLines, lines...)
	return b
}

// WithFooter sets a footer message
func (b *Banner) WithFooter(footer string) *Banner {
	b.config.Footer = footer
	return b
}

// WithWriter sets the output writer
func (b *Banner) WithWriter(writer io.Writer) *Banner {
	b.config.Writer = writer
	return b
}
