package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// organism.data-grid
// =============================================================================

// DataGridProps configures a TanStack-Table-backed DataGrid.
type DataGridProps struct {
	Columns        []GridColumn        `json:"columns"`
	RowID          string              `json:"rowId,omitempty"`
	Selection      *SelectionConfig    `json:"selection,omitempty"`
	Sorting        *SortingConfig      `json:"sorting,omitempty"`
	Filtering      *FilteringConfig    `json:"filtering,omitempty"`
	Pagination     *PaginationConfig   `json:"pagination,omitempty"`
	Grouping       *GroupingConfig     `json:"grouping,omitempty"`
	Pinning        *PinningConfig      `json:"pinning,omitempty"`
	Virtualization *VirtConfig         `json:"virtualization,omitempty"`
	Density        Density             `json:"density,omitempty"`
	StickyHeader   bool                `json:"stickyHeader,omitempty"`
	EmptyState     *EmptyStateConfig   `json:"emptyState,omitempty"`
	RowActions     []GridRowAction     `json:"rowActions,omitempty"`
	BulkActions    []GridBulkAction    `json:"bulkActions,omitempty"`
	Toolbar        *ToolbarConfig      `json:"toolbar,omitempty"`
	MasterDetail   *MasterDetailConfig `json:"masterDetail,omitempty"`
	OnRowClick     *Action             `json:"onRowClick,omitempty"`
	Export         *ExportConfig       `json:"export,omitempty"`
	Height         string              `json:"height,omitempty"`
	Resizable      bool                `json:"resizable,omitempty"`
	ClassName      string              `json:"className,omitempty"`
}

type DataGridBuilder struct {
	*Common[DataGridBuilder]
	Props DataGridProps
}

// DataGrid constructs an organism.data-grid builder. Attach data via the
// inherited .Data() setter.
func DataGrid() *DataGridBuilder {
	b := &DataGridBuilder{}
	b.Common = newCommon[DataGridBuilder]("organism.data-grid", b)
	return b
}

func (b *DataGridBuilder) Columns(cols ...GridColumn) *DataGridBuilder {
	b.Props.Columns = append(b.Props.Columns, cols...)
	return b
}
func (b *DataGridBuilder) Column(col GridColumn) *DataGridBuilder {
	b.Props.Columns = append(b.Props.Columns, col)
	return b
}
func (b *DataGridBuilder) RowID(field string) *DataGridBuilder { b.Props.RowID = field; return b }
func (b *DataGridBuilder) Selection(c *SelectionConfig) *DataGridBuilder {
	b.Props.Selection = c
	return b
}
func (b *DataGridBuilder) Sorting(c *SortingConfig) *DataGridBuilder { b.Props.Sorting = c; return b }
func (b *DataGridBuilder) Filtering(c *FilteringConfig) *DataGridBuilder {
	b.Props.Filtering = c
	return b
}
func (b *DataGridBuilder) Pagination(c *PaginationConfig) *DataGridBuilder {
	b.Props.Pagination = c
	return b
}
func (b *DataGridBuilder) PaginationFromBuilder(pb *PaginationConfigBuilder) *DataGridBuilder {
	b.Props.Pagination = pb.Build()
	return b
}
func (b *DataGridBuilder) Grouping(c *GroupingConfig) *DataGridBuilder {
	b.Props.Grouping = c
	return b
}
func (b *DataGridBuilder) Pinning(c *PinningConfig) *DataGridBuilder { b.Props.Pinning = c; return b }
func (b *DataGridBuilder) Virtualization(c *VirtConfig) *DataGridBuilder {
	b.Props.Virtualization = c
	return b
}
func (b *DataGridBuilder) Density(d Density) *DataGridBuilder { b.Props.Density = d; return b }
func (b *DataGridBuilder) StickyHeader(s bool) *DataGridBuilder {
	b.Props.StickyHeader = s
	return b
}
func (b *DataGridBuilder) EmptyState(c *EmptyStateConfig) *DataGridBuilder {
	b.Props.EmptyState = c
	return b
}
func (b *DataGridBuilder) RowActions(actions ...GridRowAction) *DataGridBuilder {
	b.Props.RowActions = append(b.Props.RowActions, actions...)
	return b
}
func (b *DataGridBuilder) BulkActions(actions ...GridBulkAction) *DataGridBuilder {
	b.Props.BulkActions = append(b.Props.BulkActions, actions...)
	return b
}
func (b *DataGridBuilder) Toolbar(c *ToolbarConfig) *DataGridBuilder { b.Props.Toolbar = c; return b }
func (b *DataGridBuilder) MasterDetail(c *MasterDetailConfig) *DataGridBuilder {
	b.Props.MasterDetail = c
	return b
}
func (b *DataGridBuilder) OnRowClick(a *Action) *DataGridBuilder   { b.Props.OnRowClick = a; return b }
func (b *DataGridBuilder) Export(c *ExportConfig) *DataGridBuilder { b.Props.Export = c; return b }
func (b *DataGridBuilder) Height(h string) *DataGridBuilder        { b.Props.Height = h; return b }
func (b *DataGridBuilder) Resizable(r bool) *DataGridBuilder       { b.Props.Resizable = r; return b }
func (b *DataGridBuilder) ClassName(c string) *DataGridBuilder     { b.Props.ClassName = c; return b }
func (b *DataGridBuilder) Build() contract.GraphNode               { return b.toNode(b.Props) }

