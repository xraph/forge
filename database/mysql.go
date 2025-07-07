package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
)

// mysqlDatabase implements SQLDatabase interface for MySQL
type mysqlDatabase struct {
	*sqlDatabase
	charset   string
	collation string
}

// NewMySQLDatabase creates a new MySQL database connection
func newMySQLDatabase(config SQLConfig) (SQLDatabase, error) {
	// Force MySQL driver
	config.Driver = "mysql"

	// Create base SQL database
	base, err := newSQLDatabase(config)
	if err != nil {
		return nil, err
	}

	db := &mysqlDatabase{
		sqlDatabase: base.(*sqlDatabase),
		charset:     "utf8mb4",
		collation:   "utf8mb4_unicode_ci",
	}

	// MySQL-specific initialization
	if err := db.initializeMySQL(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize MySQL: %w", err)
	}

	return db, nil
}

// initializeMySQL performs MySQL-specific initialization
func (db *mysqlDatabase) initializeMySQL(ctx context.Context) error {
	// Set connection parameters
	queries := []string{
		"SET SESSION sql_mode = 'STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'",
		"SET SESSION time_zone = '+00:00'",
		"SET SESSION innodb_strict_mode = 1",
		"SET SESSION autocommit = 1",
	}

	for _, query := range queries {
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			// Log warning but don't fail
			fmt.Printf("Warning: failed to execute MySQL init query '%s': %v\n", query, err)
		}
	}

	return nil
}

// buildMySQLDSN builds MySQL DSN with advanced options
func (db *mysqlDatabase) buildMySQLDSN() string {
	config := db.config

	// Set defaults
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 3306
	}

	// Build DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		config.Username, config.Password, config.Host, config.Port, config.Database)

	// Add parameters
	params := url.Values{}
	params.Set("parseTime", "true")
	params.Set("loc", "UTC")
	params.Set("charset", db.charset)
	params.Set("collation", db.collation)
	params.Set("timeout", "30s")
	params.Set("readTimeout", "30s")
	params.Set("writeTimeout", "30s")
	params.Set("maxAllowedPacket", "67108864") // 64MB

	// SSL configuration
	if config.SSLMode != "" {
		params.Set("tls", config.SSLMode)
	} else {
		params.Set("tls", "false")
	}

	// Connection pool settings
	if config.MaxOpenConns > 0 {
		params.Set("maxOpenConns", fmt.Sprintf("%d", config.MaxOpenConns))
	}
	if config.MaxIdleConns > 0 {
		params.Set("maxIdleConns", fmt.Sprintf("%d", config.MaxIdleConns))
	}
	if config.ConnMaxLifetime > 0 {
		params.Set("connMaxLifetime", config.ConnMaxLifetime.String())
	}

	dsn += "?" + params.Encode()
	return dsn
}

// CreateTable creates a table with MySQL-specific features
func (db *mysqlDatabase) CreateTable(ctx context.Context, tableName string, schema TableSchema) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := db.buildMySQLCreateTableQuery(tableName, schema)
	_, err := db.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Create indexes
	for _, index := range schema.Indexes {
		if err := db.createMySQLIndex(ctx, tableName, index); err != nil {
			return fmt.Errorf("failed to create index %s: %w", index.Name, err)
		}
	}

	return nil
}

// buildMySQLCreateTableQuery builds MySQL-specific CREATE TABLE query
func (db *mysqlDatabase) buildMySQLCreateTableQuery(tableName string, schema TableSchema) string {
	var parts []string

	// Build column definitions
	for _, col := range schema.Columns {
		parts = append(parts, db.buildMySQLColumnDefinition(col))
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
		fkColumns := make([]string, len(fk.Columns))
		for i, col := range fk.Columns {
			fkColumns[i] = db.quoteName(col)
		}

		refColumns := make([]string, len(fk.ReferencedColumns))
		for i, col := range fk.ReferencedColumns {
			refColumns[i] = db.quoteName(col)
		}

		fkDef := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
			db.quoteName(fk.Name),
			strings.Join(fkColumns, ", "),
			db.quoteName(fk.ReferencedTable),
			strings.Join(refColumns, ", "))

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
				columns := make([]string, len(constraint.Columns))
				for i, col := range constraint.Columns {
					columns[i] = db.quoteName(col)
				}
				constraintDef += " UNIQUE (" + strings.Join(columns, ", ") + ")"
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

	// Add table options
	query += fmt.Sprintf(" ENGINE=InnoDB DEFAULT CHARSET=%s COLLATE=%s", db.charset, db.collation)

	return query
}

