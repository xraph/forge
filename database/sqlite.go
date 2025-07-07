package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// sqliteDatabase implements SQLDatabase interface for SQLite
type sqliteDatabase struct {
	*sqlDatabase
	filePath string
	options  map[string]string
}

// NewSQLiteDatabase creates a new SQLite database connection
func newSQLiteDatabase(config SQLConfig) (SQLDatabase, error) {
	// Force SQLite driver
	config.Driver = "sqlite3"

	// Create base SQL database
	base, err := newSQLDatabase(config)
	if err != nil {
		return nil, err
	}

	db := &sqliteDatabase{
		sqlDatabase: base.(*sqlDatabase),
		filePath:    config.DSN,
		options:     make(map[string]string),
	}

	// SQLite-specific initialization
	if err := db.initializeSQLite(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize SQLite: %w", err)
	}

	return db, nil
}

// initializeSQLite performs SQLite-specific initialization
func (db *sqliteDatabase) initializeSQLite(ctx context.Context) error {
	// Enable foreign keys
	if _, err := db.db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Set journal mode to WAL for better concurrency
	if _, err := db.db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set WAL mode: %v\n", err)
	}

	// Set synchronous mode to NORMAL for better performance
	if _, err := db.db.ExecContext(ctx, "PRAGMA synchronous = NORMAL"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set synchronous mode: %v\n", err)
	}

	// Set cache size to 64MB
	if _, err := db.db.ExecContext(ctx, "PRAGMA cache_size = -64000"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set cache size: %v\n", err)
	}

	// Set temp store to memory
	if _, err := db.db.ExecContext(ctx, "PRAGMA temp_store = MEMORY"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set temp store: %v\n", err)
	}

	// Set mmap size to 256MB
	if _, err := db.db.ExecContext(ctx, "PRAGMA mmap_size = 268435456"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set mmap size: %v\n", err)
	}

	return nil
}

// buildSQLiteDSN builds SQLite DSN with advanced options
func (db *sqliteDatabase) buildSQLiteDSN() string {
	dsn := db.config.DSN

	// If no DSN provided, use in-memory database
	if dsn == "" {
		return ":memory:"
	}

	// If it's a file path, ensure directory exists
	if dsn != ":memory:" {
		if err := db.ensureDatabaseDirectory(dsn); err != nil {
			fmt.Printf("Warning: failed to create database directory: %v\n", err)
		}
	}

	// Add connection options
	options := []string{
		"_foreign_keys=on",
		"_journal_mode=WAL",
		"_synchronous=NORMAL",
		"_cache_size=-64000",
		"_temp_store=MEMORY",
		"_mmap_size=268435456",
	}

	// Add custom options
	for key, value := range db.options {
		options = append(options, fmt.Sprintf("%s=%s", key, value))
	}

	if len(options) > 0 {
		dsn += "?" + strings.Join(options, "&")
	}

	return dsn
}

// ensureDatabaseDirectory creates the directory for the database file if it doesn't exist
func (db *sqliteDatabase) ensureDatabaseDirectory(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

// CreateTable creates a table with SQLite-specific features
func (db *sqliteDatabase) CreateTable(ctx context.Context, tableName string, schema TableSchema) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := db.buildSQLiteCreateTableQuery(tableName, schema)
	_, err := db.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Create indexes
	for _, index := range schema.Indexes {
		if err := db.createSQLiteIndex(ctx, tableName, index); err != nil {
			return fmt.Errorf("failed to create index %s: %w", index.Name, err)
		}
	}

	return nil
}

// buildSQLiteCreateTableQuery builds SQLite-specific CREATE TABLE query
func (db *sqliteDatabase) buildSQLiteCreateTableQuery(tableName string, schema TableSchema) string {
	var parts []string

	// Build column definitions
	for _, col := range schema.Columns {
		parts = append(parts, db.buildSQLiteColumnDefinition(col))
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

		fkDef := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
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
		switch constraint.Type {
		case "CHECK":
			parts = append(parts, "CHECK "+constraint.Expression)
		case "UNIQUE":
			if len(constraint.Columns) > 0 {
				columns := make([]string, len(constraint.Columns))
				for i, col := range constraint.Columns {
					columns[i] = db.quoteName(col)
				}
				parts = append(parts, "UNIQUE ("+strings.Join(columns, ", ")+")")
			}
		}
	}

	query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)",
		db.quoteName(tableName),
		strings.Join(parts, ",\n  "))

	return query
}

