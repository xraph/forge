package cli

import (
	"io"
	"os"

	"github.com/fatih/color"
)

var (
	// Color functions for output
	Green   = color.New(color.FgGreen).SprintFunc()
	Red     = color.New(color.FgRed).SprintFunc()
	Yellow  = color.New(color.FgYellow).SprintFunc()
	Blue    = color.New(color.FgBlue).SprintFunc()
	Cyan    = color.New(color.FgCyan).SprintFunc()
	Magenta = color.New(color.FgMagenta).SprintFunc()
	White   = color.New(color.FgWhite).SprintFunc()
	Gray    = color.New(color.FgHiBlack).SprintFunc()

	// Style functions
	Bold      = color.New(color.Bold).SprintFunc()
	Underline = color.New(color.Underline).SprintFunc()
	Italic    = color.New(color.Italic).SprintFunc()

	// Combined styles
	BoldGreen  = color.New(color.FgGreen, color.Bold).SprintFunc()
	BoldRed    = color.New(color.FgRed, color.Bold).SprintFunc()
	BoldYellow = color.New(color.FgYellow, color.Bold).SprintFunc()
	BoldBlue   = color.New(color.FgBlue, color.Bold).SprintFunc()
)

// ColorConfig controls color output behavior
type ColorConfig struct {
	Enabled    bool
	ForceColor bool
	NoColor    bool
}

// DefaultColorConfig returns the default color configuration
func DefaultColorConfig() ColorConfig {
	return ColorConfig{
		Enabled:    isTerminal(os.Stdout),
		ForceColor: os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != "",
		NoColor:    os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0",
	}
}

// isTerminal checks if the writer is a terminal
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		// Use fatih/color's built-in detection
		stat, _ := f.Stat()
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// ConfigureColors configures the global color settings
func ConfigureColors(config ColorConfig) {
	if config.NoColor {
		color.NoColor = true
	} else if config.ForceColor {
		color.NoColor = false
	} else {
		color.NoColor = !config.Enabled
	}
}

// Colorize applies a color function to a string if colors are enabled
func Colorize(colorFunc func(...any) string, s string) string {
	if color.NoColor {
		return s
	}
	return colorFunc(s)
}
