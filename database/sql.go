package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// sqlDatabase implements SQLDatabase interface
type sqlDatabase struct {
	db     *sqlx.DB
	config SQLConfig
	driver string

	// Connection stats
	connected bool
	stats     map[string]interface{}
	mu        sync.RWMutex

	// Migration table
	migrationTable string
}

// transaction implements Transaction interface
type transaction struct {
	tx     *sqlx.Tx
	db     *sqlDatabase
	closed bool
	mu     sync.RWMutex
}

// NewSQLDatabase creates a new SQL database connection
func newSQLDatabase(config SQLConfig) (SQLDatabase, error) {
	db := &sqlDatabase{
		config:         config,
		driver:         config.Driver,
		stats:          make(map[string]interface{}),
		migrationTable: "schema_migrations",
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return db, nil
}

// Connect establishes database connection
func (db *sqlDatabase) Connect(ctx context.Context) error {
	var dsn string
	var err error

	// Build DSN based on driver
	if db.config.DSN != "" {
		dsn = db.config.DSN
	} else {
		dsn, err = db.buildDSN()
		if err != nil {
			return fmt.Errorf("failed to build DSN: %w", err)
		}
	}

	// Open database connection
	db.db, err = sqlx.Open(db.driver, dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	if db.config.MaxOpenConns > 0 {
		db.db.SetMaxOpenConns(db.config.MaxOpenConns)
	} else {
		db.db.SetMaxOpenConns(25) // Default
	}

	if db.config.MaxIdleConns > 0 {
		db.db.SetMaxIdleConns(db.config.MaxIdleConns)
	} else {
		db.db.SetMaxIdleConns(10) // Default
	}

	if db.config.ConnMaxLifetime > 0 {
		db.db.SetConnMaxLifetime(db.config.ConnMaxLifetime)
	} else {
		db.db.SetConnMaxLifetime(5 * time.Minute) // Default
	}

	if db.config.ConnMaxIdleTime > 0 {
		db.db.SetConnMaxIdleTime(db.config.ConnMaxIdleTime)
	} else {
		db.db.SetConnMaxIdleTime(5 * time.Minute) // Default
	}

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize migration table if auto-migrate is enabled
	if db.config.AutoMigrate {
		if err := db.ensureMigrationTable(ctx); err != nil {
			return fmt.Errorf("failed to create migration table: %w", err)
		}
	}

	db.mu.Lock()
	db.connected = true
	db.mu.Unlock()

	return nil
}

// Close closes database connection
func (db *sqlDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db != nil {
		err := db.db.Close()
		db.connected = false
		return err
	}
	return nil
}

// Ping tests database connection
func (db *sqlDatabase) Ping(ctx context.Context) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}
	return db.db.PingContext(ctx)
}

// IsConnected returns connection status
func (db *sqlDatabase) IsConnected() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.connected
}

// Stats returns database statistics
func (db *sqlDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	if db.db != nil {
		dbStats := db.db.Stats()
		stats["max_open_connections"] = dbStats.MaxOpenConnections
		stats["open_connections"] = dbStats.OpenConnections
		stats["in_use"] = dbStats.InUse
		stats["idle"] = dbStats.Idle
		stats["wait_count"] = dbStats.WaitCount
		stats["wait_duration"] = dbStats.WaitDuration
		stats["max_idle_closed"] = dbStats.MaxIdleClosed
		stats["max_idle_time_closed"] = dbStats.MaxIdleTimeClosed
		stats["max_lifetime_closed"] = dbStats.MaxLifetimeClosed
	}

	return stats
}

// Query executes a query
func (db *sqlDatabase) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return db.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query returning a single row
func (db *sqlDatabase) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if db.db == nil {
		return nil
	}
	return db.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a query without returning rows
func (db *sqlDatabase) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return db.db.ExecContext(ctx, query, args...)
}

// Prepare creates a prepared statement
func (db *sqlDatabase) Prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return db.db.PrepareContext(ctx, query)
}

// Begin starts a transaction
func (db *sqlDatabase) Begin(ctx context.Context) (Transaction, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	tx, err := db.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &transaction{tx: tx, db: db}, nil
}

// BeginTx starts a transaction with options
func (db *sqlDatabase) BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	tx, err := db.db.BeginTxx(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &transaction{tx: tx, db: db}, nil
}

