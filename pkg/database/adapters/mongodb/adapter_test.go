package mongodb_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/metrics"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	mongoAdapter "github.com/xraph/forge/pkg/database/adapters/mongodb"
	"github.com/xraph/forge/pkg/logger"
)

// Test models
type TestUser struct {
	ID      string    `bson:"_id"`
	Name    string    `bson:"name"`
	Email   string    `bson:"email"`
	Age     int       `bson:"age"`
	Created time.Time `bson:"created"`
	Updated time.Time `bson:"updated,omitempty"`
}

type TestPost struct {
	ID      string    `bson:"_id"`
	UserID  string    `bson:"user_id"`
	Title   string    `bson:"title"`
	Content string    `bson:"content"`
	Tags    []string  `bson:"tags"`
	Created time.Time `bson:"created"`
}

// Test fixtures
var (
	testLogger  common.Logger
	testMetrics common.Metrics
)

func init() {
	testLogger = logger.NewNoopLogger()
	testMetrics = metrics.NewMockMetricsCollector()
}

// TestMongoAdapterBasics tests basic adapter functionality
func TestMongoAdapterBasics(t *testing.T) {
	adapter := mongoAdapter.NewMongoAdapter(testLogger, testMetrics)

	t.Run("adapter info", func(t *testing.T) {
		assert.Equal(t, "mongodb", adapter.Name())
		assert.Contains(t, adapter.SupportedTypes(), "mongodb")
		assert.Contains(t, adapter.SupportedTypes(), "mongo")
		assert.False(t, adapter.SupportsMigrations())
	})

	t.Run("config validation", func(t *testing.T) {
		validConfig := &database.ConnectionConfig{
			Type:     "mongodb",
			Host:     "localhost",
			Port:     27017,
			Database: "test_db",
			Username: "",
			Password: "",
			Pool: database.PoolConfig{
				MaxOpenConns:    100,
				MaxIdleConns:    10,
				ConnMaxLifetime: time.Hour,
				ConnMaxIdleTime: 10 * time.Minute,
			},
		}

		// Valid config should pass
		assert.NoError(t, adapter.ValidateConfig(validConfig))

		// Invalid configs should fail
		invalidConfigs := []*database.ConnectionConfig{
			nil,                         // nil config
			{Type: "postgres"},          // wrong type
			{Type: "mongodb", Host: ""}, // missing host
			{Type: "mongodb", Host: "localhost", Port: 0},                   // invalid port
			{Type: "mongodb", Host: "localhost", Port: 27017, Database: ""}, // missing database
		}

		for i, config := range invalidConfigs {
			assert.Error(t, adapter.ValidateConfig(config), "invalid config %d should fail", i)
		}
	})
}

// TestMongoAdapterIntegration tests integration with a real MongoDB instance
func TestMongoAdapterIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MongoDB container
	mongoContainer, err := mongodb.RunContainer(ctx,
		testcontainers.WithImage("mongo:7"),
		mongodb.WithUsername("testuser"),
		mongodb.WithPassword("testpass"),
	)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mongoContainer.Terminate(ctx))
	}()

	// Get connection details
	host, err := mongoContainer.Host(ctx)
	require.NoError(t, err)

	port, err := mongoContainer.MappedPort(ctx, "27017/tcp")
	require.NoError(t, err)

	config := &database.ConnectionConfig{
		Type:     "mongodb",
		Host:     host,
		Port:     port.Int(),
		Database: "test_db",
		Username: "testuser",
		Password: "testpass",
		Pool: database.PoolConfig{
			MaxOpenConns:    100,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 10 * time.Minute,
		},
		Config: map[string]interface{}{
			"auth_database": "admin",
		},
	}

	adapter := mongoAdapter.NewMongoAdapter(testLogger, testMetrics)

	// Test connection creation
	conn, err := adapter.Connect(ctx, config)
	require.NoError(t, err)
	require.NotNil(t, conn)

	// Test connection lifecycle
	assert.NoError(t, conn.Start(ctx))
	assert.True(t, conn.IsConnected())

	// Test ping
	assert.NoError(t, conn.Ping(ctx))

	// Test health check
	assert.NoError(t, adapter.HealthCheck(ctx, conn))

	// Cast to MongoDB connection for specific operations
	mongoConn, ok := conn.(*mongoAdapter.MongoConnection)
	require.True(t, ok)

	// Test MongoDB operations
	testMongoOperations(t, ctx, mongoConn)

	// Test transaction
	testMongoTransactions(t, ctx, mongoConn)

	// Test connection stats
	stats := conn.Stats()
	assert.Equal(t, "mongodb", stats.Type)
	assert.True(t, stats.Connected)
	assert.Greater(t, stats.QueryCount, int64(0))

	// Test connection close
	assert.NoError(t, conn.Stop(ctx))
	assert.False(t, conn.IsConnected())
}