// buildSQLiteColumnDefinition builds SQLite-specific column definition
func (db *sqliteDatabase) buildSQLiteColumnDefinition(col ColumnDefinition) string {
	def := fmt.Sprintf("%s %s", db.quoteName(col.Name), db.mapSQLiteType(col.Type, col.Size, col.Precision, col.Scale))

	if col.NotNull {
		def += " NOT NULL"
	}

	if col.AutoIncrement {
		def += " PRIMARY KEY AUTOINCREMENT"
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

	return def
}

// mapSQLiteType maps generic types to SQLite-specific types
func (db *sqliteDatabase) mapSQLiteType(dataType string, size, precision, scale int) string {
	upperType := strings.ToUpper(dataType)

	switch upperType {
	case "VARCHAR", "CHAR":
		if size > 0 {
			return fmt.Sprintf("VARCHAR(%d)", size)
		}
		return "TEXT"
	case "TEXT":
		return "TEXT"
	case "INT", "INTEGER":
		return "INTEGER"
	case "BIGINT":
		return "INTEGER"
	case "SMALLINT":
		return "INTEGER"
	case "TINYINT":
		return "INTEGER"
	case "DECIMAL", "NUMERIC":
		if precision > 0 && scale >= 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
		}
		return "REAL"
	case "FLOAT", "DOUBLE":
		return "REAL"
	case "BOOLEAN", "BOOL":
		return "INTEGER"
	case "DATE":
		return "DATE"
	case "TIME":
		return "TIME"
	case "DATETIME", "TIMESTAMP":
		return "DATETIME"
	case "BLOB":
		return "BLOB"
	default:
		return "TEXT"
	}
}

// createSQLiteIndex creates a SQLite index
func (db *sqliteDatabase) createSQLiteIndex(ctx context.Context, tableName string, index Index) error {
	var unique string
	if index.Unique {
		unique = "UNIQUE "
	}

	// Quote column names
	columns := make([]string, len(index.Columns))
	for i, col := range index.Columns {
		columns[i] = db.quoteName(col)
	}

	query := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		unique,
		db.quoteName(index.Name),
		db.quoteName(tableName),
		strings.Join(columns, ", "))

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// TableExists checks if a table exists in SQLite
func (db *sqliteDatabase) TableExists(ctx context.Context, tableName string) (bool, error) {
	if db.db == nil {
		return false, fmt.Errorf("database not connected")
	}

	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"

	var count int
	err := db.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetTableSchema returns the schema of a table
func (db *sqliteDatabase) GetTableSchema(ctx context.Context, tableName string) (*TableSchema, error) {
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

	// Get columns using PRAGMA table_info
	rows, err := db.db.QueryContext(ctx, "PRAGMA table_info("+db.quoteName(tableName)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var col ColumnDefinition
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int

		err := rows.Scan(&cid, &col.Name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			return nil, err
		}

		col.Type = strings.ToUpper(dataType)
		col.NotNull = notNull == 1
		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}

		// Check if it's a primary key
		if pk > 0 {
			schema.PrimaryKey = append(schema.PrimaryKey, col.Name)
		}

		// Check for autoincrement (only for INTEGER PRIMARY KEY)
		if pk > 0 && strings.ToUpper(dataType) == "INTEGER" {
			col.AutoIncrement = true
		}

		schema.Columns = append(schema.Columns, col)
	}

	// Get foreign keys using PRAGMA foreign_key_list
	fkRows, err := db.db.QueryContext(ctx, "PRAGMA foreign_key_list("+db.quoteName(tableName)+")")
	if err != nil {
		return nil, err
	}
	defer fkRows.Close()

	fkMap := make(map[int]*ForeignKey)
	for fkRows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string

		err := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match)
		if err != nil {
			return nil, err
		}

		if fk, exists := fkMap[id]; exists {
			fk.Columns = append(fk.Columns, from)
			fk.ReferencedColumns = append(fk.ReferencedColumns, to)
		} else {
			fkMap[id] = &ForeignKey{
				Name:              fmt.Sprintf("fk_%s_%d", tableName, id),
				Columns:           []string{from},
				ReferencedTable:   table,
				ReferencedColumns: []string{to},
				OnUpdate:          onUpdate,
				OnDelete:          onDelete,
			}
		}
	}

	for _, fk := range fkMap {
		schema.ForeignKeys = append(schema.ForeignKeys, *fk)
	}

	// Get indexes using PRAGMA index_list
	indexRows, err := db.db.QueryContext(ctx, "PRAGMA index_list("+db.quoteName(tableName)+")")
	if err != nil {
		return nil, err
	}
	defer indexRows.Close()

	for indexRows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int

		err := indexRows.Scan(&seq, &name, &unique, &origin, &partial)
		if err != nil {
			return nil, err
		}

		// Skip auto-generated indexes
		if strings.HasPrefix(name, "sqlite_autoindex_") {
			continue
		}

		index := Index{
			Name:   name,
			Unique: unique == 1,
		}

		// Get index columns using PRAGMA index_info
		colRows, err := db.db.QueryContext(ctx, "PRAGMA index_info("+db.quoteName(name)+")")
		if err != nil {
			continue
		}

		for colRows.Next() {
			var seqno, cid int
			var colName string

			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				continue
			}

			index.Columns = append(index.Columns, colName)
		}
		colRows.Close()

		if len(index.Columns) > 0 {
			schema.Indexes = append(schema.Indexes, index)
		}
	}

	return schema, nil
}

// SQLite-specific utility functions

