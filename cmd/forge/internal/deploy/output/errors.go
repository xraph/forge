package output

import (
	"errors"
	"fmt"

	"github.com/xraph/forge/cli"
)

const (
	ExitOK           = 0
	ExitInvalidInput = 2
	ExitUnresolved   = 3
	ExitUnsupported  = 4
	ExitAccess       = 5
	ExitConflict     = 6
	ExitApplyFailed  = 7
	ExitTimeout      = 8
)

var ErrUnsupported = errors.New("not supported")

type Error struct {
	Code        int
	Message     string
	Diagnostics Diagnostics
	Cause       error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}

	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// Fail returns an error the CLI maps to code. errors.As finds the *Error.
func Fail(code int, message string, diags ...Diagnostic) error {
	return cli.WrapError(&Error{Code: code, Message: message, Diagnostics: diags}, message, code)
}

// Unsupported is exit 4 with a handoff. what names the command; instead says
// what the reader can do today.
func Unsupported(what, instead string) error {
	msg := what + " is not available in this release. " + instead

	return cli.WrapError(&Error{Code: ExitUnsupported, Message: msg, Cause: ErrUnsupported,
		Diagnostics: Diagnostics{{Code: CodeUnsupportedCommand, Severity: SeverityError, Message: msg, Fix: instead}}}, msg, ExitUnsupported)
}
