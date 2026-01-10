package sdk

import (
	"encoding/json"
	"time"
)

// Additional ContentPartTypes for UI tools.
const (
	PartTypeButtonGroup    ContentPartType = "button_group"
	PartTypeTimeline       ContentPartType = "timeline"
	PartTypeKanban         ContentPartType = "kanban"
	PartTypeMetric         ContentPartType = "metric"
	PartTypeForm           ContentPartType = "form"
	PartTypeTabs           ContentPartType = "tabs"
	PartTypeAccordion      ContentPartType = "accordion"
	PartTypeInlineCitation ContentPartType = "inline_citation"
	PartTypeCarousel       ContentPartType = "carousel"
	PartTypeGallery        ContentPartType = "gallery"
	PartTypeMap            ContentPartType = "map"
	PartTypeVideo          ContentPartType = "video"
	PartTypeAudio          ContentPartType = "audio"
	PartTypeEmbed          ContentPartType = "embed"
	PartTypeStats          ContentPartType = "stats"
)

// --- Button Group Part ---

// ButtonGroupPart represents a group of interactive buttons.
type ButtonGroupPart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Buttons     []Button        `json:"buttons"`
	Layout      ButtonLayout    `json:"layout,omitempty"`
	Alignment   string          `json:"alignment,omitempty"` // left, center, right
	Spacing     string          `json:"spacing,omitempty"`   // tight, normal, loose
}

// Button represents a single interactive button.
type Button struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Icon     string         `json:"icon,omitempty"`
	Action   ButtonAction   `json:"action"`
	Variant  ButtonVariant  `json:"variant,omitempty"`
	Size     string         `json:"size,omitempty"` // sm, md, lg
	Disabled bool           `json:"disabled,omitempty"`
	Loading  bool           `json:"loading,omitempty"`
	Tooltip  string         `json:"tooltip,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ButtonAction defines what happens when a button is clicked.
type ButtonAction struct {
	Type    ButtonActionType `json:"type"`
	Value   string           `json:"value,omitempty"`   // URL, tool name, etc.
	Payload map[string]any   `json:"payload,omitempty"` // Additional data
	Confirm *ConfirmDialog   `json:"confirm,omitempty"` // Confirmation before action
}

// ButtonActionType categorizes button actions.
type ButtonActionType string

const (
	ActionTypeLink     ButtonActionType = "link"     // Navigate to URL
	ActionTypeTool     ButtonActionType = "tool"     // Execute a tool
	ActionTypeCallback ButtonActionType = "callback" // Custom callback
	ActionTypeCopy     ButtonActionType = "copy"     // Copy text to clipboard
	ActionTypeDownload ButtonActionType = "download" // Download file
	ActionTypeSubmit   ButtonActionType = "submit"   // Submit form data
	ActionTypeDismiss  ButtonActionType = "dismiss"  // Dismiss/close
	ActionTypeExpand   ButtonActionType = "expand"   // Expand/show more
)

// ButtonVariant defines button styling.
type ButtonVariant string

const (
	ButtonPrimary   ButtonVariant = "primary"
	ButtonSecondary ButtonVariant = "secondary"
	ButtonOutline   ButtonVariant = "outline"
	ButtonGhost     ButtonVariant = "ghost"
	ButtonDanger    ButtonVariant = "danger"
	ButtonSuccess   ButtonVariant = "success"
	ButtonWarning   ButtonVariant = "warning"
	ButtonLink      ButtonVariant = "link"
)

// ButtonLayout defines how buttons are arranged.
type ButtonLayout string

const (
	ButtonLayoutHorizontal ButtonLayout = "horizontal"
	ButtonLayoutVertical   ButtonLayout = "vertical"
	ButtonLayoutGrid       ButtonLayout = "grid"
	ButtonLayoutWrap       ButtonLayout = "wrap"
)

// ConfirmDialog defines a confirmation dialog before action.
type ConfirmDialog struct {
	Title       string `json:"title"`
	Message     string `json:"message"`
	ConfirmText string `json:"confirmText,omitempty"`
	CancelText  string `json:"cancelText,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

