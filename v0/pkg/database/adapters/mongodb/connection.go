package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// MongoConnection implements database.Connection for MongoDB
type MongoConnection struct {
	*database.BaseConnection
	client       *mongo.Client
	databaseName string
}

// NewMongoConnection creates a new MongoDB connection
func NewMongoConnection(name string, config *database.ConnectionConfig, client *mongo.Client, logger common.Logger, metrics common.Metrics) database.Connection {
	baseConn := database.NewBaseConnection(name, "mongodb", config, logger, metrics)

	conn := &MongoConnection{
		BaseConnection: baseConn,
		client:         client,
		databaseName:   config.Database,
	}

	// Set the underlying DB in base connection
	baseConn.SetDB(client)

	return conn
}

// Connect establishes the MongoDB connection
func (mc *MongoConnection) Connect(ctx context.Context) error {
	if mc.client == nil {
		return fmt.Errorf("MongoDB client is not initialized")
	}

	// Test the connection with a ping
	start := time.Now()
	if err := mc.client.Ping(ctx, readpref.Primary()); err != nil {
		mc.IncrementErrorCount()
		return fmt.Errorf("failed to ping MongoDB server: %w", err)
	}

	// Verify we can access the database
	if mc.databaseName != "" {
		db := mc.client.Database(mc.databaseName)
		if err := db.RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
			mc.IncrementErrorCount()
			return fmt.Errorf("failed to access MongoDB database %s: %w", mc.databaseName, err)
		}
	}

	// Mark as connected
	mc.SetConnected(true)

	// Record connection metrics
	duration := time.Since(start)
	if mc.Metrics() != nil {
		mc.Metrics().Counter("forge.database.mongodb.connections_created", "connection", mc.Name()).Inc()
		mc.Metrics().Histogram("forge.database.mongodb.connection_duration", "connection", mc.Name()).Observe(duration.Seconds())
	}

	mc.Logger().Info("MongoDB connection established",
		logger.String("connection", mc.Name()),
		logger.String("host", mc.Config().Host),
		logger.Int("port", mc.Config().Port),
		logger.String("database", mc.databaseName),
		logger.Duration("connection_time", duration),
	)

	return nil
}

// Close closes the MongoDB connection
func (mc *MongoConnection) Close(ctx context.Context) error {
	if mc.client == nil {
		return nil
	}

	mc.Logger().Info("closing MongoDB connection",
		logger.String("connection", mc.Name()),
	)

	start := time.Now()
	if err := mc.client.Disconnect(ctx); err != nil {
		mc.IncrementErrorCount()
		return fmt.Errorf("failed to close MongoDB connection: %w", err)
	}

	mc.SetConnected(false)

	// Record disconnection metrics
	duration := time.Since(start)
	if mc.Metrics() != nil {
		mc.Metrics().Counter("forge.database.mongodb.connections_closed", "connection", mc.Name()).Inc()
		mc.Metrics().Histogram("forge.database.mongodb.disconnection_duration", "connection", mc.Name()).Observe(duration.Seconds())
	}

	mc.Logger().Info("MongoDB connection closed",
		logger.String("connection", mc.Name()),
		logger.Duration("close_time", duration),
	)

	return nil
}

// Ping tests the MongoDB connection
func (mc *MongoConnection) Ping(ctx context.Context) error {
	if mc.client == nil {
		return fmt.Errorf("MongoDB client is not initialized")
	}

	if !mc.IsConnected() {
		return fmt.Errorf("MongoDB connection is not established")
	}

	start := time.Now()
	err := mc.client.Ping(ctx, readpref.Primary())
	duration := time.Since(start)

	if err != nil {
		mc.IncrementErrorCount()
		if mc.Metrics() != nil {
			mc.Metrics().Counter("forge.database.mongodb.ping_errors", "connection", mc.Name()).Inc()
		}
		return fmt.Errorf("MongoDB ping failed: %w", err)
	}

	// Record ping metrics
	if mc.Metrics() != nil {
		mc.Metrics().Counter("forge.database.mongodb.pings", "connection", mc.Name()).Inc()
		mc.Metrics().Histogram("forge.database.mongodb.ping_duration", "connection", mc.Name()).Observe(duration.Seconds())
	}

	return nil
}

