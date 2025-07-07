package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongoDatabase implements NoSQLDatabase interface for MongoDB
type mongoDatabase struct {
	*baseNoSQLDatabase
	client   *mongo.Client
	database *mongo.Database
	dbName   string
}

// mongoCollection implements Collection interface for MongoDB
type mongoCollection struct {
	*baseCollection
	collection *mongo.Collection
	db         *mongoDatabase
}

// mongoCursor implements Cursor interface for MongoDB
type mongoCursor struct {
	*baseCursor
	cursor *mongo.Cursor
	ctx    context.Context
}

// NewMongoDatabase creates a new MongoDB database connection
func newMongoDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &mongoDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "mongodb",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		dbName: config.Database,
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes MongoDB connection
func (db *mongoDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Build connection URI
	uri := db.buildMongoURI()

	// Create client options
	clientOptions := options.Client().ApplyURI(uri)

	// Set connection pool options
	if db.config.MaxPoolSize > 0 {
		clientOptions.SetMaxPoolSize(uint64(db.config.MaxPoolSize))
	} else {
		clientOptions.SetMaxPoolSize(100) // default
	}

	if db.config.MinPoolSize > 0 {
		clientOptions.SetMinPoolSize(uint64(db.config.MinPoolSize))
	} else {
		clientOptions.SetMinPoolSize(5) // default
	}

	if db.config.MaxIdleTimeMS > 0 {
		clientOptions.SetMaxConnIdleTime(db.config.MaxIdleTimeMS)
	} else {
		clientOptions.SetMaxConnIdleTime(30 * time.Minute) // default
	}

	if db.config.ConnectTimeout > 0 {
		clientOptions.SetConnectTimeout(db.config.ConnectTimeout)
	} else {
		clientOptions.SetConnectTimeout(30 * time.Second) // default
	}

	if db.config.SocketTimeout > 0 {
		clientOptions.SetSocketTimeout(db.config.SocketTimeout)
	} else {
		clientOptions.SetSocketTimeout(30 * time.Second) // default
	}

	// Create client
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	db.client = client
	db.database = client.Database(db.dbName)

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	db.connected = true
	return nil
}

// buildMongoURI builds MongoDB connection URI
func (db *mongoDatabase) buildMongoURI() string {
	config := db.config

	// If URL is provided, use it directly
	if config.URL != "" {
		return config.URL
	}

	// Build URI from components
	var uri strings.Builder
	uri.WriteString("mongodb://")

	// Add authentication if provided
	if config.Username != "" {
		uri.WriteString(config.Username)
		if config.Password != "" {
			uri.WriteString(":")
			uri.WriteString(config.Password)
		}
		uri.WriteString("@")
	}

	// Add host and port
	host := config.Host
	if host == "" {
		host = "localhost"
	}

	port := config.Port
	if port == 0 {
		port = 27017
	}

	uri.WriteString(fmt.Sprintf("%s:%d", host, port))

	// Add database name
	if config.Database != "" {
		uri.WriteString("/")
		uri.WriteString(config.Database)
	}

	// Add options
	var options []string

	if config.AuthDB != "" {
		options = append(options, "authSource="+config.AuthDB)
	}

	if config.ReplicaSet != "" {
		options = append(options, "replicaSet="+config.ReplicaSet)
	}

	if len(options) > 0 {
		uri.WriteString("?")
		uri.WriteString(strings.Join(options, "&"))
	}

	return uri.String()
}

// Close closes MongoDB connection
func (db *mongoDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := db.client.Disconnect(ctx)
		db.connected = false
		return err
	}

	return nil
}

// Ping tests MongoDB connection
func (db *mongoDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	return db.client.Ping(ctx, nil)
}

// Collection returns a MongoDB collection
func (db *mongoDatabase) Collection(name string) Collection {
	return &mongoCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		collection: db.database.Collection(name),
		db:         db,
	}
}

// CreateCollection creates a new collection
func (db *mongoDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.database == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	return db.database.CreateCollection(ctx, name)
}

