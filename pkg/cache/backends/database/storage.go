package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
)

// CacheStorage defines the database storage interface for cache entries
type CacheStorage interface {
	// Initialize creates the necessary tables and indexes
	Initialize(ctx context.Context) error

	// Get retrieves a cache entry by key
	Get(ctx context.Context, key string) (*CacheEntry, error)

	// Set stores a cache entry
	Set(ctx context.Context, entry *CacheEntry) error

	// Delete removes a cache entry by key
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists
	Exists(ctx context.Context, key string) (bool, error)

	// Keys returns all keys matching a pattern
	Keys(ctx context.Context, pattern string) ([]string, error)

	// GetMulti retrieves multiple cache entries
	GetMulti(ctx context.Context, keys []string) (map[string]*CacheEntry, error)

	// SetMulti stores multiple cache entries
	SetMulti(ctx context.Context, entries []*CacheEntry) error

	// DeleteMulti removes multiple cache entries
	DeleteMulti(ctx context.Context, keys []string) error

	// DeletePattern removes entries matching a pattern
	DeletePattern(ctx context.Context, pattern string) error

	// Clean removes expired entries
	Clean(ctx context.Context) (int64, error)

	// Size returns the total number of entries
	Size(ctx context.Context) (int64, error)

	// Touch updates the TTL of an entry
	Touch(ctx context.Context, key string, ttl time.Duration) error

	// GetStats returns storage statistics
	GetStats(ctx context.Context) (*StorageStats, error)

	// Close closes the storage connection
	Close() error
}

