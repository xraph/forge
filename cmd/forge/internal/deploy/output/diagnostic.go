// Package output holds the diagnostics, JSON envelope and exit codes every
// deploy command shares.
package output

import "sort"

const SchemaVersion = "forge.deploy/v1"

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Field    string   `json:"field,omitempty"`
	Fix      string   `json:"fix,omitempty"`
}

type Diagnostics []Diagnostic

func (d Diagnostics) HasErrors() bool {
	for _, x := range d {
		if x.Severity == SeverityError {
			return true
		}
	}

	return false
}

func (d Diagnostics) Errors() Diagnostics {
	var out Diagnostics

	for _, x := range d {
		if x.Severity == SeverityError {
			out = append(out, x)
		}
	}

	return out
}

func (d Diagnostics) Sorted() Diagnostics {
	out := append(Diagnostics(nil), d...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}

		if a.Line != b.Line {
			return a.Line < b.Line
		}

		if a.Field != b.Field {
			return a.Field < b.Field
		}

		return a.Code < b.Code
	})

	return out
}

type Envelope struct {
	Schema      string      `json:"schema"`
	Command     string      `json:"command"`
	OK          bool        `json:"ok"`
	Data        any         `json:"data,omitempty"`
	Diagnostics Diagnostics `json:"diagnostics"`
}