// DropCollection drops a collection
func (db *mongoDatabase) DropCollection(ctx context.Context, name string) error {
	if db.database == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	return db.database.Collection(name).Drop(ctx)
}

// ListCollections lists all collections
func (db *mongoDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.database == nil {
		return nil, fmt.Errorf("MongoDB not connected")
	}

	cursor, err := db.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	return cursor, nil
}

// Drop drops the entire database
func (db *mongoDatabase) Drop(ctx context.Context) error {
	if db.database == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	return db.database.Drop(ctx)
}

// Stats returns MongoDB statistics
func (db *mongoDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	// Add MongoDB-specific stats
	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["database"] = db.dbName

	return stats
}

// MongoDB Collection implementation

// FindOne finds a single document
func (c *mongoCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	var result bson.M
	err := c.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("no documents found")
		}
		return nil, err
	}

	return result, nil
}

// Find finds multiple documents
func (c *mongoCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	cursor, err := c.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &mongoCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		cursor: cursor,
		ctx:    ctx,
	}, nil
}

// Insert inserts a single document
func (c *mongoCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	result, err := c.collection.InsertOne(ctx, document)
	if err != nil {
		return nil, err
	}

	return result.InsertedID, nil
}

// InsertMany inserts multiple documents
func (c *mongoCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	result, err := c.collection.InsertMany(ctx, documents)
	if err != nil {
		return nil, err
	}

	return result.InsertedIDs, nil
}

// Update updates multiple documents
func (c *mongoCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.collection == nil {
		return 0, fmt.Errorf("collection not initialized")
	}

	result, err := c.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}

	return result.ModifiedCount, nil
}

// UpdateOne updates a single document
func (c *mongoCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	_, err := c.collection.UpdateOne(ctx, filter, update)
	return err
}

// Delete deletes multiple documents
func (c *mongoCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.collection == nil {
		return 0, fmt.Errorf("collection not initialized")
	}

	result, err := c.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

// DeleteOne deletes a single document
func (c *mongoCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	_, err := c.collection.DeleteOne(ctx, filter)
	return err
}

// Aggregate performs aggregation
func (c *mongoCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	cursor, err := c.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}

	return &mongoCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		cursor: cursor,
		ctx:    ctx,
	}, nil
}

// Count counts documents
func (c *mongoCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.collection == nil {
		return 0, fmt.Errorf("collection not initialized")
	}

	return c.collection.CountDocuments(ctx, filter)
}

// CreateIndex creates an index
func (c *mongoCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	indexModel := mongo.IndexModel{
		Keys: keys,
	}

	if opts != nil {
		indexOptions := options.Index()

		if opts.Name != nil {
			indexOptions.SetName(*opts.Name)
		}

		if opts.Unique != nil {
			indexOptions.SetUnique(*opts.Unique)
		}

		if opts.Background != nil {
			indexOptions.SetBackground(*opts.Background)
		}

		if opts.Sparse != nil {
			indexOptions.SetSparse(*opts.Sparse)
		}

		if opts.ExpireAfter != nil {
			indexOptions.SetExpireAfterSeconds(int32(opts.ExpireAfter.Seconds()))
		}

		indexModel.Options = indexOptions
	}

	_, err := c.collection.Indexes().CreateOne(ctx, indexModel)
	return err
}

// DropIndex drops an index
func (c *mongoCollection) DropIndex(ctx context.Context, name string) error {
	if c.collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	_, err := c.collection.Indexes().DropOne(ctx, name)
	return err
}

// ListIndexes lists all indexes
func (c *mongoCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	cursor, err := c.collection.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var indexes []IndexInfo
	for cursor.Next(ctx) {
		var index bson.M
		if err := cursor.Decode(&index); err != nil {
			continue
		}

		indexInfo := IndexInfo{
			Name: index["name"].(string),
		}

		if keys, ok := index["key"].(bson.M); ok {
			indexInfo.Keys = keys
		}

		if unique, ok := index["unique"].(bool); ok {
			indexInfo.Unique = unique
		}

		indexes = append(indexes, indexInfo)
	}

	return indexes, nil
}

