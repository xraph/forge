package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SelectPrompter handles selection prompts
type SelectPrompter struct {
	reader io.Reader
	writer io.Writer
	config SelectConfig
}

// SelectConfig contains selection prompt configuration
type SelectConfig struct {
	ShowNumbers       bool `yaml:"show_numbers" json:"show_numbers"`
	StartIndex        int  `yaml:"start_index" json:"start_index"`
	DefaultIndex      int  `yaml:"default_index" json:"default_index"`
	ShowDefault       bool `yaml:"show_default" json:"show_default"`
	RetryOnInvalid    bool `yaml:"retry_on_invalid" json:"retry_on_invalid"`
	AllowPartialMatch bool `yaml:"allow_partial_match" json:"allow_partial_match"`
	CaseSensitive     bool `yaml:"case_sensitive" json:"case_sensitive"`
}

// NewSelectPrompter creates a new selection prompter
func NewSelectPrompter(reader io.Reader, writer io.Writer) *SelectPrompter {
	return &SelectPrompter{
		reader: reader,
		writer: writer,
		config: DefaultSelectConfig(),
	}
}

// NewSelectPrompterWithConfig creates a selection prompter with custom config
func NewSelectPrompterWithConfig(reader io.Reader, writer io.Writer, config SelectConfig) *SelectPrompter {
	return &SelectPrompter{
		reader: reader,
		writer: writer,
		config: config,
	}
}

// Select prompts for selection from options
func (sp *SelectPrompter) Select(message string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options provided")
	}

	scanner := bufio.NewScanner(sp.reader)

	for {
		// Display message and options
		fmt.Fprintln(sp.writer, message)
		sp.displayOptions(options)

		// Display prompt
		prompt := "Select"
		if sp.config.ShowDefault && sp.config.DefaultIndex >= 0 && sp.config.DefaultIndex < len(options) {
			prompt += fmt.Sprintf(" [%d]", sp.config.StartIndex+sp.config.DefaultIndex)
		}
		prompt += ": "

		fmt.Fprint(sp.writer, prompt)

		// Read input
		if !scanner.Scan() {
			if sp.config.DefaultIndex >= 0 && sp.config.DefaultIndex < len(options) {
				return sp.config.DefaultIndex, nil
			}
			return -1, fmt.Errorf("no input received")
		}

		input := strings.TrimSpace(scanner.Text())

		// Handle empty input
		if input == "" {
			if sp.config.DefaultIndex >= 0 && sp.config.DefaultIndex < len(options) {
				return sp.config.DefaultIndex, nil
			}
			if !sp.config.RetryOnInvalid {
				return -1, fmt.Errorf("no selection made")
			}
			fmt.Fprintln(sp.writer, "Please make a selection.")
			continue
		}

		// Parse input
		if index, err := sp.parseInput(input, options); err == nil {
			return index, nil
		}

		// Invalid input
		if !sp.config.RetryOnInvalid {
			return -1, fmt.Errorf("invalid selection: %s", input)
		}

		fmt.Fprintln(sp.writer, "Invalid selection. Please try again.")
	}
}

// SelectWithDefault prompts for selection with a specific default
func (sp *SelectPrompter) SelectWithDefault(message string, options []string, defaultIndex int) (int, error) {
	if defaultIndex < 0 || defaultIndex >= len(options) {
		return -1, fmt.Errorf("invalid default index: %d", defaultIndex)
	}

	originalDefault := sp.config.DefaultIndex
	sp.config.DefaultIndex = defaultIndex
	defer func() {
		sp.config.DefaultIndex = originalDefault
	}()

	return sp.Select(message, options)
}