func (b *ButtonGroupPart) Type() ContentPartType { return PartTypeButtonGroup }
func (b *ButtonGroupPart) ToJSON() ([]byte, error) {
	b.PartType = PartTypeButtonGroup

	return json.Marshal(b)
}

// --- Timeline Part ---

// TimelinePart represents a chronological event display.
type TimelinePart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Events      []TimelineEvent `json:"events"`
	Layout      TimelineLayout  `json:"layout,omitempty"`
	Orientation string          `json:"orientation,omitempty"` // vertical, horizontal
	Sortable    bool            `json:"sortable,omitempty"`
	Collapsible bool            `json:"collapsible,omitempty"`
}

// TimelineEvent represents a single event on the timeline.
type TimelineEvent struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
	EndTime     *time.Time     `json:"endTime,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	Color       string         `json:"color,omitempty"`
	Status      TimelineStatus `json:"status,omitempty"`
	Link        string         `json:"link,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Media       *TimelineMedia `json:"media,omitempty"`
	Actions     []Button       `json:"actions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TimelineStatus indicates the status of a timeline event.
type TimelineStatus string

const (
	TimelineStatusPending    TimelineStatus = "pending"
	TimelineStatusInProgress TimelineStatus = "in_progress"
	TimelineStatusCompleted  TimelineStatus = "completed"
	TimelineStatusCancelled  TimelineStatus = "cancelled"
	TimelineStatusError      TimelineStatus = "error"
)

// TimelineMedia represents media attached to a timeline event.
type TimelineMedia struct {
	Type      string `json:"type"` // image, video, audio, document
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Title     string `json:"title,omitempty"`
}

// TimelineLayout defines timeline styling.
type TimelineLayout string

const (
	TimelineLayoutDefault   TimelineLayout = "default"
	TimelineLayoutAlternate TimelineLayout = "alternate"
	TimelineLayoutCompact   TimelineLayout = "compact"
	TimelineLayoutDetailed  TimelineLayout = "detailed"
)

func (t *TimelinePart) Type() ContentPartType { return PartTypeTimeline }
func (t *TimelinePart) ToJSON() ([]byte, error) {
	t.PartType = PartTypeTimeline

	return json.Marshal(t)
}

// --- Kanban Part ---

// KanbanPart represents a kanban/board view.
type KanbanPart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Columns     []KanbanColumn  `json:"columns"`
	Draggable   bool            `json:"draggable,omitempty"`
	Collapsible bool            `json:"collapsible,omitempty"`
	ShowCounts  bool            `json:"showCounts,omitempty"`
	ShowSearch  bool            `json:"showSearch,omitempty"`
}

// KanbanColumn represents a column in the kanban board.
type KanbanColumn struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Color     string       `json:"color,omitempty"`
	Icon      string       `json:"icon,omitempty"`
	Limit     int          `json:"limit,omitempty"` // WIP limit
	Cards     []KanbanCard `json:"cards"`
	Collapsed bool         `json:"collapsed,omitempty"`
	Actions   []Button     `json:"actions,omitempty"`
}

// KanbanCard represents a card in a kanban column.
type KanbanCard struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Labels      []KanbanLabel  `json:"labels,omitempty"`
	Assignees   []KanbanUser   `json:"assignees,omitempty"`
	DueDate     *time.Time     `json:"dueDate,omitempty"`
	Priority    string         `json:"priority,omitempty"` // low, medium, high, urgent
	Progress    *float64       `json:"progress,omitempty"` // 0-100
	Attachments int            `json:"attachments,omitempty"`
	Comments    int            `json:"comments,omitempty"`
	Link        string         `json:"link,omitempty"`
	Thumbnail   string         `json:"thumbnail,omitempty"`
	Actions     []Button       `json:"actions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// KanbanLabel represents a label/tag on a kanban card.
type KanbanLabel struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
}

// KanbanUser represents a user assigned to a card.
type KanbanUser struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

