package database

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/go-utils/metrics"
)

// MongoDatabase wraps MongoDB client.
type MongoDatabase struct {
	name   string
	config DatabaseConfig

	// Native access
	client   *mongo.Client
	database *mongo.Database

	// Connection state
	state atomic.Int32

	logger  forge.Logger
	metrics forge.Metrics
}

// NewMongoDatabase creates a new MongoDB database instance.
func NewMongoDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (*MongoDatabase, error) {
	db := &MongoDatabase{
		name:    config.Name,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
	db.state.Store(int32(StateDisconnected))

	return db, nil
}

// Open establishes the MongoDB connection with retry logic.
func (d *MongoDatabase) Open(ctx context.Context) error {
	d.state.Store(int32(StateConnecting))

	var lastErr error

	for attempt := 0; attempt <= d.config.MaxRetries; attempt++ {
		if attempt > 0 {
			d.state.Store(int32(StateReconnecting))
			// Apply exponential backoff
			delay := min(d.config.RetryDelay*time.Duration(1<<uint(attempt-1)), 30*time.Second)

			d.logger.Info("retrying mongodb connection",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Int("max_attempts", d.config.MaxRetries+1),
				logger.Duration("delay", delay),
			)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				d.state.Store(int32(StateError))

				return ctx.Err()
			}
		}

		if err := d.openAttempt(ctx); err != nil {
			lastErr = err
			d.logger.Warn("mongodb connection attempt failed",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Error(err),
			)

			continue
		}

		d.state.Store(int32(StateConnected))
		dbName := d.getDatabaseName()
		d.logger.Info("mongodb connected",
			logger.String("name", d.name),
			logger.String("database", dbName),
			logger.String("dsn", MaskDSN(d.config.DSN, TypeMongoDB)),
			logger.Int("attempts", attempt+1),
		)

		return nil
	}

	d.state.Store(int32(StateError))

	return ErrConnectionFailed(d.name, TypeMongoDB, fmt.Errorf("failed after %d attempts: %w", d.config.MaxRetries+1, lastErr))
}

