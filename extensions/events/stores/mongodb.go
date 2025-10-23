package stores

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// MongoEventStore implements EventStore using MongoDB
type MongoEventStore struct {
	client       *mongo.Client
	database     *mongo.Database
	eventsCol    *mongo.Collection
	snapshotsCol *mongo.Collection
	logger       forge.Logger
	metrics      forge.Metrics
	config       *core.EventStoreConfig
	stats        *core.EventStoreStats
}

// MongoEvent represents an event document in MongoDB
type MongoEvent struct {
	ID          string                 `bson:"_id"`
	AggregateID string                 `bson:"aggregate_id"`
	Type        string                 `bson:"type"`
	Version     int                    `bson:"version"`
	Data        map[string]interface{} `bson:"data"`
	Metadata    map[string]interface{} `bson:"metadata,omitempty"`
	Source      string                 `bson:"source,omitempty"`
	Timestamp   time.Time              `bson:"timestamp"`
	CreatedAt   time.Time              `bson:"created_at"`
}

// MongoSnapshot represents a snapshot document in MongoDB
type MongoSnapshot struct {
	ID          string                 `bson:"_id"`
	AggregateID string                 `bson:"aggregate_id"`
	Type        string                 `bson:"type"`
	Version     int                    `bson:"version"`
	Data        map[string]interface{} `bson:"data"`
	Metadata    map[string]interface{} `bson:"metadata,omitempty"`
	Timestamp   time.Time              `bson:"timestamp"`
	CreatedAt   time.Time              `bson:"created_at"`
}

