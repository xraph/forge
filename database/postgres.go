package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

// postgresDatabase implements SQLDatabase interface for PostgreSQL
type postgresDatabase struct {
	*sqlDatabase
	schema string
}

// NewPostgresDatabase creates a new PostgreSQL database connection
func newPostgresDatabase(config SQLConfig) (SQLDatabase, error) {
	// Force PostgreSQL driver
	config.Driver = "pgx"

	// Create base SQL database
	base, err := newSQLDatabase(config)
	if err != nil {
		return nil, err
	}

	db := &postgresDatabase{
		sqlDatabase: base.(*sqlDatabase),
		schema:      "public",
	}

	// PostgreSQL-specific initialization
	if err := db.initializePostgres(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	return db, nil
}

// initializePostgres performs PostgreSQL-specific initialization
func (db *postgresDatabase) initializePostgres(ctx context.Context) error {
	// Set search path
	if db.schema != "public" {
		query := fmt.Sprintf("SET search_path TO %s", db.quoteName(db.schema))
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to set search path: %w", err)
		}
	}

	// Enable UUID extension if needed
	if err := db.enableUUIDExtension(ctx); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to enable UUID extension: %v\n", err)
	}

	return nil
}

// enableUUIDExtension enables the uuid-ossp extension
func (db *postgresDatabase) enableUUIDExtension(ctx context.Context) error {
	query := "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\""
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// buildPostgresDSN builds PostgreSQL DSN with advanced options
func (db *postgresDatabase) buildPostgresDSN() string {
	config := db.config

	// Set defaults
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 5432
	}

	// Build connection string
	var params []string

	// SSL mode
	if config.SSLMode != "" {
		params = append(params, "sslmode="+config.SSLMode)
	} else {
		params = append(params, "sslmode=disable")
	}

	// Connection timeout
	params = append(params, "connect_timeout=30")

	// Statement timeout
	params = append(params, "statement_timeout=30000")

	// Application name
	params = append(params, "application_name=forge")

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database)

	if len(params) > 0 {
		dsn += " " + strings.Join(params, " ")
	}

	return dsn
}

// CreateTable creates a table with PostgreSQL-specific features
func (db *postgresDatabase) CreateTable(ctx context.Context, tableName string, schema TableSchema) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := db.buildPostgresCreateTableQuery(tableName, schema)
	_, err := db.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Create indexes
	for _, index := range schema.Indexes {
		if err := db.createPostgresIndex(ctx, tableName, index); err != nil {
			return fmt.Errorf("failed to create index %s: %w", index.Name, err)
		}
	}

	// Add column comments
	for _, col := range schema.Columns {
		if col.Comment != "" {
			if err := db.addColumnComment(ctx, tableName, col.Name, col.Comment); err != nil {
				return fmt.Errorf("failed to add comment to column %s: %w", col.Name, err)
			}
		}
	}

	return nil
}

// buildPostgresCreateTableQuery builds PostgreSQL-specific CREATE TABLE query
func (db *postgresDatabase) buildPostgresCreateTableQuery(tableName string, schema TableSchema) string {
	var parts []string

	// Build column definitions
	for _, col := range schema.Columns {
		parts = append(parts, db.buildPostgresColumnDefinition(col))
	}

	// Add primary key
	if len(schema.PrimaryKey) > 0 {
		pk := make([]string, len(schema.PrimaryKey))
		for i, col := range schema.PrimaryKey {
			pk[i] = db.quoteName(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pk, ", ")))
	}

	// Add foreign keys
	for _, fk := range schema.ForeignKeys {
		fkDef := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
			db.quoteName(fk.Name),
			strings.Join(fk.Columns, ", "),
			db.quoteName(fk.ReferencedTable),
			strings.Join(fk.ReferencedColumns, ", "))

		if fk.OnDelete != "" {
			fkDef += " ON DELETE " + fk.OnDelete
		}
		if fk.OnUpdate != "" {
			fkDef += " ON UPDATE " + fk.OnUpdate
		}

		parts = append(parts, fkDef)
	}

	// Add constraints
	for _, constraint := range schema.Constraints {
		constraintDef := fmt.Sprintf("CONSTRAINT %s", db.quoteName(constraint.Name))

		switch constraint.Type {
		case "CHECK":
			constraintDef += " CHECK " + constraint.Expression
		case "UNIQUE":
			if len(constraint.Columns) > 0 {
				constraintDef += " UNIQUE (" + strings.Join(constraint.Columns, ", ") + ")"
			}
		default:
			constraintDef += " " + constraint.Type
			if constraint.Expression != "" {
				constraintDef += " " + constraint.Expression
			}
		}

		parts = append(parts, constraintDef)
	}

	query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)",
		db.quoteName(tableName),
		strings.Join(parts, ",\n  "))

	return query
}

