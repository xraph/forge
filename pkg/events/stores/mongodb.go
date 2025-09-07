package stores

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/logger"
)

// MongoEventStore implements EventStore interface using MongoDB
type MongoEventStore struct {
	connection       database.Connection
	client           *mongo.Client
	database         *mongo.Database
	eventsCollection *mongo.Collection
	snapsCollection  *mongo.Collection
	logger           common.Logger
	metrics          common.Metrics
	config           *eventscore.EventStoreConfig
	stats            *eventscore.EventStoreStats
}

// MongoEvent represents an event in MongoDB
type MongoEvent struct {
	ID          string                 `bson:"_id"`
	AggregateID string                 `bson:"aggregate_id"`
	Type        string                 `bson:"type"`
	Version     int                    `bson:"version"`
	Data        interface{}            `bson:"data"`
	Metadata    map[string]interface{} `bson:"metadata"`
	Source      string                 `bson:"source"`
	Timestamp   time.Time              `bson:"timestamp"`
	CreatedAt   time.Time              `bson:"created_at"`
	UpdatedAt   time.Time              `bson:"updated_at"`
}

// MongoSnapshot represents a snapshot in MongoDB
type MongoSnapshot struct {
	ID          string                 `bson:"_id"`
	AggregateID string                 `bson:"aggregate_id"`
	Type        string                 `bson:"type"`
	Version     int                    `bson:"version"`
	Data        interface{}            `bson:"data"`
	Metadata    map[string]interface{} `bson:"metadata"`
	Timestamp   time.Time              `bson:"timestamp"`
	CreatedAt   time.Time              `bson:"created_at"`
	UpdatedAt   time.Time              `bson:"updated_at"`
}

// NewMongoEventStore creates a new MongoDB event store
func NewMongoEventStore(config *eventscore.EventStoreConfig, l common.Logger, metrics common.Metrics, dbManager database.DatabaseManager) (eventscore.EventStore, error) {
	// Get database connection
	connectionName := "default"
	if config.ConnectionName != "" {
		connectionName = config.ConnectionName
	}

	connection, err := dbManager.GetConnection(connectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection '%s': %w", connectionName, err)
	}

	// Type assert to get MongoDB connection
	mongoConnection, ok := connection.(database.Connection)
	if !ok {
		return nil, fmt.Errorf("connection '%s' is not a MongoDB connection", connectionName)
	}

	client := mongoConnection.DB().(*mongo.Client)
	if client == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	// Get database name from config
	dbName := "forge_events"
	if config.Database != "" {
		dbName = config.Database
	}

	database := client.Database(dbName)
	eventsCollection := database.Collection("events")
	snapsCollection := database.Collection("snapshots")

	store := &MongoEventStore{
		connection:       mongoConnection,
		client:           client,
		database:         database,
		eventsCollection: eventsCollection,
		snapsCollection:  snapsCollection,
		logger:           l,
		metrics:          metrics,
		config:           config,
		stats: &eventscore.EventStoreStats{
			TotalEvents:     0,
			EventsByType:    make(map[string]int64),
			TotalSnapshots:  0,
			SnapshotsByType: make(map[string]int64),
			Health:          common.HealthStatusHealthy,
			ConnectionInfo: map[string]interface{}{
				"type":            "mongodb",
				"connection_name": connectionName,
				"database":        dbName,
			},
			Metrics: &eventscore.EventStoreMetrics{
				EventsSaved:      0,
				EventsRead:       0,
				SnapshotsCreated: 0,
				SnapshotsRead:    0,
				Errors:           0,
			},
		},
	}

	// Create indexes
	if err := store.createIndexes(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	// Initialize statistics
	if err := store.initializeStats(context.Background()); err != nil {
		l.Warn("failed to initialize statistics", logger.Error(err))
	}

	return store, nil
}

// createIndexes creates necessary database indexes for performance
func (mes *MongoEventStore) createIndexes(ctx context.Context) error {
	// Events collection indexes
	eventIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "aggregate_id", Value: 1},
				{Key: "version", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
		{
			Keys: bson.D{
				{Key: "type", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{{Key: "source", Value: 1}},
		},
	}

	if _, err := mes.eventsCollection.Indexes().CreateMany(ctx, eventIndexes); err != nil {
		return fmt.Errorf("failed to create event indexes: %w", err)
	}

	// Snapshots collection indexes
	snapshotIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "aggregate_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
	}

	if _, err := mes.snapsCollection.Indexes().CreateMany(ctx, snapshotIndexes); err != nil {
		return fmt.Errorf("failed to create snapshot indexes: %w", err)
	}

	return nil
}

// initializeStats loads initial statistics from database
func (mes *MongoEventStore) initializeStats(ctx context.Context) error {
	// Count total events
	totalEvents, err := mes.eventsCollection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return err
	}
	mes.stats.TotalEvents = totalEvents

	// Count events by type
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$type"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := mes.eventsCollection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var result struct {
			Type  string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			continue
		}
		mes.stats.EventsByType[result.Type] = result.Count
	}

	// Count total snapshots
	totalSnapshots, err := mes.snapsCollection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return err
	}
	mes.stats.TotalSnapshots = totalSnapshots

	// Count snapshots by type
	cursor, err = mes.snapsCollection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var result struct {
			Type  string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			continue
		}
		mes.stats.SnapshotsByType[result.Type] = result.Count
	}

	// Get oldest and newest event timestamps
	var oldest, newest MongoEvent
	if err := mes.eventsCollection.FindOne(ctx, bson.D{}, options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: 1}})).Decode(&oldest); err == nil {
		mes.stats.OldestEvent = &oldest.Timestamp
	}
	if err := mes.eventsCollection.FindOne(ctx, bson.D{}, options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: -1}})).Decode(&newest); err == nil {
		mes.stats.NewestEvent = &newest.Timestamp
	}

	return nil
}