// MultiSelect prompts for multiple selections
func (sp *SelectPrompter) MultiSelect(message string, options []string) ([]int, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	scanner := bufio.NewScanner(sp.reader)

	for {
		// Display message and options
		fmt.Fprintln(sp.writer, message)
		fmt.Fprintln(sp.writer, "(Enter comma-separated numbers or ranges like 1-3)")
		sp.displayOptions(options)

		prompt := "Select (multiple): "
		fmt.Fprint(sp.writer, prompt)

		// Read input
		if !scanner.Scan() {
			return nil, fmt.Errorf("no input received")
		}

		input := strings.TrimSpace(scanner.Text())

		// Handle empty input
		if input == "" {
			if !sp.config.RetryOnInvalid {
				return nil, fmt.Errorf("no selection made")
			}
			fmt.Fprintln(sp.writer, "Please make at least one selection.")
			continue
		}

		// Parse multiple selections
		if indices, err := sp.parseMultipleInput(input, options); err == nil {
			return indices, nil
		}

		// Invalid input
		if !sp.config.RetryOnInvalid {
			return nil, fmt.Errorf("invalid selection: %s", input)
		}

		fmt.Fprintln(sp.writer, "Invalid selection. Please try again.")
	}
}

// displayOptions displays the available options
func (sp *SelectPrompter) displayOptions(options []string) {
	for i, option := range options {
		index := i + sp.config.StartIndex
		if sp.config.ShowNumbers {
			fmt.Fprintf(sp.writer, "  %d) %s\n", index, option)
		} else {
			fmt.Fprintf(sp.writer, "  %s\n", option)
		}
	}
}

// parseInput parses a single selection input
func (sp *SelectPrompter) parseInput(input string, options []string) (int, error) {
	// Try to parse as number
	if num, err := strconv.Atoi(input); err == nil {
		index := num - sp.config.StartIndex
		if index >= 0 && index < len(options) {
			return index, nil
		}
		return -1, fmt.Errorf("number out of range: %d", num)
	}

	// Try partial matching if enabled
	if sp.config.AllowPartialMatch {
		return sp.findPartialMatch(input, options)
	}

	return -1, fmt.Errorf("invalid input: %s", input)
}

// parseMultipleInput parses multiple selection input
func (sp *SelectPrompter) parseMultipleInput(input string, options []string) ([]int, error) {
	var indices []int
	parts := strings.Split(input, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)

		// Check for range (e.g., "1-3")
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))

			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			if start > end {
				start, end = end, start
			}

			for num := start; num <= end; num++ {
				index := num - sp.config.StartIndex
				if index >= 0 && index < len(options) {
					indices = append(indices, index)
				}
			}
		} else {
			// Single selection
			if index, err := sp.parseInput(part, options); err == nil {
				indices = append(indices, index)
			} else {
				return nil, err
			}
		}
	}

	// Remove duplicates
	indices = sp.removeDuplicates(indices)

	if len(indices) == 0 {
		return nil, fmt.Errorf("no valid selections")
	}

	return indices, nil
}

// findPartialMatch finds partial matches in options
func (sp *SelectPrompter) findPartialMatch(input string, options []string) (int, error) {
	searchTerm := input
	if !sp.config.CaseSensitive {
		searchTerm = strings.ToLower(input)
	}

	var matches []int

	for i, option := range options {
		checkOption := option
		if !sp.config.CaseSensitive {
			checkOption = strings.ToLower(option)
		}

		if strings.Contains(checkOption, searchTerm) {
			matches = append(matches, i)
		}
	}

	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("no matches found for: %s", input)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("multiple matches found for: %s", input)
	}
}

// removeDuplicates removes duplicate indices
func (sp *SelectPrompter) removeDuplicates(indices []int) []int {
	keys := make(map[int]bool)
	var result []int

	for _, index := range indices {
		if !keys[index] {
			keys[index] = true
			result = append(result, index)
		}
	}

	return result
}

// DefaultSelectConfig returns default selection configuration
func DefaultSelectConfig() SelectConfig {
	return SelectConfig{
		ShowNumbers:       true,
		StartIndex:        1,
		DefaultIndex:      -1,
		ShowDefault:       true,
		RetryOnInvalid:    true,
		AllowPartialMatch: true,
		CaseSensitive:     false,
	}
}