// Transaction executes a function within a MongoDB transaction
func (mc *MongoConnection) Transaction(ctx context.Context, fn func(tx interface{}) error) error {
	if mc.client == nil {
		return fmt.Errorf("MongoDB client is not initialized")
	}

	if !mc.IsConnected() {
		return fmt.Errorf("MongoDB connection is not established")
	}

	start := time.Now()
	mc.IncrementTransactionCount()

	// Start a session for the transaction
	sess, err := mc.client.StartSession()
	if err != nil {
		mc.IncrementErrorCount()
		if mc.Metrics() != nil {
			mc.Metrics().Counter("forge.database.mongodb.transaction_errors", "connection", mc.Name()).Inc()
		}
		return fmt.Errorf("failed to start MongoDB session: %w", err)
	}
	defer sess.EndSession(ctx)

	// Execute the transaction
	txErr := mongo.WithSession(ctx, sess, func(sc mongo.SessionContext) error {
		_, err = sess.WithTransaction(sc, func(sc mongo.SessionContext) (interface{}, error) {
			// Create transaction wrapper
			tx := NewMongoTransaction(sc, sess, mc)

			// Execute the function with the transaction
			if err := fn(tx); err != nil {
				return nil, err
			}

			return nil, nil
		})
		return err
	})

	duration := time.Since(start)

	if txErr != nil {
		mc.IncrementErrorCount()
		if mc.Metrics() != nil {
			mc.Metrics().Counter("forge.database.mongodb.transaction_errors", "connection", mc.Name()).Inc()
		}
		return fmt.Errorf("failed to execute MongoDB transaction: %w", txErr)
	}

	// Record transaction metrics
	if mc.Metrics() != nil {
		mc.Metrics().Counter("forge.database.mongodb.transactions_success", "connection", mc.Name()).Inc()
		mc.Metrics().Histogram("forge.database.mongodb.transaction_duration", "connection", mc.Name()).Observe(duration.Seconds())
	}

	mc.Logger().Debug("MongoDB transaction completed",
		logger.String("connection", mc.Name()),
		logger.Duration("duration", duration),
	)

	return nil
}

// GetMongoClient returns the underlying MongoDB client
func (mc *MongoConnection) GetMongoClient() *mongo.Client {
	return mc.client
}

// GetDatabase returns the MongoDB database instance
func (mc *MongoConnection) GetDatabase() *mongo.Database {
	if mc.client == nil || mc.databaseName == "" {
		return nil
	}
	return mc.client.Database(mc.databaseName)
}

// GetDatabaseName returns the database name
func (mc *MongoConnection) GetDatabaseName() string {
	return mc.databaseName
}

// Stats returns enhanced MongoDB connection statistics
func (mc *MongoConnection) Stats() database.ConnectionStats {
	baseStats := mc.BaseConnection.Stats()

	// Add MongoDB-specific statistics if available
	// Note: MongoDB Go driver doesn't expose pool stats in the same way as SQL drivers
	// We can add more specific MongoDB metrics here if needed

	return baseStats
}

// MongoTransaction wraps the MongoDB session for transaction operations
type MongoTransaction struct {
	sessionContext mongo.SessionContext
	session        mongo.Session
	conn           *MongoConnection
}

// NewMongoTransaction creates a new MongoDB transaction wrapper
func NewMongoTransaction(sessionContext mongo.SessionContext, session mongo.Session, conn *MongoConnection) *MongoTransaction {
	return &MongoTransaction{
		sessionContext: sessionContext,
		session:        session,
		conn:           conn,
	}
}

// SessionContext returns the underlying MongoDB session context
func (mt *MongoTransaction) SessionContext() mongo.SessionContext {
	return mt.sessionContext
}

// Session returns the underlying MongoDB session
func (mt *MongoTransaction) Session() mongo.Session {
	return mt.session
}

// Database returns the database within the transaction context
func (mt *MongoTransaction) Database() *mongo.Database {
	return mt.conn.GetDatabase()
}

// Collection returns a collection within the transaction context
func (mt *MongoTransaction) Collection(name string) *mongo.Collection {
	db := mt.Database()
	if db == nil {
		return nil
	}
	return db.Collection(name)
}

// MongoOperations provides common MongoDB operations
type MongoOperations struct {
	client *mongo.Client
	db     *mongo.Database
	conn   *MongoConnection
}

// NewMongoOperations creates a new MongoDB operations helper
func NewMongoOperations(conn *MongoConnection) *MongoOperations {
	return &MongoOperations{
		client: conn.client,
		db:     conn.GetDatabase(),
		conn:   conn,
	}
}

