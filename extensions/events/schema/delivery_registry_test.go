package schema

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type failingSchemaStore struct {
	SchemaStore

	failure error
}

func (s *failingSchemaStore) GetSchemaVersions(context.Context, string) ([]*Schema, error) {
	return nil, s.failure
}
func (s *failingSchemaStore) DeleteSchema(context.Context, string) error { return s.failure }

func TestRegistrySurfacesPersistentVersionReadFailure(t *testing.T) {
	failure := errors.New("schema store unavailable")
	store := &failingSchemaStore{SchemaStore: NewMemorySchemaStore(), failure: failure}
	registry := NewSchemaRegistry(store, nil, &RegistryConfig{VersioningStrategy: "none", ValidationLevel: "disabled"}, nil, nil)
	schema := &Schema{ID: "schema-1", Name: "trade.committed", Version: 1, Type: "object", Properties: map[string]*Property{}}
	require.NoError(t, registry.RegisterSchema(context.Background(), schema))
	versions, err := registry.GetSchemaVersions(schema.Name)
	require.ErrorIs(t, err, failure)
	require.Equal(t, []*Schema{schema}, versions)
}

func TestRegistryRetainsOldVersionWhenPersistentDeleteFails(t *testing.T) {
	failure := errors.New("schema deletion failed")
	store := &failingSchemaStore{SchemaStore: NewMemorySchemaStore(), failure: failure}
	registry := NewSchemaRegistry(store, nil, &RegistryConfig{VersioningStrategy: "none", ValidationLevel: "disabled", MaxVersions: 1}, nil, nil)
	old := &Schema{ID: "schema-1", Name: "trade.committed", Version: 1, Type: "object", Properties: map[string]*Property{}}
	require.NoError(t, registry.RegisterSchema(context.Background(), old))
	next := &Schema{ID: "schema-2", Name: old.Name, Version: 2, Type: "object", Properties: map[string]*Property{}}
	require.ErrorIs(t, registry.RegisterSchema(context.Background(), next), failure)
	require.Same(t, old, registry.schemas[old.Name][1])
}

func TestSchemaCacheConcurrentReadsAndExpiredEntries(t *testing.T) {
	cache := NewMemorySchemaCache()
	cache.Set("expired", &Schema{ID: "old"}, -time.Second)
	cache.Set("live", &Schema{ID: "live"}, time.Hour)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				cache.Get("expired")
				cache.Get("live")
				cache.Stats()
			}
		})
	}

	wg.Wait()
	require.Equal(t, int64(2000), cache.Stats()["hits"])
	require.Equal(t, int64(2000), cache.Stats()["misses"])
	require.Equal(t, 1, cache.Stats()["entries"])
}
