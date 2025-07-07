package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

// cockroachDatabase implements SQLDatabase interface for CockroachDB
type cockroachDatabase struct {
	*sqlDatabase
	schema      string
	clusterInfo map[string]interface{}
}

// NewCockroachDatabase creates a new CockroachDB database connection
func newCockroachDatabase(config SQLConfig) (SQLDatabase, error) {
	// Force PostgreSQL-compatible driver
	config.Driver = "pgx"

	// Create base SQL database
	base, err := newSQLDatabase(config)
	if err != nil {
		return nil, err
	}

	db := &cockroachDatabase{
		sqlDatabase: base.(*sqlDatabase),
		schema:      "public",
		clusterInfo: make(map[string]interface{}),
	}

	// CockroachDB-specific initialization
	if err := db.initializeCockroachDB(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize CockroachDB: %w", err)
	}

	return db, nil
}

// initializeCockroachDB performs CockroachDB-specific initialization
func (db *cockroachDatabase) initializeCockroachDB(ctx context.Context) error {
	// Set search path
	if db.schema != "public" {
		query := fmt.Sprintf("SET search_path TO %s", db.quoteName(db.schema))
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to set search path: %w", err)
		}
	}

	// Set transaction isolation level to serializable (CockroachDB default)
	if _, err := db.db.ExecContext(ctx, "SET default_transaction_isolation = 'serializable'"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set transaction isolation: %v\n", err)
	}

	// Set statement timeout
	if _, err := db.db.ExecContext(ctx, "SET statement_timeout = '30s'"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to set statement timeout: %v\n", err)
	}

	// Get cluster information
	if err := db.loadClusterInfo(ctx); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to load cluster info: %v\n", err)
	}

	return nil
}

// loadClusterInfo loads CockroachDB cluster information
func (db *cockroachDatabase) loadClusterInfo(ctx context.Context) error {
	// Get cluster version
	var version string
	err := db.db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return err
	}
	db.clusterInfo["version"] = version

	// Get node info
	rows, err := db.db.QueryContext(ctx, "SELECT node_id, address, sql_address, build_tag FROM crdb_internal.gossip_nodes")
	if err != nil {
		return err
	}
	defer rows.Close()

	var nodes []map[string]interface{}
	for rows.Next() {
		var nodeID int
		var address, sqlAddress, buildTag string

		if err := rows.Scan(&nodeID, &address, &sqlAddress, &buildTag); err != nil {
			continue
		}

		node := map[string]interface{}{
			"node_id":     nodeID,
			"address":     address,
			"sql_address": sqlAddress,
			"build_tag":   buildTag,
		}
		nodes = append(nodes, node)
	}

	db.clusterInfo["nodes"] = nodes
	return nil
}

// buildCockroachDSN builds CockroachDB DSN with advanced options
func (db *cockroachDatabase) buildCockroachDSN() string {
	config := db.config

	// Set defaults
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 26257 // CockroachDB default port
	}

	// Build connection string
	var params []string

	// SSL mode (CockroachDB requires SSL by default)
	if config.SSLMode != "" {
		params = append(params, "sslmode="+config.SSLMode)
	} else {
		params = append(params, "sslmode=require")
	}

	// Connection timeout
	params = append(params, "connect_timeout=30")

	// Statement timeout
	params = append(params, "statement_timeout=30000")

	// Application name
	params = append(params, "application_name=forge")

	// CockroachDB specific options
	params = append(params, "default_transaction_isolation=serializable")

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database)

	if len(params) > 0 {
		dsn += " " + strings.Join(params, " ")
	}

	return dsn
}