// =============================================================================
// organism.kanban
// =============================================================================

type KanbanColumn struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Accept []string `json:"accept,omitempty"` // status values this column accepts
	Color  string   `json:"color,omitempty"`  // muted | accent | destructive (shadcn only)
	Limit  int      `json:"limit,omitempty"`
}

type KanbanProps struct {
	Columns     []KanbanColumn `json:"columns"`
	StatusField string         `json:"statusField"`          // field on each card used to assign columns
	CardIntent  string         `json:"cardIntent,omitempty"` // intent that renders a card
	OnMove      *Action        `json:"onMove,omitempty"`     // dispatched on drop with { id, from, to }
	OnCardClick *Action        `json:"onCardClick,omitempty"`
	ClassName   string         `json:"className,omitempty"`
}

type KanbanBuilder struct {
	*Common[KanbanBuilder]
	Props KanbanProps
}

func Kanban() *KanbanBuilder {
	b := &KanbanBuilder{}
	b.Common = newCommon[KanbanBuilder]("organism.kanban", b)
	return b
}

func (b *KanbanBuilder) Columns(cols ...KanbanColumn) *KanbanBuilder {
	b.Props.Columns = append(b.Props.Columns, cols...)
	return b
}
func (b *KanbanBuilder) StatusField(f string) *KanbanBuilder { b.Props.StatusField = f; return b }
func (b *KanbanBuilder) CardIntent(i string) *KanbanBuilder  { b.Props.CardIntent = i; return b }
func (b *KanbanBuilder) OnMove(a *Action) *KanbanBuilder     { b.Props.OnMove = a; return b }
func (b *KanbanBuilder) OnCardClick(a *Action) *KanbanBuilder {
	b.Props.OnCardClick = a
	return b
}
func (b *KanbanBuilder) ClassName(c string) *KanbanBuilder { b.Props.ClassName = c; return b }
func (b *KanbanBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// organism.calendar
// =============================================================================

type CalendarView string

const (
	CalendarMonth CalendarView = "month"
	CalendarWeek  CalendarView = "week"
	CalendarDay   CalendarView = "day"
)

type CalendarProps struct {
	View         CalendarView `json:"view,omitempty"`
	TitleField   string       `json:"titleField,omitempty"`
	StartField   string       `json:"startField,omitempty"`
	EndField     string       `json:"endField,omitempty"`
	ColorField   string       `json:"colorField,omitempty"`
	OnEventClick *Action      `json:"onEventClick,omitempty"`
	OnCreate     *Action      `json:"onCreate,omitempty"`
	ClassName    string       `json:"className,omitempty"`
}

type CalendarBuilder struct {
	*Common[CalendarBuilder]
	Props CalendarProps
}

func Calendar() *CalendarBuilder {
	b := &CalendarBuilder{}
	b.Common = newCommon[CalendarBuilder]("organism.calendar", b)
	b.Props.View = CalendarMonth
	b.Props.TitleField = "title"
	b.Props.StartField = "start"
	b.Props.EndField = "end"
	return b
}

func (b *CalendarBuilder) View(v CalendarView) *CalendarBuilder { b.Props.View = v; return b }
func (b *CalendarBuilder) TitleField(f string) *CalendarBuilder { b.Props.TitleField = f; return b }
func (b *CalendarBuilder) StartField(f string) *CalendarBuilder { b.Props.StartField = f; return b }
func (b *CalendarBuilder) EndField(f string) *CalendarBuilder   { b.Props.EndField = f; return b }
func (b *CalendarBuilder) ColorField(f string) *CalendarBuilder { b.Props.ColorField = f; return b }
func (b *CalendarBuilder) OnEventClick(a *Action) *CalendarBuilder {
	b.Props.OnEventClick = a
	return b
}
func (b *CalendarBuilder) OnCreate(a *Action) *CalendarBuilder { b.Props.OnCreate = a; return b }
func (b *CalendarBuilder) ClassName(c string) *CalendarBuilder { b.Props.ClassName = c; return b }
func (b *CalendarBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// organism.timeline
// =============================================================================

type TimelineProps struct {
	TitleField       string `json:"titleField,omitempty"`
	DescriptionField string `json:"descriptionField,omitempty"`
	TimestampField   string `json:"timestampField,omitempty"`
	IconField        string `json:"iconField,omitempty"`
	GroupBy          string `json:"groupBy,omitempty"`    // field name to group entries under
	RenderItem       string `json:"renderItem,omitempty"` // intent name to render each item
	ClassName        string `json:"className,omitempty"`
}

type TimelineBuilder struct {
	*Common[TimelineBuilder]
	Props TimelineProps
}

func Timeline() *TimelineBuilder {
	b := &TimelineBuilder{}
	b.Common = newCommon[TimelineBuilder]("organism.timeline", b)
	b.Props.TitleField = "title"
	b.Props.TimestampField = "timestamp"
	return b
}

func (b *TimelineBuilder) TitleField(f string) *TimelineBuilder { b.Props.TitleField = f; return b }
func (b *TimelineBuilder) DescriptionField(f string) *TimelineBuilder {
	b.Props.DescriptionField = f
	return b
}
func (b *TimelineBuilder) TimestampField(f string) *TimelineBuilder {
	b.Props.TimestampField = f
	return b
}
func (b *TimelineBuilder) IconField(f string) *TimelineBuilder { b.Props.IconField = f; return b }
func (b *TimelineBuilder) GroupBy(f string) *TimelineBuilder   { b.Props.GroupBy = f; return b }
func (b *TimelineBuilder) RenderItem(intent string) *TimelineBuilder {
	b.Props.RenderItem = intent
	return b
}
func (b *TimelineBuilder) ClassName(c string) *TimelineBuilder { b.Props.ClassName = c; return b }
func (b *TimelineBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// organism.tree-view
// =============================================================================

type TreeViewProps struct {
	ChildrenField string  `json:"childrenField,omitempty"`
	LabelField    string  `json:"labelField,omitempty"`
	IconField     string  `json:"iconField,omitempty"`
	NodeIntent    string  `json:"nodeIntent,omitempty"`    // intent to render each node
	DefaultExpand int     `json:"defaultExpand,omitempty"` // default expand depth (0 = root only)
	OnSelect      *Action `json:"onSelect,omitempty"`
	ClassName     string  `json:"className,omitempty"`
}

type TreeViewBuilder struct {
	*Common[TreeViewBuilder]
	Props TreeViewProps
}

func TreeView() *TreeViewBuilder {
	b := &TreeViewBuilder{}
	b.Common = newCommon[TreeViewBuilder]("organism.tree-view", b)
	b.Props.ChildrenField = "children"
	b.Props.LabelField = "label"
	return b
}

func (b *TreeViewBuilder) ChildrenField(f string) *TreeViewBuilder {
	b.Props.ChildrenField = f
	return b
}
func (b *TreeViewBuilder) LabelField(f string) *TreeViewBuilder { b.Props.LabelField = f; return b }
func (b *TreeViewBuilder) IconField(f string) *TreeViewBuilder  { b.Props.IconField = f; return b }
func (b *TreeViewBuilder) NodeIntent(i string) *TreeViewBuilder { b.Props.NodeIntent = i; return b }
func (b *TreeViewBuilder) DefaultExpand(n int) *TreeViewBuilder { b.Props.DefaultExpand = n; return b }
func (b *TreeViewBuilder) OnSelect(a *Action) *TreeViewBuilder  { b.Props.OnSelect = a; return b }
func (b *TreeViewBuilder) ClassName(c string) *TreeViewBuilder  { b.Props.ClassName = c; return b }
func (b *TreeViewBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// organism.stepper
// =============================================================================

type StepperStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

type StepperProps struct {
	Steps       []StepperStep `json:"steps"`
	Initial     string        `json:"initial,omitempty"`
	Orientation Orientation   `json:"orientation,omitempty"`
	OnNext      *Action       `json:"onNext,omitempty"`
	OnPrev      *Action       `json:"onPrev,omitempty"`
	OnComplete  *Action       `json:"onComplete,omitempty"`
	ClassName   string        `json:"className,omitempty"`
}

type StepperBuilder struct {
	*Common[StepperBuilder]
	Props StepperProps
}

func Stepper() *StepperBuilder {
	b := &StepperBuilder{}
	b.Common = newCommon[StepperBuilder]("organism.stepper", b)
	return b
}

func (b *StepperBuilder) Steps(steps ...StepperStep) *StepperBuilder {
	b.Props.Steps = append(b.Props.Steps, steps...)
	return b
}
func (b *StepperBuilder) Step(id, title string) *StepperBuilder {
	b.Props.Steps = append(b.Props.Steps, StepperStep{ID: id, Title: title})
	return b
}
func (b *StepperBuilder) Initial(id string) *StepperBuilder { b.Props.Initial = id; return b }
func (b *StepperBuilder) Orientation(o Orientation) *StepperBuilder {
	b.Props.Orientation = o
	return b
}
func (b *StepperBuilder) OnNext(a *Action) *StepperBuilder     { b.Props.OnNext = a; return b }
func (b *StepperBuilder) OnPrev(a *Action) *StepperBuilder     { b.Props.OnPrev = a; return b }
func (b *StepperBuilder) OnComplete(a *Action) *StepperBuilder { b.Props.OnComplete = a; return b }

// Panel attaches the content for a specific step. Each step's panel goes
// into the slot whose name matches the step ID.
func (b *StepperBuilder) Panel(stepID string, nodes ...contract.GraphNode) *StepperBuilder {
	return b.Slot(stepID, nodes...)
}

func (b *StepperBuilder) ClassName(c string) *StepperBuilder { b.Props.ClassName = c; return b }
func (b *StepperBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }

// =============================================================================
// organism.chart
// =============================================================================

type ChartType string

const (
	ChartLine     ChartType = "line"
	ChartBar      ChartType = "bar"
	ChartArea     ChartType = "area"
	ChartPie      ChartType = "pie"
	ChartScatter  ChartType = "scatter"
	ChartComposed ChartType = "composed"
)

// ChartSeries declares one series in the chart. ColorToken must reference
// a shadcn chart-* token (chart-1..5) — no arbitrary hex.
type ChartSeries struct {
	Key        string    `json:"key"`
	Label      string    `json:"label,omitempty"`
	Type       ChartType `json:"type,omitempty"`       // for composed charts
	ColorToken string    `json:"colorToken,omitempty"` // "chart-1".."chart-5"
}

type ChartProps struct {
	Type        ChartType     `json:"type"`
	XAxis       string        `json:"xAxis,omitempty"` // field name on each row
	YAxis       string        `json:"yAxis,omitempty"`
	Series      []ChartSeries `json:"series,omitempty"`
	ShowGrid    bool          `json:"showGrid,omitempty"`
	ShowLegend  bool          `json:"showLegend,omitempty"`
	ShowTooltip bool          `json:"showTooltip,omitempty"`
	Height      int           `json:"height,omitempty"`
	Stacked     bool          `json:"stacked,omitempty"`
	ClassName   string        `json:"className,omitempty"`
}

type ChartBuilder struct {
	*Common[ChartBuilder]
	Props ChartProps
}

func Chart(t ChartType) *ChartBuilder {
	b := &ChartBuilder{}
	b.Common = newCommon[ChartBuilder]("organism.chart", b)
	b.Props.Type = t
	b.Props.ShowGrid = true
	b.Props.ShowLegend = true
	b.Props.ShowTooltip = true
	b.Props.Height = 300
	return b
}

func (b *ChartBuilder) XAxis(field string) *ChartBuilder { b.Props.XAxis = field; return b }
func (b *ChartBuilder) YAxis(field string) *ChartBuilder { b.Props.YAxis = field; return b }
func (b *ChartBuilder) Series(s ...ChartSeries) *ChartBuilder {
	b.Props.Series = append(b.Props.Series, s...)
	return b
}
func (b *ChartBuilder) ShowGrid(s bool) *ChartBuilder    { b.Props.ShowGrid = s; return b }
func (b *ChartBuilder) ShowLegend(s bool) *ChartBuilder  { b.Props.ShowLegend = s; return b }
func (b *ChartBuilder) ShowTooltip(s bool) *ChartBuilder { b.Props.ShowTooltip = s; return b }
func (b *ChartBuilder) Height(h int) *ChartBuilder       { b.Props.Height = h; return b }
func (b *ChartBuilder) Stacked(s bool) *ChartBuilder     { b.Props.Stacked = s; return b }
func (b *ChartBuilder) ClassName(c string) *ChartBuilder { b.Props.ClassName = c; return b }
func (b *ChartBuilder) Build() contract.GraphNode        { return b.toNode(b.Props) }

// =============================================================================
// organism.code-editor
// =============================================================================

type CodeEditorProps struct {
	Name        string  `json:"name,omitempty"`
	Language    string  `json:"language,omitempty"`
	Value       string  `json:"value,omitempty"`
	Default     string  `json:"default,omitempty"`
	ReadOnly    bool    `json:"readOnly,omitempty"`
	Theme       string  `json:"theme,omitempty"` // "auto" | "light" | "dark"
	LineNumbers bool    `json:"lineNumbers,omitempty"`
	Height      string  `json:"height,omitempty"`
	OnChange    *Action `json:"onChange,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type CodeEditorBuilder struct {
	*Common[CodeEditorBuilder]
	Props CodeEditorProps
}

func CodeEditor() *CodeEditorBuilder {
	b := &CodeEditorBuilder{}
	b.Common = newCommon[CodeEditorBuilder]("organism.code-editor", b)
	b.Props.LineNumbers = true
	b.Props.Theme = "auto"
	return b
}

func (b *CodeEditorBuilder) Name(n string) *CodeEditorBuilder      { b.Props.Name = n; return b }
func (b *CodeEditorBuilder) Language(l string) *CodeEditorBuilder  { b.Props.Language = l; return b }
func (b *CodeEditorBuilder) Value(v string) *CodeEditorBuilder     { b.Props.Value = v; return b }
func (b *CodeEditorBuilder) Default(v string) *CodeEditorBuilder   { b.Props.Default = v; return b }
func (b *CodeEditorBuilder) ReadOnly(r bool) *CodeEditorBuilder    { b.Props.ReadOnly = r; return b }
func (b *CodeEditorBuilder) Theme(t string) *CodeEditorBuilder     { b.Props.Theme = t; return b }
func (b *CodeEditorBuilder) LineNumbers(s bool) *CodeEditorBuilder { b.Props.LineNumbers = s; return b }
func (b *CodeEditorBuilder) Height(h string) *CodeEditorBuilder    { b.Props.Height = h; return b }
func (b *CodeEditorBuilder) OnChange(a *Action) *CodeEditorBuilder { b.Props.OnChange = a; return b }
func (b *CodeEditorBuilder) ClassName(c string) *CodeEditorBuilder { b.Props.ClassName = c; return b }
func (b *CodeEditorBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// organism.notification-center
// =============================================================================

type NotificationCenterProps struct {
	TitleField        string  `json:"titleField,omitempty"`
	BodyField         string  `json:"bodyField,omitempty"`
	TimestampField    string  `json:"timestampField,omitempty"`
	ReadField         string  `json:"readField,omitempty"`
	IconField         string  `json:"iconField,omitempty"`
	HrefField         string  `json:"hrefField,omitempty"`
	OnMarkRead        *Action `json:"onMarkRead,omitempty"`
	OnMarkAllRead     *Action `json:"onMarkAllRead,omitempty"`
	OnClick           *Action `json:"onClick,omitempty"`
	UnreadOnlyDefault bool    `json:"unreadOnlyDefault,omitempty"`
	ClassName         string  `json:"className,omitempty"`
}

type NotificationCenterBuilder struct {
	*Common[NotificationCenterBuilder]
	Props NotificationCenterProps
}

func NotificationCenter() *NotificationCenterBuilder {
	b := &NotificationCenterBuilder{}
	b.Common = newCommon[NotificationCenterBuilder]("organism.notification-center", b)
	b.Props.TitleField = "title"
	b.Props.BodyField = "body"
	b.Props.TimestampField = "timestamp"
	b.Props.ReadField = "read"
	return b
}

func (b *NotificationCenterBuilder) TitleField(f string) *NotificationCenterBuilder {
	b.Props.TitleField = f
	return b
}
func (b *NotificationCenterBuilder) BodyField(f string) *NotificationCenterBuilder {
	b.Props.BodyField = f
	return b
}
func (b *NotificationCenterBuilder) TimestampField(f string) *NotificationCenterBuilder {
	b.Props.TimestampField = f
	return b
}
func (b *NotificationCenterBuilder) ReadField(f string) *NotificationCenterBuilder {
	b.Props.ReadField = f
	return b
}
func (b *NotificationCenterBuilder) IconField(f string) *NotificationCenterBuilder {
	b.Props.IconField = f
	return b
}
func (b *NotificationCenterBuilder) HrefField(f string) *NotificationCenterBuilder {
	b.Props.HrefField = f
	return b
}
func (b *NotificationCenterBuilder) OnMarkRead(a *Action) *NotificationCenterBuilder {
	b.Props.OnMarkRead = a
	return b
}
func (b *NotificationCenterBuilder) OnMarkAllRead(a *Action) *NotificationCenterBuilder {
	b.Props.OnMarkAllRead = a
	return b
}
func (b *NotificationCenterBuilder) OnClick(a *Action) *NotificationCenterBuilder {
	b.Props.OnClick = a
	return b
}
func (b *NotificationCenterBuilder) UnreadOnlyDefault(s bool) *NotificationCenterBuilder {
	b.Props.UnreadOnlyDefault = s
	return b
}
func (b *NotificationCenterBuilder) ClassName(c string) *NotificationCenterBuilder {
	b.Props.ClassName = c
	return b
}
func (b *NotificationCenterBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// organism.comments
// =============================================================================

type CommentsProps struct {
	AuthorField    string  `json:"authorField,omitempty"`
	AvatarField    string  `json:"avatarField,omitempty"`
	BodyField      string  `json:"bodyField,omitempty"`
	TimestampField string  `json:"timestampField,omitempty"`
	OnPost         *Action `json:"onPost,omitempty"`
	OnDelete       *Action `json:"onDelete,omitempty"`
	CurrentUserID  string  `json:"currentUserId,omitempty"`
	ReadOnly       bool    `json:"readOnly,omitempty"`
	ClassName      string  `json:"className,omitempty"`
}

type CommentsBuilder struct {
	*Common[CommentsBuilder]
	Props CommentsProps
}

func Comments() *CommentsBuilder {
	b := &CommentsBuilder{}
	b.Common = newCommon[CommentsBuilder]("organism.comments", b)
	b.Props.AuthorField = "author"
	b.Props.AvatarField = "avatar"
	b.Props.BodyField = "body"
	b.Props.TimestampField = "timestamp"
	return b
}

func (b *CommentsBuilder) AuthorField(f string) *CommentsBuilder { b.Props.AuthorField = f; return b }
func (b *CommentsBuilder) AvatarField(f string) *CommentsBuilder { b.Props.AvatarField = f; return b }
func (b *CommentsBuilder) BodyField(f string) *CommentsBuilder   { b.Props.BodyField = f; return b }
func (b *CommentsBuilder) TimestampField(f string) *CommentsBuilder {
	b.Props.TimestampField = f
	return b
}
func (b *CommentsBuilder) OnPost(a *Action) *CommentsBuilder   { b.Props.OnPost = a; return b }
func (b *CommentsBuilder) OnDelete(a *Action) *CommentsBuilder { b.Props.OnDelete = a; return b }
func (b *CommentsBuilder) CurrentUserID(id string) *CommentsBuilder {
	b.Props.CurrentUserID = id
	return b
}
func (b *CommentsBuilder) ReadOnly(r bool) *CommentsBuilder    { b.Props.ReadOnly = r; return b }
func (b *CommentsBuilder) ClassName(c string) *CommentsBuilder { b.Props.ClassName = c; return b }
func (b *CommentsBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }
