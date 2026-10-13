package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/xraph/forge/cli"
)

// mustAddSubcommand attaches sub to parent and panics if the cli rejects it.
// Plugin Commands() methods have no error return, and the only way
// AddSubcommand fails is a duplicate name or alias. With static command names
// that is a programming error, and panicking makes every test that builds the
// command tree catch it instead of the subcommand silently going missing.
func mustAddSubcommand(parent, sub cli.Command) {
	if err := parent.AddSubcommand(sub); err != nil {
		panic(fmt.Sprintf("register %q under %q: %v", sub.Name(), parent.Name(), err))
	}
}

// writeTemplate renders a Go text/template string and writes the result to a file.
func writeTemplate(path string, tmplStr string, data any) error {
	tmpl, err := template.New("scaffold").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse template for %s: %w", filepath.Base(path), err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template for %s: %w", filepath.Base(path), err)
	}

	return os.WriteFile(path, []byte(buf.String()), 0644)
}

// toDisplayName converts a snake_case or camelCase name to a display name.
func toDisplayName(name string) string {
	// Replace underscores with spaces
	s := strings.ReplaceAll(name, "_", " ")
	// Capitalize first letter of each word
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

// relPath returns a relative path from base to target, falling back to target on error.
func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}

	return rel
}