// Vacuum performs a vacuum operation
func (db *sqliteDatabase) Vacuum(ctx context.Context) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	_, err := db.db.ExecContext(ctx, "VACUUM")
	return err
}

// Analyze updates table statistics
func (db *sqliteDatabase) Analyze(ctx context.Context, tableName string) error {
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

// Reindex rebuilds indexes
func (db *sqliteDatabase) Reindex(ctx context.Context, indexName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := "REINDEX"
	if indexName != "" {
		query += " " + db.quoteName(indexName)
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// GetPragma gets a PRAGMA value
func (db *sqliteDatabase) GetPragma(ctx context.Context, pragma string) (string, error) {
	if db.db == nil {
		return "", fmt.Errorf("database not connected")
	}

	query := "PRAGMA " + pragma

	var value string
	err := db.db.QueryRowContext(ctx, query).Scan(&value)
	if err != nil {
		return "", err
	}

	return value, nil
}

// SetPragma sets a PRAGMA value
func (db *sqliteDatabase) SetPragma(ctx context.Context, pragma, value string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("PRAGMA %s = %s", pragma, value)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// GetDatabaseSize returns the size of the database file
func (db *sqliteDatabase) GetDatabaseSize(ctx context.Context) (int64, error) {
	if db.filePath == ":memory:" {
		return 0, fmt.Errorf("cannot get size of in-memory database")
	}

	if db.filePath == "" {
		return 0, fmt.Errorf("database file path not set")
	}

	info, err := os.Stat(db.filePath)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// GetTableList returns all tables in the database
func (db *sqliteDatabase) GetTableList(ctx context.Context) ([]string, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := db.db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
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

// GetConnectionInfo returns SQLite connection information
func (db *sqliteDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	info := make(map[string]interface{})

	// Get SQLite version
	version, err := db.GetPragma(ctx, "user_version")
	if err == nil {
		info["user_version"] = version
	}

	// Get schema version
	schemaVersion, err := db.GetPragma(ctx, "schema_version")
	if err == nil {
		info["schema_version"] = schemaVersion
	}

	// Get journal mode
	journalMode, err := db.GetPragma(ctx, "journal_mode")
	if err == nil {
		info["journal_mode"] = journalMode
	}

	// Get synchronous mode
	syncMode, err := db.GetPragma(ctx, "synchronous")
	if err == nil {
		info["synchronous"] = syncMode
	}

	// Get cache size
	cacheSize, err := db.GetPragma(ctx, "cache_size")
	if err == nil {
		info["cache_size"] = cacheSize
	}

	// Get page size
	pageSize, err := db.GetPragma(ctx, "page_size")
	if err == nil {
		info["page_size"] = pageSize
	}

	// Get page count
	pageCount, err := db.GetPragma(ctx, "page_count")
	if err == nil {
		info["page_count"] = pageCount
	}

	// Get database size
	if size, err := db.GetDatabaseSize(ctx); err == nil {
		info["file_size"] = size
	}

	info["file_path"] = db.filePath

	return info, nil
}

// Backup creates a backup of the database
func (db *sqliteDatabase) Backup(ctx context.Context, backupPath string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	if db.filePath == ":memory:" {
		return fmt.Errorf("cannot backup in-memory database")
	}

	// Ensure backup directory exists
	if err := db.ensureDatabaseDirectory(backupPath); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Use SQLite backup API via SQL command
	query := fmt.Sprintf("ATTACH DATABASE '%s' AS backup", backupPath)
	if _, err := db.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to attach backup database: %w", err)
	}

	defer func() {
		db.db.ExecContext(ctx, "DETACH DATABASE backup")
	}()

	// Get list of tables
	tables, err := db.GetTableList(ctx)
	if err != nil {
		return fmt.Errorf("failed to get table list: %w", err)
	}

	// Copy each table
	for _, table := range tables {
		// Get table schema
		var sql string
		err := db.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&sql)
		if err != nil {
			return fmt.Errorf("failed to get schema for table %s: %w", table, err)
		}

		// Create table in backup
		if _, err := db.db.ExecContext(ctx, strings.Replace(sql, "CREATE TABLE", "CREATE TABLE backup.", 1)); err != nil {
			return fmt.Errorf("failed to create table %s in backup: %w", table, err)
		}

		// Copy data
		copyQuery := fmt.Sprintf("INSERT INTO backup.%s SELECT * FROM %s", db.quoteName(table), db.quoteName(table))
		if _, err := db.db.ExecContext(ctx, copyQuery); err != nil {
			return fmt.Errorf("failed to copy data for table %s: %w", table, err)
		}
	}

	return nil
}

// quoteName quotes an identifier for SQLite
func (db *sqliteDatabase) quoteName(name string) string {
	return fmt.Sprintf(`"%s"`, name)
}

// init function to override the SQLite constructor
func init() {
	NewSQLiteDatabase = func(config SQLConfig) (SQLDatabase, error) {
		return newSQLiteDatabase(config)
	}
}