func (k *KanbanPart) Type() ContentPartType { return PartTypeKanban }
func (k *KanbanPart) ToJSON() ([]byte, error) {
	k.PartType = PartTypeKanban

	return json.Marshal(k)
}

// --- Metric Part ---

// MetricPart represents a KPI/metric display.
type MetricPart struct {
	PartType    ContentPartType `json:"type"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Metrics     []Metric        `json:"metrics"`
	Layout      MetricLayout    `json:"layout,omitempty"`
	Columns     int             `json:"columns,omitempty"` // Grid columns
}

// Metric represents a single metric/KPI.
type Metric struct {
	ID             string         `json:"id"`
	Label          string         `json:"label"`
	Value          any            `json:"value"`
	FormattedValue string         `json:"formattedValue,omitempty"`
	Unit           string         `json:"unit,omitempty"`
	Icon           string         `json:"icon,omitempty"`
	Color          string         `json:"color,omitempty"`
	Trend          *MetricTrend   `json:"trend,omitempty"`
	Sparkline      []float64      `json:"sparkline,omitempty"`
	Target         *MetricTarget  `json:"target,omitempty"`
	Status         MetricStatus   `json:"status,omitempty"`
	Period         string         `json:"period,omitempty"` // e.g., "Last 7 days"
	LastUpdated    *time.Time     `json:"lastUpdated,omitempty"`
	Link           string         `json:"link,omitempty"`
	Description    string         `json:"description,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// MetricTrend represents the trend direction and change.
type MetricTrend struct {
	Direction  TrendDirection `json:"direction"`
	Value      float64        `json:"value"`
	Percentage float64        `json:"percentage,omitempty"`
	Period     string         `json:"period,omitempty"`
}

// TrendDirection indicates metric trend.
type TrendDirection string

const (
	TrendUp     TrendDirection = "up"
	TrendDown   TrendDirection = "down"
	TrendStable TrendDirection = "stable"
)

// MetricTarget represents a target/goal for the metric.
type MetricTarget struct {
	Value    any     `json:"value"`
	Label    string  `json:"label,omitempty"`
	Progress float64 `json:"progress,omitempty"` // 0-100
	Achieved bool    `json:"achieved,omitempty"`
}

// MetricStatus indicates the metric status.
type MetricStatus string

const (
	MetricStatusGood    MetricStatus = "good"
	MetricStatusWarning MetricStatus = "warning"
	MetricStatusBad     MetricStatus = "bad"
	MetricStatusNeutral MetricStatus = "neutral"
)

// MetricLayout defines metric display style.
type MetricLayout string

const (
	MetricLayoutGrid    MetricLayout = "grid"
	MetricLayoutList    MetricLayout = "list"
	MetricLayoutCompact MetricLayout = "compact"
	MetricLayoutCards   MetricLayout = "cards"
)

func (m *MetricPart) Type() ContentPartType { return PartTypeMetric }
func (m *MetricPart) ToJSON() ([]byte, error) {
	m.PartType = PartTypeMetric

	return json.Marshal(m)
}

// --- Form Part ---

// FormPart represents an interactive form.
type FormPart struct {
	PartType      ContentPartType `json:"type"`
	ID            string          `json:"id"`
	Title         string          `json:"title,omitempty"`
	Description   string          `json:"description,omitempty"`
	Fields        []FormField     `json:"fields"`
	Sections      []FormSection   `json:"sections,omitempty"` // For grouped fields
	Actions       []Button        `json:"actions,omitempty"`
	Layout        FormLayout      `json:"layout,omitempty"`
	Validation    *FormValidation `json:"validation,omitempty"`
	DefaultValues map[string]any  `json:"defaultValues,omitempty"`
	Disabled      bool            `json:"disabled,omitempty"`
	ReadOnly      bool            `json:"readOnly,omitempty"`
}

// FormField represents a single form field.
type FormField struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Label        string            `json:"label"`
	Type         FormFieldType     `json:"type"`
	Placeholder  string            `json:"placeholder,omitempty"`
	Description  string            `json:"description,omitempty"`
	Required     bool              `json:"required,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
	DefaultValue any               `json:"defaultValue,omitempty"`
	Options      []FormFieldOption `json:"options,omitempty"` // For select, radio, checkbox
	Validation   *FieldValidation  `json:"validation,omitempty"`
	Conditions   []FieldCondition  `json:"conditions,omitempty"` // Show/hide based on other fields
	Width        string            `json:"width,omitempty"`      // full, half, third
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// FormFieldType categorizes form field types.
type FormFieldType string

const (
	FieldTypeText        FormFieldType = "text"
	FieldTypeTextarea    FormFieldType = "textarea"
	FieldTypeNumber      FormFieldType = "number"
	FieldTypeEmail       FormFieldType = "email"
	FieldTypePassword    FormFieldType = "password"
	FieldTypeSelect      FormFieldType = "select"
	FieldTypeMultiSelect FormFieldType = "multi_select"
	FieldTypeRadio       FormFieldType = "radio"
	FieldTypeCheckbox    FormFieldType = "checkbox"
	FieldTypeToggle      FormFieldType = "toggle"
	FieldTypeDate        FormFieldType = "date"
	FieldTypeDateTime    FormFieldType = "datetime"
	FieldTypeTime        FormFieldType = "time"
	FieldTypeFile        FormFieldType = "file"
	FieldTypeRange       FormFieldType = "range"
	FieldTypeColor       FormFieldType = "color"
	FieldTypeRating      FormFieldType = "rating"
	FieldTypeSlider      FormFieldType = "slider"
	FieldTypeRichText    FormFieldType = "rich_text"
	FieldTypeJSON        FormFieldType = "json"
	FieldTypeHidden      FormFieldType = "hidden"
)

// FormFieldOption represents an option for select/radio/checkbox fields.
type FormFieldOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Group    string `json:"group,omitempty"` // For grouped options
}

// FieldValidation defines validation rules for a field.
type FieldValidation struct {
	MinLength   *int     `json:"minLength,omitempty"`
	MaxLength   *int     `json:"maxLength,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Pattern     string   `json:"pattern,omitempty"` // Regex pattern
	CustomError string   `json:"customError,omitempty"`
}

