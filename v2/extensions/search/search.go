package search

import (
	"context"
	"time"
)

// Search represents a unified search interface supporting multiple backends
type Search interface {
	// Connection management
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error

	// Index management
	CreateIndex(ctx context.Context, name string, schema IndexSchema) error
	DeleteIndex(ctx context.Context, name string) error
	ListIndexes(ctx context.Context) ([]string, error)
	GetIndexInfo(ctx context.Context, name string) (*IndexInfo, error)

	// Document operations
	Index(ctx context.Context, index string, doc Document) error
	BulkIndex(ctx context.Context, index string, docs []Document) error
	Get(ctx context.Context, index string, id string) (*Document, error)
	Delete(ctx context.Context, index string, id string) error
	Update(ctx context.Context, index string, id string, doc Document) error

	// Search operations
	Search(ctx context.Context, query SearchQuery) (*SearchResults, error)
	Suggest(ctx context.Context, query SuggestQuery) (*SuggestResults, error)
	Autocomplete(ctx context.Context, query AutocompleteQuery) (*AutocompleteResults, error)

	// Analytics
	Stats(ctx context.Context) (*SearchStats, error)
}

// Document represents a searchable document
type Document struct {
	ID     string                 `json:"id"`
	Fields map[string]interface{} `json:"fields"`
}

// IndexSchema defines the structure of a search index
type IndexSchema struct {
	Fields       []FieldSchema          `json:"fields"`
	Settings     map[string]interface{} `json:"settings,omitempty"`
	Synonyms     []Synonym              `json:"synonyms,omitempty"`
	StopWords    []string               `json:"stop_words,omitempty"`
	Analyzers    map[string]Analyzer    `json:"analyzers,omitempty"`
	Ranking      *RankingConfig         `json:"ranking,omitempty"`
	Faceting     *FacetingConfig        `json:"faceting,omitempty"`
	Highlighting *HighlightConfig       `json:"highlighting,omitempty"`
}

// FieldSchema defines a single field in the index
type FieldSchema struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"` // text, keyword, integer, float, boolean, date, geo_point
	Required     bool        `json:"required"`
	Searchable   bool        `json:"searchable"`
	Filterable   bool        `json:"filterable"`
	Sortable     bool        `json:"sortable"`
	Faceted      bool        `json:"faceted"`
	Stored       bool        `json:"stored"`
	Index        bool        `json:"index"`
	Boost        float64     `json:"boost,omitempty"`
	Analyzer     string      `json:"analyzer,omitempty"`
	Format       string      `json:"format,omitempty"` // For dates
	Locale       string      `json:"locale,omitempty"`
	DefaultValue interface{} `json:"default_value,omitempty"`
}

// Synonym defines term synonyms
type Synonym struct {
	Terms []string `json:"terms"`
}

// Analyzer defines text analysis configuration
type Analyzer struct {
	Type        string   `json:"type"` // standard, simple, whitespace, keyword, pattern
	Tokenizer   string   `json:"tokenizer,omitempty"`
	Filters     []string `json:"filters,omitempty"`
	CharFilters []string `json:"char_filters,omitempty"`
}

// RankingConfig defines ranking rules
type RankingConfig struct {
	Rules   []string           `json:"rules"`
	Weights map[string]float64 `json:"weights,omitempty"`
}

// FacetingConfig defines faceting options
type FacetingConfig struct {
	MaxValues int    `json:"max_values"`
	Sort      string `json:"sort"` // count, name
}

// HighlightConfig defines highlighting options
type HighlightConfig struct {
	PreTag  string `json:"pre_tag"`
	PostTag string `json:"post_tag"`
}

// IndexInfo contains metadata about an index
type IndexInfo struct {
	Name          string      `json:"name"`
	DocumentCount int64       `json:"document_count"`
	IndexSize     int64       `json:"index_size"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Schema        IndexSchema `json:"schema"`
}

// SearchQuery represents a search request
type SearchQuery struct {
	Index           string                 `json:"index"`
	Query           string                 `json:"query"`
	Filters         []Filter               `json:"filters,omitempty"`
	Sort            []SortField            `json:"sort,omitempty"`
	Facets          []string               `json:"facets,omitempty"`
	Offset          int                    `json:"offset,omitempty"`
	Limit           int                    `json:"limit,omitempty"`
	Highlight       bool                   `json:"highlight,omitempty"`
	HighlightFields []string               `json:"highlight_fields,omitempty"`
	Fields          []string               `json:"fields,omitempty"` // Fields to return
	MinScore        float64                `json:"min_score,omitempty"`
	BoostFields     map[string]float64     `json:"boost_fields,omitempty"`
	FuzzyLevel      int                    `json:"fuzzy_level,omitempty"` // 0=exact, 1-2=fuzzy
	Options         map[string]interface{} `json:"options,omitempty"`
}

// Filter represents a search filter
type Filter struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // =, !=, >, >=, <, <=, IN, NOT IN, BETWEEN, EXISTS
	Value    interface{} `json:"value"`
}

// SortField defines sorting
type SortField struct {
	Field string `json:"field"`
	Order string `json:"order"` // asc, desc
}

// SearchResults contains search response
type SearchResults struct {
	Hits           []Hit              `json:"hits"`
	Total          int64              `json:"total"`
	Offset         int                `json:"offset"`
	Limit          int                `json:"limit"`
	ProcessingTime time.Duration      `json:"processing_time_ms"`
	Facets         map[string][]Facet `json:"facets,omitempty"`
	Query          string             `json:"query"`
	Exhaustive     bool               `json:"exhaustive"`
}

// Hit represents a single search result
type Hit struct {
	ID         string                 `json:"id"`
	Score      float64                `json:"score"`
	Document   map[string]interface{} `json:"document"`
	Highlights map[string][]string    `json:"highlights,omitempty"`
}

// Facet represents a facet value
type Facet struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// SuggestQuery represents a suggestion request
type SuggestQuery struct {
	Index string `json:"index"`
	Query string `json:"query"`
	Field string `json:"field"`
	Limit int    `json:"limit,omitempty"`
	Fuzzy bool   `json:"fuzzy,omitempty"`
}

// SuggestResults contains suggestion response
type SuggestResults struct {
	Suggestions    []Suggestion  `json:"suggestions"`
	ProcessingTime time.Duration `json:"processing_time_ms"`
}

// Suggestion represents a single suggestion
type Suggestion struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// AutocompleteQuery represents an autocomplete request
type AutocompleteQuery struct {
	Index string `json:"index"`
	Query string `json:"query"`
	Field string `json:"field"`
	Limit int    `json:"limit,omitempty"`
}

// AutocompleteResults contains autocomplete response
type AutocompleteResults struct {
	Completions    []Completion  `json:"completions"`
	ProcessingTime time.Duration `json:"processing_time_ms"`
}

// Completion represents a single completion
type Completion struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// SearchStats contains search engine statistics
type SearchStats struct {
	IndexCount    int64                  `json:"index_count"`
	DocumentCount int64                  `json:"document_count"`
	TotalSize     int64                  `json:"total_size"`
	Queries       int64                  `json:"queries"`
	AvgLatency    time.Duration          `json:"avg_latency_ms"`
	Uptime        time.Duration          `json:"uptime"`
	Version       string                 `json:"version"`
	Extra         map[string]interface{} `json:"extra,omitempty"`
}