// NewMongoEventStore creates a new MongoDB event store
func NewMongoEventStore(client *mongo.Client, dbName string, config *core.EventStoreConfig, logger forge.Logger, metrics forge.Metrics) (*MongoEventStore, error) {
	database := client.Database(dbName)
	eventsCol := database.Collection("events")
	snapshotsCol := database.Collection("snapshots")

	store := &MongoEventStore{
		client:       client,
		database:     database,
		eventsCol:    eventsCol,
		snapshotsCol: snapshotsCol,
		logger:       logger,
		metrics:      metrics,
		config:       config,
		stats: &core.EventStoreStats{
			TotalEvents:     0,
			EventsByType:    make(map[string]int64),
			TotalSnapshots:  0,
			SnapshotsByType: make(map[string]int64),
			Metrics: &core.EventStoreMetrics{
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

	// Initialize stats
	if err := store.initializeStats(context.Background()); err != nil {
		if logger != nil {
			logger.Warn("failed to initialize statistics", forge.F("error", err))
		}
	}

	return store, nil
}

// createIndexes creates necessary indexes
func (mes *MongoEventStore) createIndexes(ctx context.Context) error {
	// Events indexes
	eventIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "aggregate_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
		{
			Keys: bson.D{
				{Key: "aggregate_id", Value: 1},
				{Key: "version", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err := mes.eventsCol.Indexes().CreateMany(ctx, eventIndexes)
	if err != nil {
		return fmt.Errorf("failed to create event indexes: %w", err)
	}

	// Snapshots indexes
	snapshotIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "aggregate_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
	}

	_, err = mes.snapshotsCol.Indexes().CreateMany(ctx, snapshotIndexes)
	if err != nil {
		return fmt.Errorf("failed to create snapshot indexes: %w", err)
	}

	return nil
}

// initializeStats loads current statistics
func (mes *MongoEventStore) initializeStats(ctx context.Context) error {
	// Count total events
	count, err := mes.eventsCol.CountDocuments(ctx, bson.D{})
	if err != nil {
		return err
	}
	mes.stats.TotalEvents = count

	// Count events by type
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$type"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := mes.eventsCol.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var result struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			return err
		}
		mes.stats.EventsByType[result.ID] = result.Count
	}

	// Count total snapshots
	count, err = mes.snapshotsCol.CountDocuments(ctx, bson.D{})
	if err != nil {
		return err
	}
	mes.stats.TotalSnapshots = count

	return nil
}

// SaveEvent implements EventStore
func (mes *MongoEventStore) SaveEvent(ctx context.Context, event *core.Event) error {
	start := time.Now()

	doc := MongoEvent{
		ID:          event.ID,
		AggregateID: event.AggregateID,
		Type:        event.Type,
		Version:     event.Version,
		Data:        event.Data,
		Metadata:    event.Metadata,
		Source:      event.Source,
		Timestamp:   event.Timestamp,
		CreatedAt:   time.Now(),
	}

	_, err := mes.eventsCol.InsertOne(ctx, doc)
	if err != nil {
		mes.stats.Metrics.Errors++
		if mes.metrics != nil {
			mes.metrics.Counter("forge.events.store.save_errors").Inc()
		}
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Update stats
	mes.stats.Metrics.EventsSaved++
	mes.stats.TotalEvents++
	mes.stats.EventsByType[event.Type]++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved").Inc()
		mes.metrics.Histogram("forge.events.store.save_duration").Observe(time.Since(start).Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("event saved to MongoDB", forge.F("event_id", event.ID), forge.F("aggregate_id", event.AggregateID), forge.F("type", event.Type))
	}

	return nil
}

// SaveEvents implements EventStore
func (mes *MongoEventStore) SaveEvents(ctx context.Context, events []*core.Event) error {
	if len(events) == 0 {
		return nil
	}

	docs := make([]interface{}, len(events))
	for i, event := range events {
		docs[i] = MongoEvent{
			ID:          event.ID,
			AggregateID: event.AggregateID,
			Type:        event.Type,
			Version:     event.Version,
			Data:        event.Data,
			Metadata:    event.Metadata,
			Source:      event.Source,
			Timestamp:   event.Timestamp,
			CreatedAt:   time.Now(),
		}
		mes.stats.EventsByType[event.Type]++
	}

	_, err := mes.eventsCol.InsertMany(ctx, docs)
	if err != nil {
		mes.stats.Metrics.Errors++
		return fmt.Errorf("failed to save events: %w", err)
	}

	mes.stats.Metrics.EventsSaved += int64(len(events))
	mes.stats.TotalEvents += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved").Add(float64(len(events)))
	}

	return nil
}

// GetEvent implements EventStore
func (mes *MongoEventStore) GetEvent(ctx context.Context, eventID string) (*core.Event, error) {
	var doc MongoEvent
	err := mes.eventsCol.FindOne(ctx, bson.D{{Key: "_id", Value: eventID}}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("event not found: %s", eventID)
	}
	if err != nil {
		mes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	event := mes.docToEvent(&doc)
	mes.stats.Metrics.EventsRead++
	return event, nil
}

// GetEventsByAggregate implements EventStore
func (mes *MongoEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*core.Event, error) {
	filter := bson.D{
		{Key: "aggregate_id", Value: aggregateID},
		{Key: "version", Value: bson.D{{Key: "$gte", Value: fromVersion}}},
	}

	opts := options.Find().SetSort(bson.D{{Key: "version", Value: 1}})
	cursor, err := mes.eventsCol.Find(ctx, filter, opts)
	if err != nil {
		mes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer cursor.Close(ctx)

	events := make([]*core.Event, 0)
	for cursor.Next(ctx) {
		var doc MongoEvent
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode event: %w", err)
		}
		events = append(events, mes.docToEvent(&doc))
	}

	mes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// GetEventsByType implements EventStore
func (mes *MongoEventStore) GetEventsByType(ctx context.Context, eventType string, fromTime, toTime time.Time) ([]*core.Event, error) {
	filter := bson.D{
		{Key: "type", Value: eventType},
		{Key: "timestamp", Value: bson.D{
			{Key: "$gte", Value: fromTime},
			{Key: "$lte", Value: toTime},
		}},
	}

	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})
	cursor, err := mes.eventsCol.Find(ctx, filter, opts)
	if err != nil {
		mes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer cursor.Close(ctx)

	events := make([]*core.Event, 0)
	for cursor.Next(ctx) {
		var doc MongoEvent
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode event: %w", err)
		}
		events = append(events, mes.docToEvent(&doc))
	}

	mes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// QueryEvents implements EventStore
func (mes *MongoEventStore) QueryEvents(ctx context.Context, criteria *core.EventCriteria) ([]*core.Event, error) {
	filter := bson.D{}

	if criteria.AggregateID != "" {
		filter = append(filter, bson.E{Key: "aggregate_id", Value: criteria.AggregateID})
	}
	if criteria.EventType != "" {
		filter = append(filter, bson.E{Key: "type", Value: criteria.EventType})
	}
	if !criteria.FromTime.IsZero() || !criteria.ToTime.IsZero() {
		timeFilter := bson.D{}
		if !criteria.FromTime.IsZero() {
			timeFilter = append(timeFilter, bson.E{Key: "$gte", Value: criteria.FromTime})
		}
		if !criteria.ToTime.IsZero() {
			timeFilter = append(timeFilter, bson.E{Key: "$lte", Value: criteria.ToTime})
		}
		filter = append(filter, bson.E{Key: "timestamp", Value: timeFilter})
	}

	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})
	if criteria.Limit > 0 {
		opts = opts.SetLimit(int64(criteria.Limit))
	}

	cursor, err := mes.eventsCol.Find(ctx, filter, opts)
	if err != nil {
		mes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer cursor.Close(ctx)

	events := make([]*core.Event, 0)
	for cursor.Next(ctx) {
		var doc MongoEvent
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode event: %w", err)
		}
		events = append(events, mes.docToEvent(&doc))
	}

	mes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// CreateSnapshot implements EventStore
func (mes *MongoEventStore) CreateSnapshot(ctx context.Context, snapshot *core.Snapshot) error {
	doc := MongoSnapshot{
		ID:          snapshot.ID,
		AggregateID: snapshot.AggregateID,
		Type:        snapshot.Type,
		Version:     snapshot.Version,
		Data:        snapshot.Data,
		Metadata:    snapshot.Metadata,
		Timestamp:   snapshot.Timestamp,
		CreatedAt:   time.Now(),
	}

	opts := options.Replace().SetUpsert(true)
	_, err := mes.snapshotsCol.ReplaceOne(ctx, bson.D{{Key: "aggregate_id", Value: snapshot.AggregateID}}, doc, opts)
	if err != nil {
		mes.stats.Metrics.Errors++
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	mes.stats.Metrics.SnapshotsCreated++
	mes.stats.TotalSnapshots++
	mes.stats.SnapshotsByType[snapshot.Type]++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_created").Inc()
	}

	return nil
}

// GetSnapshot implements EventStore
func (mes *MongoEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*core.Snapshot, error) {
	var doc MongoSnapshot
	err := mes.snapshotsCol.FindOne(ctx, bson.D{{Key: "aggregate_id", Value: aggregateID}}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("snapshot not found: %s", aggregateID)
	}
	if err != nil {
		mes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	snapshot := mes.docToSnapshot(&doc)
	mes.stats.Metrics.SnapshotsRead++
	return snapshot, nil
}

// GetStats implements EventStore
func (mes *MongoEventStore) GetStats() *core.EventStoreStats {
	return mes.stats
}

// Close implements EventStore
func (mes *MongoEventStore) Close(ctx context.Context) error {
	// Client is managed externally
	return nil
}

// docToEvent converts a MongoEvent to Event
func (mes *MongoEventStore) docToEvent(doc *MongoEvent) *core.Event {
	return &core.Event{
		ID:          doc.ID,
		AggregateID: doc.AggregateID,
		Type:        doc.Type,
		Version:     doc.Version,
		Data:        doc.Data,
		Metadata:    doc.Metadata,
		Source:      doc.Source,
		Timestamp:   doc.Timestamp,
	}
}

// docToSnapshot converts a MongoSnapshot to Snapshot
func (mes *MongoEventStore) docToSnapshot(doc *MongoSnapshot) *core.Snapshot {
	return &core.Snapshot{
		ID:          doc.ID,
		AggregateID: doc.AggregateID,
		Type:        doc.Type,
		Version:     doc.Version,
		Data:        doc.Data,
		Metadata:    doc.Metadata,
		Timestamp:   doc.Timestamp,
	}
}
