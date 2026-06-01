package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// organism.data-grid — types
// =============================================================================

// ColumnType is the renderer hint for a DataGrid column. The React side
// uses this to format the cell value (currency, date, badge, etc.).
type ColumnType string

const (
	ColText     ColumnType = "text"
	ColNumber   ColumnType = "number"
	ColDate     ColumnType = "date"
	ColDateTime ColumnType = "datetime"
	ColCurrency ColumnType = "currency"
	ColBoolean  ColumnType = "boolean"
	ColBadge    ColumnType = "badge"
	ColAvatar   ColumnType = "avatar"
	ColLink     ColumnType = "link"
	ColActions  ColumnType = "actions"
	ColCustom   ColumnType = "custom"
)

// ColumnPin names where a column is fixed.
type ColumnPin string

const (
	PinNone  ColumnPin = ""
	PinLeft  ColumnPin = "left"
	PinRight ColumnPin = "right"
)

// Aggregation names the function used to summarize a grouped column.
type Aggregation string

const (
	AggSum   Aggregation = "sum"
	AggAvg   Aggregation = "avg"
	AggMin   Aggregation = "min"
	AggMax   Aggregation = "max"
	AggCount Aggregation = "count"
)

// FilterType names the column filter UI.
type FilterType string

const (
	FilterText        FilterType = "text"
	FilterNumberRange FilterType = "number-range"
	FilterDateRange   FilterType = "date-range"
	FilterSelect      FilterType = "select"
	FilterMultiSelect FilterType = "multi-select"
	FilterBoolean     FilterType = "boolean"
)

// CellRender names a custom cell renderer intent for Type=custom columns.
// The named intent receives `{ row, value, column }` as props.
type CellRender struct {
	Intent string         `json:"intent"`
	Props  map[string]any `json:"props,omitempty"`
}

// GridColumn defines one column in a DataGrid.
type GridColumn struct {
	ID            string        `json:"id"`
	Field         string        `json:"field"`
	Header        string        `json:"header"`
	Type          ColumnType    `json:"type,omitempty"`
	Format        string        `json:"format,omitempty"`
	Width         string        `json:"width,omitempty"`
	MinWidth      string        `json:"minWidth,omitempty"`
	MaxWidth      string        `json:"maxWidth,omitempty"`
	Sortable      bool          `json:"sortable,omitempty"`
	Filterable    bool          `json:"filterable,omitempty"`
	Hideable      bool          `json:"hideable,omitempty"`
	Resizable     bool          `json:"resizable,omitempty"`
	Pinned        ColumnPin     `json:"pinned,omitempty"`
	Aggregation   Aggregation   `json:"aggregation,omitempty"`
	Align         Align         `json:"align,omitempty"`
	Truncate      bool          `json:"truncate,omitempty"`
	Cell          *CellRender   `json:"cell,omitempty"`
	FilterType    FilterType    `json:"filterType,omitempty"`
	FilterOptions *OptionSource `json:"filterOptions,omitempty"`
	Description   string        `json:"description,omitempty"`
	Hidden        bool          `json:"hidden,omitempty"`
}

// SelectionMode names DataGrid selection behavior.
type SelectionMode string

const (
	SelectionNone   SelectionMode = "none"
	SelectionSingle SelectionMode = "single"
	SelectionMulti  SelectionMode = "multi"
)

// SelectionConfig configures row selection.
type SelectionConfig struct {
	Mode     SelectionMode `json:"mode"`
	OnChange *Action       `json:"onChange,omitempty"`
}

// SelectionMulti is a shorthand for { mode: multi }.
func SelectionMultiCfg() *SelectionConfig { return &SelectionConfig{Mode: SelectionMulti} }

// SortMode names sorting strategy.
type SortMode string

const (
	SortClient SortMode = "client"
	SortServer SortMode = "server"
)

// SortDir names sort direction.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// SortRule is one column's initial sort state.
type SortRule struct {
	Column    string  `json:"column"`
	Direction SortDir `json:"direction"`
}

// SortingConfig configures sort behavior.
type SortingConfig struct {
	Mode    SortMode   `json:"mode,omitempty"`
	Initial []SortRule `json:"initial,omitempty"`
	Multi   bool       `json:"multi,omitempty"`
}

// FilterMode names filter strategy.
type FilterMode string

const (
	FilterClient FilterMode = "client"
	FilterServer FilterMode = "server"
)