// CreateTable creates a table with CockroachDB-specific features
func (db *cockroachDatabase) CreateTable(ctx context.Context, tableName string, schema TableSchema) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := db.buildCockroachCreateTableQuery(tableName, schema)
	_, err := db.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Create indexes
	for _, index := range schema.Indexes {
		if err := db.createCockroachIndex(ctx, tableName, index); err != nil {
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

// buildCockroachCreateTableQuery builds CockroachDB-specific CREATE TABLE query
func (db *cockroachDatabase) buildCockroachCreateTableQuery(tableName string, schema TableSchema) string {
	var parts []string

	// Build column definitions
	for _, col := range schema.Columns {
		parts = append(parts, db.buildCockroachColumnDefinition(col))
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

// buildCockroachColumnDefinition builds CockroachDB-specific column definition
func (db *cockroachDatabase) buildCockroachColumnDefinition(col ColumnDefinition) string {
	def := fmt.Sprintf("%s %s", db.quoteName(col.Name), db.mapCockroachType(col.Type, col.Size, col.Precision, col.Scale))

	if col.NotNull {
		def += " NOT NULL"
	}

	if col.AutoIncrement {
		// CockroachDB uses SERIAL or sequence-based auto-increment
		switch strings.ToUpper(col.Type) {
		case "BIGINT":
			def = fmt.Sprintf("%s SERIAL8", db.quoteName(col.Name))
		case "SMALLINT":
			def = fmt.Sprintf("%s SERIAL2", db.quoteName(col.Name))
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
			if v == "NOW()" || v == "CURRENT_TIMESTAMP" || v == "uuid_v4()" {
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

// mapCockroachType maps generic types to CockroachDB-specific types
func (db *cockroachDatabase) mapCockroachType(dataType string, size, precision, scale int) string {
	upperType := strings.ToUpper(dataType)

	switch upperType {
	case "VARCHAR":
		if size > 0 {
			return fmt.Sprintf("VARCHAR(%d)", size)
		}
		return "STRING"
	case "CHAR":
		if size > 0 {
			return fmt.Sprintf("CHAR(%d)", size)
		}
		return "CHAR(1)"
	case "TEXT":
		return "STRING"
	case "INT", "INTEGER":
		return "INT"
	case "BIGINT":
		return "INT8"
	case "SMALLINT":
		return "INT2"
	case "DECIMAL", "NUMERIC":
		if precision > 0 && scale >= 0 {
			return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
		}
		return "DECIMAL"
	case "FLOAT":
		return "FLOAT4"
	case "DOUBLE":
		return "FLOAT8"
	case "BOOLEAN", "BOOL":
		return "BOOL"
	case "DATE":
		return "DATE"
	case "TIME":
		return "TIME"
	case "DATETIME", "TIMESTAMP":
		return "TIMESTAMP"
	case "TIMESTAMPTZ":
		return "TIMESTAMPTZ"
	case "JSON":
		return "JSON"
	case "JSONB":
		return "JSONB"
	case "UUID":
		return "UUID"
	case "BYTEA", "BLOB":
		return "BYTES"
	case "ARRAY":
		return "ARRAY"
	case "INET":
		return "INET"
	default:
		return dataType
	}
}

// createCockroachIndex creates a CockroachDB index
func (db *cockroachDatabase) createCockroachIndex(ctx context.Context, tableName string, index Index) error {
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
func (db *cockroachDatabase) addColumnComment(ctx context.Context, tableName, columnName, comment string) error {
	query := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '%s'",
		db.quoteName(tableName),
		db.quoteName(columnName),
		strings.Replace(comment, "'", "''", -1))

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// TableExists checks if a table exists in CockroachDB
func (db *cockroachDatabase) TableExists(ctx context.Context, tableName string) (bool, error) {
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

// CockroachDB-specific utility functions

// ShowRanges shows table ranges (CockroachDB specific)
func (db *cockroachDatabase) ShowRanges(ctx context.Context, tableName string) ([]map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := "SHOW RANGES FROM TABLE " + db.quoteName(tableName)

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var ranges []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		rangeInfo := make(map[string]interface{})
		for i, col := range columns {
			rangeInfo[col] = values[i]
		}

		ranges = append(ranges, rangeInfo)
	}

	return ranges, nil
}

// ShowPartitions shows table partitions (CockroachDB specific)
func (db *cockroachDatabase) ShowPartitions(ctx context.Context, tableName string) ([]map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := "SHOW PARTITIONS FROM TABLE " + db.quoteName(tableName)

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var partitions []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		partitionInfo := make(map[string]interface{})
		for i, col := range columns {
			partitionInfo[col] = values[i]
		}

		partitions = append(partitions, partitionInfo)
	}

	return partitions, nil
}

// ShowJobs shows cluster jobs (CockroachDB specific)
func (db *cockroachDatabase) ShowJobs(ctx context.Context) ([]map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := "SHOW JOBS"

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var jobs []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		jobInfo := make(map[string]interface{})
		for i, col := range columns {
			jobInfo[col] = values[i]
		}

		jobs = append(jobs, jobInfo)
	}

	return jobs, nil
}

// GetClusterSettings gets cluster settings
func (db *cockroachDatabase) GetClusterSettings(ctx context.Context) (map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := "SHOW CLUSTER SETTINGS"

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]interface{})
	for rows.Next() {
		var variable, value, settingType, description string

		if err := rows.Scan(&variable, &value, &settingType, &description); err != nil {
			continue
		}

		settings[variable] = map[string]interface{}{
			"value":       value,
			"type":        settingType,
			"description": description,
		}
	}

	return settings, nil
}

// SetClusterSetting sets a cluster setting
func (db *cockroachDatabase) SetClusterSetting(ctx context.Context, setting, value string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	query := fmt.Sprintf("SET CLUSTER SETTING %s = %s", setting, value)
	_, err := db.db.ExecContext(ctx, query)
	return err
}

// GetNodeStatus gets node status information
func (db *cockroachDatabase) GetNodeStatus(ctx context.Context) ([]map[string]interface{}, error) {
	if db.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := "SELECT * FROM crdb_internal.node_runtime_info"

	rows, err := db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var nodeStatus []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		nodeInfo := make(map[string]interface{})
		for i, col := range columns {
			nodeInfo[col] = values[i]
		}

		nodeStatus = append(nodeStatus, nodeInfo)
	}

	return nodeStatus, nil
}

// GetConnectionInfo returns CockroachDB connection information
func (db *cockroachDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
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

	// Get session ID
	var sessionID string
	err = db.db.QueryRowContext(ctx, "SELECT session_id()").Scan(&sessionID)
	if err != nil {
		return nil, err
	}
	info["session_id"] = sessionID

	// Add cluster info
	for k, v := range db.clusterInfo {
		info[k] = v
	}

	return info, nil
}

// Backup creates a backup (CockroachDB specific)
func (db *cockroachDatabase) Backup(ctx context.Context, destination string, tables []string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	var query string
	if len(tables) > 0 {
		tableList := make([]string, len(tables))
		for i, table := range tables {
			tableList[i] = db.quoteName(table)
		}
		query = fmt.Sprintf("BACKUP TABLE %s TO '%s'", strings.Join(tableList, ", "), destination)
	} else {
		query = fmt.Sprintf("BACKUP DATABASE %s TO '%s'", db.quoteName(db.config.Database), destination)
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// Restore restores from a backup (CockroachDB specific)
func (db *cockroachDatabase) Restore(ctx context.Context, source string, newDBName string) error {
	if db.db == nil {
		return fmt.Errorf("database not connected")
	}

	var query string
	if newDBName != "" {
		query = fmt.Sprintf("RESTORE DATABASE %s FROM '%s' AS OF SYSTEM TIME '-1m'", db.quoteName(newDBName), source)
	} else {
		query = fmt.Sprintf("RESTORE DATABASE %s FROM '%s' AS OF SYSTEM TIME '-1m'", db.quoteName(db.config.Database), source)
	}

	_, err := db.db.ExecContext(ctx, query)
	return err
}

// Error handling for CockroachDB-specific errors
func (db *cockroachDatabase) handleCockroachError(err error) error {
	if err == nil {
		return nil
	}

	if pqErr, ok := err.(*pq.Error); ok {
		switch pqErr.Code {
		case "40001": // serialization_failure
			return fmt.Errorf("serialization failure (retry transaction): %s", pqErr.Message)
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
			return fmt.Errorf("cockroachdb error %s: %s", pqErr.Code, pqErr.Message)
		}
	}

	return err
}

// Override error-prone methods to use CockroachDB error handling
func (db *cockroachDatabase) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := db.sqlDatabase.Exec(ctx, query, args...)
	return result, db.handleCockroachError(err)
}

func (db *cockroachDatabase) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	rows, err := db.sqlDatabase.Query(ctx, query, args...)
	return rows, db.handleCockroachError(err)
}

func (db *cockroachDatabase) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// Note: QueryRow doesn't return an error directly, but the error is returned on Scan
	return db.sqlDatabase.QueryRow(ctx, query, args...)
}

// quoteName quotes an identifier name for CockroachDB
func (db *cockroachDatabase) quoteName(name string) string {
	return fmt.Sprintf(`"%s"`, name)
}

// init function to override the CockroachDB constructor
func init() {
	NewCockroachDatabase = func(config SQLConfig) (SQLDatabase, error) {
		return newCockroachDatabase(config)
	}
}