// SaveEvent saves a single event
func (mes *MongoEventStore) SaveEvent(ctx context.Context, event *eventscore.Event) error {
	if err := event.Validate(); err != nil {
		mes.recordError()
		return fmt.Errorf("invalid event: %w", err)
	}

	start := time.Now()

	mongoEvent := mes.convertEventToMongo(event)

	if _, err := mes.eventsCollection.InsertOne(ctx, mongoEvent); err != nil {
		mes.recordError()
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Update statistics
	mes.stats.TotalEvents++
	mes.stats.EventsByType[event.Type]++
	mes.stats.Metrics.EventsSaved++

	// Update oldest/newest timestamps
	if mes.stats.OldestEvent == nil || event.Timestamp.Before(*mes.stats.OldestEvent) {
		mes.stats.OldestEvent = &event.Timestamp
	}
	if mes.stats.NewestEvent == nil || event.Timestamp.After(*mes.stats.NewestEvent) {
		mes.stats.NewestEvent = &event.Timestamp
	}

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.AverageWriteTime = (mes.stats.Metrics.AverageWriteTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved", "store", "mongodb").Inc()
		mes.metrics.Histogram("forge.events.store.save_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("event saved to mongodb store",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.String("aggregate_id", event.AggregateID),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// SaveEvents saves multiple events atomically
func (mes *MongoEventStore) SaveEvents(ctx context.Context, events []*eventscore.Event) error {
	if len(events) == 0 {
		return nil
	}

	start := time.Now()

	// Convert all events
	mongoEvents := make([]interface{}, len(events))
	for i, event := range events {
		if err := event.Validate(); err != nil {
			mes.recordError()
			return fmt.Errorf("invalid event %s: %w", event.ID, err)
		}
		mongoEvents[i] = mes.convertEventToMongo(event)
	}

	// Use transaction for atomicity
	err := mes.connection.Transaction(ctx, func(tx any) error {
		_, err := mes.eventsCollection.InsertMany(ctx, mongoEvents)
		return err
	})

	if err != nil {
		mes.recordError()
		return fmt.Errorf("failed to save events in transaction: %w", err)
	}

	// Update statistics
	mes.stats.TotalEvents += int64(len(events))
	mes.stats.Metrics.EventsSaved += int64(len(events))

	for _, event := range events {
		mes.stats.EventsByType[event.Type]++

		// Update oldest/newest timestamps
		if mes.stats.OldestEvent == nil || event.Timestamp.Before(*mes.stats.OldestEvent) {
			mes.stats.OldestEvent = &event.Timestamp
		}
		if mes.stats.NewestEvent == nil || event.Timestamp.After(*mes.stats.NewestEvent) {
			mes.stats.NewestEvent = &event.Timestamp
		}
	}

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.AverageWriteTime = (mes.stats.Metrics.AverageWriteTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved", "store", "mongodb").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.batch_save_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("events saved to mongodb store",
			logger.Int("count", len(events)),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetEvent retrieves a single event by ID
func (mes *MongoEventStore) GetEvent(ctx context.Context, eventID string) (*eventscore.Event, error) {
	start := time.Now()

	var mongoEvent MongoEvent
	if err := mes.eventsCollection.FindOne(ctx, bson.D{{Key: "_id", Value: eventID}}).Decode(&mongoEvent); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("event with ID %s not found", eventID)
		}
		mes.recordError()
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	event := mes.convertMongoToEvent(&mongoEvent)

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.EventsRead++
	mes.stats.Metrics.AverageReadTime = (mes.stats.Metrics.AverageReadTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Inc()
		mes.metrics.Histogram("forge.events.store.read_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	return event, nil
}

// GetEvents retrieves events by various criteria
func (mes *MongoEventStore) GetEvents(ctx context.Context, criteria eventscore.EventCriteria) (*eventscore.EventCollection, error) {
	if err := criteria.Validate(); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("invalid criteria: %w", err)
	}

	start := time.Now()

	// Build query filter
	filter := mes.buildFilter(criteria)

	// Count total records
	total, err := mes.eventsCollection.CountDocuments(ctx, filter)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to count events: %w", err)
	}

	// Build find options with sorting and pagination
	findOptions := options.Find()
	findOptions.SetSort(mes.buildSort(criteria.SortBy, criteria.SortOrder))
	findOptions.SetSkip(criteria.Offset)
	findOptions.SetLimit(int64(criteria.Limit))

	cursor, err := mes.eventsCollection.Find(ctx, filter, findOptions)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEvents []MongoEvent
	if err := cursor.All(ctx, &mongoEvents); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	// Convert to events
	events := make([]eventscore.Event, len(mongoEvents))
	for i, mongoEvent := range mongoEvents {
		events[i] = *mes.convertMongoToEvent(&mongoEvent)
	}

	collection := eventscore.NewEventCollection(events, int(total), criteria.Offset, criteria.Limit)

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.EventsRead += int64(len(events))
	mes.stats.Metrics.AverageReadTime = (mes.stats.Metrics.AverageReadTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.query_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	return collection, nil
}

// GetEventsByAggregate retrieves all events for a specific aggregate
func (mes *MongoEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*eventscore.Event, error) {
	start := time.Now()

	filter := bson.D{{Key: "aggregate_id", Value: aggregateID}}
	if fromVersion > 0 {
		filter = append(filter, bson.E{Key: "version", Value: bson.D{{Key: "$gte", Value: fromVersion}}})
	}

	findOptions := options.Find().SetSort(bson.D{{Key: "version", Value: 1}})

	cursor, err := mes.eventsCollection.Find(ctx, filter, findOptions)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to get events for aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEvents []MongoEvent
	if err := cursor.All(ctx, &mongoEvents); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	events := make([]*eventscore.Event, len(mongoEvents))
	for i, mongoEvent := range mongoEvents {
		events[i] = mes.convertMongoToEvent(&mongoEvent)
	}

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.aggregate_query_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	return events, nil
}

// GetEventsByType retrieves events of a specific type
func (mes *MongoEventStore) GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*eventscore.Event, error) {
	filter := bson.D{{Key: "type", Value: eventType}}
	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetSkip(offset).
		SetLimit(int64(limit))

	cursor, err := mes.eventsCollection.Find(ctx, filter, findOptions)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to get events by type: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEvents []MongoEvent
	if err := cursor.All(ctx, &mongoEvents); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	events := make([]*eventscore.Event, len(mongoEvents))
	for i, mongoEvent := range mongoEvents {
		events[i] = mes.convertMongoToEvent(&mongoEvent)
	}

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsSince retrieves events since a specific timestamp
func (mes *MongoEventStore) GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	filter := bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}}}
	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: 1}}).
		SetSkip(offset).
		SetLimit(int64(limit))

	cursor, err := mes.eventsCollection.Find(ctx, filter, findOptions)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to get events since timestamp: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEvents []MongoEvent
	if err := cursor.All(ctx, &mongoEvents); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	events := make([]*eventscore.Event, len(mongoEvents))
	for i, mongoEvent := range mongoEvents {
		events[i] = mes.convertMongoToEvent(&mongoEvent)
	}

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsInRange retrieves events within a time range
func (mes *MongoEventStore) GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	filter := bson.D{
		{Key: "timestamp", Value: bson.D{
			{Key: "$gte", Value: start},
			{Key: "$lte", Value: end},
		}},
	}
	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: 1}}).
		SetSkip(offset).
		SetLimit(int64(limit))

	cursor, err := mes.eventsCollection.Find(ctx, filter, findOptions)
	if err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to get events in range: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEvents []MongoEvent
	if err := cursor.All(ctx, &mongoEvents); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	events := make([]*eventscore.Event, len(mongoEvents))
	for i, mongoEvent := range mongoEvents {
		events[i] = mes.convertMongoToEvent(&mongoEvent)
	}

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Add(float64(len(events)))
	}

	return events, nil
}