// testMongoOperations tests MongoDB-specific operations
func testMongoOperations(t *testing.T, ctx context.Context, conn *mongoAdapter.MongoConnection) {
	ops := conn.GetOperations()

	// Test data
	testUser := &TestUser{
		ID:      "user_001",
		Name:    "John Doe",
		Email:   "john@example.com",
		Age:     30,
		Created: time.Now(),
	}

	testPost := &TestPost{
		ID:      "post_001",
		UserID:  testUser.ID,
		Title:   "Test Post",
		Content: "This is a test post content",
		Tags:    []string{"test", "mongodb", "forge"},
		Created: time.Now(),
	}

	t.Run("insert operations", func(t *testing.T) {
		// Test InsertOne
		result, err := ops.InsertOne(ctx, "users", testUser)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, testUser.ID, result.InsertedID)

		// Test InsertMany
		posts := []interface{}{testPost}
		manyResult, err := ops.InsertMany(ctx, "posts", posts)
		assert.NoError(t, err)
		assert.NotNil(t, manyResult)
		assert.Len(t, manyResult.InsertedIDs, 1)
	})

	t.Run("find operations", func(t *testing.T) {
		// Test FindOne
		result := ops.FindOne(ctx, "users", bson.M{"_id": testUser.ID})
		assert.NoError(t, result.Err())

		var foundUser TestUser
		err := result.Decode(&foundUser)
		assert.NoError(t, err)
		assert.Equal(t, testUser.ID, foundUser.ID)
		assert.Equal(t, testUser.Email, foundUser.Email)

		// Test Find
		cursor, err := ops.Find(ctx, "posts", bson.M{"user_id": testUser.ID})
		assert.NoError(t, err)
		defer cursor.Close(ctx)

		var posts []TestPost
		err = cursor.All(ctx, &posts)
		assert.NoError(t, err)
		assert.Len(t, posts, 1)
		assert.Equal(t, testPost.Title, posts[0].Title)
	})

	t.Run("update operations", func(t *testing.T) {
		// Test UpdateOne
		update := bson.M{"$set": bson.M{"age": 31, "updated": time.Now()}}
		result, err := ops.UpdateOne(ctx, "users", bson.M{"_id": testUser.ID}, update)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), result.ModifiedCount)

		// Test UpdateMany
		update = bson.M{"$addToSet": bson.M{"tags": "updated"}}
		manyResult, err := ops.UpdateMany(ctx, "posts", bson.M{"user_id": testUser.ID}, update)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), manyResult.ModifiedCount)
	})

	t.Run("count operations", func(t *testing.T) {
		count, err := ops.CountDocuments(ctx, "users", bson.M{})
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)

		count, err = ops.CountDocuments(ctx, "posts", bson.M{"user_id": testUser.ID})
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
	})

	t.Run("index operations", func(t *testing.T) {
		// Create index on email field
		indexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "email", Value: 1}},
		}

		indexName, err := ops.CreateIndex(ctx, "users", indexModel)
		assert.NoError(t, err)
		assert.NotEmpty(t, indexName)
	})

	t.Run("delete operations", func(t *testing.T) {
		// Test DeleteOne
		result, err := ops.DeleteOne(ctx, "posts", bson.M{"_id": testPost.ID})
		assert.NoError(t, err)
		assert.Equal(t, int64(1), result.DeletedCount)

		// Test DeleteMany
		manyResult, err := ops.DeleteMany(ctx, "users", bson.M{})
		assert.NoError(t, err)
		assert.Equal(t, int64(1), manyResult.DeletedCount)
	})
}

