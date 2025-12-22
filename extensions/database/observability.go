package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/xraph/forge"
)

// QueryPlan represents the execution plan for a query.
type QueryPlan struct {
	// Query is the SQL query that was analyzed
	Query string `json:"query"`

	// Plan is the formatted execution plan
	Plan string `json:"plan"`

	// Cost is the estimated query cost (database-specific)
	Cost float64 `json:"cost"`

	// Duration is how long the query took to execute
	Duration time.Duration `json:"duration"`

	// Database type (postgres, mysql, sqlite)
	DBType DatabaseType `json:"db_type"`
}

// ExplainQuery executes EXPLAIN on a query and returns the execution plan.
// This is useful for debugging slow queries and optimizing performance.
//
// Example:
//
//	query := db.NewSelect().Model((*User)(nil)).Where("email = ?", "test@example.com")
//	plan, err := database.ExplainQuery(ctx, db, query)
//	if err != nil {
//	    log.Error("failed to explain query", err)
//	}
//	log.Info("Query plan", "plan", plan.Plan, "cost", plan.Cost)
func ExplainQuery(ctx context.Context, db *bun.DB, query *bun.SelectQuery) (*QueryPlan, error) {
	// Get the database type from the dialect
	dbType := getDBTypeFromDialect(db)

	// Get the query string
	queryStr := query.String()

	// Build EXPLAIN query based on database type
	var explainQuery string
	switch dbType {
	case TypePostgres:
		explainQuery = fmt.Sprintf("EXPLAIN (FORMAT JSON, ANALYZE) %s", queryStr)
	case TypeMySQL:
		explainQuery = fmt.Sprintf("EXPLAIN FORMAT=JSON %s", queryStr)
	case TypeSQLite:
		explainQuery = fmt.Sprintf("EXPLAIN QUERY PLAN %s", queryStr)
	default:
		return nil, fmt.Errorf("EXPLAIN not supported for database type: %s", dbType)
	}

	start := time.Now()

	// Execute EXPLAIN query directly using raw SQL
	var rows *sql.Rows
	rows, err := db.DB.QueryContext(ctx, explainQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute EXPLAIN: %w", err)
	}
	defer rows.Close()

	duration := time.Since(start)

	// Parse the results based on database type
	plan, cost, err := parseExplainResults(rows, dbType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EXPLAIN results: %w", err)
	}

	return &QueryPlan{
		Query:    queryStr,
		Plan:     plan,
		Cost:     cost,
		Duration: duration,
		DBType:   dbType,
	}, nil
}

// parseExplainResults parses EXPLAIN output based on database type.
func parseExplainResults(rows *sql.Rows, dbType DatabaseType) (string, float64, error) {
	var plan string
	var cost float64

	switch dbType {
	case TypePostgres:
		// PostgreSQL returns JSON
		for rows.Next() {
			var jsonPlan string
			if err := rows.Scan(&jsonPlan); err != nil {
				return "", 0, err
			}
			plan += jsonPlan

			// Try to extract cost from JSON
			var planData []map[string]any
			if err := json.Unmarshal([]byte(jsonPlan), &planData); err == nil {
				if len(planData) > 0 {
					if planObj, ok := planData[0]["Plan"].(map[string]any); ok {
						if totalCost, ok := planObj["Total Cost"].(float64); ok {
							cost = totalCost
						}
					}
				}
			}
		}

	case TypeMySQL:
		// MySQL returns JSON
		for rows.Next() {
			var jsonPlan string
			if err := rows.Scan(&jsonPlan); err != nil {
				return "", 0, err
			}
			plan += jsonPlan

			// Try to extract cost from JSON
			var planData map[string]any
			if err := json.Unmarshal([]byte(jsonPlan), &planData); err == nil {
				if queryBlock, ok := planData["query_block"].(map[string]any); ok {
					if costInfo, ok := queryBlock["cost_info"].(map[string]any); ok {
						if queryCost, ok := costInfo["query_cost"].(string); ok {
							fmt.Sscanf(queryCost, "%f", &cost)
						}
					}
				}
			}
		}

	case TypeSQLite:
		// SQLite returns text rows
		var planLines []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				return "", 0, err
			}
			planLines = append(planLines, fmt.Sprintf("%d|%d|%d|%s", id, parent, notused, detail))
		}
		plan = strings.Join(planLines, "\n")
		// SQLite doesn't provide a cost estimate
		cost = 0
	}

	if err := rows.Err(); err != nil {
		return "", 0, err
	}

	return plan, cost, nil
}

// getDBTypeFromDialect determines the database type from the Bun dialect.
func getDBTypeFromDialect(db *bun.DB) DatabaseType {
	dialectName := fmt.Sprintf("%T", db.Dialect())
	if strings.Contains(dialectName, "pg") || strings.Contains(dialectName, "Pg") {
		return TypePostgres
	}
	if strings.Contains(dialectName, "mysql") || strings.Contains(dialectName, "MySQL") {
		return TypeMySQL
	}
	if strings.Contains(dialectName, "sqlite") || strings.Contains(dialectName, "SQLite") {
		return TypeSQLite
	}
	return TypeSQLite // Default fallback
}

// ObservabilityQueryHook is an enhanced query hook that can automatically EXPLAIN slow queries.
type ObservabilityQueryHook struct {
	logger                  forge.Logger
	metrics                 forge.Metrics
	dbName                  string
	dbType                  DatabaseType
	slowQueryThreshold      time.Duration
	disableSlowQueryLogging bool
	autoExplain             bool
	autoExplainThresh       time.Duration
}

