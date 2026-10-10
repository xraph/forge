package stores

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
	"go.mongodb.org/mongo-driver/bson"
)

var errRowInterrupted = errors.New("row stream interrupted")

type interruptedConnector struct{}

func (interruptedConnector) Connect(context.Context) (driver.Conn, error) {
	return interruptedConnection{}, nil
}
func (interruptedConnector) Driver() driver.Driver { return interruptedDriver{} }

type interruptedDriver struct{}

func (interruptedDriver) Open(string) (driver.Conn, error) { return interruptedConnection{}, nil }

type interruptedConnection struct{}

func (interruptedConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (interruptedConnection) Close() error              { return nil }
func (interruptedConnection) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (interruptedConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	return &interruptedRows{count: strings.HasPrefix(query, "SELECT COUNT(*)")}, nil
}

type interruptedRows struct{ count, read bool }

func (*interruptedRows) Columns() []string { return []string{"value"} }
func (*interruptedRows) Close() error      { return nil }
func (r *interruptedRows) Next(values []driver.Value) error {
	if !r.count {
		return errRowInterrupted
	}

	if r.read {
		return io.EOF
	}

	values[0] = int64(0)
	r.read = true

	return nil
}

func TestPostgresPropagatesRowIterationFailure(t *testing.T) {
	db := sql.OpenDB(interruptedConnector{})

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	store := &PostgresEventStore{db: db, stats: &core.EventStoreStats{EventsByType: map[string]int64{}, Metrics: &core.EventStoreMetrics{}}}

	queries := map[string]func() error{
		"statistics": func() error { return store.initializeStats(context.Background()) },
		"aggregate": func() error {
			events, err := store.GetEventsByAggregate(context.Background(), "trade-1", 0)
			require.Nil(t, events)

			return err
		},
		"type": func() error {
			events, err := store.GetEventsByType(context.Background(), "trade.committed", time.Time{}, time.Now())
			require.Nil(t, events)

			return err
		},
		"unbounded criteria": func() error {
			events, err := store.QueryEvents(context.Background(), &core.EventCriteria{Limit: 5})
			require.Nil(t, events)

			return err
		},
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, query(), errRowInterrupted) })
	}
}

func TestMongoFilterAcceptsIndependentTimeBounds(t *testing.T) {
	start, end := time.Now().Add(-time.Hour), time.Now()
	for name, criteria := range map[string]*core.EventCriteria{
		"no bounds": {}, "start only": {StartTime: &start}, "end only": {EndTime: &end}, "both": {StartTime: &start, EndTime: &end},
	} {
		t.Run(name, func(t *testing.T) {
			filter := mongoEventFilter(criteria)
			if criteria.StartTime == nil && criteria.EndTime == nil {
				require.Empty(t, filter)

				return
			}

			require.Len(t, filter, 1)
			require.Equal(t, "timestamp", filter[0].Key)
			bounds := filter[0].Value.(bson.D)

			expected := bson.D{}
			if criteria.StartTime != nil {
				expected = append(expected, bson.E{Key: "$gte", Value: criteria.StartTime})
			}

			if criteria.EndTime != nil {
				expected = append(expected, bson.E{Key: "$lte", Value: criteria.EndTime})
			}

			require.Equal(t, expected, bounds)
		})
	}
}