// CacheEntry represents a cache entry stored in the database
type CacheEntry struct {
	Key         string                 `db:"key" json:"key"`
	Value       []byte                 `db:"value" json:"value"`
	ExpiresAt   *time.Time             `db:"expires_at" json:"expires_at"`
	CreatedAt   time.Time              `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time              `db:"updated_at" json:"updated_at"`
	AccessedAt  time.Time              `db:"accessed_at" json:"accessed_at"`
	Tags        []string               `db:"tags" json:"tags"`
	Metadata    map[string]interface{} `db:"metadata" json:"metadata"`
	Size        int64                  `db:"size" json:"size"`
	AccessCount int64                  `db:"access_count" json:"access_count"`
	Namespace   string                 `db:"namespace" json:"namespace"`
}

// IsExpired checks if the cache entry has expired
func (e *CacheEntry) IsExpired() bool {
	if e.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*e.ExpiresAt)
}

// TTL returns the time to live for the entry
func (e *CacheEntry) TTL() time.Duration {
	if e.ExpiresAt == nil {
		return 0 // No expiration
	}

	remaining := e.ExpiresAt.Sub(time.Now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// StorageStats provides statistics about the cache storage
type StorageStats struct {
	TotalEntries   int64            `json:"total_entries"`
	ExpiredEntries int64            `json:"expired_entries"`
	TotalSize      int64            `json:"total_size"`
	AverageSize    float64          `json:"average_size"`
	OldestEntry    *time.Time       `json:"oldest_entry"`
	NewestEntry    *time.Time       `json:"newest_entry"`
	Namespaces     map[string]int64 `json:"namespaces"`
	TagCounts      map[string]int64 `json:"tag_counts"`
	AccessStats    *AccessStats     `json:"access_stats"`
	DatabaseInfo   *DatabaseInfo    `json:"database_info"`
}

// AccessStats provides access pattern statistics
type AccessStats struct {
	TotalAccesses  int64     `json:"total_accesses"`
	MostAccessed   []KeyStat `json:"most_accessed"`
	RecentAccesses []KeyStat `json:"recent_accesses"`
	LastAccessTime time.Time `json:"last_access_time"`
}

// KeyStat represents statistics for a specific key
type KeyStat struct {
	Key         string    `json:"key"`
	AccessCount int64     `json:"access_count"`
	LastAccess  time.Time `json:"last_access"`
	Size        int64     `json:"size"`
}

// DatabaseInfo provides information about the database
type DatabaseInfo struct {
	Driver      string `json:"driver"`
	Version     string `json:"version"`
	TableSize   int64  `json:"table_size"`
	IndexSize   int64  `json:"index_size"`
	Connections int    `json:"connections"`
}

// PostgresStorage implements CacheStorage for PostgreSQL
type PostgresStorage struct {
	db          *sql.DB
	tableName   string
	namespace   string
	cleanupStmt *sql.Stmt
	initialized bool
}

// NewPostgresStorage creates a new PostgreSQL cache storage
func NewPostgresStorage(db *sql.DB, tableName, namespace string) *PostgresStorage {
	if tableName == "" {
		tableName = "cache_entries"
	}

	return &PostgresStorage{
		db:        db,
		tableName: tableName,
		namespace: namespace,
	}
}

// Initialize creates the necessary tables and indexes
func (p *PostgresStorage) Initialize(ctx context.Context) error {
	if p.initialized {
		return nil
	}

	// Create table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			key VARCHAR(255) NOT NULL,
			namespace VARCHAR(100) NOT NULL DEFAULT '',
			value BYTEA NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			accessed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			tags TEXT[],
			metadata JSONB,
			size BIGINT NOT NULL DEFAULT 0,
			access_count BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (key, namespace)
		)`, p.tableName)

	if _, err := p.db.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_expires_at ON %s (expires_at) WHERE expires_at IS NOT NULL", p.tableName, p.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_namespace ON %s (namespace)", p.tableName, p.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_tags ON %s USING GIN (tags)", p.tableName, p.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_accessed_at ON %s (accessed_at)", p.tableName, p.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s (created_at)", p.tableName, p.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_metadata ON %s USING GIN (metadata)", p.tableName, p.tableName),
	}

	for _, indexSQL := range indexes {
		if _, err := p.db.ExecContext(ctx, indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Prepare cleanup statement
	cleanupSQL := fmt.Sprintf("DELETE FROM %s WHERE expires_at IS NOT NULL AND expires_at < NOW()", p.tableName)
	var err error
	p.cleanupStmt, err = p.db.Prepare(cleanupSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare cleanup statement: %w", err)
	}

	p.initialized = true
	return nil
}

// Get retrieves a cache entry by key
func (p *PostgresStorage) Get(ctx context.Context, key string) (*CacheEntry, error) {
	query := fmt.Sprintf(`
		SELECT key, value, expires_at, created_at, updated_at, accessed_at, tags, metadata, size, access_count
		FROM %s 
		WHERE key = $1 AND namespace = $2 
		AND (expires_at IS NULL OR expires_at > NOW())`, p.tableName)

	entry := &CacheEntry{Namespace: p.namespace}
	var tagsStr sql.NullString
	var metadataStr sql.NullString

	err := p.db.QueryRowContext(ctx, query, key, p.namespace).Scan(
		&entry.Key,
		&entry.Value,
		&entry.ExpiresAt,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&entry.AccessedAt,
		&tagsStr,
		&metadataStr,
		&entry.Size,
		&entry.AccessCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, cachecore.ErrCacheNotFound
		}
		return nil, fmt.Errorf("failed to get cache entry: %w", err)
	}

	// Parse tags
	if tagsStr.Valid && tagsStr.String != "" {
		tags := strings.Trim(tagsStr.String, "{}")
		if tags != "" {
			entry.Tags = strings.Split(tags, ",")
		}
	}

	// Parse metadata
	if metadataStr.Valid && metadataStr.String != "" {
		if err := json.Unmarshal([]byte(metadataStr.String), &entry.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	// Update access count and time
	go p.updateAccess(context.Background(), key)

	return entry, nil
}

// Set stores a cache entry
func (p *PostgresStorage) Set(ctx context.Context, entry *CacheEntry) error {
	if entry.Namespace == "" {
		entry.Namespace = p.namespace
	}

	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.AccessedAt = now
	entry.Size = int64(len(entry.Value))

	// Serialize metadata
	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("failed to serialize metadata: %w", err)
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (key, namespace, value, expires_at, created_at, updated_at, accessed_at, tags, metadata, size, access_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (key, namespace) 
		DO UPDATE SET 
			value = EXCLUDED.value,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at,
			accessed_at = EXCLUDED.accessed_at,
			tags = EXCLUDED.tags,
			metadata = EXCLUDED.metadata,
			size = EXCLUDED.size,
			access_count = %s.access_count + 1`, p.tableName, p.tableName)

	_, err := p.db.ExecContext(ctx, query,
		entry.Key,
		entry.Namespace,
		entry.Value,
		entry.ExpiresAt,
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.AccessedAt,
		entry.Tags,
		metadataJSON,
		entry.Size,
		entry.AccessCount,
	)

	if err != nil {
		return fmt.Errorf("failed to set cache entry: %w", err)
	}

	return nil
}

// Delete removes a cache entry by key
func (p *PostgresStorage) Delete(ctx context.Context, key string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE key = $1 AND namespace = $2", p.tableName)
	result, err := p.db.ExecContext(ctx, query, key, p.namespace)
	if err != nil {
		return fmt.Errorf("failed to delete cache entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return cachecore.ErrCacheNotFound
	}

	return nil
}

// Exists checks if a key exists
func (p *PostgresStorage) Exists(ctx context.Context, key string) (bool, error) {
	query := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s 
			WHERE key = $1 AND namespace = $2 
			AND (expires_at IS NULL OR expires_at > NOW())
		)`, p.tableName)

	var exists bool
	err := p.db.QueryRowContext(ctx, query, key, p.namespace).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return exists, nil
}

// Keys returns all keys matching a pattern
func (p *PostgresStorage) Keys(ctx context.Context, pattern string) ([]string, error) {
	var query string
	var args []interface{}

	if pattern == "*" {
		query = fmt.Sprintf(`
			SELECT key FROM %s 
			WHERE namespace = $1 
			AND (expires_at IS NULL OR expires_at > NOW())
			ORDER BY key`, p.tableName)
		args = []interface{}{p.namespace}
	} else {
		// Convert shell pattern to SQL LIKE pattern
		sqlPattern := strings.ReplaceAll(pattern, "*", "%")
		sqlPattern = strings.ReplaceAll(sqlPattern, "?", "_")

		query = fmt.Sprintf(`
			SELECT key FROM %s 
			WHERE namespace = $1 AND key LIKE $2 
			AND (expires_at IS NULL OR expires_at > NOW())
			ORDER BY key`, p.tableName)
		args = []interface{}{p.namespace, sqlPattern}
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate keys: %w", err)
	}

	return keys, nil
}

// GetMulti retrieves multiple cache entries
func (p *PostgresStorage) GetMulti(ctx context.Context, keys []string) (map[string]*CacheEntry, error) {
	if len(keys) == 0 {
		return make(map[string]*CacheEntry), nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(keys))
	args := make([]interface{}, len(keys)+1)
	args[0] = p.namespace

	for i, key := range keys {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = key
	}

	query := fmt.Sprintf(`
		SELECT key, value, expires_at, created_at, updated_at, accessed_at, tags, metadata, size, access_count
		FROM %s 
		WHERE namespace = $1 AND key IN (%s)
		AND (expires_at IS NULL OR expires_at > NOW())`,
		p.tableName, strings.Join(placeholders, ","))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get multiple entries: %w", err)
	}
	defer rows.Close()

	entries := make(map[string]*CacheEntry)
	for rows.Next() {
		entry := &CacheEntry{Namespace: p.namespace}
		var tagsStr sql.NullString
		var metadataStr sql.NullString

		err := rows.Scan(
			&entry.Key,
			&entry.Value,
			&entry.ExpiresAt,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.AccessedAt,
			&tagsStr,
			&metadataStr,
			&entry.Size,
			&entry.AccessCount,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan entry: %w", err)
		}

		// Parse tags and metadata (similar to Get method)
		if tagsStr.Valid && tagsStr.String != "" {
			tags := strings.Trim(tagsStr.String, "{}")
			if tags != "" {
				entry.Tags = strings.Split(tags, ",")
			}
		}

		if metadataStr.Valid && metadataStr.String != "" {
			if err := json.Unmarshal([]byte(metadataStr.String), &entry.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		entries[entry.Key] = entry
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate entries: %w", err)
	}

	return entries, nil
}

// SetMulti stores multiple cache entries
func (p *PostgresStorage) SetMulti(ctx context.Context, entries []*CacheEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, entry := range entries {
		if err := p.setInTx(ctx, tx, entry); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteMulti removes multiple cache entries
func (p *PostgresStorage) DeleteMulti(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	placeholders := make([]string, len(keys))
	args := make([]interface{}, len(keys)+1)
	args[0] = p.namespace

	for i, key := range keys {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = key
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE namespace = $1 AND key IN (%s)",
		p.tableName, strings.Join(placeholders, ","))

	_, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete multiple entries: %w", err)
	}

	return nil
}

// DeletePattern removes entries matching a pattern
func (p *PostgresStorage) DeletePattern(ctx context.Context, pattern string) error {
	var query string
	var args []interface{}

	if pattern == "*" {
		query = fmt.Sprintf("DELETE FROM %s WHERE namespace = $1", p.tableName)
		args = []interface{}{p.namespace}
	} else {
		sqlPattern := strings.ReplaceAll(pattern, "*", "%")
		sqlPattern = strings.ReplaceAll(sqlPattern, "?", "_")

		query = fmt.Sprintf("DELETE FROM %s WHERE namespace = $1 AND key LIKE $2", p.tableName)
		args = []interface{}{p.namespace, sqlPattern}
	}

	_, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete pattern: %w", err)
	}

	return nil
}

// Clean removes expired entries
func (p *PostgresStorage) Clean(ctx context.Context) (int64, error) {
	result, err := p.cleanupStmt.ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to clean expired entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get affected rows: %w", err)
	}

	return rowsAffected, nil
}

// Size returns the total number of entries
func (p *PostgresStorage) Size(ctx context.Context) (int64, error) {
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s 
		WHERE namespace = $1 
		AND (expires_at IS NULL OR expires_at > NOW())`, p.tableName)

	var count int64
	err := p.db.QueryRowContext(ctx, query, p.namespace).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get size: %w", err)
	}

	return count, nil
}

// Touch updates the TTL of an entry
func (p *PostgresStorage) Touch(ctx context.Context, key string, ttl time.Duration) error {
	var expiresAt *time.Time
	if ttl > 0 {
		expiry := time.Now().Add(ttl)
		expiresAt = &expiry
	}

	query := fmt.Sprintf(`
		UPDATE %s 
		SET expires_at = $1, accessed_at = NOW(), access_count = access_count + 1
		WHERE key = $2 AND namespace = $3 
		AND (expires_at IS NULL OR expires_at > NOW())`, p.tableName)

	result, err := p.db.ExecContext(ctx, query, expiresAt, key, p.namespace)
	if err != nil {
		return fmt.Errorf("failed to touch entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return cachecore.ErrCacheNotFound
	}

	return nil
}

// GetStats returns storage statistics
func (p *PostgresStorage) GetStats(ctx context.Context) (*StorageStats, error) {
	stats := &StorageStats{
		Namespaces: make(map[string]int64),
		TagCounts:  make(map[string]int64),
	}

	// Get basic counts
	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE expires_at IS NOT NULL AND expires_at < NOW()) as expired,
			SUM(size) as total_size,
			AVG(size) as avg_size,
			MIN(created_at) as oldest,
			MAX(created_at) as newest
		FROM %s WHERE namespace = $1`, p.tableName)

	var oldest, newest sql.NullTime
	err := p.db.QueryRowContext(ctx, query, p.namespace).Scan(
		&stats.TotalEntries,
		&stats.ExpiredEntries,
		&stats.TotalSize,
		&stats.AverageSize,
		&oldest,
		&newest,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get basic stats: %w", err)
	}

	if oldest.Valid {
		stats.OldestEntry = &oldest.Time
	}
	if newest.Valid {
		stats.NewestEntry = &newest.Time
	}

	// Get namespace counts
	namespaceQuery := fmt.Sprintf("SELECT namespace, COUNT(*) FROM %s GROUP BY namespace", p.tableName)
	rows, err := p.db.QueryContext(ctx, namespaceQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var namespace string
		var count int64
		if err := rows.Scan(&namespace, &count); err != nil {
			return nil, fmt.Errorf("failed to scan namespace stats: %w", err)
		}
		stats.Namespaces[namespace] = count
	}

	return stats, nil
}

// Close closes the storage connection
func (p *PostgresStorage) Close() error {
	if p.cleanupStmt != nil {
		p.cleanupStmt.Close()
	}
	return nil
}

// Helper methods

// updateAccess updates the access count and time for a key
func (p *PostgresStorage) updateAccess(ctx context.Context, key string) {
	query := fmt.Sprintf(`
		UPDATE %s 
		SET accessed_at = NOW(), access_count = access_count + 1
		WHERE key = $1 AND namespace = $2`, p.tableName)

	p.db.ExecContext(ctx, query, key, p.namespace)
}

// setInTx stores a cache entry within a transaction
func (p *PostgresStorage) setInTx(ctx context.Context, tx *sql.Tx, entry *CacheEntry) error {
	if entry.Namespace == "" {
		entry.Namespace = p.namespace
	}

	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.AccessedAt = now
	entry.Size = int64(len(entry.Value))

	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("failed to serialize metadata: %w", err)
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (key, namespace, value, expires_at, created_at, updated_at, accessed_at, tags, metadata, size, access_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (key, namespace) 
		DO UPDATE SET 
			value = EXCLUDED.value,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at,
			accessed_at = EXCLUDED.accessed_at,
			tags = EXCLUDED.tags,
			metadata = EXCLUDED.metadata,
			size = EXCLUDED.size,
			access_count = %s.access_count + 1`, p.tableName, p.tableName)

	_, err := tx.ExecContext(ctx, query,
		entry.Key,
		entry.Namespace,
		entry.Value,
		entry.ExpiresAt,
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.AccessedAt,
		entry.Tags,
		metadataJSON,
		entry.Size,
		entry.AccessCount,
	)

	return err
}