// WithTransaction executes a function within a transaction
func (db *sqlDatabase) WithTransaction(ctx context.Context, fn func(Transaction) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction error: %v, rollback error: %v", err, rbErr)
		}
		return err
	}

	return tx.Commit()
}

// Select executes a query and scans results into destination
func (db *sqlDatabase) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}
	return db.db.SelectContext(ctx, dest, query, args...)
}

// Get executes a query and scans the result into destination
func (db *sqlDatabase) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}
	return db.db.GetContext(ctx, dest, query, args...)
}

// CreateTable creates a new table
func (db *sqlDatabase) CreateTable(ctx context.Context, tableName string, schema TableSchema) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := db.buildCreateTableQuery(tableName, schema)
	_, err := db.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Create indexes
	for _, index := range schema.Indexes {
		if err := db.createIndex(ctx, tableName, index); err != nil {
			return fmt.Errorf("failed to create index %s: %w", index.Name, err)
		}
	}

	return nil
}

// DropTable drops a table
func (db *sqlDatabase) DropTable(ctx context.Context, tableName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", db.quoteName(tableName))
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// TableExists checks if a table exists
func (db *sqlDatabase) TableExists(ctx context.Context, tableName string) (bool, error) {
	if db.db == nil {
		return false, fmt.Errorf("database not connected")
	}

	var query string
	switch db.driver {
	case "postgres", "pgx":
		query = "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)"
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
	case "sqlite", "sqlite3":
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?"
	default:
		return false, fmt.Errorf("unsupported driver: %s", db.driver)
	}

	var exists bool
	err := db.db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// GetMigrationVersion returns the current migration version
func (db *sqlDatabase) GetMigrationVersion(ctx context.Context) (int, error) {
	if db.db == nil {
		return 0, fmt.Errorf("database not connected")
	}

	exists, err := db.TableExists(ctx, db.migrationTable)
	if err != nil {
		return 0, err
	}

	if !exists {
		return 0, nil
	}

	query := fmt.Sprintf("SELECT MAX(version) FROM %s", db.quoteName(db.migrationTable))
	var version sql.NullInt64
	err = db.db.QueryRowContext(ctx, query).Scan(&version)
	if err != nil {
		return 0, err
	}

	if version.Valid {
		return int(version.Int64), nil
	}

	return 0, nil
}

// SetMigrationVersion sets the migration version
func (db *sqlDatabase) SetMigrationVersion(ctx context.Context, version int) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("INSERT INTO %s (version, applied_at) VALUES (?, ?)", db.quoteName(db.migrationTable))
	_, err := db.db.ExecContext(ctx, query, version, time.Now())
	return err
}

// Connection pool methods
func (db *sqlDatabase) SetMaxOpenConns(n int) {
	if db.db != nil {
		db.db.SetMaxOpenConns(n)
	}
}

func (db *sqlDatabase) SetMaxIdleConns(n int) {
	if db.db != nil {
		db.db.SetMaxIdleConns(n)
	}
}

func (db *sqlDatabase) SetConnMaxLifetime(d time.Duration) {
	if db.db != nil {
		db.db.SetConnMaxLifetime(d)
	}
}

func (db *sqlDatabase) SetConnMaxIdleTime(d time.Duration) {
	if db.db != nil {
		db.db.SetConnMaxIdleTime(d)
	}
}

// Transaction implementation

// Query executes a query within transaction
func (tx *transaction) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return nil, fmt.Errorf("transaction is closed")
	}
	return tx.tx.QueryContext(ctx, query, args...)
}

// QueryRow executes a query returning a single row within transaction
func (tx *transaction) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return nil
	}
	return tx.tx.QueryRowContext(ctx, query, args...)
}

// Exec executes a query without returning rows within transaction
func (tx *transaction) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return nil, fmt.Errorf("transaction is closed")
	}
	return tx.tx.ExecContext(ctx, query, args...)
}

// Prepare creates a prepared statement within transaction
func (tx *transaction) Prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return nil, fmt.Errorf("transaction is closed")
	}
	return tx.tx.PrepareContext(ctx, query)
}

// Commit commits the transaction
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.closed {
		return fmt.Errorf("transaction is closed")
	}

	err := tx.tx.Commit()
	tx.closed = true
	return err
}

// Rollback rolls back the transaction
func (tx *transaction) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.closed {
		return fmt.Errorf("transaction is closed")
	}

	err := tx.tx.Rollback()
	tx.closed = true
	return err
}