// FieldCondition defines when a field should be shown/hidden.
type FieldCondition struct {
	Field    string `json:"field"`    // Field to watch
	Operator string `json:"operator"` // eq, neq, gt, lt, contains, etc.
	Value    any    `json:"value"`
	Action   string `json:"action"` // show, hide, enable, disable
}

// FormSection groups form fields.
type FormSection struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Fields      []FormField `json:"fields"`
	Collapsible bool        `json:"collapsible,omitempty"`
	Collapsed   bool        `json:"collapsed,omitempty"`
}

// FormValidation defines form-level validation.
type FormValidation struct {
	ValidateOnChange bool   `json:"validateOnChange,omitempty"`
	ValidateOnBlur   bool   `json:"validateOnBlur,omitempty"`
	ShowErrors       bool   `json:"showErrors,omitempty"`
	ErrorPosition    string `json:"errorPosition,omitempty"` // inline, top, bottom
}

// FormLayout defines form layout options.
type FormLayout string

const (
	FormLayoutVertical   FormLayout = "vertical"
	FormLayoutHorizontal FormLayout = "horizontal"
	FormLayoutInline     FormLayout = "inline"
	FormLayoutGrid       FormLayout = "grid"
)

func (f *FormPart) Type() ContentPartType { return PartTypeForm }
func (f *FormPart) ToJSON() ([]byte, error) {
	f.PartType = PartTypeForm

	return json.Marshal(f)
}

// --- Tabs Part ---

// TabsPart represents tabbed content sections.
type TabsPart struct {
	PartType    ContentPartType `json:"type"`
	Tabs        []Tab           `json:"tabs"`
	DefaultTab  string          `json:"defaultTab,omitempty"`
	Orientation string          `json:"orientation,omitempty"` // horizontal, vertical
	Variant     string          `json:"variant,omitempty"`     // default, pills, underline
}