// NewObservabilityQueryHook creates a new observability query hook.
func NewObservabilityQueryHook(logger forge.Logger, metrics forge.Metrics, dbName string, dbType DatabaseType, slowQueryThreshold time.Duration, disableSlowQueryLogging bool) *ObservabilityQueryHook {
	return &ObservabilityQueryHook{
		logger:                  logger,
		metrics:                 metrics,
		dbName:                  dbName,
		dbType:                  dbType,
		slowQueryThreshold:      slowQueryThreshold,
		disableSlowQueryLogging: disableSlowQueryLogging,
		autoExplain:             false,
		autoExplainThresh:       0,
	}
}

// WithAutoExplain enables automatic EXPLAIN for queries slower than the threshold.
func (h *ObservabilityQueryHook) WithAutoExplain(threshold time.Duration) *ObservabilityQueryHook {
	h.autoExplain = true
	h.autoExplainThresh = threshold
	return h
}

// BeforeQuery is called before query execution.
func (h *ObservabilityQueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery is called after query execution.
func (h *ObservabilityQueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	duration := time.Since(event.StartTime)

	// Log slow queries
	if !h.disableSlowQueryLogging && duration > h.slowQueryThreshold {
		h.logger.Warn("slow query detected",
			forge.F("db", h.dbName),
			forge.F("query", event.Query),
			forge.F("duration", duration.String()),
			forge.F("threshold", h.slowQueryThreshold.String()),
		)

		// Auto-EXPLAIN if enabled and query is slower than auto-explain threshold
		if h.autoExplain && duration > h.autoExplainThresh {
			h.explainSlowQuery(ctx, event)
		}
	}

	// Record metrics
	if h.metrics != nil {
		h.metrics.Histogram("db_query_duration",
			"db", h.dbName,
			"operation", event.Operation(),
		).Observe(duration.Seconds())

		if event.Err != nil {
			h.metrics.Counter("db_query_errors",
				"db", h.dbName,
				"operation", event.Operation(),
			).Inc()

			h.logger.Error("query error",
				forge.F("db", h.dbName),
				forge.F("query", event.Query),
				forge.F("error", event.Err.Error()),
			)
		}
	}
}

// explainSlowQuery attempts to EXPLAIN a slow query.
func (h *ObservabilityQueryHook) explainSlowQuery(ctx context.Context, event *bun.QueryEvent) {
	// Only EXPLAIN SELECT queries
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(event.Query)), "SELECT") {
		return
	}

	// Build EXPLAIN query based on database type
	var explainQuery string
	switch h.dbType {
	case TypePostgres:
		explainQuery = fmt.Sprintf("EXPLAIN (FORMAT JSON) %s", event.Query)
	case TypeMySQL:
		explainQuery = fmt.Sprintf("EXPLAIN FORMAT=JSON %s", event.Query)
	case TypeSQLite:
		explainQuery = fmt.Sprintf("EXPLAIN QUERY PLAN %s", event.Query)
	default:
		return
	}

	// Execute EXPLAIN in a new context with timeout
	explainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := event.DB.QueryContext(explainCtx, explainQuery)
	if err != nil {
		h.logger.Warn("failed to auto-explain slow query",
			forge.F("db", h.dbName),
			forge.F("error", err.Error()),
		)
		return
	}
	defer rows.Close()

	// Parse the plan
	plan, cost, err := parseExplainResults(rows, h.dbType)
	if err != nil {
		h.logger.Warn("failed to parse EXPLAIN results",
			forge.F("db", h.dbName),
			forge.F("error", err.Error()),
		)
		return
	}

	// Log the query plan
	h.logger.Warn("query plan for slow query",
		forge.F("db", h.dbName),
		forge.F("query", event.Query),
		forge.F("cost", cost),
		forge.F("plan", plan),
	)
}

// EnableAutoExplain is a convenience function to enable auto-EXPLAIN on a database.
// This should be called during database initialization.
//
// Example:
//
//	db := database.MustGetSQL(c)
//	database.EnableAutoExplain(db, 1*time.Second)
func EnableAutoExplain(db *bun.DB, threshold time.Duration) {
	// This is a placeholder - in reality, you'd need to replace the existing hook
	// with an ObservabilityQueryHook. This would be done during SQLDatabase creation.
	// For now, this function serves as documentation of the feature.
}

// QueryStats provides statistics about query execution.
type QueryStats struct {
	TotalQueries    int64         `json:"total_queries"`
	SlowQueries     int64         `json:"slow_queries"`
	FailedQueries   int64         `json:"failed_queries"`
	AverageDuration time.Duration `json:"average_duration"`
	MaxDuration     time.Duration `json:"max_duration"`
}

// FormatQueryPlan formats a query plan for human-readable output.
func FormatQueryPlan(plan *QueryPlan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Query: %s\n", plan.Query))
	sb.WriteString(fmt.Sprintf("Database: %s\n", plan.DBType))
	sb.WriteString(fmt.Sprintf("Cost: %.2f\n", plan.Cost))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", plan.Duration))
	sb.WriteString("Plan:\n")

	// Try to pretty-print JSON plans
	if plan.DBType == TypePostgres || plan.DBType == TypeMySQL {
		var prettyJSON map[string]any
		if err := json.Unmarshal([]byte(plan.Plan), &prettyJSON); err == nil {
			pretty, err := json.MarshalIndent(prettyJSON, "", "  ")
			if err == nil {
				sb.WriteString(string(pretty))
				return sb.String()
			}
		}
	}

	// Fallback to raw plan
	sb.WriteString(plan.Plan)

	return sb.String()
}