// testMongoTransactions tests MongoDB transaction functionality
func testMongoTransactions(t *testing.T, ctx context.Context, conn *mongoAdapter.MongoConnection) {
	testUser := &TestUser{
		ID:      "tx_user_001",
		Name:    "Transaction User",
		Email:   "tx@example.com",
		Age:     25,
		Created: time.Now(),
	}

	testPost := &TestPost{
		ID:      "tx_post_001",
		UserID:  testUser.ID,
		Title:   "Transaction Post",
		Content: "This post was created in a transaction",
		Tags:    []string{"transaction", "test"},
		Created: time.Now(),
	}

	t.Run("successful transaction", func(t *testing.T) {
		err := conn.Transaction(ctx, func(tx interface{}) error {
			mongoTx, ok := tx.(*mongoAdapter.MongoTransaction)
			if !ok {
				return fmt.Errorf("invalid transaction type")
			}

			// Insert user
			userColl := mongoTx.Collection("users")
			if userColl == nil {
				return fmt.Errorf("failed to get users collection")
			}

			_, err := userColl.InsertOne(mongoTx.SessionContext(), testUser)
			if err != nil {
				return err
			}

			// Insert post
			postColl := mongoTx.Collection("posts")
			if postColl == nil {
				return fmt.Errorf("failed to get posts collection")
			}

			_, err = postColl.InsertOne(mongoTx.SessionContext(), testPost)
			return err
		})

		assert.NoError(t, err)

		// Verify both documents were inserted
		ops := conn.GetOperations()

		userResult := ops.FindOne(ctx, "users", bson.M{"_id": testUser.ID})
		assert.NoError(t, userResult.Err())

		postResult := ops.FindOne(ctx, "posts", bson.M{"_id": testPost.ID})
		assert.NoError(t, postResult.Err())
	})

	t.Run("failed transaction rollback", func(t *testing.T) {
		failUser := &TestUser{
			ID:      "fail_user_001",
			Name:    "Fail User",
			Email:   "fail@example.com",
			Age:     30,
			Created: time.Now(),
		}

		err := conn.Transaction(ctx, func(tx interface{}) error {
			mongoTx, ok := tx.(*mongoAdapter.MongoTransaction)
			if !ok {
				return fmt.Errorf("invalid transaction type")
			}

			// Insert user (this should succeed)
			userColl := mongoTx.Collection("users")
			_, err := userColl.InsertOne(mongoTx.SessionContext(), failUser)
			if err != nil {
				return err
			}

			// Force an error to test rollback
			return fmt.Errorf("intentional transaction failure")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "intentional transaction failure")

		// Verify the user was NOT inserted due to rollback
		ops := conn.GetOperations()
		result := ops.FindOne(ctx, "users", bson.M{"_id": failUser.ID})
		assert.Equal(t, mongo.ErrNoDocuments, result.Err())
	})

	// Cleanup
	ops := conn.GetOperations()
	ops.DeleteMany(ctx, "users", bson.M{})
	ops.DeleteMany(ctx, "posts", bson.M{})
}

// TestMongoAdapterConfiguration tests MongoDB-specific configuration
func TestMongoAdapterConfiguration(t *testing.T) {
	t.Run("mongo config creation", func(t *testing.T) {
		config := mongoAdapter.NewMongoConfig()
		assert.NotNil(t, config)
		assert.Equal(t, "mongodb", config.Type)
		assert.Equal(t, "localhost", config.Host)
		assert.Equal(t, 27017, config.Port)
		assert.Equal(t, 10*time.Second, config.ConnectTimeout)
		assert.Equal(t, "primary", config.ReadPreference)
		assert.Equal(t, "majority", config.WriteConcern)

		// Test Apply method
		config.AuthDatabase = "admin"
		config.SSL = true
		config.ReplicaSet = "rs0"
		config.Apply()

		assert.Equal(t, "admin", config.ConnectionConfig.Config["auth_database"])
		assert.Equal(t, true, config.ConnectionConfig.Config["ssl"])
		assert.Equal(t, "rs0", config.ConnectionConfig.Config["replica_set"])
	})

	t.Run("connection string generation", func(t *testing.T) {
		config := &database.ConnectionConfig{
			Type:     "mongodb",
			Host:     "localhost",
			Port:     27017,
			Database: "test_db",
			Username: "user",
			Password: "pass",
			Config: map[string]interface{}{
				"auth_database": "admin",
				"ssl":           true,
				"replica_set":   "rs0",
			},
		}

		connStr := config.ConnectionString()
		assert.Contains(t, connStr, "mongodb://")
		assert.Contains(t, connStr, "user:pass@")
		assert.Contains(t, connStr, "localhost:27017")
		assert.Contains(t, connStr, "/test_db")
		assert.Contains(t, connStr, "authSource=admin")
		assert.Contains(t, connStr, "ssl=true")
		assert.Contains(t, connStr, "replicaSet=rs0")
	})
}

// TestDatabaseManagerWithMongo tests the database manager with MongoDB adapter
func TestDatabaseManagerWithMongo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start MongoDB container
	mongoContainer, err := mongodb.RunContainer(ctx,
		testcontainers.WithImage("mongo:7"),
	)
	require.NoError(t, err)
	defer mongoContainer.Terminate(ctx)

	// Get connection details
	host, err := mongoContainer.Host(ctx)
	require.NoError(t, err)

	port, err := mongoContainer.MappedPort(ctx, "27017/tcp")
	require.NoError(t, err)

	// Create database manager
	manager := database.NewManager(testLogger, testMetrics, config.NewTestConfigManager())

	// Register MongoDB adapter
	adapter := mongoAdapter.NewMongoAdapter(testLogger, testMetrics)
	err = manager.RegisterAdapter(adapter)
	require.NoError(t, err)

	// Configure database
	dbConfig := &database.DatabaseConfig{
		Default:        "mongodb",
		AutoMigrate:    false,
		MigrationsPath: "./migrations",
		Connections: map[string]database.ConnectionConfig{
			"mongodb": {
				Type:     "mongodb",
				Host:     host,
				Port:     port.Int(),
				Database: "test_db",
				Pool: database.PoolConfig{
					MaxOpenConns:    100,
					MaxIdleConns:    10,
					ConnMaxLifetime: time.Hour,
					ConnMaxIdleTime: 10 * time.Minute,
				},
			},
		},
		Metrics: database.MetricsConfig{
			Enabled:            true,
			CollectionInterval: 15 * time.Second,
			SlowQueryThreshold: time.Second,
			EnableQueryMetrics: true,
			EnablePoolMetrics:  true,
		},
		HealthCheck: database.HealthCheckConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
		},
	}

	err = manager.SetConfig(dbConfig)
	require.NoError(t, err)

	// Start manager
	err = manager.Start(ctx)
	require.NoError(t, err)

	// Test operations
	conn, err := manager.GetConnection("mongodb")
	require.NoError(t, err)
	assert.NotNil(t, conn)

	defaultConn, err := manager.GetDefaultConnection()
	require.NoError(t, err)
	assert.Equal(t, conn, defaultConn)

	// Test health check
	err = manager.HealthCheckAll(ctx)
	assert.NoError(t, err)

	// Test stats
	stats := manager.GetStats()
	assert.Equal(t, 1, stats.TotalConnections)
	assert.Equal(t, 1, stats.ActiveConnections)
	assert.Equal(t, 1, stats.RegisteredAdapters)
	assert.True(t, stats.HealthCheckPassed)

	// Stop manager
	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

