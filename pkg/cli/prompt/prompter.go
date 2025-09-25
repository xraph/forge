package prompt

import (
	"io"
)

// Prompter combines all prompt types
type Prompter struct {
	confirmPrompter     *ConfirmPrompter
	selectPrompter      *SelectPrompter
	interactivePrompter *InteractiveSelector
	inputPrompter       *InputPrompter
	passwordPrompter    *PasswordPrompter
}

// NewPrompter creates a new prompter with all capabilities
func NewPrompter(reader io.Reader, writer io.Writer) *Prompter {
	return &Prompter{
		confirmPrompter:     NewConfirmPrompter(reader, writer),
		selectPrompter:      NewSelectPrompter(reader, writer),
		inputPrompter:       NewInputPrompter(reader, writer),
		passwordPrompter:    NewPasswordPrompter(reader, writer),
		interactivePrompter: NewInteractiveSelector(reader, writer),
	}
}

// Confirm prompts for confirmation
func (p *Prompter) Confirm(message string) bool {
	return p.confirmPrompter.Confirm(message)
}

// ConfirmWithDefault prompts for confirmation with default
func (p *Prompter) ConfirmWithDefault(message string, defaultValue bool) bool {
	return p.confirmPrompter.ConfirmWithDefault(message, defaultValue)
}

// Select prompts for selection (fallback to basic select)
func (p *Prompter) Select(message string, options []string) (int, error) {
	return p.selectPrompter.Select(message, options)
}

// InteractiveSelect provides rich interactive selection using survey
func (p *Prompter) InteractiveSelect(message string, options []SelectionOption) (int, *SelectionOption, error) {
	return p.interactivePrompter.Select(message, options)
}

// MultiSelect prompts for multiple selections using survey
func (p *Prompter) MultiSelect(message string, options []SelectionOption) ([]int, []*SelectionOption, error) {
	return p.interactivePrompter.MultiSelect(message, options)
}

// MultiSelectStrings prompts for multiple selections (fallback)
func (p *Prompter) MultiSelectStrings(message string, options []string) ([]int, error) {
	return p.selectPrompter.MultiSelect(message, options)
}

// Input prompts for input
func (p *Prompter) Input(message string, validate ValidationFunc) (string, error) {
	return p.inputPrompter.Input(message, validate)
}

// RequiredInput prompts for required input
func (p *Prompter) RequiredInput(message string, validate ValidationFunc) (string, error) {
	return p.inputPrompter.RequiredInput(message, validate)
}

// Password prompts for password
func (p *Prompter) Password(message string) (string, error) {
	return p.passwordPrompter.Password(message)
}

// PasswordWithConfirm prompts for password with confirmation
func (p *Prompter) PasswordWithConfirm(message string) (string, error) {
	return p.passwordPrompter.PasswordWithConfirm(message)
}
