package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// MongoAdapter implements database.DatabaseAdapter for MongoDB
type MongoAdapter struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewMongoAdapter creates a new MongoDB adapter
func NewMongoAdapter(logger common.Logger, metrics common.Metrics) database.DatabaseAdapter {
	return &MongoAdapter{
		logger:  logger,
		metrics: metrics,
	}
}

// Name returns the adapter name
func (ma *MongoAdapter) Name() string {
	return "mongodb"
}

// SupportedTypes returns the database types supported by this adapter
func (ma *MongoAdapter) SupportedTypes() []string {
	return []string{"mongodb", "mongo"}
}

// Connect creates a new MongoDB connection
func (ma *MongoAdapter) Connect(ctx context.Context, config *database.ConnectionConfig) (database.Connection, error) {
	if err := ma.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create MongoDB client options
	opts, err := ma.buildMongoOptions(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build MongoDB options: %w", err)
	}

	// Create MongoDB client
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Test connection
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	// Create connection wrapper
	connectionName := fmt.Sprintf("mongodb_%s_%d", config.Host, config.Port)
	if config.Database != "" {
		connectionName = fmt.Sprintf("mongodb_%s_%d_%s", config.Host, config.Port, config.Database)
	}

	conn := NewMongoConnection(connectionName, config, client, ma.logger, ma.metrics)

	ma.logger.Info("MongoDB connection created",
		logger.String("connection", connectionName),
		logger.String("host", config.Host),
		logger.Int("port", config.Port),
		logger.String("database", config.Database),
	)

	return conn, nil
}

// ValidateConfig validates the MongoDB configuration
func (ma *MongoAdapter) ValidateConfig(config *database.ConnectionConfig) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	if config.Type != "mongodb" && config.Type != "mongo" {
		return fmt.Errorf("invalid database type: %s", config.Type)
	}

	if config.Host == "" {
		return fmt.Errorf("host is required")
	}

	if config.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	if config.Database == "" {
		return fmt.Errorf("database name is required for MongoDB")
	}

	// Validate pool configuration
	if config.Pool.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be greater than 0")
	}

	if config.Pool.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative")
	}

	if config.Pool.MaxIdleConns > config.Pool.MaxOpenConns {
		return fmt.Errorf("max idle connections cannot be greater than max open connections")
	}

	return nil
}

// SupportsMigrations returns false as MongoDB doesn't support traditional schema migrations
func (ma *MongoAdapter) SupportsMigrations() bool {
	return false
}

// Migrate is not supported for MongoDB
func (ma *MongoAdapter) Migrate(ctx context.Context, conn database.Connection, migrationsPath string) error {
	return fmt.Errorf("schema migrations are not supported for MongoDB")
}