// Tab represents a single tab.
type Tab struct {
	ID       string        `json:"id"`
	Label    string        `json:"label"`
	Icon     string        `json:"icon,omitempty"`
	Badge    string        `json:"badge,omitempty"`
	Disabled bool          `json:"disabled,omitempty"`
	Content  []ContentPart `json:"content"`
}

func (t *TabsPart) Type() ContentPartType { return PartTypeTabs }
func (t *TabsPart) ToJSON() ([]byte, error) {
	t.PartType = PartTypeTabs

	return json.Marshal(t)
}

// --- Accordion Part ---

// AccordionPart represents expandable/collapsible sections.
type AccordionPart struct {
	PartType      ContentPartType `json:"type"`
	Items         []AccordionItem `json:"items"`
	AllowMultiple bool            `json:"allowMultiple,omitempty"`
	DefaultOpen   []string        `json:"defaultOpen,omitempty"` // IDs of initially open items
	Variant       string          `json:"variant,omitempty"`     // default, bordered, separated
}

// AccordionItem represents a single accordion section.
type AccordionItem struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle,omitempty"`
	Icon     string        `json:"icon,omitempty"`
	Badge    string        `json:"badge,omitempty"`
	Disabled bool          `json:"disabled,omitempty"`
	Content  []ContentPart `json:"content"`
}

func (a *AccordionPart) Type() ContentPartType { return PartTypeAccordion }
func (a *AccordionPart) ToJSON() ([]byte, error) {
	a.PartType = PartTypeAccordion

	return json.Marshal(a)
}

// --- Inline Citation Part ---

// InlineCitationPart represents inline citations with reference links.
type InlineCitationPart struct {
	PartType    ContentPartType  `json:"type"`
	Text        string           `json:"text"` // The cited text
	Citations   []InlineCitation `json:"citations"`
	ShowNumbers bool             `json:"showNumbers,omitempty"` // Show [1], [2], etc.
	Clickable   bool             `json:"clickable,omitempty"`   // Show popover on click
}

// InlineCitation represents a single inline citation.
type InlineCitation struct {
	ID       string  `json:"id"`
	Number   int     `json:"number,omitempty"`
	Position int     `json:"position"` // Character position in text
	Length   int     `json:"length"`   // Length of cited text
	Source   *Source `json:"source"`
}

// Source represents a citation source.
type Source struct {
	Title       string     `json:"title"`
	URL         string     `json:"url,omitempty"`
	Author      string     `json:"author,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	Type        string     `json:"type,omitempty"` // article, book, website, etc.
	Publisher   string     `json:"publisher,omitempty"`
	Excerpt     string     `json:"excerpt,omitempty"`
	Confidence  float64    `json:"confidence,omitempty"` // 0-1 confidence score
	Icon        string     `json:"icon,omitempty"`
}

func (i *InlineCitationPart) Type() ContentPartType { return PartTypeInlineCitation }
func (i *InlineCitationPart) ToJSON() ([]byte, error) {
	i.PartType = PartTypeInlineCitation

	return json.Marshal(i)
}

// --- Stats Part (Multiple metrics in a compact display) ---

// StatsPart represents a collection of stats in a compact display.
type StatsPart struct {
	PartType ContentPartType `json:"type"`
	Title    string          `json:"title,omitempty"`
	Stats    []Stat          `json:"stats"`
	Layout   string          `json:"layout,omitempty"`  // row, grid, column
	Variant  string          `json:"variant,omitempty"` // default, card, minimal
}

// Stat represents a single stat.
type Stat struct {
	Label       string `json:"label"`
	Value       any    `json:"value"`
	Unit        string `json:"unit,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Change      string `json:"change,omitempty"`      // "+5%", "-10", etc.
	ChangeColor string `json:"changeColor,omitempty"` // green, red, etc.
}

func (s *StatsPart) Type() ContentPartType { return PartTypeStats }
func (s *StatsPart) ToJSON() ([]byte, error) {
	s.PartType = PartTypeStats

	return json.Marshal(s)
}