// buildMySQLColumnDefinition builds MySQL-specific column definition
func (db *mysqlDatabase) buildMySQLColumnDefinition(col ColumnDefinition) string {
	def := fmt.Sprintf("%s %s", db.quoteName(col.Name), db.mapMySQLType(col.Type, col.Size, col.Precision, col.Scale))

	if col.NotNull {
		def += " NOT NULL"
	}

	if col.AutoIncrement {
		def += " AUTO_INCREMENT"
	}

	if col.DefaultValue != nil && !col.AutoIncrement {
		switch v := col.DefaultValue.(type) {
		case string:
			if v == "NOW()" || v == "CURRENT_TIMESTAMP" {
				def += " DEFAULT " + v
			} else {
				def += " DEFAULT '" + strings.Replace(v, "'", "''", -1) + "'"
			}
		case bool:
			if v {
				def += " DEFAULT 1"
			} else {
				def += " DEFAULT 0"
			}
		default:
			def += fmt.Sprintf(" DEFAULT %v", v)
		}
	}

	if col.Comment != "" {
		def += fmt.Sprintf(" COMMENT '%s'", strings.Replace(col.Comment, "'", "''", -1))
	}

	return def
}

// mapMySQLType maps generic types to MySQL-specific types
func (db *mysqlDatabase) mapMySQLType(dataType string, size, precision, scale int) string {
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
	case "LONGTEXT":
		return "LONGTEXT"
	case "INT", "INTEGER":
		return "INT"
	case "BIGINT":
		return "BIGINT"
	case "SMALLINT":
		return "SMALLINT"
	case "TINYINT":
		return "TINYINT"
	case "DECIMAL", "NUMERIC":
		if precision > 0 && scale >= 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
		}
		return "DECIMAL"
	case "FLOAT":
		return "FLOAT"
	case "DOUBLE":
		return "DOUBLE"
	case "BOOLEAN", "BOOL":
		return "BOOLEAN"
	case "DATE":
		return "DATE"
	case "TIME":
		return "TIME"
	case "DATETIME", "TIMESTAMP":
		return "DATETIME"
	case "JSON":
		return "JSON"
	case "BLOB":
		return "BLOB"
	case "LONGBLOB":
		return "LONGBLOB"
	case "BINARY":
		if size > 0 {
			return fmt.Sprintf("BINARY(%d)", size)
		}
		return "BINARY(1)"
	case "VARBINARY":
		if size > 0 {
			return fmt.Sprintf("VARBINARY(%d)", size)
		}
		return "VARBINARY(255)"
	default:
		return dataType
	}
}