// Select executes a query and scans results into destination within transaction
func (tx *transaction) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return fmt.Errorf("transaction is closed")
	}
	return tx.tx.SelectContext(ctx, dest, query, args...)
}

// Get executes a query and scans the result into destination within transaction
func (tx *transaction) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if tx.closed {
		return fmt.Errorf("transaction is closed")
	}
	return tx.tx.GetContext(ctx, dest, query, args...)
}

// Helper methods

// buildDSN builds a DSN string based on driver and configuration
func (db *sqlDatabase) buildDSN() (string, error) {
	switch db.driver {
	case "postgres", "pgx":
		return db.buildPostgresDSN(), nil
	case "mysql":
		return db.buildMySQLDSN(), nil
	case "sqlite", "sqlite3":
		return db.buildSQLiteDSN(), nil
	default:
		return "", fmt.Errorf("unsupported driver: %s", db.driver)
	}
}

// buildPostgresDSN builds PostgreSQL DSN
func (db *sqlDatabase) buildPostgresDSN() string {
	if db.config.Host == "" {
		db.config.Host = "localhost"
	}
	if db.config.Port == 0 {
		db.config.Port = 5432
	}

	var params []string
	if db.config.SSLMode != "" {
		params = append(params, "sslmode="+db.config.SSLMode)
	} else {
		params = append(params, "sslmode=disable")
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		db.config.Host, db.config.Port, db.config.Username, db.config.Password, db.config.Database)

	if len(params) > 0 {
		dsn += " " + strings.Join(params, " ")
	}

	return dsn
}

// buildMySQLDSN builds MySQL DSN
func (db *sqlDatabase) buildMySQLDSN() string {
	if db.config.Host == "" {
		db.config.Host = "localhost"
	}
	if db.config.Port == 0 {
		db.config.Port = 3306
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		db.config.Username, db.config.Password, db.config.Host, db.config.Port, db.config.Database)

	var params []string
	if db.config.SSLMode != "" {
		params = append(params, "tls="+db.config.SSLMode)
	}
	params = append(params, "parseTime=true")

	if len(params) > 0 {
		dsn += "?" + strings.Join(params, "&")
	}

	return dsn
}

// buildSQLiteDSN builds SQLite DSN
func (db *sqlDatabase) buildSQLiteDSN() string {
	if db.config.Database == "" {
		return ":memory:"
	}
	return db.config.Database
}

// buildCreateTableQuery builds CREATE TABLE query
func (db *sqlDatabase) buildCreateTableQuery(tableName string, schema TableSchema) string {
	var parts []string

	// Build column definitions
	for _, col := range schema.Columns {
		parts = append(parts, db.buildColumnDefinition(col))
	}

	// Add primary key
	if len(schema.PrimaryKey) > 0 {
		pk := strings.Join(schema.PrimaryKey, ", ")
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", pk))
	}

	// Add foreign keys
	for _, fk := range schema.ForeignKeys {
		fkDef := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
			fk.Name,
			strings.Join(fk.Columns, ", "),
			fk.ReferencedTable,
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
		constraintDef := fmt.Sprintf("CONSTRAINT %s %s", constraint.Name, constraint.Type)
		if constraint.Expression != "" {
			constraintDef += " " + constraint.Expression
		}
		parts = append(parts, constraintDef)
	}

	query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)",
		db.quoteName(tableName),
		strings.Join(parts, ",\n  "))

	return query
}

// buildColumnDefinition builds column definition
func (db *sqlDatabase) buildColumnDefinition(col ColumnDefinition) string {
	def := fmt.Sprintf("%s %s", db.quoteName(col.Name), col.Type)

	if col.Size > 0 && (strings.Contains(strings.ToUpper(col.Type), "VARCHAR") || strings.Contains(strings.ToUpper(col.Type), "CHAR")) {
		def += fmt.Sprintf("(%d)", col.Size)
	}

	if col.Precision > 0 && col.Scale >= 0 {
		def += fmt.Sprintf("(%d,%d)", col.Precision, col.Scale)
	}

	if col.NotNull {
		def += " NOT NULL"
	}

	if col.AutoIncrement {
		switch db.driver {
		case "postgres", "pgx":
			def = strings.Replace(def, col.Type, "SERIAL", 1)
		case "mysql":
			def += " AUTO_INCREMENT"
		case "sqlite", "sqlite3":
			def += " PRIMARY KEY AUTOINCREMENT"
		}
	}

	if col.DefaultValue != nil {
		switch v := col.DefaultValue.(type) {
		case string:
			def += fmt.Sprintf(" DEFAULT '%s'", v)
		default:
			def += fmt.Sprintf(" DEFAULT %v", v)
		}
	}

	if col.Comment != "" {
		switch db.driver {
		case "postgres", "pgx":
			// PostgreSQL comments are added separately
		case "mysql":
			def += fmt.Sprintf(" COMMENT '%s'", col.Comment)
		case "sqlite", "sqlite3":
			// SQLite doesn't support column comments
		}
	}

	return def
}

