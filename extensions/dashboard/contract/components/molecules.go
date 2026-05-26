package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// molecule.field
// =============================================================================

// FieldProps configures a labeled form-control wrapper. The actual control
// (atom.input, atom.select, etc.) lives in the "control" slot.
type FieldProps struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	HelpText    string `json:"helpText,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Error       string `json:"error,omitempty"`
	For         string `json:"for,omitempty"`
	ClassName   string `json:"className,omitempty"`
}

type FieldBuilder struct {
	*Common[FieldBuilder]
	Props FieldProps
}

// Field constructs a molecule.field builder. Pass the control via Control().
func Field(label string) *FieldBuilder {
	b := &FieldBuilder{}
	b.Common = newCommon[FieldBuilder]("molecule.field", b)
	b.Props.Label = label
	return b
}

func (b *FieldBuilder) Description(d string) *FieldBuilder { b.Props.Description = d; return b }
func (b *FieldBuilder) HelpText(h string) *FieldBuilder    { b.Props.HelpText = h; return b }
func (b *FieldBuilder) Required(r bool) *FieldBuilder      { b.Props.Required = r; return b }
func (b *FieldBuilder) Optional(o bool) *FieldBuilder      { b.Props.Optional = o; return b }
func (b *FieldBuilder) Error(e string) *FieldBuilder       { b.Props.Error = e; return b }
func (b *FieldBuilder) For(id string) *FieldBuilder        { b.Props.For = id; return b }
func (b *FieldBuilder) ClassName(c string) *FieldBuilder   { b.Props.ClassName = c; return b }

// Control attaches the form control node.
func (b *FieldBuilder) Control(node contract.GraphNode) *FieldBuilder {
	return b.Slot("control", node)
}

func (b *FieldBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// molecule.stat-card
// =============================================================================

// Trend marks a stat's directional movement.
type Trend string

const (
	TrendUp   Trend = "up"
	TrendDown Trend = "down"
	TrendFlat Trend = "flat"
)

// StatCardProps configures a metric tile.
type StatCardProps struct {
	Label         string  `json:"label"`
	Value         string  `json:"value"`
	PreviousValue string  `json:"previousValue,omitempty"`
	Delta         float64 `json:"delta,omitempty"`
	DeltaFormat   string  `json:"deltaFormat,omitempty"` // "percent" | "absolute"
	Icon          string  `json:"icon,omitempty"`
	Trend         Trend   `json:"trend,omitempty"`
	Description   string  `json:"description,omitempty"`
	Loading       bool    `json:"loading,omitempty"`
	ClassName     string  `json:"className,omitempty"`
}

type StatCardBuilder struct {
	*Common[StatCardBuilder]
	Props StatCardProps
}

func StatCard(label string) *StatCardBuilder {
	b := &StatCardBuilder{}
	b.Common = newCommon[StatCardBuilder]("molecule.stat-card", b)
	b.Props.Label = label
	return b
}

func (b *StatCardBuilder) Value(v string) *StatCardBuilder { b.Props.Value = v; return b }
func (b *StatCardBuilder) PreviousValue(v string) *StatCardBuilder {
	b.Props.PreviousValue = v
	return b
}
func (b *StatCardBuilder) Delta(d float64) *StatCardBuilder      { b.Props.Delta = d; return b }
func (b *StatCardBuilder) DeltaFormat(f string) *StatCardBuilder { b.Props.DeltaFormat = f; return b }
func (b *StatCardBuilder) Icon(i string) *StatCardBuilder        { b.Props.Icon = i; return b }
func (b *StatCardBuilder) Trend(t Trend) *StatCardBuilder        { b.Props.Trend = t; return b }
func (b *StatCardBuilder) Description(d string) *StatCardBuilder { b.Props.Description = d; return b }
func (b *StatCardBuilder) Loading(l bool) *StatCardBuilder       { b.Props.Loading = l; return b }
func (b *StatCardBuilder) ClassName(c string) *StatCardBuilder   { b.Props.ClassName = c; return b }
func (b *StatCardBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.alert
// =============================================================================

// AlertVariant names the alert's visual tone. Only canonical shadcn tokens
// are exposed — no success/warning/info.
type AlertVariant string

const (
	AlertDefault     AlertVariant = "default"
	AlertDestructive AlertVariant = "destructive"
	AlertMuted       AlertVariant = "muted"
	AlertAccent      AlertVariant = "accent"
)

// AlertProps configures a banner alert.
type AlertProps struct {
	Variant     AlertVariant `json:"variant,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Icon        string       `json:"icon,omitempty"`
	Dismissible bool         `json:"dismissible,omitempty"`
	OnDismiss   *Action      `json:"onDismiss,omitempty"`
	ClassName   string       `json:"className,omitempty"`
}

