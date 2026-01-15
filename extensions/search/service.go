package search

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// SearchService wraps a Search implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type SearchService struct {
	config  Config
	backend Search
	logger  forge.Logger
	metrics forge.Metrics
}

// NewSearchService creates a new search service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewSearchService(config Config, logger forge.Logger, metrics forge.Metrics) (*SearchService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid search config: %w", err)
	}

	// Create search backend based on driver
	var (
		backend Search
		err     error
	)

	switch config.Driver {
	case "inmemory":
		backend = NewInMemorySearch(config, logger, metrics)

	case "elasticsearch":
		backend, err = NewElasticsearchSearch(config, logger, metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to create elasticsearch search: %w", err)
		}

	case "meilisearch":
		backend, err = NewMeilisearchSearch(config, logger, metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to create meilisearch search: %w", err)
		}

	case "typesense":
		backend, err = NewTypesenseSearch(config, logger, metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to create typesense search: %w", err)
		}

	default:
		return nil, fmt.Errorf("unknown search driver: %s", config.Driver)
	}

	return &SearchService{
		config:  config,
		backend: backend,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *SearchService) Name() string {
	return "search-service"
}

// Start starts the search service by connecting to the backend.
// This is called automatically by Vessel during container.Start().
func (s *SearchService) Start(ctx context.Context) error {
	s.logger.Info("starting search service",
		forge.F("driver", s.config.Driver),
	)

	if err := s.backend.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to search: %w", err)
	}

	s.logger.Info("search service started",
		forge.F("driver", s.config.Driver),
		forge.F("url", s.config.URL),
	)

	return nil
}

// Stop stops the search service by disconnecting from the backend.
// This is called automatically by Vessel during container.Stop().
func (s *SearchService) Stop(ctx context.Context) error {
	s.logger.Info("stopping search service")

	if s.backend != nil {
		if err := s.backend.Disconnect(ctx); err != nil {
			s.logger.Error("failed to disconnect search",
				forge.F("error", err),
			)
			// Don't return error, log and continue
		}
	}

	s.logger.Info("search service stopped")
	return nil
}

// Health checks if the search service is healthy.
// This is called by the health check system.
func (s *SearchService) Health(ctx context.Context) error {
	if s.backend == nil {
		return errors.New("search not initialized")
	}

	if err := s.backend.Ping(ctx); err != nil {
		return fmt.Errorf("search health check failed: %w", err)
	}

	return nil
}

// Backend returns the underlying search implementation.
// This allows other services to use the search directly.
func (s *SearchService) Backend() Search {
	return s.backend
}

// Ensure SearchService implements Search interface for convenience
// This allows SearchService to be used directly as a Search

func (s *SearchService) Connect(ctx context.Context) error {
	return s.backend.Connect(ctx)
}

func (s *SearchService) Disconnect(ctx context.Context) error {
	return s.backend.Disconnect(ctx)
}

func (s *SearchService) Ping(ctx context.Context) error {
	return s.backend.Ping(ctx)
}

func (s *SearchService) CreateIndex(ctx context.Context, name string, schema IndexSchema) error {
	return s.backend.CreateIndex(ctx, name, schema)
}

func (s *SearchService) DeleteIndex(ctx context.Context, name string) error {
	return s.backend.DeleteIndex(ctx, name)
}

func (s *SearchService) ListIndexes(ctx context.Context) ([]string, error) {
	return s.backend.ListIndexes(ctx)
}

func (s *SearchService) GetIndexInfo(ctx context.Context, name string) (*IndexInfo, error) {
	return s.backend.GetIndexInfo(ctx, name)
}

func (s *SearchService) Index(ctx context.Context, index string, doc Document) error {
	return s.backend.Index(ctx, index, doc)
}

func (s *SearchService) BulkIndex(ctx context.Context, index string, docs []Document) error {
	return s.backend.BulkIndex(ctx, index, docs)
}

func (s *SearchService) Get(ctx context.Context, index string, id string) (*Document, error) {
	return s.backend.Get(ctx, index, id)
}

func (s *SearchService) Delete(ctx context.Context, index string, id string) error {
	return s.backend.Delete(ctx, index, id)
}

func (s *SearchService) Update(ctx context.Context, index string, id string, doc Document) error {
	return s.backend.Update(ctx, index, id, doc)
}

func (s *SearchService) Search(ctx context.Context, query SearchQuery) (*SearchResults, error) {
	return s.backend.Search(ctx, query)
}

func (s *SearchService) Suggest(ctx context.Context, query SuggestQuery) (*SuggestResults, error) {
	return s.backend.Suggest(ctx, query)
}

func (s *SearchService) Autocomplete(ctx context.Context, query AutocompleteQuery) (*AutocompleteResults, error) {
	return s.backend.Autocomplete(ctx, query)
}

func (s *SearchService) Stats(ctx context.Context) (*SearchStats, error) {
	return s.backend.Stats(ctx)
}