// BulkWrite performs bulk operations
func (c *mongoCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	// Convert operations to MongoDB bulk write models
	var models []mongo.WriteModel
	for _, op := range operations {
		if model, ok := op.(mongo.WriteModel); ok {
			models = append(models, model)
		}
	}

	if len(models) == 0 {
		return fmt.Errorf("no valid operations provided")
	}

	_, err := c.collection.BulkWrite(ctx, models)
	return err
}

// MongoDB Cursor implementation

// Next moves to the next document
func (c *mongoCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.cursor == nil {
		return false
	}

	if c.cursor.Next(ctx) {
		c.index++
		return true
	}

	return false
}

// Decode decodes the current document
func (c *mongoCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return fmt.Errorf("cursor is closed")
	}

	return c.cursor.Decode(dest)
}

// All decodes all documents
func (c *mongoCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return fmt.Errorf("cursor is closed")
	}

	return c.cursor.All(ctx, dest)
}

// Close closes the cursor
func (c *mongoCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cursor != nil {
		err := c.cursor.Close(ctx)
		c.closed = true
		return err
	}

	return nil
}

// Current returns the current document
func (c *mongoCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return nil
	}

	var result bson.M
	if err := c.cursor.Decode(&result); err != nil {
		return nil
	}

	return result
}

// Err returns the last error
func (c *mongoCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cursor == nil {
		return c.err
	}

	return c.cursor.Err()
}

// MongoDB-specific utility functions

// GetDatabase returns the underlying MongoDB database
func (db *mongoDatabase) GetDatabase() *mongo.Database {
	return db.database
}

// GetClient returns the underlying MongoDB client
func (db *mongoDatabase) GetClient() *mongo.Client {
	return db.client
}

// RunCommand runs a database command
func (db *mongoDatabase) RunCommand(ctx context.Context, command interface{}) (interface{}, error) {
	if db.database == nil {
		return nil, fmt.Errorf("MongoDB not connected")
	}

	var result bson.M
	err := db.database.RunCommand(ctx, command).Decode(&result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetDatabaseStats returns database statistics
func (db *mongoDatabase) GetDatabaseStats(ctx context.Context) (map[string]interface{}, error) {
	if db.database == nil {
		return nil, fmt.Errorf("MongoDB not connected")
	}

	var stats bson.M
	err := db.database.RunCommand(ctx, bson.D{{"dbStats", 1}}).Decode(&stats)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for k, v := range stats {
		result[k] = v
	}

	return result, nil
}

// GetCollectionStats returns collection statistics
func (c *mongoCollection) GetCollectionStats(ctx context.Context) (map[string]interface{}, error) {
	if c.collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	var stats bson.M
	err := c.db.database.RunCommand(ctx, bson.D{{"collStats", c.name}}).Decode(&stats)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for k, v := range stats {
		result[k] = v
	}

	return result, nil
}

// CreateUser creates a database user
func (db *mongoDatabase) CreateUser(ctx context.Context, username, password string, roles []string) error {
	if db.database == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	roleObjs := make([]bson.M, len(roles))
	for i, role := range roles {
		roleObjs[i] = bson.M{"role": role, "db": db.dbName}
	}

	command := bson.D{
		{"createUser", username},
		{"pwd", password},
		{"roles", roleObjs},
	}

	return db.database.RunCommand(ctx, command).Err()
}

// DropUser drops a database user
func (db *mongoDatabase) DropUser(ctx context.Context, username string) error {
	if db.database == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	command := bson.D{{"dropUser", username}}
	return db.database.RunCommand(ctx, command).Err()
}

// StartSession starts a new session
func (db *mongoDatabase) StartSession(ctx context.Context) (interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("MongoDB not connected")
	}

	return db.client.StartSession()
}

// WithTransaction executes a function within a transaction
func (db *mongoDatabase) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	if db.client == nil {
		return fmt.Errorf("MongoDB not connected")
	}

	session, err := db.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessCtx)
	})

	return err
}

// init function to override the MongoDB constructor
func init() {
	NewMongoDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newMongoDatabase(config)
	}
}
