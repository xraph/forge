package prompt

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelectPrompter_DisplayOptions_Alignment(t *testing.T) {
	tests := []struct {
		name         string
		options      []string
		showNumbers  bool
		startIndex   int
		wantContains []string // What output should contain
		wantNotStart string   // What output should NOT start with
	}{
		{
			name:        "numbered options should be left-aligned",
			options:     []string{"Database", "Cache", "Events"},
			showNumbers: true,
			startIndex:  1,
			wantContains: []string{
				"1) Database",
				"2) Cache",
				"3) Events",
			},
			wantNotStart: "  ", // Should not start with spaces
		},
		{
			name:        "non-numbered options should be left-aligned",
			options:     []string{"Development", "Staging", "Production"},
			showNumbers: false,
			wantContains: []string{
				"Development",
				"Staging",
				"Production",
			},
			wantNotStart: "  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			sp := &SelectPrompter{
				writer: &buf,
				config: SelectConfig{
					ShowNumbers: tt.showNumbers,
					StartIndex:  tt.startIndex,
				},
			}

			sp.displayOptions(tt.options)

			output := buf.String()
			lines := strings.Split(output, "\n")

			// Check that output contains expected strings
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing expected string %q\nGot:\n%s", want, output)
				}
			}

			// Check that lines don't start with unwanted prefix
			for _, line := range lines {
				if line == "" {
					continue
				}
				if strings.HasPrefix(line, tt.wantNotStart) {
					t.Errorf("line should not start with %q but got: %q", tt.wantNotStart, line)
				}
			}
		})
	}
}

func TestInteractiveSelector_Template_Alignment(t *testing.T) {
	// Test that the select template doesn't have extra leading spaces
	templateStr := `
{{- if .ShowHelp }}{{- color .Config.Icons.Help.Format }}{{ .Config.Icons.Help.Text }} {{ .Help }}{{color "reset"}}{{"\n"}}{{end}}
{{- color .Config.Icons.Question.Format }}{{ .Config.Icons.Question.Text }} {{color "reset"}}
{{- color "default+hb"}}{{ .Message }}{{ .FilterMessage }}{{color "reset"}}
{{- if .ShowAnswer}}{{color "cyan"}} {{.Answer}}{{color "reset"}}{{"\n"}}
{{- else}}
{{- color "cyan"}}[Use arrows to move, enter to select{{- if and .Help (not .ShowHelp)}}, {{ .Config.HelpInput }} for more help{{end}}]{{color "reset"}}
{{- "\n"}}
{{- range $ix, $choice := .PageEntries}}
{{- if eq $ix $.SelectedIndex}}{{color $.Config.Icons.SelectFocus.Format }}{{ $.Config.Icons.SelectFocus.Text }}{{color "reset"}}{{else}} {{end}}
{{- $choice.Value}}
{{- if eq $ix $.SelectedIndex}}{{color $.Config.Icons.SelectFocus.Format }}{{color "reset"}}{{end}}{{"\n"}}
{{- end}}
{{- end}}`

	// Check that template lines starting with {{- don't have leading spaces
	// (except for within the template code itself)
	lines := strings.Split(templateStr, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		// Template directives should start from column 0
		if strings.HasPrefix(strings.TrimSpace(line), "{{-") && strings.HasPrefix(line, " ") {
			t.Errorf("line %d has unwanted leading spaces: %q", i, line)
		}
	}
}

func TestMultiSelectTemplate_Alignment(t *testing.T) {
	// Test that the multi-select template doesn't have extra leading spaces
	templateStr := `
{{- if .ShowHelp }}{{- color .Config.Icons.Help.Format }}{{ .Config.Icons.Help.Text }} {{ .Help }}{{color "reset"}}{{"\n"}}{{end}}
{{- color .Config.Icons.Question.Format }}{{ .Config.Icons.Question.Text }} {{color "reset"}}
{{- color "default+hb"}}{{ .Message }}{{ .FilterMessage }}{{color "reset"}}
{{- if .ShowAnswer}}{{color "cyan"}} {{.Answer}}{{color "reset"}}{{"\n"}}
{{- else}}
{{- color "cyan"}}[Use arrows to move, space to select, enter to confirm{{- if and .Help (not .ShowHelp)}}, {{ .Config.HelpInput }} for more help{{end}}]{{color "reset"}}
{{- "\n"}}
{{- range $ix, $option := .PageEntries}}
{{- if eq $ix $.SelectedIndex}}{{color $.Config.Icons.SelectFocus.Format }}{{ $.Config.Icons.SelectFocus.Text }} {{color "reset"}}{{else}}{{color "default"}}  {{color "reset"}}{{end}}
{{- if index $.Checked $option.Index }}{{color $.Config.Icons.MarkedOption.Format }}{{ $.Config.Icons.MarkedOption.Text }}{{color "reset"}}{{else}}{{color $.Config.Icons.UnmarkedOption.Format }}{{ $.Config.Icons.UnmarkedOption.Text }}{{color "reset"}}{{end}}
{{- " "}}{{$option.Value}}{{"\n"}}
{{- end}}
{{- end}}`

	lines := strings.Split(templateStr, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		// Template directives should start from column 0
		if strings.HasPrefix(strings.TrimSpace(line), "{{-") && strings.HasPrefix(line, " ") {
			t.Errorf("line %d has unwanted leading spaces: %q", i, line)
		}
	}
}