// createMySQLIndex creates a MySQL index
func (db *mysqlDatabase) createMySQLIndex(ctx context.Context, tableName string, index Index) error {
	var unique string
	if index.Unique {
		unique = "UNIQUE "
	}

	var indexType string
	if index.Type != "" {
		indexType = fmt.Sprintf(" USING %s", strings.ToUpper(index.Type))
	}

	// Quote column names
	columns := make([]string, len(index.Columns))
	for i, col := range index.Columns {
		columns[i] = db.quoteName(col)
	}

	query := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)%s",
		unique,
		db.quoteName(index.Name),
		db.quoteName(tableName),
		strings.Join(columns, ", "),
		indexType)

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// TableExists checks if a table exists in MySQL
func (db *mysqlDatabase) TableExists(ctx context.Context, tableName string) (bool, error) {
	if db.db == nil {
		return false, fmt.Errorf("database not connected")
	}

	query := `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() AND table_name = ?`

	var count int
	err := db.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetTableSchema returns the schema of a table
func (db *mysqlDatabase) GetTableSchema(ctx context.Context, tableName string) (*TableSchema, error) {
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
			column_comment,
			extra
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`

	rows, err := db.db.QueryContext(ctx, columnsQuery, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var col ColumnDefinition
		var dataType string
		var maxLength, precision, scale sql.NullInt64
		var nullable, defaultValue, comment, extra sql.NullString

		err := rows.Scan(&col.Name, &dataType, &maxLength, &precision, &scale, &nullable, &defaultValue, &comment, &extra)
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
		col.NotNull = nullable.String == "NO"
		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}
		if comment.Valid {
			col.Comment = comment.String
		}

		// Check for auto-increment
		if extra.Valid && strings.Contains(strings.ToLower(extra.String), "auto_increment") {
			col.AutoIncrement = true
		}

		schema.Columns = append(schema.Columns, col)
	}

	// Get primary key
	pkQuery := `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE() AND table_name = ? AND constraint_name = 'PRIMARY'
		ORDER BY ordinal_position`

	pkRows, err := db.db.QueryContext(ctx, pkQuery, tableName)
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

	// Get foreign keys
	fkQuery := `
		SELECT 
			kcu.constraint_name,
			kcu.column_name,
			kcu.referenced_table_name,
			kcu.referenced_column_name,
			rc.update_rule,
			rc.delete_rule
		FROM information_schema.key_column_usage kcu
		JOIN information_schema.referential_constraints rc ON kcu.constraint_name = rc.constraint_name
		WHERE kcu.table_schema = DATABASE() AND kcu.table_name = ?
		  AND kcu.referenced_table_name IS NOT NULL
		ORDER BY kcu.constraint_name, kcu.ordinal_position`

	fkRows, err := db.db.QueryContext(ctx, fkQuery, tableName)
	if err != nil {
		return nil, err
	}
	defer fkRows.Close()

	fkMap := make(map[string]*ForeignKey)
	for fkRows.Next() {
		var constraintName, columnName, refTable, refColumn, updateRule, deleteRule string
		if err := fkRows.Scan(&constraintName, &columnName, &refTable, &refColumn, &updateRule, &deleteRule); err != nil {
			return nil, err
		}

		if fk, exists := fkMap[constraintName]; exists {
			fk.Columns = append(fk.Columns, columnName)
			fk.ReferencedColumns = append(fk.ReferencedColumns, refColumn)
		} else {
			fkMap[constraintName] = &ForeignKey{
				Name:              constraintName,
				Columns:           []string{columnName},
				ReferencedTable:   refTable,
				ReferencedColumns: []string{refColumn},
				OnUpdate:          updateRule,
				OnDelete:          deleteRule,
			}
		}
	}

	for _, fk := range fkMap {
		schema.ForeignKeys = append(schema.ForeignKeys, *fk)
	}

	return schema, nil
}

// MySQL-specific utility functions

// ShowTables returns all tables in the current database
func (db *mysqlDatabase) ShowTables(ctx context.Context) ([]string, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := db.db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, nil
}

// ShowCreateTable returns the CREATE TABLE statement for a table
func (db *mysqlDatabase) ShowCreateTable(ctx context.Context, tableName string) (string, error) {
	if db.db == nil {
		return "", fmt.Errorf("database not connected")
	}

	query := "SHOW CREATE TABLE " + db.quoteName(tableName)

	var table, createStmt string
	err := db.db.QueryRowContext(ctx, query).Scan(&table, &createStmt)
	if err != nil {
		return "", err
	}

	return createStmt, nil
}

// OptimizeTable optimizes a table
func (db *mysqlDatabase) OptimizeTable(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "OPTIMIZE TABLE " + db.quoteName(tableName)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// AnalyzeTable analyzes a table
func (db *mysqlDatabase) AnalyzeTable(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "ANALYZE TABLE " + db.quoteName(tableName)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// CheckTable checks a table for errors
func (db *mysqlDatabase) CheckTable(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "CHECK TABLE " + db.quoteName(tableName)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// RepairTable repairs a table
func (db *mysqlDatabase) RepairTable(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "REPAIR TABLE " + db.quoteName(tableName)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// GetConnectionInfo returns MySQL connection information
func (db *mysqlDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	info := make(map[string]interface{})

	// Get version
	var version string
	err := db.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	if err != nil {
		return nil, err
	}
	info["version"] = version

	// Get current database
	var database string
	err = db.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database)
	if err != nil {
		return nil, err
	}
	info["database"] = database

	// Get current user
	var user string
	err = db.db.QueryRowContext(ctx, "SELECT USER()").Scan(&user)
	if err != nil {
		return nil, err
	}
	info["user"] = user

	// Get connection id
	var connectionID int64
	err = db.db.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connectionID)
	if err != nil {
		return nil, err
	}
	info["connection_id"] = connectionID

	return info, nil
}

// Error handling for MySQL-specific errors
func (db *mysqlDatabase) handleMySQLError(err error) error {
	if err == nil {
		return nil
	}

	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		switch mysqlErr.Number {
		case 1062: // ER_DUP_ENTRY
			return fmt.Errorf("duplicate entry: %s", mysqlErr.Message)
		case 1451: // ER_ROW_IS_REFERENCED_2
			return fmt.Errorf("foreign key constraint violation: %s", mysqlErr.Message)
		case 1452: // ER_NO_REFERENCED_ROW_2
			return fmt.Errorf("foreign key constraint violation: %s", mysqlErr.Message)
		case 1364: // ER_NO_DEFAULT_FOR_FIELD
			return fmt.Errorf("field doesn't have a default value: %s", mysqlErr.Message)
		case 1146: // ER_NO_SUCH_TABLE
			return fmt.Errorf("table doesn't exist: %s", mysqlErr.Message)
		case 1054: // ER_BAD_FIELD_ERROR
			return fmt.Errorf("unknown column: %s", mysqlErr.Message)
		case 1044: // ER_DBACCESS_DENIED_ERROR
			return fmt.Errorf("access denied to database: %s", mysqlErr.Message)
		case 1045: // ER_ACCESS_DENIED_ERROR
			return fmt.Errorf("access denied: %s", mysqlErr.Message)
		default:
			return fmt.Errorf("mysql error %d: %s", mysqlErr.Number, mysqlErr.Message)
		}
	}

	return err
}

// Override error-prone methods to use MySQL error handling
func (db *mysqlDatabase) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := db.sqlDatabase.Exec(ctx, query, args...)
	return result, db.handleMySQLError(err)
}

func (db *mysqlDatabase) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	rows, err := db.sqlDatabase.Query(ctx, query, args...)
	return rows, db.handleMySQLError(err)
}

func (db *mysqlDatabase) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// Note: QueryRow doesn't return an error directly, but the error is returned on Scan
	return db.sqlDatabase.QueryRow(ctx, query, args...)
}

// quoteName quotes an identifier for MySQL
func (db *mysqlDatabase) quoteName(name string) string {
	return fmt.Sprintf("`%s`", name)
}

// init function to override the MySQL constructor
func init() {
	NewMySQLDatabase = func(config SQLConfig) (SQLDatabase, error) {
		return newMySQLDatabase(config)
	}
}