// buildPostgresColumnDefinition builds PostgreSQL-specific column definition
func (db *postgresDatabase) buildPostgresColumnDefinition(col ColumnDefinition) string {
	def := fmt.Sprintf("%s %s", db.quoteName(col.Name), db.mapPostgresType(col.Type, col.Size, col.Precision, col.Scale))

	if col.NotNull {
		def += " NOT NULL"
	}

	if col.AutoIncrement {
		// PostgreSQL uses SERIAL types for auto-increment
		switch strings.ToUpper(col.Type) {
		case "BIGINT":
			def = fmt.Sprintf("%s BIGSERIAL", db.quoteName(col.Name))
		case "SMALLINT":
			def = fmt.Sprintf("%s SMALLSERIAL", db.quoteName(col.Name))
		default:
			def = fmt.Sprintf("%s SERIAL", db.quoteName(col.Name))
		}

		if col.NotNull {
			def += " NOT NULL"
		}
	}

	if col.DefaultValue != nil && !col.AutoIncrement {
		switch v := col.DefaultValue.(type) {
		case string:
			if v == "NOW()" || v == "CURRENT_TIMESTAMP" {
				def += " DEFAULT " + v
			} else {
				def += " DEFAULT '" + v + "'"
			}
		case bool:
			def += fmt.Sprintf(" DEFAULT %t", v)
		default:
			def += fmt.Sprintf(" DEFAULT %v", v)
		}
	}

	return def
}

// mapPostgresType maps generic types to PostgreSQL-specific types
func (db *postgresDatabase) mapPostgresType(dataType string, size, precision, scale int) string {
	upperType := strings.ToUpper(dataType)

	switch upperType {
	case "VARCHAR":
		if size > 0 {
			return fmt.Sprintf("VARCHAR(%d)", size)
		}
		return "TEXT"
	case "CHAR":
		if size > 0 {
			return fmt.Sprintf("CHAR(%d)", size)
		}
		return "CHAR(1)"
	case "TEXT":
		return "TEXT"
	case "INT", "INTEGER":
		return "INTEGER"
	case "BIGINT":
		return "BIGINT"
	case "SMALLINT":
		return "SMALLINT"
	case "DECIMAL", "NUMERIC":
		if precision > 0 && scale >= 0 {
			return fmt.Sprintf("NUMERIC(%d,%d)", precision, scale)
		}
		return "NUMERIC"
	case "FLOAT":
		return "REAL"
	case "DOUBLE":
		return "DOUBLE PRECISION"
	case "BOOLEAN", "BOOL":
		return "BOOLEAN"
	case "DATE":
		return "DATE"
	case "TIME":
		return "TIME"
	case "DATETIME", "TIMESTAMP":
		return "TIMESTAMP"
	case "TIMESTAMPTZ":
		return "TIMESTAMP WITH TIME ZONE"
	case "JSON":
		return "JSON"
	case "JSONB":
		return "JSONB"
	case "UUID":
		return "UUID"
	case "BYTEA", "BLOB":
		return "BYTEA"
	default:
		return dataType
	}
}

