package cli

import (
	"io"
	"os"

	"github.com/fatih/color"
)

var (
	// Green applies green color to output.
	Green = color.New(color.FgGreen).SprintFunc()
	// Red applies red color to output.
	Red = color.New(color.FgRed).SprintFunc()
	// Yellow applies yellow color to output.
	Yellow = color.New(color.FgYellow).SprintFunc()
	// Blue applies blue color to output.
	Blue = color.New(color.FgBlue).SprintFunc()
	// Cyan applies cyan color to output.
	Cyan = color.New(color.FgCyan).SprintFunc()
	// Magenta applies magenta color to output.
	Magenta = color.New(color.FgMagenta).SprintFunc()
	// White applies white color to output.
	White = color.New(color.FgWhite).SprintFunc()
	// Gray applies gray color to output.
	Gray = color.New(color.FgHiBlack).SprintFunc()

	// Bold applies bold style to output.
	Bold = color.New(color.Bold).SprintFunc()
	// Underline applies underline style to output.
	Underline = color.New(color.Underline).SprintFunc()
	// Italic applies italic style to output.
	Italic = color.New(color.Italic).SprintFunc()

	// BoldGreen applies bold green color to output.
	BoldGreen = color.New(color.FgGreen, color.Bold).SprintFunc()
	// BoldRed applies bold red color to output.
	BoldRed = color.New(color.FgRed, color.Bold).SprintFunc()
	// BoldYellow applies bold yellow color to output.
	BoldYellow = color.New(color.FgYellow, color.Bold).SprintFunc()
	// BoldBlue applies bold blue color to output.
	BoldBlue = color.New(color.FgBlue, color.Bold).SprintFunc()
)

// ColorConfig controls color output behavior.
type ColorConfig struct {
	Enabled    bool
	ForceColor bool
	NoColor    bool
}

// DefaultColorConfig returns the default color configuration.
func DefaultColorConfig() ColorConfig {
	return ColorConfig{
		Enabled:    isTerminal(os.Stdout),
		ForceColor: os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != "",
		NoColor:    os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0",
	}
}

// isTerminal checks if the writer is a terminal.
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		// Use fatih/color's built-in detection
		stat, _ := f.Stat()

		return (stat.Mode() & os.ModeCharDevice) != 0
	}

	return false
}

// ConfigureColors configures the global color settings.
func ConfigureColors(config ColorConfig) {
	if config.NoColor { //nolint:gocritic // ifElseChain: priority-based config clearer with if-else
		color.NoColor = true
	} else if config.ForceColor {
		color.NoColor = false
	} else {
		color.NoColor = !config.Enabled
	}
}

// Colorize applies a color function to a string if colors are enabled.
func Colorize(colorFunc func(...any) string, s string) string {
	if color.NoColor {
		return s
	}

	return colorFunc(s)
}