// HealthCheck performs a health check on the MongoDB connection
func (ma *MongoAdapter) HealthCheck(ctx context.Context, conn database.Connection) error {
	mongoConn, ok := conn.(*MongoConnection)
	if !ok {
		return fmt.Errorf("connection is not a MongoDB connection")
	}

	client := mongoConn.GetMongoClient()

	// Ping the MongoDB server
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Try to list databases to ensure we have access
	databaseNames, err := client.ListDatabaseNames(ctx, map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	// Check if our target database exists or if we can access it
	targetDB := mongoConn.GetDatabaseName()
	dbFound := false
	for _, dbName := range databaseNames {
		if dbName == targetDB {
			dbFound = true
			break
		}
	}

	// If database doesn't exist, try to create a test collection and remove it
	if !dbFound {
		db := client.Database(targetDB)
		testCollection := db.Collection("_health_check_" + fmt.Sprintf("%d", time.Now().UnixNano()))

		// Insert a test document
		_, err := testCollection.InsertOne(ctx, map[string]interface{}{"test": true})
		if err != nil {
			return fmt.Errorf("failed to write test document: %w", err)
		}

		// Remove the test collection
		if err := testCollection.Drop(ctx); err != nil {
			ma.logger.Warn("failed to cleanup test collection",
				logger.String("collection", testCollection.Name()),
				logger.Error(err),
			)
		}
	}

	return nil
}

// buildMongoOptions builds MongoDB client options from configuration
func (ma *MongoAdapter) buildMongoOptions(config *database.ConnectionConfig) (*options.ClientOptions, error) {
	// Build connection URI
	uri := ma.buildConnectionURI(config)

	opts := options.Client().ApplyURI(uri)

	// Set connection pool options
	opts.SetMaxPoolSize(uint64(config.Pool.MaxOpenConns))
	opts.SetMinPoolSize(uint64(config.Pool.MaxIdleConns))
	opts.SetMaxConnIdleTime(config.Pool.ConnMaxIdleTime)

	// Set default timeouts
	opts.SetConnectTimeout(10 * time.Second)
	opts.SetServerSelectionTimeout(5 * time.Second)
	opts.SetSocketTimeout(30 * time.Second)

	// Apply configuration-specific settings
	if connectTimeout, ok := config.Config["connect_timeout"].(time.Duration); ok {
		opts.SetConnectTimeout(connectTimeout)
	}
	if serverSelectionTimeout, ok := config.Config["server_selection_timeout"].(time.Duration); ok {
		opts.SetServerSelectionTimeout(serverSelectionTimeout)
	}
	if socketTimeout, ok := config.Config["socket_timeout"].(time.Duration); ok {
		opts.SetSocketTimeout(socketTimeout)
	}
	if maxConnIdleTime, ok := config.Config["max_conn_idle_time"].(time.Duration); ok {
		opts.SetMaxConnIdleTime(maxConnIdleTime)
	}

	// Set read preference
	if readPref, ok := config.Config["read_preference"].(string); ok {
		switch readPref {
		case "primary":
			opts.SetReadPreference(readpref.Primary())
		case "primaryPreferred":
			opts.SetReadPreference(readpref.PrimaryPreferred())
		case "secondary":
			opts.SetReadPreference(readpref.Secondary())
		case "secondaryPreferred":
			opts.SetReadPreference(readpref.SecondaryPreferred())
		case "nearest":
			opts.SetReadPreference(readpref.Nearest())
		}
	}

	// Set write concern
	if writeConcern, ok := config.Config["write_concern"].(string); ok {
		wc := &writeconcern.WriteConcern{}
		switch writeConcern {
		case "majority":
			wc.W = writeconcern.Majority()
		case "acknowledged":
			wc.W = writeconcern.W1()
		case "unacknowledged":
			writeconcern.Unacknowledged()
		}
		opts.SetWriteConcern(wc)
	}

	// Set read concern
	if readConcern, ok := config.Config["read_concern"].(string); ok {
		var rc *readconcern.ReadConcern
		switch readConcern {
		case "local":
			rc = readconcern.Local()
		case "available":
			rc = readconcern.Available()
		case "majority":
			rc = readconcern.Majority()
		case "linearizable":
			rc = readconcern.Linearizable()
		case "snapshot":
			rc = readconcern.Snapshot()
		}
		opts.SetReadConcern(rc)
	}

	// Enable retryable writes by default
	opts.SetRetryWrites(true)

	// Enable retryable reads by default
	opts.SetRetryReads(true)

	return opts, nil
}

// buildConnectionURI builds the MongoDB connection URI
func (ma *MongoAdapter) buildConnectionURI(config *database.ConnectionConfig) string {
	uri := "mongodb://"

	// Add authentication if provided
	if config.Username != "" {
		uri += config.Username
		if config.Password != "" {
			uri += ":" + config.Password
		}
		uri += "@"
	}

	// Add host and port
	uri += fmt.Sprintf("%s:%d", config.Host, config.Port)

	// Add database name
	if config.Database != "" {
		uri += "/" + config.Database
	}

	// Add query parameters
	queryParams := make([]string, 0)

	// Add authentication database if specified
	if authDB, ok := config.Config["auth_database"].(string); ok && authDB != "" {
		queryParams = append(queryParams, "authSource="+authDB)
	}

	// Add authentication mechanism if specified
	if authMech, ok := config.Config["auth_mechanism"].(string); ok && authMech != "" {
		queryParams = append(queryParams, "authMechanism="+authMech)
	}

	// Add SSL/TLS configuration
	if ssl, ok := config.Config["ssl"].(bool); ok && ssl {
		queryParams = append(queryParams, "ssl=true")

		if sslCert, ok := config.Config["ssl_cert_file"].(string); ok && sslCert != "" {
			queryParams = append(queryParams, "sslcertificatekeyfile="+sslCert)
		}

		if sslCA, ok := config.Config["ssl_ca_file"].(string); ok && sslCA != "" {
			queryParams = append(queryParams, "sslcertificateauthorityfile="+sslCA)
		}

		if sslInsecure, ok := config.Config["ssl_insecure"].(bool); ok && sslInsecure {
			queryParams = append(queryParams, "sslinsecure=true")
		}
	}

	// Add replica set name if specified
	if replicaSet, ok := config.Config["replica_set"].(string); ok && replicaSet != "" {
		queryParams = append(queryParams, "replicaSet="+replicaSet)
	}

	// Add connection timeout
	if connectTimeoutMS, ok := config.Config["connect_timeout_ms"].(int); ok && connectTimeoutMS > 0 {
		queryParams = append(queryParams, fmt.Sprintf("connectTimeoutMS=%d", connectTimeoutMS))
	}

	// Add server selection timeout
	if serverSelectionTimeoutMS, ok := config.Config["server_selection_timeout_ms"].(int); ok && serverSelectionTimeoutMS > 0 {
		queryParams = append(queryParams, fmt.Sprintf("serverSelectionTimeoutMS=%d", serverSelectionTimeoutMS))
	}

	// Add query parameters to URI
	if len(queryParams) > 0 {
		uri += "?"
		for i, param := range queryParams {
			if i > 0 {
				uri += "&"
			}
			uri += param
		}
	}

	return uri
}

// MongoConfig contains MongoDB-specific configuration
type MongoConfig struct {
	*database.ConnectionConfig
	AuthDatabase             string        `yaml:"auth_database" json:"auth_database"`
	AuthMechanism            string        `yaml:"auth_mechanism" json:"auth_mechanism"`
	ReplicaSet               string        `yaml:"replica_set" json:"replica_set"`
	SSL                      bool          `yaml:"ssl" json:"ssl"`
	SSLCertFile              string        `yaml:"ssl_cert_file" json:"ssl_cert_file"`
	SSLCAFile                string        `yaml:"ssl_ca_file" json:"ssl_ca_file"`
	SSLInsecure              bool          `yaml:"ssl_insecure" json:"ssl_insecure"`
	ConnectTimeout           time.Duration `yaml:"connect_timeout" json:"connect_timeout"`
	ServerSelectionTimeout   time.Duration `yaml:"server_selection_timeout" json:"server_selection_timeout"`
	SocketTimeout            time.Duration `yaml:"socket_timeout" json:"socket_timeout"`
	MaxConnIdleTime          time.Duration `yaml:"max_conn_idle_time" json:"max_conn_idle_time"`
	ReadPreference           string        `yaml:"read_preference" json:"read_preference"`
	WriteConcern             string        `yaml:"write_concern" json:"write_concern"`
	ReadConcern              string        `yaml:"read_concern" json:"read_concern"`
	ConnectTimeoutMS         int           `yaml:"connect_timeout_ms" json:"connect_timeout_ms"`
	ServerSelectionTimeoutMS int           `yaml:"server_selection_timeout_ms" json:"server_selection_timeout_ms"`
}

// NewMongoConfig creates a new MongoDB configuration
func NewMongoConfig() *MongoConfig {
	return &MongoConfig{
		ConnectionConfig: &database.ConnectionConfig{
			Type:     "mongodb",
			Host:     "localhost",
			Port:     27017,
			Database: "",
			Username: "",
			Password: "",
			Pool: database.PoolConfig{
				MaxOpenConns:    100,
				MaxIdleConns:    10,
				ConnMaxLifetime: time.Hour,
				ConnMaxIdleTime: 10 * time.Minute,
			},
			Retry: database.RetryConfig{
				MaxAttempts:   3,
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      5 * time.Second,
				BackoffFactor: 2.0,
			},
		},
		AuthDatabase:             "",
		AuthMechanism:            "",
		ReplicaSet:               "",
		SSL:                      false,
		SSLCertFile:              "",
		SSLCAFile:                "",
		SSLInsecure:              false,
		ConnectTimeout:           10 * time.Second,
		ServerSelectionTimeout:   5 * time.Second,
		SocketTimeout:            30 * time.Second,
		MaxConnIdleTime:          10 * time.Minute,
		ReadPreference:           "primary",
		WriteConcern:             "majority",
		ReadConcern:              "local",
		ConnectTimeoutMS:         10000,
		ServerSelectionTimeoutMS: 5000,
	}
}

// Apply applies MongoDB-specific configuration
func (mc *MongoConfig) Apply() {
	if mc.ConnectionConfig.Config == nil {
		mc.ConnectionConfig.Config = make(map[string]interface{})
	}

	mc.ConnectionConfig.Config["auth_database"] = mc.AuthDatabase
	mc.ConnectionConfig.Config["auth_mechanism"] = mc.AuthMechanism
	mc.ConnectionConfig.Config["replica_set"] = mc.ReplicaSet
	mc.ConnectionConfig.Config["ssl"] = mc.SSL
	mc.ConnectionConfig.Config["ssl_cert_file"] = mc.SSLCertFile
	mc.ConnectionConfig.Config["ssl_ca_file"] = mc.SSLCAFile
	mc.ConnectionConfig.Config["ssl_insecure"] = mc.SSLInsecure
	mc.ConnectionConfig.Config["connect_timeout"] = mc.ConnectTimeout
	mc.ConnectionConfig.Config["server_selection_timeout"] = mc.ServerSelectionTimeout
	mc.ConnectionConfig.Config["socket_timeout"] = mc.SocketTimeout
	mc.ConnectionConfig.Config["max_conn_idle_time"] = mc.MaxConnIdleTime
	mc.ConnectionConfig.Config["read_preference"] = mc.ReadPreference
	mc.ConnectionConfig.Config["write_concern"] = mc.WriteConcern
	mc.ConnectionConfig.Config["read_concern"] = mc.ReadConcern
	mc.ConnectionConfig.Config["connect_timeout_ms"] = mc.ConnectTimeoutMS
	mc.ConnectionConfig.Config["server_selection_timeout_ms"] = mc.ServerSelectionTimeoutMS
}
