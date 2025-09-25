package database

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// CreateMigrationConfig contains configuration for creating new migrations
type CreateMigrationConfig struct {
	Name      string `json:"name"`
	Type      string `json:"type"`      // "sql" or "go"
	Template  string `json:"template"`  // Template to use
	Package   string `json:"package"`   // Go package name
	Directory string `json:"directory"` // Directory to create in
}

// CreateMigrationResult represents the result of creating a migration
type CreateMigrationResult struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Template string `json:"template"`
	Type     string `json:"type"`
}

// CreateMigration creates a new migration file
func CreateMigration(ctx context.Context, config CreateMigrationConfig) (*CreateMigrationResult, error) {
	timestamp := time.Now().Format("20060102150405")

	var filename string
	var template string

	switch config.Type {
	case "sql":
		filename = fmt.Sprintf("%s_%s.sql", timestamp, config.Name)
		template = getSQLMigrationTemplate()
	case "go":
		filename = fmt.Sprintf("%s_%s.go", timestamp, config.Name)
		template = getGoMigrationTemplate(config.Package, config.Name)
	default:
		return nil, fmt.Errorf("unsupported migration type: %s", config.Type)
	}

	path := filepath.Join(config.Directory, filename)

	result := &CreateMigrationResult{
		Filename: filename,
		Path:     path,
		Template: template,
		Type:     config.Type,
	}

	return result, nil
}

// getSQLMigrationTemplate returns the SQL migration template
func getSQLMigrationTemplate() string {
	return `-- Migration: {{.Name}}
-- Created: {{.CreatedAt}}

-- +migrate Up
-- SQL statements for the UP migration go here


-- +migrate Down  
-- SQL statements for the DOWN migration go here

`
}

// getGoMigrationTemplate returns the Go migration template
func getGoMigrationTemplate(packageName, migrationName string) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"database/sql"
)

func init() {
	migrations.Register(&Migration_%s{})
}

type Migration_%s struct{}

func (m *Migration_%s) Name() string {
	return "%s"
}

func (m *Migration_%s) Up(ctx context.Context, tx *sql.Tx) error {
	// TODO: Implement UP migration
	return nil
}

func (m *Migration_%s) Down(ctx context.Context, tx *sql.Tx) error {
	// TODO: Implement DOWN migration  
	return nil
}
`, packageName, migrationName, migrationName, migrationName, migrationName, migrationName, migrationName)
}