// DeleteEvent soft deletes an event by ID
func (mes *MongoEventStore) DeleteEvent(ctx context.Context, eventID string) error {
	filter := bson.D{{Key: "_id", Value: eventID}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "metadata._deleted", Value: true},
		{Key: "metadata._deleted_at", Value: time.Now()},
		{Key: "updated_at", Value: time.Now()},
	}}}

	result, err := mes.eventsCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		mes.recordError()
		return fmt.Errorf("failed to delete event: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("event with ID %s not found", eventID)
	}

	if mes.logger != nil {
		mes.logger.Debug("event marked as deleted",
			logger.String("event_id", eventID),
		)
	}

	return nil
}

// DeleteEventsByAggregate soft deletes all events for an aggregate
func (mes *MongoEventStore) DeleteEventsByAggregate(ctx context.Context, aggregateID string) error {
	filter := bson.D{{Key: "aggregate_id", Value: aggregateID}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "metadata._deleted", Value: true},
		{Key: "metadata._deleted_at", Value: time.Now()},
		{Key: "updated_at", Value: time.Now()},
	}}}

	result, err := mes.eventsCollection.UpdateMany(ctx, filter, update)
	if err != nil {
		mes.recordError()
		return fmt.Errorf("failed to delete events for aggregate: %w", err)
	}

	if mes.logger != nil {
		mes.logger.Debug("events marked as deleted for aggregate",
			logger.String("aggregate_id", aggregateID),
			logger.Int64("count", result.ModifiedCount),
		)
	}

	return nil
}

