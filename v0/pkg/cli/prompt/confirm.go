package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ConfirmPrompter handles confirmation prompts
type ConfirmPrompter struct {
	reader io.Reader
	writer io.Writer
	config ConfirmConfig
}

// ConfirmConfig contains confirmation prompt configuration
type ConfirmConfig struct {
	DefaultValue   bool     `yaml:"default_value" json:"default_value"`
	YesValues      []string `yaml:"yes_values" json:"yes_values"`
	NoValues       []string `yaml:"no_values" json:"no_values"`
	CaseSensitive  bool     `yaml:"case_sensitive" json:"case_sensitive"`
	ShowDefault    bool     `yaml:"show_default" json:"show_default"`
	RetryOnInvalid bool     `yaml:"retry_on_invalid" json:"retry_on_invalid"`
}

// NewConfirmPrompter creates a new confirmation prompter
func NewConfirmPrompter(reader io.Reader, writer io.Writer) *ConfirmPrompter {
	return &ConfirmPrompter{
		reader: reader,
		writer: writer,
		config: DefaultConfirmConfig(),
	}
}

// NewConfirmPrompterWithConfig creates a confirmation prompter with custom config
func NewConfirmPrompterWithConfig(reader io.Reader, writer io.Writer, config ConfirmConfig) *ConfirmPrompter {
	return &ConfirmPrompter{
		reader: reader,
		writer: writer,
		config: config,
	}
}

// Confirm prompts for confirmation
func (cp *ConfirmPrompter) Confirm(message string) bool {
	scanner := bufio.NewScanner(cp.reader)

	for {
		// Display prompt
		prompt := message
		if cp.config.ShowDefault {
			if cp.config.DefaultValue {
				prompt += " [Y/n]"
			} else {
				prompt += " [y/N]"
			}
		} else {
			prompt += " [y/n]"
		}
		prompt += ": "

		fmt.Fprint(cp.writer, prompt)

		// Read input
		if !scanner.Scan() {
			return cp.config.DefaultValue
		}

		input := strings.TrimSpace(scanner.Text())

		// Handle empty input
		if input == "" {
			return cp.config.DefaultValue
		}

		// Check input
		if result, valid := cp.parseInput(input); valid {
			return result
		}

		// Invalid input
		if !cp.config.RetryOnInvalid {
			return cp.config.DefaultValue
		}

		fmt.Fprintln(cp.writer, "Please answer yes or no.")
	}
}

// ConfirmWithDefault prompts for confirmation with a specific default
func (cp *ConfirmPrompter) ConfirmWithDefault(message string, defaultValue bool) bool {
	originalDefault := cp.config.DefaultValue
	cp.config.DefaultValue = defaultValue
	defer func() {
		cp.config.DefaultValue = originalDefault
	}()

	return cp.Confirm(message)
}

// parseInput parses the user input
func (cp *ConfirmPrompter) parseInput(input string) (bool, bool) {
	if !cp.config.CaseSensitive {
		input = strings.ToLower(input)
	}

	// Check yes values
	for _, yesValue := range cp.config.YesValues {
		checkValue := yesValue
		if !cp.config.CaseSensitive {
			checkValue = strings.ToLower(yesValue)
		}
		if input == checkValue {
			return true, true
		}
	}

	// Check no values
	for _, noValue := range cp.config.NoValues {
		checkValue := noValue
		if !cp.config.CaseSensitive {
			checkValue = strings.ToLower(noValue)
		}
		if input == checkValue {
			return false, true
		}
	}

	return false, false
}

// DefaultConfirmConfig returns default confirmation configuration
func DefaultConfirmConfig() ConfirmConfig {
	return ConfirmConfig{
		DefaultValue:   false,
		YesValues:      []string{"y", "yes", "1", "true"},
		NoValues:       []string{"n", "no", "0", "false"},
		CaseSensitive:  false,
		ShowDefault:    true,
		RetryOnInvalid: true,
	}
}