// quoteName quotes an identifier name
func (db *sqlDatabase) quoteName(name string) string {
	switch db.driver {
	case "postgres", "pgx":
		return fmt.Sprintf(`"%s"`, name)
	case "mysql":
		return fmt.Sprintf("`%s`", name)
	case "sqlite", "sqlite3":
		return fmt.Sprintf(`"%s"`, name)
	default:
		return name
	}
}

// createIndex creates an index
func (db *sqlDatabase) createIndex(ctx context.Context, tableName string, index Index) error {
	var unique string
	if index.Unique {
		unique = "UNIQUE "
	}

	var indexType string
	if index.Type != "" {
		switch db.driver {
		case "postgres", "pgx":
			indexType = fmt.Sprintf(" USING %s", index.Type)
		case "mysql":
			indexType = fmt.Sprintf(" USING %s", index.Type)
		case "sqlite", "sqlite3":
			// SQLite doesn't support index types in CREATE INDEX
		}
	}

	query := fmt.Sprintf("CREATE %sINDEX %s ON %s%s (%s)",
		unique,
		db.quoteName(index.Name),
		db.quoteName(tableName),
		indexType,
		strings.Join(index.Columns, ", "))

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// ensureMigrationTable creates the migration table if it doesn't exist
func (db *sqlDatabase) ensureMigrationTable(ctx context.Context) error {
	schema := TableSchema{
		Columns: []ColumnDefinition{
			{
				Name:    "version",
				Type:    "INTEGER",
				NotNull: true,
			},
			{
				Name:    "applied_at",
				Type:    "TIMESTAMP",
				NotNull: true,
			},
		},
		PrimaryKey: []string{"version"},
	}

	exists, err := db.TableExists(ctx, db.migrationTable)
	if err != nil {
		return err
	}

	if !exists {
		return db.CreateTable(ctx, db.migrationTable, schema)
	}

	return nil
}

// Helper function to check if a value is a pointer to a struct
func isStructPtr(v interface{}) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

// Helper function to check if a value is a slice of structs or struct pointers
func isStructSlice(v interface{}) bool {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Ptr {
		return false
	}

	elem := t.Elem()
	if elem.Kind() != reflect.Slice {
		return false
	}

	sliceElem := elem.Elem()
	return sliceElem.Kind() == reflect.Struct ||
		(sliceElem.Kind() == reflect.Ptr && sliceElem.Elem().Kind() == reflect.Struct)
}

// validateDestination validates that the destination is appropriate for the operation
func validateDestination(dest interface{}, expectSlice bool) error {
	if dest == nil {
		return fmt.Errorf("destination cannot be nil")
	}

	if expectSlice {
		if !isStructSlice(dest) {
			return fmt.Errorf("destination must be a pointer to a slice of structs for Select operations")
		}
	} else {
		if !isStructPtr(dest) {
			return fmt.Errorf("destination must be a pointer to a struct for Get operations")
		}
	}

	return nil
}

// init function to register the SQL database constructor
func init() {
	NewPostgresDatabase = func(config SQLConfig) (SQLDatabase, error) {
		config.Driver = "pgx"
		return newSQLDatabase(config)
	}

	NewMySQLDatabase = func(config SQLConfig) (SQLDatabase, error) {
		config.Driver = "mysql"
		return newSQLDatabase(config)
	}

	NewSQLiteDatabase = func(config SQLConfig) (SQLDatabase, error) {
		config.Driver = "sqlite3"
		return newSQLDatabase(config)
	}

	NewCockroachDatabase = func(config SQLConfig) (SQLDatabase, error) {
		config.Driver = "pgx"
		return newSQLDatabase(config)
	}
}