// openAttempt performs a single connection attempt.
func (d *MongoDatabase) openAttempt(ctx context.Context) error {
	// Add timeout for connection
	connectCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		connectCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

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
	client, err := mongo.Connect(connectCtx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Verify connection
	if err := client.Ping(connectCtx, nil); err != nil {
		client.Disconnect(ctx)

		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	d.client = client

	// Get database name from config or URI
	dbName := d.getDatabaseName()
	d.database = client.Database(dbName)

	return nil
}

// Close closes the MongoDB connection.
func (d *MongoDatabase) Close(ctx context.Context) error {
	if d.client != nil {
		err := d.client.Disconnect(ctx)
		if err != nil {
			d.state.Store(int32(StateError))

			return ErrConnectionFailed(d.name, TypeMongoDB, fmt.Errorf("failed to close: %w", err))
		}

		d.state.Store(int32(StateDisconnected))
		d.logger.Info("mongodb closed", logger.String("name", d.name))

		return nil
	}

	return nil
}

// Ping checks MongoDB connectivity.
func (d *MongoDatabase) Ping(ctx context.Context) error {
	if d.client == nil {
		return ErrDatabaseNotOpened(d.name)
	}

	// Add timeout if configured
	pingCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		pingCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

	return d.client.Ping(pingCtx, nil)
}

// IsOpen returns whether the database is connected.
func (d *MongoDatabase) IsOpen() bool {
	return d.State() == StateConnected
}

// State returns the current connection state.
func (d *MongoDatabase) State() ConnectionState {
	return ConnectionState(d.state.Load())
}

// Name returns the database name.
func (d *MongoDatabase) Name() string {
	return d.name
}

// Type returns the database type.
func (d *MongoDatabase) Type() DatabaseType {
	return TypeMongoDB
}

// Driver returns the *mongo.Client for native driver access.
func (d *MongoDatabase) Driver() any {
	return d.client
}

// Client returns the MongoDB client.
func (d *MongoDatabase) Client() *mongo.Client {
	return d.client
}

// Database returns the MongoDB database.
func (d *MongoDatabase) Database() *mongo.Database {
	return d.database
}

// Collection returns a MongoDB collection.
func (d *MongoDatabase) Collection(name string) *mongo.Collection {
	return d.database.Collection(name)
}

// Health returns the health status.
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

// Stats returns MongoDB statistics.
func (d *MongoDatabase) Stats() DatabaseStats {
	if d.client == nil || d.database == nil {
		return DatabaseStats{}
	}

	// Get server status for connection stats
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result struct {
		Connections struct {
			Current      int32 `bson:"current"`
			Available    int32 `bson:"available"`
			TotalCreated int32 `bson:"totalCreated"`
		} `bson:"connections"`
	}

	err := d.database.RunCommand(ctx, map[string]any{"serverStatus": 1}).Decode(&result)
	if err != nil {
		// Return empty stats if we can't get server status
		return DatabaseStats{}
	}

	return DatabaseStats{
		OpenConnections: int(result.Connections.Current),
		InUse:           int(result.Connections.Current), // MongoDB doesn't distinguish
		Idle:            int(result.Connections.Available),
	}
}

// Transaction executes a function in a MongoDB transaction with panic recovery.
func (d *MongoDatabase) Transaction(ctx context.Context, fn func(sessCtx mongo.SessionContext) error) (err error) {
	session, err := d.client.StartSession()
	if err != nil {
		return ErrTransactionFailed(d.name, TypeMongoDB, fmt.Errorf("failed to start session: %w", err))
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		defer func() {
			if r := recover(); r != nil {
				err = ErrPanicRecovered(d.name, TypeMongoDB, r)
				d.logger.Error("panic recovered in transaction",
					logger.String("db", d.name),
					logger.Any("panic", r),
				)

				if d.metrics != nil {
					d.metrics.Counter("db_transaction_panics",
						metrics.WithLabel("db", d.name),
					).Inc()
				}
			}
		}()

		return nil, fn(sessCtx)
	})

	return err
}

// TransactionWithOptions executes a function in a MongoDB transaction with options and panic recovery.
func (d *MongoDatabase) TransactionWithOptions(ctx context.Context, opts *options.TransactionOptions, fn func(sessCtx mongo.SessionContext) error) (err error) {
	session, err := d.client.StartSession()
	if err != nil {
		return ErrTransactionFailed(d.name, TypeMongoDB, fmt.Errorf("failed to start session: %w", err))
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		defer func() {
			if r := recover(); r != nil {
				err = ErrPanicRecovered(d.name, TypeMongoDB, r)
				d.logger.Error("panic recovered in transaction",
					logger.String("db", d.name),
					logger.Any("panic", r),
				)

				if d.metrics != nil {
					d.metrics.Counter("db_transaction_panics",
						metrics.WithLabel("db", d.name),
					).Inc()
				}
			}
		}()

		return nil, fn(sessCtx)
	}, opts)

	return err
}

// Helper: Get database name from config or DSN.
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

// Helper: Command monitor for observability.
func (d *MongoDatabase) commandMonitor() *event.CommandMonitor {
	return &event.CommandMonitor{
		Started: func(ctx context.Context, evt *event.CommandStartedEvent) {
			// Log command start if needed (can enable for debugging)
		},
		Succeeded: func(ctx context.Context, evt *event.CommandSucceededEvent) {
			duration := time.Duration(evt.DurationNanos)

			// Log slow commands using configurable threshold
			if duration > d.config.SlowQueryThreshold {
				d.logger.Warn("slow mongodb command",
					logger.String("db", d.name),
					logger.String("command", evt.CommandName),
					logger.Duration("duration", duration),
					logger.Duration("threshold", d.config.SlowQueryThreshold),
				)
			}

			// Record metrics
			if d.metrics != nil {
				d.metrics.Histogram("db_command_duration",
					metrics.WithLabel("db", d.name),
					metrics.WithLabel("command", evt.CommandName),
				).Observe(duration.Seconds())
			}
		},
		Failed: func(ctx context.Context, evt *event.CommandFailedEvent) {
			if d.metrics != nil {
				d.metrics.Counter("db_command_errors",
					metrics.WithLabel("db", d.name),
					metrics.WithLabel("command", evt.CommandName),
				).Inc()
			}

			d.logger.Error("mongodb command failed",
				logger.String("db", d.name),
				logger.String("command", evt.CommandName),
				logger.String("failure", evt.Failure),
			)
		},
	}
}
