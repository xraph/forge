package prompt

import (
	"fmt"
	"io"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
)

// InteractiveSelector provides an interactive console selection interface
type InteractiveSelector struct {
	reader io.Reader
	writer io.Writer
	config InteractiveSelectorConfig
}

// InteractiveSelectorConfig contains configuration for interactive selection
type InteractiveSelectorConfig struct {
	PageSize      int  `yaml:"page_size" json:"page_size"`
	FilterEnabled bool `yaml:"filter_enabled" json:"filter_enabled"`
	ShowHelp      bool `yaml:"show_help" json:"show_help"`
}

// SelectionOption represents a selectable option with metadata
type SelectionOption struct {
	Value       string
	Description string
	Disabled    bool
	Metadata    map[string]interface{}
}

// NewInteractiveSelector creates a new interactive selector
func NewInteractiveSelector(reader io.Reader, writer io.Writer) *InteractiveSelector {
	return &InteractiveSelector{
		reader: reader,
		writer: writer,
		config: DefaultInteractiveSelectorConfig(),
	}
}

// Select displays an interactive selection menu using survey
func (is *InteractiveSelector) Select(title string, options []SelectionOption) (int, *SelectionOption, error) {
	if len(options) == 0 {
		return -1, nil, fmt.Errorf("no options provided")
	}

	// Convert options to survey format
	surveyOptions := make([]string, len(options))
	optionMap := make(map[string]int) // Map display text back to index

	for i, option := range options {
		displayText := option.Value
		if option.Description != "" {
			displayText = fmt.Sprintf("%s (%s)", option.Value, option.Description)
		}
		if option.Disabled {
			displayText = fmt.Sprintf("%s (disabled)", displayText)
		}

		surveyOptions[i] = displayText
		optionMap[displayText] = i
	}

	// Create the survey prompt
	prompt := &survey.Select{
		Message:  title,
		Options:  surveyOptions,
		PageSize: is.config.PageSize,
	}

	// Add filter if enabled
	if is.config.FilterEnabled {
		prompt.Filter = func(filter, option string, index int) bool {
			// Check if the option is disabled
			if options[index].Disabled {
				return false
			}

			filterLower := strings.ToLower(filter)
			optionLower := strings.ToLower(option)
			return strings.Contains(optionLower, filterLower)
		}
	}

	// Set custom templates for better appearance
	survey.SelectQuestionTemplate = `
{{- if .ShowHelp }}{{- color .Config.Icons.Help.Format }}{{ .Config.Icons.Help.Text }} {{ .Help }}{{color "reset"}}{{"\n"}}{{end}}
{{- color .Config.Icons.Question.Format }}{{ .Config.Icons.Question.Text }} {{color "reset"}}
{{- color "default+hb"}}{{ .Message }}{{ .FilterMessage }}{{color "reset"}}
{{- if .ShowAnswer}}{{color "cyan"}} {{.Answer}}{{color "reset"}}{{"\n"}}
{{- else}}
  {{- "  "}}{{- color "cyan"}}[Use arrows to move, enter to select{{- if and .Help (not .ShowHelp)}}, {{ .Config.HelpInput }} for more help{{end}}]{{color "reset"}}
  {{- "\n"}}
  {{- range $ix, $choice := .PageEntries}}
    {{- if eq $ix $.SelectedIndex}}{{color $.Config.Icons.SelectFocus.Format }}{{ $.Config.Icons.SelectFocus.Text }}{{color "reset"}}{{else}} {{end}}
    {{- $choice.Value}}
    {{- if eq $ix $.SelectedIndex}}{{color $.Config.Icons.SelectFocus.Format }}{{color "reset"}}{{end}}{{"\n"}}
  {{- end}}
{{- end}}`

	var answer string
	err := survey.AskOne(prompt, &answer)

	if err != nil {
		if err == terminal.InterruptErr {
			return -1, nil, fmt.Errorf("selection cancelled")
		}
		return -1, nil, fmt.Errorf("selection failed: %w", err)
	}

	// Find the selected index
	selectedIndex, exists := optionMap[answer]
	if !exists {
		return -1, nil, fmt.Errorf("invalid selection: %s", answer)
	}

	// Check if selected option is disabled
	if options[selectedIndex].Disabled {
		return -1, nil, fmt.Errorf("selected option is disabled")
	}

	return selectedIndex, &options[selectedIndex], nil
}

// MultiSelect provides multiple selection functionality
func (is *InteractiveSelector) MultiSelect(title string, options []SelectionOption) ([]int, []*SelectionOption, error) {
	if len(options) == 0 {
		return nil, nil, fmt.Errorf("no options provided")
	}

	// Convert options to survey format
	surveyOptions := make([]string, len(options))
	for i, option := range options {
		displayText := option.Value
		if option.Description != "" {
			displayText = fmt.Sprintf("%s (%s)", option.Value, option.Description)
		}
		surveyOptions[i] = displayText
	}

	// Create the survey prompt
	prompt := &survey.MultiSelect{
		Message:  title,
		Options:  surveyOptions,
		PageSize: is.config.PageSize,
	}

	var answers []string
	err := survey.AskOne(prompt, &answers)

	if err != nil {
		if err == terminal.InterruptErr {
			return nil, nil, fmt.Errorf("selection cancelled")
		}
		return nil, nil, fmt.Errorf("selection failed: %w", err)
	}

	// Convert answers back to indices and options
	var selectedIndices []int
	var selectedOptions []*SelectionOption

	for _, answer := range answers {
		for i, option := range surveyOptions {
			if option == answer && !options[i].Disabled {
				selectedIndices = append(selectedIndices, i)
				selectedOptions = append(selectedOptions, &options[i])
				break
			}
		}
	}

	return selectedIndices, selectedOptions, nil
}

// DefaultInteractiveSelectorConfig returns default configuration
func DefaultInteractiveSelectorConfig() InteractiveSelectorConfig {
	return InteractiveSelectorConfig{
		PageSize:      10,
		FilterEnabled: true,
		ShowHelp:      true,
	}
}