// GetLastEvent gets the last event for an aggregate
func (mes *MongoEventStore) GetLastEvent(ctx context.Context, aggregateID string) (*eventscore.Event, error) {
	filter := bson.D{{Key: "aggregate_id", Value: aggregateID}}
	findOptions := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})

	var mongoEvent MongoEvent
	if err := mes.eventsCollection.FindOne(ctx, filter, findOptions).Decode(&mongoEvent); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("no events found for aggregate %s", aggregateID)
		}
		mes.recordError()
		return nil, fmt.Errorf("failed to get last event: %w", err)
	}

	event := mes.convertMongoToEvent(&mongoEvent)

	mes.stats.Metrics.EventsRead++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "mongodb").Inc()
	}

	return event, nil
}

// GetEventCount gets the total count of events
func (mes *MongoEventStore) GetEventCount(ctx context.Context) (int64, error) {
	count, err := mes.eventsCollection.CountDocuments(ctx, bson.D{})
	if err != nil {
		mes.recordError()
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// GetEventCountByType gets the count of events by type
func (mes *MongoEventStore) GetEventCountByType(ctx context.Context, eventType string) (int64, error) {
	filter := bson.D{{Key: "type", Value: eventType}}
	count, err := mes.eventsCollection.CountDocuments(ctx, filter)
	if err != nil {
		mes.recordError()
		return 0, fmt.Errorf("failed to count events by type: %w", err)
	}
	return count, nil
}

// CreateSnapshot creates a snapshot of an aggregate's state
func (mes *MongoEventStore) CreateSnapshot(ctx context.Context, snapshot *eventscore.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		mes.recordError()
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	start := time.Now()

	mongoSnapshot := mes.convertSnapshotToMongo(snapshot)

	// Use upsert to handle conflicts
	filter := bson.D{{Key: "aggregate_id", Value: snapshot.AggregateID}}
	update := bson.D{{Key: "$set", Value: mongoSnapshot}}
	options := options.Update().SetUpsert(true)

	if _, err := mes.snapsCollection.UpdateOne(ctx, filter, update, options); err != nil {
		mes.recordError()
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Update statistics
	mes.stats.TotalSnapshots++
	mes.stats.SnapshotsByType[snapshot.Type]++
	mes.stats.Metrics.SnapshotsCreated++

	// Record metrics
	duration := time.Since(start)

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_created", "store", "mongodb").Inc()
		mes.metrics.Histogram("forge.events.store.snapshot_save_duration", "store", "mongodb").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("snapshot created",
			logger.String("snapshot_id", snapshot.ID),
			logger.String("aggregate_id", snapshot.AggregateID),
			logger.String("type", snapshot.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetSnapshot retrieves the latest snapshot for an aggregate
func (mes *MongoEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*eventscore.Snapshot, error) {
	filter := bson.D{{Key: "aggregate_id", Value: aggregateID}}

	var mongoSnapshot MongoSnapshot
	if err := mes.snapsCollection.FindOne(ctx, filter).Decode(&mongoSnapshot); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("no snapshot found for aggregate %s", aggregateID)
		}
		mes.recordError()
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	snapshot := mes.convertMongoToSnapshot(&mongoSnapshot)

	mes.stats.Metrics.SnapshotsRead++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_read", "store", "mongodb").Inc()
	}

	return snapshot, nil
}

// DeleteSnapshot deletes a snapshot
func (mes *MongoEventStore) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	filter := bson.D{{Key: "_id", Value: snapshotID}}

	result, err := mes.snapsCollection.DeleteOne(ctx, filter)
	if err != nil {
		mes.recordError()
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("snapshot with ID %s not found", snapshotID)
	}

	mes.stats.TotalSnapshots--

	if mes.logger != nil {
		mes.logger.Debug("snapshot deleted",
			logger.String("snapshot_id", snapshotID),
		)
	}

	return nil
}

// Close closes the event store connection
func (mes *MongoEventStore) Close(ctx context.Context) error {
	// The database connection is managed by the database package
	// so we don't need to close it here
	if mes.logger != nil {
		mes.logger.Info("mongodb event store closed")
	}
	return nil
}

// HealthCheck checks if the event store is healthy
func (mes *MongoEventStore) HealthCheck(ctx context.Context) error {
	if err := mes.connection.OnHealthCheck(ctx); err != nil {
		mes.stats.Health = common.HealthStatusUnhealthy
		return fmt.Errorf("database connection unhealthy: %w", err)
	}

	// Test a simple ping to the database
	if err := mes.client.Ping(ctx, nil); err != nil {
		mes.stats.Health = common.HealthStatusUnhealthy
		mes.recordError()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	mes.stats.Health = common.HealthStatusHealthy
	return nil
}

// GetStats returns event store statistics
func (mes *MongoEventStore) GetStats() *eventscore.EventStoreStats {
	// Update real-time statistics
	if err := mes.initializeStats(context.Background()); err != nil && mes.logger != nil {
		mes.logger.Warn("failed to update statistics", logger.Error(err))
	}

	// Create a copy to avoid external modifications
	statsCopy := *mes.stats
	statsCopy.EventsByType = make(map[string]int64)
	statsCopy.SnapshotsByType = make(map[string]int64)
	statsCopy.ConnectionInfo = make(map[string]interface{})

	for k, v := range mes.stats.EventsByType {
		statsCopy.EventsByType[k] = v
	}
	for k, v := range mes.stats.SnapshotsByType {
		statsCopy.SnapshotsByType[k] = v
	}
	for k, v := range mes.stats.ConnectionInfo {
		statsCopy.ConnectionInfo[k] = v
	}

	// Copy metrics
	if mes.stats.Metrics != nil {
		metricsCopy := *mes.stats.Metrics
		statsCopy.Metrics = &metricsCopy
	}

	return &statsCopy
}

// Helper methods

// convertEventToMongo converts an Event to MongoEvent
func (mes *MongoEventStore) convertEventToMongo(event *eventscore.Event) *MongoEvent {
	now := time.Now()
	return &MongoEvent{
		ID:          event.ID,
		AggregateID: event.AggregateID,
		Type:        event.Type,
		Version:     event.Version,
		Data:        event.Data,
		Metadata:    event.Metadata,
		Source:      event.Source,
		Timestamp:   event.Timestamp,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// convertMongoToEvent converts a MongoEvent to Event
func (mes *MongoEventStore) convertMongoToEvent(mongoEvent *MongoEvent) *eventscore.Event {
	return &eventscore.Event{
		ID:          mongoEvent.ID,
		AggregateID: mongoEvent.AggregateID,
		Type:        mongoEvent.Type,
		Version:     mongoEvent.Version,
		Data:        mongoEvent.Data,
		Metadata:    mongoEvent.Metadata,
		Source:      mongoEvent.Source,
		Timestamp:   mongoEvent.Timestamp,
	}
}

// convertSnapshotToMongo converts a Snapshot to MongoSnapshot
func (mes *MongoEventStore) convertSnapshotToMongo(snapshot *eventscore.Snapshot) *MongoSnapshot {
	now := time.Now()
	return &MongoSnapshot{
		ID:          snapshot.ID,
		AggregateID: snapshot.AggregateID,
		Type:        snapshot.Type,
		Version:     snapshot.Version,
		Data:        snapshot.Data,
		Metadata:    snapshot.Metadata,
		Timestamp:   snapshot.Timestamp,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// convertMongoToSnapshot converts a MongoSnapshot to Snapshot
func (mes *MongoEventStore) convertMongoToSnapshot(mongoSnapshot *MongoSnapshot) *eventscore.Snapshot {
	return &eventscore.Snapshot{
		ID:          mongoSnapshot.ID,
		AggregateID: mongoSnapshot.AggregateID,
		Type:        mongoSnapshot.Type,
		Version:     mongoSnapshot.Version,
		Data:        mongoSnapshot.Data,
		Metadata:    mongoSnapshot.Metadata,
		Timestamp:   mongoSnapshot.Timestamp,
	}
}

// buildFilter builds MongoDB filter from event criteria
func (mes *MongoEventStore) buildFilter(criteria eventscore.EventCriteria) bson.D {
	filter := bson.D{}

	// Filter by event types
	if len(criteria.EventTypes) > 0 {
		filter = append(filter, bson.E{Key: "type", Value: bson.D{{Key: "$in", Value: criteria.EventTypes}}})
	}

	// Filter by aggregate IDs
	if len(criteria.AggregateIDs) > 0 {
		filter = append(filter, bson.E{Key: "aggregate_id", Value: bson.D{{Key: "$in", Value: criteria.AggregateIDs}}})
	}

	// Filter by time range
	if criteria.StartTime != nil || criteria.EndTime != nil {
		timeFilter := bson.D{}
		if criteria.StartTime != nil {
			timeFilter = append(timeFilter, bson.E{Key: "$gte", Value: *criteria.StartTime})
		}
		if criteria.EndTime != nil {
			timeFilter = append(timeFilter, bson.E{Key: "$lte", Value: *criteria.EndTime})
		}
		filter = append(filter, bson.E{Key: "timestamp", Value: timeFilter})
	}

	// Filter by sources
	if len(criteria.Sources) > 0 {
		filter = append(filter, bson.E{Key: "source", Value: bson.D{{Key: "$in", Value: criteria.Sources}}})
	}

	// Filter by metadata
	for key, value := range criteria.Metadata {
		filter = append(filter, bson.E{Key: "metadata." + key, Value: value})
	}

	// Exclude deleted events
	filter = append(filter, bson.E{Key: "metadata._deleted", Value: bson.D{{Key: "$ne", Value: true}}})

	return filter
}

// buildSort builds MongoDB sort from criteria
func (mes *MongoEventStore) buildSort(sortBy, sortOrder string) bson.D {
	if sortBy == "" {
		sortBy = "timestamp"
	}

	order := 1 // ascending
	if sortOrder == "desc" {
		order = -1
	}

	return bson.D{{Key: sortBy, Value: order}}
}

// recordError increments the error count
func (mes *MongoEventStore) recordError() {
	mes.stats.Metrics.Errors++
	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.errors", "store", "mongodb").Inc()
	}
}