// --- Carousel Part ---

// CarouselPart represents a carousel/slider of content.
type CarouselPart struct {
	PartType    ContentPartType `json:"type"`
	Items       []CarouselItem  `json:"items"`
	AutoPlay    bool            `json:"autoPlay,omitempty"`
	Interval    int             `json:"interval,omitempty"` // ms
	ShowDots    bool            `json:"showDots,omitempty"`
	ShowArrows  bool            `json:"showArrows,omitempty"`
	Loop        bool            `json:"loop,omitempty"`
	ItemsToShow int             `json:"itemsToShow,omitempty"`
}

// CarouselItem represents a single carousel item.
type CarouselItem struct {
	ID          string        `json:"id"`
	Content     []ContentPart `json:"content,omitempty"` // Custom content
	Image       string        `json:"image,omitempty"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Link        string        `json:"link,omitempty"`
	Actions     []Button      `json:"actions,omitempty"`
}

func (c *CarouselPart) Type() ContentPartType { return PartTypeCarousel }
func (c *CarouselPart) ToJSON() ([]byte, error) {
	c.PartType = PartTypeCarousel

	return json.Marshal(c)
}

// --- Gallery Part ---

// GalleryPart represents an image/media gallery.
type GalleryPart struct {
	PartType ContentPartType `json:"type"`
	Title    string          `json:"title,omitempty"`
	Items    []GalleryItem   `json:"items"`
	Layout   string          `json:"layout,omitempty"` // grid, masonry, list
	Columns  int             `json:"columns,omitempty"`
	Gap      string          `json:"gap,omitempty"`
	Lightbox bool            `json:"lightbox,omitempty"`
}

// GalleryItem represents a single gallery item.
type GalleryItem struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"` // image, video
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

func (g *GalleryPart) Type() ContentPartType { return PartTypeGallery }
func (g *GalleryPart) ToJSON() ([]byte, error) {
	g.PartType = PartTypeGallery

	return json.Marshal(g)
}

// --- Builder Functions ---

// NewButtonGroupPart creates a new button group part.
func NewButtonGroupPart(buttons ...Button) *ButtonGroupPart {
	return &ButtonGroupPart{
		PartType: PartTypeButtonGroup,
		Buttons:  buttons,
		Layout:   ButtonLayoutHorizontal,
	}
}

// NewTimelinePart creates a new timeline part.
func NewTimelinePart(events ...TimelineEvent) *TimelinePart {
	return &TimelinePart{
		PartType:    PartTypeTimeline,
		Events:      events,
		Orientation: "vertical",
	}
}

// NewKanbanPart creates a new kanban part.
func NewKanbanPart(columns ...KanbanColumn) *KanbanPart {
	return &KanbanPart{
		PartType:   PartTypeKanban,
		Columns:    columns,
		Draggable:  true,
		ShowCounts: true,
	}
}

// NewMetricPart creates a new metric part.
func NewMetricPart(metrics ...Metric) *MetricPart {
	return &MetricPart{
		PartType: PartTypeMetric,
		Metrics:  metrics,
		Layout:   MetricLayoutGrid,
		Columns:  3,
	}
}

// NewFormPart creates a new form part.
func NewFormPart(id string, fields ...FormField) *FormPart {
	return &FormPart{
		PartType: PartTypeForm,
		ID:       id,
		Fields:   fields,
		Layout:   FormLayoutVertical,
	}
}

// NewTabsPart creates a new tabs part.
func NewTabsPart(tabs ...Tab) *TabsPart {
	var defaultTab string
	if len(tabs) > 0 {
		defaultTab = tabs[0].ID
	}

	return &TabsPart{
		PartType:    PartTypeTabs,
		Tabs:        tabs,
		DefaultTab:  defaultTab,
		Orientation: "horizontal",
	}
}

// NewAccordionPart creates a new accordion part.
func NewAccordionPart(items ...AccordionItem) *AccordionPart {
	return &AccordionPart{
		PartType:      PartTypeAccordion,
		Items:         items,
		AllowMultiple: false,
	}
}

// NewInlineCitationPart creates a new inline citation part.
func NewInlineCitationPart(text string, citations ...InlineCitation) *InlineCitationPart {
	return &InlineCitationPart{
		PartType:    PartTypeInlineCitation,
		Text:        text,
		Citations:   citations,
		ShowNumbers: true,
		Clickable:   true,
	}
}

// NewStatsPart creates a new stats part.
func NewStatsPart(stats ...Stat) *StatsPart {
	return &StatsPart{
		PartType: PartTypeStats,
		Stats:    stats,
		Layout:   "row",
	}
}

// NewCarouselPart creates a new carousel part.
func NewCarouselPart(items ...CarouselItem) *CarouselPart {
	return &CarouselPart{
		PartType:    PartTypeCarousel,
		Items:       items,
		ShowDots:    true,
		ShowArrows:  true,
		Loop:        true,
		ItemsToShow: 1,
	}
}

// NewGalleryPart creates a new gallery part.
func NewGalleryPart(items ...GalleryItem) *GalleryPart {
	return &GalleryPart{
		PartType: PartTypeGallery,
		Items:    items,
		Layout:   "grid",
		Columns:  3,
		Lightbox: true,
	}
}

// --- Button Builder ---

// ButtonBuilder provides a fluent API for creating buttons.
type ButtonBuilder struct {
	button Button
}

// NewButton creates a new button builder.
func NewButton(id, label string) *ButtonBuilder {
	return &ButtonBuilder{
		button: Button{
			ID:      id,
			Label:   label,
			Variant: ButtonPrimary,
			Size:    "md",
		},
	}
}

// WithIcon sets the button icon.
func (b *ButtonBuilder) WithIcon(icon string) *ButtonBuilder {
	b.button.Icon = icon

	return b
}

// WithVariant sets the button variant.
func (b *ButtonBuilder) WithVariant(variant ButtonVariant) *ButtonBuilder {
	b.button.Variant = variant

	return b
}

// WithSize sets the button size.
func (b *ButtonBuilder) WithSize(size string) *ButtonBuilder {
	b.button.Size = size

	return b
}

// WithTooltip sets the button tooltip.
func (b *ButtonBuilder) WithTooltip(tooltip string) *ButtonBuilder {
	b.button.Tooltip = tooltip

	return b
}

// Disabled sets the button as disabled.
func (b *ButtonBuilder) Disabled() *ButtonBuilder {
	b.button.Disabled = true

	return b
}

// WithLinkAction sets a link action.
func (b *ButtonBuilder) WithLinkAction(url string) *ButtonBuilder {
	b.button.Action = ButtonAction{
		Type:  ActionTypeLink,
		Value: url,
	}

	return b
}

// WithToolAction sets a tool execution action.
func (b *ButtonBuilder) WithToolAction(toolName string, payload map[string]any) *ButtonBuilder {
	b.button.Action = ButtonAction{
		Type:    ActionTypeTool,
		Value:   toolName,
		Payload: payload,
	}

	return b
}

// WithCallbackAction sets a callback action.
func (b *ButtonBuilder) WithCallbackAction(callback string, payload map[string]any) *ButtonBuilder {
	b.button.Action = ButtonAction{
		Type:    ActionTypeCallback,
		Value:   callback,
		Payload: payload,
	}

	return b
}

// WithCopyAction sets a copy-to-clipboard action.
func (b *ButtonBuilder) WithCopyAction(text string) *ButtonBuilder {
	b.button.Action = ButtonAction{
		Type:  ActionTypeCopy,
		Value: text,
	}

	return b
}

// WithConfirm adds a confirmation dialog before action.
func (b *ButtonBuilder) WithConfirm(title, message string) *ButtonBuilder {
	b.button.Action.Confirm = &ConfirmDialog{
		Title:   title,
		Message: message,
	}

	return b
}

// Build returns the completed button.
func (b *ButtonBuilder) Build() Button {
	return b.button
}