// FilteringConfig configures filter UI and behavior.
type FilteringConfig struct {
	Mode    FilterMode     `json:"mode,omitempty"`
	Global  bool           `json:"global,omitempty"`
	PerCol  bool           `json:"perCol,omitempty"`
	Default map[string]any `json:"default,omitempty"`
}

// PageMode names pagination strategy.
type PageMode string

const (
	PageClient PageMode = "client"
	PageServer PageMode = "server"
)

// PaginationConfig configures DataGrid pagination.
type PaginationConfig struct {
	Mode            PageMode `json:"mode,omitempty"`
	PageSize        int      `json:"pageSize,omitempty"`
	PageSizeOptions []int    `json:"pageSizeOptions,omitempty"`
	InitialPage     int      `json:"initialPage,omitempty"`
}

// ServerPagination returns a config preset for server-driven pagination.
func ServerPagination() *PaginationConfigBuilder {
	return &PaginationConfigBuilder{cfg: PaginationConfig{Mode: PageServer, PageSize: 25}}
}

// ClientPagination returns a config preset for client-driven pagination.
func ClientPagination() *PaginationConfigBuilder {
	return &PaginationConfigBuilder{cfg: PaginationConfig{Mode: PageClient, PageSize: 25}}
}

// PaginationConfigBuilder is a fluent builder for PaginationConfig.
type PaginationConfigBuilder struct {
	cfg PaginationConfig
}

func (b *PaginationConfigBuilder) PageSize(n int) *PaginationConfigBuilder {
	b.cfg.PageSize = n
	return b
}
func (b *PaginationConfigBuilder) PageSizeOptions(opts ...int) *PaginationConfigBuilder {
	b.cfg.PageSizeOptions = opts
	return b
}
func (b *PaginationConfigBuilder) InitialPage(n int) *PaginationConfigBuilder {
	b.cfg.InitialPage = n
	return b
}
func (b *PaginationConfigBuilder) Build() *PaginationConfig { c := b.cfg; return &c }

// GroupingConfig configures row grouping.
type GroupingConfig struct {
	By []string `json:"by,omitempty"` // column ids
}

// PinningConfig configures initial column pinning.
type PinningConfig struct {
	Left  []string `json:"left,omitempty"`
	Right []string `json:"right,omitempty"`
}

// VirtConfig configures row virtualization.
type VirtConfig struct {
	Enabled  bool `json:"enabled,omitempty"`
	Overscan int  `json:"overscan,omitempty"`
}

// Density names DataGrid row spacing.
type Density string

const (
	DensityCompact  Density = "compact"
	DensityNormal   Density = "normal"
	DensitySpacious Density = "spacious"
)

// EmptyStateConfig customizes the "no rows" state.
type EmptyStateConfig struct {
	Icon        string `json:"icon,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// GridRowAction is one entry in the per-row action menu.
type GridRowAction struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	Icon     string              `json:"icon,omitempty"`
	Variant  ColorVariant        `json:"variant,omitempty"`
	Disabled *contract.Predicate `json:"disabled,omitempty"`
	Visible  *contract.Predicate `json:"visible,omitempty"`
	Action   *Action             `json:"action"`
}

// GridBulkAction is one entry in the bulk-action toolbar (shown when
// selection > 0).
type GridBulkAction struct {
	ID      string       `json:"id"`
	Label   string       `json:"label"`
	Icon    string       `json:"icon,omitempty"`
	Variant ColorVariant `json:"variant,omitempty"`
	Action  *Action      `json:"action"`
}

// ToolbarConfig controls the DataGrid toolbar contents.
type ToolbarConfig struct {
	Search        bool   `json:"search,omitempty"`
	ColumnPicker  bool   `json:"columnPicker,omitempty"`
	DensityToggle bool   `json:"densityToggle,omitempty"`
	ExportButton  bool   `json:"exportButton,omitempty"`
	Refresh       bool   `json:"refresh,omitempty"`
	Title         string `json:"title,omitempty"`
}

// MasterDetailConfig enables expandable rows.
type MasterDetailConfig struct {
	Intent string `json:"intent"` // intent rendered inside the expanded row
}

// ExportConfig configures export options.
type ExportConfig struct {
	CSV      bool   `json:"csv,omitempty"`
	JSON     bool   `json:"json,omitempty"`
	Excel    bool   `json:"excel,omitempty"`
	Filename string `json:"filename,omitempty"`
}
