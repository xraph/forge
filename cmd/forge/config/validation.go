// v2/cmd/forge/config/validation.go
package config

import (
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field      string // The config field with the error
	Message    string // Human-readable error message
	Suggestion string // Suggested fix
	Level      string // "error" or "warning"
}

// Error implements the error interface.
func (v ValidationError) Error() string {
	if v.Suggestion != "" {
		return fmt.Sprintf("%s: %s\nSuggestion: %s", v.Field, v.Message, v.Suggestion)
	}

	return fmt.Sprintf("%s: %s", v.Field, v.Message)
}

// Validate checks the configuration and returns any validation errors or warnings.
func (c *ForgeConfig) Validate() []ValidationError {
	var errs []ValidationError

	// Required fields
	if c.Project.Name == "" {
		errs = append(errs, ValidationError{
			Field:      "project.name",
			Level:      "error",
			Message:    "project name is required",
			Suggestion: `Add to .forge.yaml:\nproject:\n  name: "my-project"`,
		})
	}

	// Single-module requires module path
	if c.IsSingleModule() && c.Project.Module == "" {
		errs = append(errs, ValidationError{
			Field:      "project.module",
			Level:      "error",
			Message:    "module path is required for single-module layout",
			Suggestion: `Add to .forge.yaml:\nproject:\n  module: "github.com/org/project"`,
		})
	}

	// Multi-module workspace validation
	if c.IsMultiModule() {
		if !c.Project.Workspace.Enabled {
			errs = append(errs, ValidationError{
				Field:      "project.workspace.enabled",
				Level:      "warning",
				Message:    "workspace should be enabled for multi-module layout",
				Suggestion: `Add to .forge.yaml:\nproject:\n  workspace:\n    enabled: true`,
			})
		}
	}

	// Validate database driver if connections are specified
	if len(c.Database.Connections) > 0 && c.Database.Driver == "" {
		errs = append(errs, ValidationError{
			Field:      "database.driver",
			Level:      "warning",
			Message:    "database driver not specified but connections are defined",
			Suggestion: `Add to .forge.yaml:\ndatabase:\n  driver: "postgres"  # or mysql, sqlite`,
		})
	}

	// Validate deploy config
	if len(c.Deploy.Environments) > 0 && c.Deploy.Registry == "" {
		errs = append(errs, ValidationError{
			Field:      "deploy.registry",
			Level:      "warning",
			Message:    "deployment environments defined but no registry specified",
			Suggestion: `Add to .forge.yaml:\ndeploy:\n  registry: "ghcr.io/myorg"`,
		})
	}

	// Check for empty environment names
	for i, env := range c.Deploy.Environments {
		if env.Name == "" {
			errs = append(errs, ValidationError{
				Field:      fmt.Sprintf("deploy.environments[%d].name", i),
				Level:      "error",
				Message:    "environment name cannot be empty",
				Suggestion: `Add name to environment:\nenvironments:\n  - name: "staging"`,
			})
		}
	}

	// Validate build apps if explicitly defined
	for i, app := range c.Build.Apps {
		if app.Name == "" {
			errs = append(errs, ValidationError{
				Field:      fmt.Sprintf("build.apps[%d].name", i),
				Level:      "error",
				Message:    "app name cannot be empty",
				Suggestion: "Remove the empty app entry or add a name",
			})
		}

		if app.Cmd == "" && app.Module == "" {
			errs = append(errs, ValidationError{
				Field:      fmt.Sprintf("build.apps[%d]", i),
				Level:      "warning",
				Message:    fmt.Sprintf("app '%s' has no cmd or module path specified", app.Name),
				Suggestion: "Specify either 'cmd' (single-module) or 'module' (multi-module)",
			})
		}
	}

	return errs
}

// HasErrors returns true if there are any error-level validation issues.
func (c *ForgeConfig) HasErrors() bool {
	errs := c.Validate()
	for _, err := range errs {
		if err.Level == "error" {
			return true
		}
	}

	return false
}

// HasWarnings returns true if there are any warning-level validation issues.
func (c *ForgeConfig) HasWarnings() bool {
	errs := c.Validate()
	for _, err := range errs {
		if err.Level == "warning" {
			return true
		}
	}

	return false
}

// FormatValidationErrors formats validation errors for display.
func FormatValidationErrors(errs []ValidationError) string {
	if len(errs) == 0 {
		return ""
	}

	var sb strings.Builder

	// Group by level
	var errors, warnings []ValidationError
	for _, err := range errs {
		if err.Level == "error" {
			errors = append(errors, err)
		} else {
			warnings = append(warnings, err)
		}
	}

	if len(errors) > 0 {
		sb.WriteString("Configuration Errors:\n")
		for _, err := range errors {
			sb.WriteString(fmt.Sprintf("  ✗ %s: %s\n", err.Field, err.Message))
			if err.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("    → %s\n", err.Suggestion))
			}
		}
		sb.WriteString("\n")
	}

	if len(warnings) > 0 {
		sb.WriteString("Configuration Warnings:\n")
		for _, warn := range warnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s: %s\n", warn.Field, warn.Message))
			if warn.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("    → %s\n", warn.Suggestion))
			}
		}
	}

	return sb.String()
}