type AlertBuilder struct {
	*Common[AlertBuilder]
	Props AlertProps
}

func Alert() *AlertBuilder {
	b := &AlertBuilder{}
	b.Common = newCommon[AlertBuilder]("molecule.alert", b)
	return b
}

func (b *AlertBuilder) Variant(v AlertVariant) *AlertBuilder { b.Props.Variant = v; return b }
func (b *AlertBuilder) TitleText(t string) *AlertBuilder     { b.Props.Title = t; return b }
func (b *AlertBuilder) Description(d string) *AlertBuilder   { b.Props.Description = d; return b }
func (b *AlertBuilder) Icon(i string) *AlertBuilder          { b.Props.Icon = i; return b }
func (b *AlertBuilder) Dismissible(d bool) *AlertBuilder     { b.Props.Dismissible = d; return b }
func (b *AlertBuilder) OnDismiss(a *Action) *AlertBuilder    { b.Props.OnDismiss = a; return b }
func (b *AlertBuilder) ClassName(c string) *AlertBuilder     { b.Props.ClassName = c; return b }
func (b *AlertBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// molecule.search-bar
// =============================================================================

type SearchBarProps struct {
	Placeholder string  `json:"placeholder,omitempty"`
	Value       string  `json:"value,omitempty"`
	Default     string  `json:"default,omitempty"`
	Name        string  `json:"name,omitempty"`
	OnChange    *Action `json:"onChange,omitempty"`
	OnSubmit    *Action `json:"onSubmit,omitempty"`
	DebounceMs  int     `json:"debounceMs,omitempty"`
	Clearable   bool    `json:"clearable,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type SearchBarBuilder struct {
	*Common[SearchBarBuilder]
	Props SearchBarProps
}

func SearchBar() *SearchBarBuilder {
	b := &SearchBarBuilder{}
	b.Common = newCommon[SearchBarBuilder]("molecule.search-bar", b)
	b.Props.Clearable = true
	return b
}

func (b *SearchBarBuilder) Placeholder(p string) *SearchBarBuilder { b.Props.Placeholder = p; return b }
func (b *SearchBarBuilder) Value(v string) *SearchBarBuilder       { b.Props.Value = v; return b }
func (b *SearchBarBuilder) Default(v string) *SearchBarBuilder     { b.Props.Default = v; return b }
func (b *SearchBarBuilder) Name(n string) *SearchBarBuilder        { b.Props.Name = n; return b }
func (b *SearchBarBuilder) OnChange(a *Action) *SearchBarBuilder   { b.Props.OnChange = a; return b }
func (b *SearchBarBuilder) OnSubmit(a *Action) *SearchBarBuilder   { b.Props.OnSubmit = a; return b }
func (b *SearchBarBuilder) DebounceMs(ms int) *SearchBarBuilder    { b.Props.DebounceMs = ms; return b }
func (b *SearchBarBuilder) Clearable(c bool) *SearchBarBuilder     { b.Props.Clearable = c; return b }
func (b *SearchBarBuilder) ClassName(c string) *SearchBarBuilder   { b.Props.ClassName = c; return b }
func (b *SearchBarBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// molecule.empty-state / error-state / loading-state
// =============================================================================

type EmptyStateProps struct {
	Icon        string `json:"icon,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ClassName   string `json:"className,omitempty"`
}

type EmptyStateBuilder struct {
	*Common[EmptyStateBuilder]
	Props EmptyStateProps
}

func EmptyState(title string) *EmptyStateBuilder {
	b := &EmptyStateBuilder{}
	b.Common = newCommon[EmptyStateBuilder]("molecule.empty-state", b)
	b.Props.Title = title
	return b
}

func (b *EmptyStateBuilder) Icon(i string) *EmptyStateBuilder { b.Props.Icon = i; return b }
func (b *EmptyStateBuilder) Description(d string) *EmptyStateBuilder {
	b.Props.Description = d
	return b
}
func (b *EmptyStateBuilder) ClassName(c string) *EmptyStateBuilder { b.Props.ClassName = c; return b }
func (b *EmptyStateBuilder) Actions(nodes ...contract.GraphNode) *EmptyStateBuilder {
	return b.Slot("actions", nodes...)
}
func (b *EmptyStateBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

type ErrorStateProps struct {
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Error       string  `json:"error,omitempty"`
	OnRetry     *Action `json:"onRetry,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type ErrorStateBuilder struct {
	*Common[ErrorStateBuilder]
	Props ErrorStateProps
}

func ErrorState(title string) *ErrorStateBuilder {
	b := &ErrorStateBuilder{}
	b.Common = newCommon[ErrorStateBuilder]("molecule.error-state", b)
	b.Props.Title = title
	return b
}

func (b *ErrorStateBuilder) Description(d string) *ErrorStateBuilder {
	b.Props.Description = d
	return b
}
func (b *ErrorStateBuilder) Error(e string) *ErrorStateBuilder     { b.Props.Error = e; return b }
func (b *ErrorStateBuilder) OnRetry(a *Action) *ErrorStateBuilder  { b.Props.OnRetry = a; return b }
func (b *ErrorStateBuilder) ClassName(c string) *ErrorStateBuilder { b.Props.ClassName = c; return b }
func (b *ErrorStateBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

type LoadingStateProps struct {
	Label     string `json:"label,omitempty"`
	ClassName string `json:"className,omitempty"`
}

type LoadingStateBuilder struct {
	*Common[LoadingStateBuilder]
	Props LoadingStateProps
}

func LoadingState() *LoadingStateBuilder {
	b := &LoadingStateBuilder{}
	b.Common = newCommon[LoadingStateBuilder]("molecule.loading-state", b)
	return b
}

func (b *LoadingStateBuilder) Label(l string) *LoadingStateBuilder { b.Props.Label = l; return b }
func (b *LoadingStateBuilder) ClassName(c string) *LoadingStateBuilder {
	b.Props.ClassName = c
	return b
}
func (b *LoadingStateBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// molecule.breadcrumb
// =============================================================================

type BreadcrumbItem struct {
	Label string `json:"label"`
	Href  string `json:"href,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type BreadcrumbProps struct {
	Items     []BreadcrumbItem `json:"items"`
	Separator string           `json:"separator,omitempty"` // "/" | ">" | icon name
	ClassName string           `json:"className,omitempty"`
}

type BreadcrumbBuilder struct {
	*Common[BreadcrumbBuilder]
	Props BreadcrumbProps
}

func Breadcrumb() *BreadcrumbBuilder {
	b := &BreadcrumbBuilder{}
	b.Common = newCommon[BreadcrumbBuilder]("molecule.breadcrumb", b)
	return b
}

func (b *BreadcrumbBuilder) Item(label, href string) *BreadcrumbBuilder {
	b.Props.Items = append(b.Props.Items, BreadcrumbItem{Label: label, Href: href})
	return b
}
func (b *BreadcrumbBuilder) AddItem(item BreadcrumbItem) *BreadcrumbBuilder {
	b.Props.Items = append(b.Props.Items, item)
	return b
}
func (b *BreadcrumbBuilder) Separator(s string) *BreadcrumbBuilder { b.Props.Separator = s; return b }
func (b *BreadcrumbBuilder) ClassName(c string) *BreadcrumbBuilder { b.Props.ClassName = c; return b }
func (b *BreadcrumbBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.pagination
// =============================================================================

type PaginationProps struct {
	Page             int     `json:"page"`
	PageSize         int     `json:"pageSize"`
	Total            int     `json:"total,omitempty"`
	PageSizeOptions  []int   `json:"pageSizeOptions,omitempty"`
	SiblingCount     int     `json:"siblingCount,omitempty"`
	ShowFirstLast    bool    `json:"showFirstLast,omitempty"`
	ShowPageSize     bool    `json:"showPageSize,omitempty"`
	OnPageChange     *Action `json:"onPageChange,omitempty"`
	OnPageSizeChange *Action `json:"onPageSizeChange,omitempty"`
	ClassName        string  `json:"className,omitempty"`
}

type PaginationBuilder struct {
	*Common[PaginationBuilder]
	Props PaginationProps
}

func Pagination() *PaginationBuilder {
	b := &PaginationBuilder{}
	b.Common = newCommon[PaginationBuilder]("molecule.pagination", b)
	b.Props.Page = 1
	b.Props.PageSize = 25
	b.Props.SiblingCount = 1
	return b
}

func (b *PaginationBuilder) Page(p int) *PaginationBuilder     { b.Props.Page = p; return b }
func (b *PaginationBuilder) PageSize(s int) *PaginationBuilder { b.Props.PageSize = s; return b }
func (b *PaginationBuilder) Total(t int) *PaginationBuilder    { b.Props.Total = t; return b }
func (b *PaginationBuilder) PageSizeOptions(opts ...int) *PaginationBuilder {
	b.Props.PageSizeOptions = opts
	return b
}
func (b *PaginationBuilder) SiblingCount(n int) *PaginationBuilder {
	b.Props.SiblingCount = n
	return b
}
func (b *PaginationBuilder) ShowFirstLast(s bool) *PaginationBuilder {
	b.Props.ShowFirstLast = s
	return b
}
func (b *PaginationBuilder) ShowPageSize(s bool) *PaginationBuilder {
	b.Props.ShowPageSize = s
	return b
}
func (b *PaginationBuilder) OnPageChange(a *Action) *PaginationBuilder {
	b.Props.OnPageChange = a
	return b
}
func (b *PaginationBuilder) OnPageSizeChange(a *Action) *PaginationBuilder {
	b.Props.OnPageSizeChange = a
	return b
}
func (b *PaginationBuilder) ClassName(c string) *PaginationBuilder { b.Props.ClassName = c; return b }
func (b *PaginationBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.tag-input
// =============================================================================

type TagInputProps struct {
	Name        string   `json:"name"`
	Value       []string `json:"value,omitempty"`
	Default     []string `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	MaxTags     int      `json:"maxTags,omitempty"`
	Validate    string   `json:"validate,omitempty"` // regex pattern
	Disabled    bool     `json:"disabled,omitempty"`
	OnChange    *Action  `json:"onChange,omitempty"`
	Error       string   `json:"error,omitempty"`
	ClassName   string   `json:"className,omitempty"`
}

type TagInputBuilder struct {
	*Common[TagInputBuilder]
	Props TagInputProps
}

func TagInput(name string) *TagInputBuilder {
	b := &TagInputBuilder{}
	b.Common = newCommon[TagInputBuilder]("molecule.tag-input", b)
	b.Props.Name = name
	return b
}

func (b *TagInputBuilder) Value(v []string) *TagInputBuilder     { b.Props.Value = v; return b }
func (b *TagInputBuilder) Default(v []string) *TagInputBuilder   { b.Props.Default = v; return b }
func (b *TagInputBuilder) Placeholder(p string) *TagInputBuilder { b.Props.Placeholder = p; return b }
func (b *TagInputBuilder) MaxTags(n int) *TagInputBuilder        { b.Props.MaxTags = n; return b }
func (b *TagInputBuilder) Validate(p string) *TagInputBuilder    { b.Props.Validate = p; return b }
func (b *TagInputBuilder) Disabled(d bool) *TagInputBuilder      { b.Props.Disabled = d; return b }
func (b *TagInputBuilder) OnChange(a *Action) *TagInputBuilder   { b.Props.OnChange = a; return b }
func (b *TagInputBuilder) Error(e string) *TagInputBuilder       { b.Props.Error = e; return b }
func (b *TagInputBuilder) ClassName(c string) *TagInputBuilder   { b.Props.ClassName = c; return b }
func (b *TagInputBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.file-upload
// =============================================================================

type FileUploadProps struct {
	Name      string  `json:"name"`
	Accept    string  `json:"accept,omitempty"`
	Multiple  bool    `json:"multiple,omitempty"`
	MaxSize   int     `json:"maxSize,omitempty"` // bytes
	MaxFiles  int     `json:"maxFiles,omitempty"`
	Disabled  bool    `json:"disabled,omitempty"`
	OnChange  *Action `json:"onChange,omitempty"`
	Hint      string  `json:"hint,omitempty"`
	Error     string  `json:"error,omitempty"`
	ClassName string  `json:"className,omitempty"`
}

type FileUploadBuilder struct {
	*Common[FileUploadBuilder]
	Props FileUploadProps
}

func FileUpload(name string) *FileUploadBuilder {
	b := &FileUploadBuilder{}
	b.Common = newCommon[FileUploadBuilder]("molecule.file-upload", b)
	b.Props.Name = name
	return b
}

func (b *FileUploadBuilder) Accept(a string) *FileUploadBuilder    { b.Props.Accept = a; return b }
func (b *FileUploadBuilder) Multiple(m bool) *FileUploadBuilder    { b.Props.Multiple = m; return b }
func (b *FileUploadBuilder) MaxSize(s int) *FileUploadBuilder      { b.Props.MaxSize = s; return b }
func (b *FileUploadBuilder) MaxFiles(n int) *FileUploadBuilder     { b.Props.MaxFiles = n; return b }
func (b *FileUploadBuilder) Disabled(d bool) *FileUploadBuilder    { b.Props.Disabled = d; return b }
func (b *FileUploadBuilder) OnChange(a *Action) *FileUploadBuilder { b.Props.OnChange = a; return b }
func (b *FileUploadBuilder) Hint(h string) *FileUploadBuilder      { b.Props.Hint = h; return b }
func (b *FileUploadBuilder) Error(e string) *FileUploadBuilder     { b.Props.Error = e; return b }
func (b *FileUploadBuilder) ClassName(c string) *FileUploadBuilder { b.Props.ClassName = c; return b }
func (b *FileUploadBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.date-picker
// =============================================================================

type DatePickerMode string

const (
	DateModeSingle DatePickerMode = "single"
	DateModeRange  DatePickerMode = "range"
)

type DatePickerProps struct {
	Name          string         `json:"name"`
	Value         string         `json:"value,omitempty"`
	Default       string         `json:"default,omitempty"`
	Mode          DatePickerMode `json:"mode,omitempty"`
	Min           string         `json:"min,omitempty"`
	Max           string         `json:"max,omitempty"`
	DisabledDates []string       `json:"disabledDates,omitempty"`
	Format        string         `json:"format,omitempty"`
	Placeholder   string         `json:"placeholder,omitempty"`
	Disabled      bool           `json:"disabled,omitempty"`
	OnChange      *Action        `json:"onChange,omitempty"`
	Error         string         `json:"error,omitempty"`
	ClassName     string         `json:"className,omitempty"`
}

type DatePickerBuilder struct {
	*Common[DatePickerBuilder]
	Props DatePickerProps
}

func DatePicker(name string) *DatePickerBuilder {
	b := &DatePickerBuilder{}
	b.Common = newCommon[DatePickerBuilder]("molecule.date-picker", b)
	b.Props.Name = name
	b.Props.Mode = DateModeSingle
	return b
}

func (b *DatePickerBuilder) Value(v string) *DatePickerBuilder        { b.Props.Value = v; return b }
func (b *DatePickerBuilder) Default(v string) *DatePickerBuilder      { b.Props.Default = v; return b }
func (b *DatePickerBuilder) Mode(m DatePickerMode) *DatePickerBuilder { b.Props.Mode = m; return b }
func (b *DatePickerBuilder) Min(m string) *DatePickerBuilder          { b.Props.Min = m; return b }
func (b *DatePickerBuilder) Max(m string) *DatePickerBuilder          { b.Props.Max = m; return b }
func (b *DatePickerBuilder) DisabledDates(d ...string) *DatePickerBuilder {
	b.Props.DisabledDates = d
	return b
}
func (b *DatePickerBuilder) Format(f string) *DatePickerBuilder { b.Props.Format = f; return b }
func (b *DatePickerBuilder) Placeholder(p string) *DatePickerBuilder {
	b.Props.Placeholder = p
	return b
}
func (b *DatePickerBuilder) Disabled(d bool) *DatePickerBuilder    { b.Props.Disabled = d; return b }
func (b *DatePickerBuilder) OnChange(a *Action) *DatePickerBuilder { b.Props.OnChange = a; return b }
func (b *DatePickerBuilder) Error(e string) *DatePickerBuilder     { b.Props.Error = e; return b }
func (b *DatePickerBuilder) ClassName(c string) *DatePickerBuilder { b.Props.ClassName = c; return b }
func (b *DatePickerBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// molecule.combobox
// =============================================================================

type ComboboxProps struct {
	Name         string        `json:"name"`
	Placeholder  string        `json:"placeholder,omitempty"`
	Options      []Option      `json:"options,omitempty"`
	OptionSource *OptionSource `json:"optionSource,omitempty"` // dynamic options
	Value        string        `json:"value,omitempty"`
	Multiple     bool          `json:"multiple,omitempty"`
	Values       []string      `json:"values,omitempty"`
	Searchable   bool          `json:"searchable,omitempty"`
	Creatable    bool          `json:"creatable,omitempty"`
	Clearable    bool          `json:"clearable,omitempty"`
	OnSearch     *Action       `json:"onSearch,omitempty"`
	OnChange     *Action       `json:"onChange,omitempty"`
	Disabled     bool          `json:"disabled,omitempty"`
	Error        string        `json:"error,omitempty"`
	ClassName    string        `json:"className,omitempty"`
}

type ComboboxBuilder struct {
	*Common[ComboboxBuilder]
	Props ComboboxProps
}

func Combobox(name string) *ComboboxBuilder {
	b := &ComboboxBuilder{}
	b.Common = newCommon[ComboboxBuilder]("molecule.combobox", b)
	b.Props.Name = name
	b.Props.Searchable = true
	return b
}

func (b *ComboboxBuilder) Placeholder(p string) *ComboboxBuilder { b.Props.Placeholder = p; return b }
func (b *ComboboxBuilder) Options(opts ...Option) *ComboboxBuilder {
	b.Props.Options = append(b.Props.Options, opts...)
	return b
}
func (b *ComboboxBuilder) OptionSource(s *OptionSource) *ComboboxBuilder {
	b.Props.OptionSource = s
	return b
}
func (b *ComboboxBuilder) Value(v string) *ComboboxBuilder      { b.Props.Value = v; return b }
func (b *ComboboxBuilder) Multiple(m bool) *ComboboxBuilder     { b.Props.Multiple = m; return b }
func (b *ComboboxBuilder) Values(vs ...string) *ComboboxBuilder { b.Props.Values = vs; return b }
func (b *ComboboxBuilder) Searchable(s bool) *ComboboxBuilder   { b.Props.Searchable = s; return b }
func (b *ComboboxBuilder) Creatable(c bool) *ComboboxBuilder    { b.Props.Creatable = c; return b }
func (b *ComboboxBuilder) Clearable(c bool) *ComboboxBuilder    { b.Props.Clearable = c; return b }
func (b *ComboboxBuilder) OnSearch(a *Action) *ComboboxBuilder  { b.Props.OnSearch = a; return b }
func (b *ComboboxBuilder) OnChange(a *Action) *ComboboxBuilder  { b.Props.OnChange = a; return b }
func (b *ComboboxBuilder) Disabled(d bool) *ComboboxBuilder     { b.Props.Disabled = d; return b }
func (b *ComboboxBuilder) Error(e string) *ComboboxBuilder      { b.Props.Error = e; return b }
func (b *ComboboxBuilder) ClassName(c string) *ComboboxBuilder  { b.Props.ClassName = c; return b }
func (b *ComboboxBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// molecule.command-palette
// =============================================================================

// PaletteItem is a single entry in a command palette group.
type PaletteItem struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Icon     string   `json:"icon,omitempty"`
	Shortcut []string `json:"shortcut,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
	Action   *Action  `json:"action,omitempty"`
}

// PaletteGroup groups palette items under a heading.
type PaletteGroup struct {
	Label string        `json:"label,omitempty"`
	Items []PaletteItem `json:"items"`
}

type CommandPaletteProps struct {
	Placeholder     string         `json:"placeholder,omitempty"`
	Groups          []PaletteGroup `json:"groups"`
	TriggerShortcut []string       `json:"triggerShortcut,omitempty"`
	EmptyMessage    string         `json:"emptyMessage,omitempty"`
	ClassName       string         `json:"className,omitempty"`
}

type CommandPaletteBuilder struct {
	*Common[CommandPaletteBuilder]
	Props CommandPaletteProps
}

func CommandPalette() *CommandPaletteBuilder {
	b := &CommandPaletteBuilder{}
	b.Common = newCommon[CommandPaletteBuilder]("molecule.command-palette", b)
	return b
}

func (b *CommandPaletteBuilder) Placeholder(p string) *CommandPaletteBuilder {
	b.Props.Placeholder = p
	return b
}
func (b *CommandPaletteBuilder) AddGroup(group PaletteGroup) *CommandPaletteBuilder {
	b.Props.Groups = append(b.Props.Groups, group)
	return b
}
func (b *CommandPaletteBuilder) Group(label string, items ...PaletteItem) *CommandPaletteBuilder {
	b.Props.Groups = append(b.Props.Groups, PaletteGroup{Label: label, Items: items})
	return b
}
func (b *CommandPaletteBuilder) TriggerShortcut(keys ...string) *CommandPaletteBuilder {
	b.Props.TriggerShortcut = keys
	return b
}
func (b *CommandPaletteBuilder) EmptyMessage(m string) *CommandPaletteBuilder {
	b.Props.EmptyMessage = m
	return b
}
func (b *CommandPaletteBuilder) ClassName(c string) *CommandPaletteBuilder {
	b.Props.ClassName = c
	return b
}
func (b *CommandPaletteBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// molecule.dropdown-menu
// =============================================================================

// MenuItem is a dropdown entry. Set Separator=true for a divider; set Items
// for a submenu.
type MenuItem struct {
	Label     string       `json:"label,omitempty"`
	Icon      string       `json:"icon,omitempty"`
	Shortcut  []string     `json:"shortcut,omitempty"`
	Variant   ColorVariant `json:"variant,omitempty"`
	Disabled  bool         `json:"disabled,omitempty"`
	Separator bool         `json:"separator,omitempty"`
	Action    *Action      `json:"action,omitempty"`
	Items     []MenuItem   `json:"items,omitempty"`
}

type DropdownMenuProps struct {
	TriggerLabel string     `json:"triggerLabel,omitempty"`
	TriggerIcon  string     `json:"triggerIcon,omitempty"`
	Items        []MenuItem `json:"items"`
	Side         string     `json:"side,omitempty"`  // "top" | "bottom" | "left" | "right"
	Align        string     `json:"align,omitempty"` // "start" | "center" | "end"
	ClassName    string     `json:"className,omitempty"`
}

type DropdownMenuBuilder struct {
	*Common[DropdownMenuBuilder]
	Props DropdownMenuProps
}

func DropdownMenu() *DropdownMenuBuilder {
	b := &DropdownMenuBuilder{}
	b.Common = newCommon[DropdownMenuBuilder]("molecule.dropdown-menu", b)
	return b
}

func (b *DropdownMenuBuilder) TriggerLabel(l string) *DropdownMenuBuilder {
	b.Props.TriggerLabel = l
	return b
}
func (b *DropdownMenuBuilder) TriggerIcon(i string) *DropdownMenuBuilder {
	b.Props.TriggerIcon = i
	return b
}
func (b *DropdownMenuBuilder) AddItem(item MenuItem) *DropdownMenuBuilder {
	b.Props.Items = append(b.Props.Items, item)
	return b
}
func (b *DropdownMenuBuilder) Items(items ...MenuItem) *DropdownMenuBuilder {
	b.Props.Items = append(b.Props.Items, items...)
	return b
}
func (b *DropdownMenuBuilder) Side(s string) *DropdownMenuBuilder  { b.Props.Side = s; return b }
func (b *DropdownMenuBuilder) Align(a string) *DropdownMenuBuilder { b.Props.Align = a; return b }

// Trigger attaches a custom trigger node. When set it overrides
// TriggerLabel/TriggerIcon.
func (b *DropdownMenuBuilder) Trigger(node contract.GraphNode) *DropdownMenuBuilder {
	return b.Slot("trigger", node)
}

func (b *DropdownMenuBuilder) ClassName(c string) *DropdownMenuBuilder {
	b.Props.ClassName = c
	return b
}
func (b *DropdownMenuBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// molecule.list-item
// =============================================================================

type ListItemProps struct {
	Title          string  `json:"title"`
	Description    string  `json:"description,omitempty"`
	Icon           string  `json:"icon,omitempty"`
	AvatarSrc      string  `json:"avatarSrc,omitempty"`
	AvatarFallback string  `json:"avatarFallback,omitempty"`
	Href           string  `json:"href,omitempty"`
	Selected       bool    `json:"selected,omitempty"`
	Disabled       bool    `json:"disabled,omitempty"`
	OnClick        *Action `json:"onClick,omitempty"`
	ClassName      string  `json:"className,omitempty"`
}

type ListItemBuilder struct {
	*Common[ListItemBuilder]
	Props ListItemProps
}

func ListItem(title string) *ListItemBuilder {
	b := &ListItemBuilder{}
	b.Common = newCommon[ListItemBuilder]("molecule.list-item", b)
	b.Props.Title = title
	return b
}

func (b *ListItemBuilder) Description(d string) *ListItemBuilder { b.Props.Description = d; return b }
func (b *ListItemBuilder) Icon(i string) *ListItemBuilder        { b.Props.Icon = i; return b }
func (b *ListItemBuilder) Avatar(src, fallback string) *ListItemBuilder {
	b.Props.AvatarSrc = src
	b.Props.AvatarFallback = fallback
	return b
}
func (b *ListItemBuilder) Href(h string) *ListItemBuilder     { b.Props.Href = h; return b }
func (b *ListItemBuilder) Selected(s bool) *ListItemBuilder   { b.Props.Selected = s; return b }
func (b *ListItemBuilder) Disabled(d bool) *ListItemBuilder   { b.Props.Disabled = d; return b }
func (b *ListItemBuilder) OnClick(a *Action) *ListItemBuilder { b.Props.OnClick = a; return b }
func (b *ListItemBuilder) Trailing(nodes ...contract.GraphNode) *ListItemBuilder {
	return b.Slot("trailing", nodes...)
}
func (b *ListItemBuilder) ClassName(c string) *ListItemBuilder { b.Props.ClassName = c; return b }
func (b *ListItemBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// molecule.chip
// =============================================================================

type ChipProps struct {
	Label     string       `json:"label"`
	Icon      string       `json:"icon,omitempty"`
	Variant   ColorVariant `json:"variant,omitempty"`
	Removable bool         `json:"removable,omitempty"`
	OnRemove  *Action      `json:"onRemove,omitempty"`
	OnClick   *Action      `json:"onClick,omitempty"`
	Disabled  bool         `json:"disabled,omitempty"`
	ClassName string       `json:"className,omitempty"`
}

type ChipBuilder struct {
	*Common[ChipBuilder]
	Props ChipProps
}

func Chip(label string) *ChipBuilder {
	b := &ChipBuilder{}
	b.Common = newCommon[ChipBuilder]("molecule.chip", b)
	b.Props.Label = label
	return b
}

func (b *ChipBuilder) Icon(i string) *ChipBuilder          { b.Props.Icon = i; return b }
func (b *ChipBuilder) Variant(v ColorVariant) *ChipBuilder { b.Props.Variant = v; return b }
func (b *ChipBuilder) Removable(r bool) *ChipBuilder       { b.Props.Removable = r; return b }
func (b *ChipBuilder) OnRemove(a *Action) *ChipBuilder     { b.Props.OnRemove = a; return b }
func (b *ChipBuilder) OnClick(a *Action) *ChipBuilder      { b.Props.OnClick = a; return b }
func (b *ChipBuilder) Disabled(d bool) *ChipBuilder        { b.Props.Disabled = d; return b }
func (b *ChipBuilder) ClassName(c string) *ChipBuilder     { b.Props.ClassName = c; return b }
func (b *ChipBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// molecule.rating
// =============================================================================

type RatingProps struct {
	Name      string  `json:"name,omitempty"`
	Value     float64 `json:"value,omitempty"`
	Default   float64 `json:"default,omitempty"`
	Max       int     `json:"max,omitempty"`
	ReadOnly  bool    `json:"readOnly,omitempty"`
	Precision float64 `json:"precision,omitempty"` // 1 | 0.5 | 0.25
	Size      Size    `json:"size,omitempty"`
	OnChange  *Action `json:"onChange,omitempty"`
	ClassName string  `json:"className,omitempty"`
}

type RatingBuilder struct {
	*Common[RatingBuilder]
	Props RatingProps
}

func Rating() *RatingBuilder {
	b := &RatingBuilder{}
	b.Common = newCommon[RatingBuilder]("molecule.rating", b)
	b.Props.Max = 5
	b.Props.Precision = 1
	return b
}

func (b *RatingBuilder) Name(n string) *RatingBuilder       { b.Props.Name = n; return b }
func (b *RatingBuilder) Value(v float64) *RatingBuilder     { b.Props.Value = v; return b }
func (b *RatingBuilder) Default(v float64) *RatingBuilder   { b.Props.Default = v; return b }
func (b *RatingBuilder) Max(m int) *RatingBuilder           { b.Props.Max = m; return b }
func (b *RatingBuilder) ReadOnly(r bool) *RatingBuilder     { b.Props.ReadOnly = r; return b }
func (b *RatingBuilder) Precision(p float64) *RatingBuilder { b.Props.Precision = p; return b }
func (b *RatingBuilder) Size(s Size) *RatingBuilder         { b.Props.Size = s; return b }
func (b *RatingBuilder) OnChange(a *Action) *RatingBuilder  { b.Props.OnChange = a; return b }
func (b *RatingBuilder) ClassName(c string) *RatingBuilder  { b.Props.ClassName = c; return b }
func (b *RatingBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }
