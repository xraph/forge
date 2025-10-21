package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/logger"
)

// MongoDatabase wraps MongoDB client
type MongoDatabase struct {
	name   string
	config DatabaseConfig

	// Native access
	client   *mongo.Client
	database *mongo.Database

	logger  forge.Logger
	metrics forge.Metrics
}

// NewMongoDatabase creates a new MongoDB database instance
func NewMongoDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (*MongoDatabase, error) {
	return &MongoDatabase{
		name:    config.Name,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Open establishes the MongoDB connection
func (d *MongoDatabase) Open(ctx context.Context) error {
	// Parse connection string
	clientOptions := options.Client().ApplyURI(d.config.DSN)

	// Configure connection pool
	if d.config.MaxOpenConns > 0 {
		maxPoolSize := uint64(d.config.MaxOpenConns)
		clientOptions.SetMaxPoolSize(maxPoolSize)
	}

	if d.config.MaxIdleConns > 0 {
		minPoolSize := uint64(d.config.MaxIdleConns)
		clientOptions.SetMinPoolSize(minPoolSize)
	}

	if d.config.ConnMaxIdleTime > 0 {
		clientOptions.SetMaxConnIdleTime(d.config.ConnMaxIdleTime)
	}

	// Add command monitor for observability
	clientOptions.SetMonitor(d.commandMonitor())

	// Connect
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Verify connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	d.client = client

	// Get database name from config or URI
	dbName := d.getDatabaseName()
	d.database = client.Database(dbName)

	d.logger.Info("mongodb connected",
		logger.String("name", d.name),
		logger.String("database", dbName),
	)

	return nil
}

// Close closes the MongoDB connection
func (d *MongoDatabase) Close(ctx context.Context) error {
	if d.client != nil {
		return d.client.Disconnect(ctx)
	}
	return nil
}

// Ping checks MongoDB connectivity
func (d *MongoDatabase) Ping(ctx context.Context) error {
	if d.client == nil {
		return fmt.Errorf("database not opened")
	}
	return d.client.Ping(ctx, nil)
}

// Name returns the database name
func (d *MongoDatabase) Name() string {
	return d.name
}

// Type returns the database type
func (d *MongoDatabase) Type() DatabaseType {
	return TypeMongoDB
}

// Driver returns the *mongo.Client for native driver access
func (d *MongoDatabase) Driver() interface{} {
	return d.client
}

// Client returns the MongoDB client
func (d *MongoDatabase) Client() *mongo.Client {
	return d.client
}

// Database returns the MongoDB database
func (d *MongoDatabase) Database() *mongo.Database {
	return d.database
}

// Collection returns a MongoDB collection
func (d *MongoDatabase) Collection(name string) *mongo.Collection {
	return d.database.Collection(name)
}

// Health returns the health status
func (d *MongoDatabase) Health(ctx context.Context) HealthStatus {
	start := time.Now()

	status := HealthStatus{
		CheckedAt: time.Now(),
	}

	if err := d.Ping(ctx); err != nil {
		status.Healthy = false
		status.Message = err.Error()
		return status
	}

	status.Healthy = true
	status.Message = "ok"
	status.Latency = time.Since(start)

	return status
}

// Stats returns MongoDB statistics
func (d *MongoDatabase) Stats() DatabaseStats {
	// MongoDB doesn't expose pool stats directly
	// Return empty stats for now
	return DatabaseStats{}
}

// Transaction executes a function in a MongoDB transaction
func (d *MongoDatabase) Transaction(ctx context.Context, fn func(sessCtx mongo.SessionContext) error) error {
	session, err := d.client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessCtx)
	})

	return err
}

// TransactionWithOptions executes a function in a MongoDB transaction with options
func (d *MongoDatabase) TransactionWithOptions(ctx context.Context, opts *options.TransactionOptions, fn func(sessCtx mongo.SessionContext) error) error {
	session, err := d.client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessCtx)
	}, opts)

	return err
}

// Helper: Get database name from config or DSN
func (d *MongoDatabase) getDatabaseName() string {
	if dbName, ok := d.config.Config["database"].(string); ok {
		return dbName
	}

	// Parse from URI
	uri := d.config.DSN
	if idx := strings.LastIndex(uri, "/"); idx != -1 {
		dbName := uri[idx+1:]
		if idx := strings.Index(dbName, "?"); idx != -1 {
			dbName = dbName[:idx]
		}
		if dbName != "" {
			return dbName
		}
	}

	return "default"
}

// Helper: Command monitor for observability
func (d *MongoDatabase) commandMonitor() *event.CommandMonitor {
	return &event.CommandMonitor{
		Started: func(ctx context.Context, evt *event.CommandStartedEvent) {
			// Log command start if needed
		},
		Succeeded: func(ctx context.Context, evt *event.CommandSucceededEvent) {
			duration := time.Duration(evt.DurationNanos)

			// Log slow commands
			if duration > 100*time.Millisecond {
				d.logger.Warn("slow mongodb command",
					logger.String("db", d.name),
					logger.String("command", evt.CommandName),
					logger.Duration("duration", duration),
				)
			}

			// Record metrics
			if d.metrics != nil {
				d.metrics.Histogram("db_command_duration",
					"db", d.name,
					"command", evt.CommandName,
				).Observe(duration.Seconds())
			}
		},
		Failed: func(ctx context.Context, evt *event.CommandFailedEvent) {
			if d.metrics != nil {
				d.metrics.Counter("db_command_errors",
					"db", d.name,
					"command", evt.CommandName,
				).Inc()
			}

			d.logger.Error("mongodb command failed",
				logger.String("db", d.name),
				logger.String("command", evt.CommandName),
				logger.Error(evt.Failure),
			)
		},
	}
}