// createPostgresIndex creates a PostgreSQL index
func (db *postgresDatabase) createPostgresIndex(ctx context.Context, tableName string, index Index) error {
	var unique string
	if index.Unique {
		unique = "UNIQUE "
	}

	var method string
	if index.Type != "" {
		method = fmt.Sprintf(" USING %s", strings.ToUpper(index.Type))
	}

	// Quote column names
	columns := make([]string, len(index.Columns))
	for i, col := range index.Columns {
		columns[i] = db.quoteName(col)
	}

	query := fmt.Sprintf("CREATE %sINDEX %s ON %s%s (%s)",
		unique,
		db.quoteName(index.Name),
		db.quoteName(tableName),
		method,
		strings.Join(columns, ", "))

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// addColumnComment adds a comment to a column
func (db *postgresDatabase) addColumnComment(ctx context.Context, tableName, columnName, comment string) error {
	query := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '%s'",
		db.quoteName(tableName),
		db.quoteName(columnName),
		strings.Replace(comment, "'", "''", -1))

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// TableExists checks if a table exists in PostgreSQL
func (db *postgresDatabase) TableExists(ctx context.Context, tableName string) (bool, error) {
	if db.db == nil {
		return false, fmt.Errorf("database not connected")
	}

	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = $2
		)`

	var exists bool
	err := db.db.QueryRowContext(ctx, query, db.schema, tableName).Scan(&exists)
	return exists, err
}

// GetTableSchema returns the schema of a table
func (db *postgresDatabase) GetTableSchema(ctx context.Context, tableName string) (*TableSchema, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	schema := &TableSchema{
		Columns:     []ColumnDefinition{},
		PrimaryKey:  []string{},
		ForeignKeys: []ForeignKey{},
		Indexes:     []Index{},
		Constraints: []Constraint{},
	}

	// Get columns
	columnsQuery := `
		SELECT 
			column_name,
			data_type,
			character_maximum_length,
			numeric_precision,
			numeric_scale,
			is_nullable,
			column_default,
			col_description(pgc.oid, pa.attnum) as comment
		FROM information_schema.columns isc
		LEFT JOIN pg_class pgc ON pgc.relname = isc.table_name
		LEFT JOIN pg_attribute pa ON pa.attrelid = pgc.oid AND pa.attname = isc.column_name
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`

	rows, err := db.db.QueryContext(ctx, columnsQuery, db.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var col ColumnDefinition
		var dataType string
		var maxLength, precision, scale sql.NullInt64
		var nullable string
		var defaultValue, comment sql.NullString

		err := rows.Scan(&col.Name, &dataType, &maxLength, &precision, &scale, &nullable, &defaultValue, &comment)
		if err != nil {
			return nil, err
		}

		col.Type = strings.ToUpper(dataType)
		if maxLength.Valid {
			col.Size = int(maxLength.Int64)
		}
		if precision.Valid {
			col.Precision = int(precision.Int64)
		}
		if scale.Valid {
			col.Scale = int(scale.Int64)
		}
		col.NotNull = nullable == "NO"
		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}
		if comment.Valid {
			col.Comment = comment.String
		}

		// Check for auto-increment (SERIAL types)
		if strings.Contains(strings.ToLower(col.Type), "serial") {
			col.AutoIncrement = true
		}

		schema.Columns = append(schema.Columns, col)
	}

	// Get primary key
	pkQuery := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = (
			SELECT c.oid FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relname = $2 AND n.nspname = $1
		) AND i.indisprimary
		ORDER BY a.attnum`

	pkRows, err := db.db.QueryContext(ctx, pkQuery, db.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer pkRows.Close()

	for pkRows.Next() {
		var colName string
		if err := pkRows.Scan(&colName); err != nil {
			return nil, err
		}
		schema.PrimaryKey = append(schema.PrimaryKey, colName)
	}

	return schema, nil
}

// PostgreSQL-specific utility functions

// CreateSchema creates a new schema
func (db *postgresDatabase) CreateSchema(ctx context.Context, schemaName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", db.quoteName(schemaName))
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// DropSchema drops a schema
func (db *postgresDatabase) DropSchema(ctx context.Context, schemaName string, cascade bool) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("DROP SCHEMA IF EXISTS %s", db.quoteName(schemaName))
	if cascade {
		query += " CASCADE"
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// ListSchemas lists all schemas
func (db *postgresDatabase) ListSchemas(ctx context.Context) ([]string, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := `
		SELECT schema_name 
		FROM information_schema.schemata 
		WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		ORDER BY schema_name`

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// VACUUM performs a vacuum operation
func (db *postgresDatabase) Vacuum(ctx context.Context, tableName string, full bool) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "VACUUM"
	if full {
		query += " FULL"
	}

	if tableName != "" {
		query += " " + db.quoteName(tableName)
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// ANALYZE updates table statistics
func (db *postgresDatabase) Analyze(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "ANALYZE"
	if tableName != "" {
		query += " " + db.quoteName(tableName)
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// GetConnectionInfo returns PostgreSQL connection information
func (db *postgresDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	info := make(map[string]interface{})

	// Get version
	var version string
	err := db.db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return nil, err
	}
	info["version"] = version

	// Get current database
	var database string
	err = db.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database)
	if err != nil {
		return nil, err
	}
	info["database"] = database

	// Get current user
	var user string
	err = db.db.QueryRowContext(ctx, "SELECT current_user").Scan(&user)
	if err != nil {
		return nil, err
	}
	info["user"] = user

	// Get current schema
	var schema string
	err = db.db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema)
	if err != nil {
		return nil, err
	}
	info["schema"] = schema

	return info, nil
}

// Error handling for PostgreSQL-specific errors
func (db *postgresDatabase) handlePostgresError(err error) error {
	if err == nil {
		return nil
	}

	if pqErr, ok := err.(*pq.Error); ok {
		switch pqErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("unique constraint violation: %s", pqErr.Message)
		case "23503": // foreign_key_violation
			return fmt.Errorf("foreign key constraint violation: %s", pqErr.Message)
		case "23502": // not_null_violation
			return fmt.Errorf("not null constraint violation: %s", pqErr.Message)
		case "23514": // check_violation
			return fmt.Errorf("check constraint violation: %s", pqErr.Message)
		case "42P01": // undefined_table
			return fmt.Errorf("table does not exist: %s", pqErr.Message)
		case "42703": // undefined_column
			return fmt.Errorf("column does not exist: %s", pqErr.Message)
		default:
			return fmt.Errorf("postgres error %s: %s", pqErr.Code, pqErr.Message)
		}
	}

	return err
}

// Exec Override error-prone methods to use PostgreSQL error handling
func (db *postgresDatabase) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := db.sqlDatabase.Exec(ctx, query, args...)
	return result, db.handlePostgresError(err)
}

func (db *postgresDatabase) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	rows, err := db.sqlDatabase.Query(ctx, query, args...)
	return rows, db.handlePostgresError(err)
}

func (db *postgresDatabase) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// Note: QueryRow doesn't return an error directly, but the error is returned on Scan
	return db.sqlDatabase.QueryRow(ctx, query, args...)
}

// init function to override the PostgreSQL constructor
func init() {
	NewPostgresDatabase = func(config SQLConfig) (SQLDatabase, error) {
		return newPostgresDatabase(config)
	}
}