// InsertOne inserts a single document
func (mo *MongoOperations) InsertOne(ctx context.Context, collection string, document interface{}) (*mongo.InsertOneResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.InsertOne(ctx, document)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "insertOne",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB InsertOne failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "insertOne",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "insertOne",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// InsertMany inserts multiple documents
func (mo *MongoOperations) InsertMany(ctx context.Context, collection string, documents []interface{}) (*mongo.InsertManyResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.InsertMany(ctx, documents)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "insertMany",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB InsertMany failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "insertMany",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "insertMany",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// FindOne finds a single document
func (mo *MongoOperations) FindOne(ctx context.Context, collection string, filter interface{}) *mongo.SingleResult {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result := coll.FindOne(ctx, filter)
	duration := time.Since(start)

	// Check if the operation had an error
	if result.Err() != nil && result.Err() != mongo.ErrNoDocuments {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "findOne",
				"collection", collection,
			).Inc()
		}
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "findOne",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "findOne",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result
}

// Find finds multiple documents
func (mo *MongoOperations) Find(ctx context.Context, collection string, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	cursor, err := coll.Find(ctx, filter, opts...)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "find",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB Find failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "find",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "find",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return cursor, nil
}

// UpdateOne updates a single document
func (mo *MongoOperations) UpdateOne(ctx context.Context, collection string, filter interface{}, update interface{}) (*mongo.UpdateResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.UpdateOne(ctx, filter, update)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "updateOne",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB UpdateOne failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "updateOne",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "updateOne",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// UpdateMany updates multiple documents
func (mo *MongoOperations) UpdateMany(ctx context.Context, collection string, filter interface{}, update interface{}) (*mongo.UpdateResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.UpdateMany(ctx, filter, update)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "updateMany",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB UpdateMany failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "updateMany",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "updateMany",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// DeleteOne deletes a single document
func (mo *MongoOperations) DeleteOne(ctx context.Context, collection string, filter interface{}) (*mongo.DeleteResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.DeleteOne(ctx, filter)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "deleteOne",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB DeleteOne failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "deleteOne",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "deleteOne",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// DeleteMany deletes multiple documents
func (mo *MongoOperations) DeleteMany(ctx context.Context, collection string, filter interface{}) (*mongo.DeleteResult, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.DeleteMany(ctx, filter)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "deleteMany",
				"collection", collection,
			).Inc()
		}
		return nil, fmt.Errorf("MongoDB DeleteMany failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "deleteMany",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "deleteMany",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return result, nil
}

// CountDocuments counts documents in a collection
func (mo *MongoOperations) CountDocuments(ctx context.Context, collection string, filter interface{}) (int64, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	count, err := coll.CountDocuments(ctx, filter)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "countDocuments",
				"collection", collection,
			).Inc()
		}
		return 0, fmt.Errorf("MongoDB CountDocuments failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "countDocuments",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "countDocuments",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	return count, nil
}

// CreateIndex creates an index on a collection
func (mo *MongoOperations) CreateIndex(ctx context.Context, collection string, model mongo.IndexModel) (string, error) {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	result, err := coll.Indexes().CreateOne(ctx, model)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "createIndex",
				"collection", collection,
			).Inc()
		}
		return "", fmt.Errorf("MongoDB CreateIndex failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "createIndex",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "createIndex",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	mo.conn.Logger().Info("MongoDB index created",
		logger.String("connection", mo.conn.Name()),
		logger.String("collection", collection),
		logger.String("index", result),
		logger.Duration("duration", duration),
	)

	return result, nil
}

// DropCollection drops a collection
func (mo *MongoOperations) DropCollection(ctx context.Context, collection string) error {
	start := time.Now()
	mo.conn.IncrementQueryCount()

	coll := mo.db.Collection(collection)
	err := coll.Drop(ctx)
	duration := time.Since(start)

	if err != nil {
		mo.conn.IncrementErrorCount()
		if mo.conn.Metrics() != nil {
			mo.conn.Metrics().Counter("forge.database.mongodb.operation_errors",
				"connection", mo.conn.Name(),
				"operation", "dropCollection",
				"collection", collection,
			).Inc()
		}
		return fmt.Errorf("MongoDB DropCollection failed: %w", err)
	}

	// Record operation metrics
	if mo.conn.Metrics() != nil {
		mo.conn.Metrics().Counter("forge.database.mongodb.operations",
			"connection", mo.conn.Name(),
			"operation", "dropCollection",
			"collection", collection,
		).Inc()
		mo.conn.Metrics().Histogram("forge.database.mongodb.operation_duration",
			"connection", mo.conn.Name(),
			"operation", "dropCollection",
			"collection", collection,
		).Observe(duration.Seconds())
	}

	mo.conn.Logger().Info("MongoDB collection dropped",
		logger.String("connection", mo.conn.Name()),
		logger.String("collection", collection),
		logger.Duration("duration", duration),
	)

	return nil
}

// GetOperations returns a MongoDB operations helper for this connection
func (mc *MongoConnection) GetOperations() *MongoOperations {
	return NewMongoOperations(mc)
}

// ConnectionString returns a sanitized connection string
func (mc *MongoConnection) ConnectionString() string {
	config := mc.Config()
	if config.Password != "" {
		return fmt.Sprintf("mongodb://***@%s:%d/%s", config.Host, config.Port, mc.databaseName)
	}
	return fmt.Sprintf("mongodb://%s:%d/%s", config.Host, config.Port, mc.databaseName)
}