// Benchmark tests
func BenchmarkMongoOperations(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	ctx := context.Background()

	// Setup MongoDB container
	mongoContainer, err := mongodb.RunContainer(ctx,
		testcontainers.WithImage("mongo:7"),
	)
	require.NoError(b, err)
	defer mongoContainer.Terminate(ctx)

	// Get connection details
	host, err := mongoContainer.Host(ctx)
	require.NoError(b, err)

	port, err := mongoContainer.MappedPort(ctx, "27017/tcp")
	require.NoError(b, err)

	config := &database.ConnectionConfig{
		Type:     "mongodb",
		Host:     host,
		Port:     port.Int(),
		Database: "benchmark_db",
		Pool: database.PoolConfig{
			MaxOpenConns:    100,
			MaxIdleConns:    50,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 10 * time.Minute,
		},
	}

	adapter := mongoAdapter.NewMongoAdapter(testLogger, testMetrics)
	conn, err := adapter.Connect(ctx, config)
	require.NoError(b, err)

	err = conn.Start(ctx)
	require.NoError(b, err)

	mongoConn := conn.(*mongoAdapter.MongoConnection)
	ops := mongoConn.GetOperations()

	b.Run("InsertOne", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			user := &TestUser{
				ID:      fmt.Sprintf("bench_user_%d", i),
				Name:    "Benchmark User",
				Email:   fmt.Sprintf("bench%d@example.com", i),
				Age:     25,
				Created: time.Now(),
			}
			_, err := ops.InsertOne(ctx, "bench_users", user)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FindOne", func(b *testing.B) {
		// Pre-insert some data
		for i := 0; i < 100; i++ {
			user := &TestUser{
				ID:      fmt.Sprintf("find_user_%d", i),
				Name:    "Find User",
				Email:   fmt.Sprintf("find%d@example.com", i),
				Age:     30,
				Created: time.Now(),
			}
			ops.InsertOne(ctx, "find_users", user)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			userID := fmt.Sprintf("find_user_%d", i%100)
			result := ops.FindOne(ctx, "find_users", bson.M{"_id": userID})
			if result.Err() != nil && result.Err() != mongo.ErrNoDocuments {
				b.Fatal(result.Err())
			}
		}
	})

	conn.Stop(ctx)
}
