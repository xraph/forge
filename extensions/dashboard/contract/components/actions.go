package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// ActionKind discriminates the side effect an Action triggers.
type ActionKind string

const (
	ActionKindNavigate    ActionKind = "navigate"
	ActionKindOpenDialog  ActionKind = "openDialog"
	ActionKindCloseDialog ActionKind = "closeDialog"
	ActionKindRunCommand  ActionKind = "runCommand"
	ActionKindSetState    ActionKind = "setState"
	ActionKindEmit        ActionKind = "emit"
	ActionKindConfirm     ActionKind = "confirm"
	ActionKindCopy        ActionKind = "copy"
	ActionKindToast       ActionKind = "toast"
)

// Action describes an event-handler side effect. It serializes to a small
// JSON object the React shell knows how to dispatch. Exactly one Kind is
// active; the other fields are interpreted based on Kind.
type Action struct {
	Kind ActionKind `json:"kind"`

	// Navigate
	Href    string `json:"href,omitempty"`
	Replace bool   `json:"replace,omitempty"`

	// OpenDialog / CloseDialog
	Dialog string `json:"dialog,omitempty"`

	// RunCommand — intent + params resolved at dispatch time
	Intent      string                          `json:"intent,omitempty"`
	Contributor string                          `json:"contributor,omitempty"`
	Params      map[string]contract.ParamSource `json:"params,omitempty"`

	// SetState — updates client-side form/page state
	StateKey   string `json:"stateKey,omitempty"`
	StateValue any    `json:"stateValue,omitempty"`

	// Emit — fire a named client-side event
	Event   string         `json:"event,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`

	// Confirm — wrap any action behind a confirmation modal
	ConfirmMessage string  `json:"confirmMessage,omitempty"`
	ConfirmTitle   string  `json:"confirmTitle,omitempty"`
	ConfirmAction  *Action `json:"confirmAction,omitempty"`

	// Copy — write a value to the clipboard
	CopyValue string `json:"copyValue,omitempty"`

	// Toast — surface a notification
	ToastMessage string       `json:"toastMessage,omitempty"`
	ToastVariant ToastVariant `json:"toastVariant,omitempty"`
}

// ToastVariant is the visual tone of a toast.
type ToastVariant string

const (
	ToastDefault     ToastVariant = "default"
	ToastDestructive ToastVariant = "destructive"
)

// Navigate builds a navigation Action.
func Navigate(href string) *Action {
	return &Action{Kind: ActionKindNavigate, Href: href}
}

// NavigateReplace builds a replacing navigation Action (history replace).
func NavigateReplace(href string) *Action {
	return &Action{Kind: ActionKindNavigate, Href: href, Replace: true}
}

// OpenDialog builds an Action that opens the named dialog.
func OpenDialog(name string) *Action {
	return &Action{Kind: ActionKindOpenDialog, Dialog: name}
}

// CloseDialog builds an Action that closes the named dialog. Pass "" to
// close the current dialog.
func CloseDialog(name string) *Action {
	return &Action{Kind: ActionKindCloseDialog, Dialog: name}
}

// RunCommand builds an Action that dispatches a contract command intent.
func RunCommand(intent string) *Action {
	return &Action{Kind: ActionKindRunCommand, Intent: intent}
}

// SetState builds an Action that writes a value into the client state at
// the given key.
func SetState(key string, value any) *Action {
	return &Action{Kind: ActionKindSetState, StateKey: key, StateValue: value}
}

// Emit fires a named event on the client event bus.
func Emit(event string) *Action {
	return &Action{Kind: ActionKindEmit, Event: event}
}

// Confirm wraps another Action behind a confirmation modal.
func Confirm(title, message string, action *Action) *Action {
	return &Action{
		Kind:           ActionKindConfirm,
		ConfirmTitle:   title,
		ConfirmMessage: message,
		ConfirmAction:  action,
	}
}

// Copy builds a copy-to-clipboard Action.
func Copy(value string) *Action {
	return &Action{Kind: ActionKindCopy, CopyValue: value}
}

// Toast builds a toast-notification Action.
func Toast(message string) *Action {
	return &Action{Kind: ActionKindToast, ToastMessage: message, ToastVariant: ToastDefault}
}

// ToastError builds a destructive-variant toast.
func ToastError(message string) *Action {
	return &Action{Kind: ActionKindToast, ToastMessage: message, ToastVariant: ToastDestructive}
}

// WithContributor scopes a RunCommand action to a specific contributor.
func (a *Action) WithContributor(c string) *Action {
	a.Contributor = c
	return a
}

// WithParams attaches a parameter map to a RunCommand action.
func (a *Action) WithParams(params map[string]contract.ParamSource) *Action {
	a.Params = params
	return a
}
