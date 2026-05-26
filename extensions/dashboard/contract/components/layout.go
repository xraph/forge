package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// layout.row
// =============================================================================

// RowProps configures a horizontal flex container. Children render side by
// side; wrap onto new lines when Wrap is true.
type RowProps struct {
	Gap       Spacing `json:"gap,omitempty"`
	Align     Align   `json:"align,omitempty"`
	Justify   Justify `json:"justify,omitempty"`
	Wrap      bool    `json:"wrap,omitempty"`
	Padding   Spacing `json:"padding,omitempty"`
	ClassName string  `json:"className,omitempty"`
}

// RowBuilder is the typed builder for a layout.row node.
type RowBuilder struct {
	*Common[RowBuilder]
	Props RowProps
}

// Row constructs a layout.row builder. Children passed here flow into the
// "children" slot.
func Row(children ...contract.GraphNode) *RowBuilder {
	b := &RowBuilder{}
	b.Common = newCommon[RowBuilder]("layout.row", b)
	b.children = append(b.children, children...)
	return b
}

// Gap sets the gap between children.
func (b *RowBuilder) Gap(g Spacing) *RowBuilder { b.Props.Gap = g; return b }

// Align sets the cross-axis alignment.
func (b *RowBuilder) Align(a Align) *RowBuilder { b.Props.Align = a; return b }

// Justify sets the main-axis justification.
func (b *RowBuilder) Justify(j Justify) *RowBuilder { b.Props.Justify = j; return b }

// Wrap enables flex wrapping onto new lines.
func (b *RowBuilder) Wrap(w bool) *RowBuilder { b.Props.Wrap = w; return b }

// Padding sets the padding around the row.
func (b *RowBuilder) Padding(p Spacing) *RowBuilder { b.Props.Padding = p; return b }

// ClassName appends extra Tailwind classes (escape hatch).
func (b *RowBuilder) ClassName(c string) *RowBuilder { b.Props.ClassName = c; return b }

// Build assembles the GraphNode.
func (b *RowBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// layout.column
// =============================================================================

// ColumnProps configures a vertical flex container.
type ColumnProps struct {
	Gap       Spacing `json:"gap,omitempty"`
	Align     Align   `json:"align,omitempty"`
	Justify   Justify `json:"justify,omitempty"`
	Padding   Spacing `json:"padding,omitempty"`
	ClassName string  `json:"className,omitempty"`
}

type ColumnBuilder struct {
	*Common[ColumnBuilder]
	Props ColumnProps
}

// Column constructs a layout.column builder.
func Column(children ...contract.GraphNode) *ColumnBuilder {
	b := &ColumnBuilder{}
	b.Common = newCommon[ColumnBuilder]("layout.column", b)
	b.children = append(b.children, children...)
	return b
}

func (b *ColumnBuilder) Gap(g Spacing) *ColumnBuilder      { b.Props.Gap = g; return b }
func (b *ColumnBuilder) Align(a Align) *ColumnBuilder      { b.Props.Align = a; return b }
func (b *ColumnBuilder) Justify(j Justify) *ColumnBuilder  { b.Props.Justify = j; return b }
func (b *ColumnBuilder) Padding(p Spacing) *ColumnBuilder  { b.Props.Padding = p; return b }
func (b *ColumnBuilder) ClassName(c string) *ColumnBuilder { b.Props.ClassName = c; return b }
func (b *ColumnBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// layout.grid
// =============================================================================

// GridProps configures a CSS grid container. Cols accepts a fixed integer
// (use Cols(n)) or a responsive ResponsiveCols struct.
type GridProps struct {
	Cols      ResponsiveCols `json:"cols,omitempty"`
	Gap       Spacing        `json:"gap,omitempty"`
	RowGap    Spacing        `json:"rowGap,omitempty"`
	ColGap    Spacing        `json:"colGap,omitempty"`
	Padding   Spacing        `json:"padding,omitempty"`
	ClassName string         `json:"className,omitempty"`
}

type GridBuilder struct {
	*Common[GridBuilder]
	Props GridProps
}

// Grid constructs a layout.grid builder.
func Grid(children ...contract.GraphNode) *GridBuilder {
	b := &GridBuilder{}
	b.Common = newCommon[GridBuilder]("layout.grid", b)
	b.children = append(b.children, children...)
	return b
}

// Cols sets a fixed column count across all breakpoints. Use ResponsiveCols
// directly for per-breakpoint counts.
func (b *GridBuilder) Cols(n int) *GridBuilder                  { b.Props.Cols = Cols(n); return b }
func (b *GridBuilder) Responsive(c ResponsiveCols) *GridBuilder { b.Props.Cols = c; return b }
func (b *GridBuilder) Gap(g Spacing) *GridBuilder               { b.Props.Gap = g; return b }
func (b *GridBuilder) RowGap(g Spacing) *GridBuilder            { b.Props.RowGap = g; return b }
func (b *GridBuilder) ColGap(g Spacing) *GridBuilder            { b.Props.ColGap = g; return b }
func (b *GridBuilder) Padding(p Spacing) *GridBuilder           { b.Props.Padding = p; return b }
func (b *GridBuilder) ClassName(c string) *GridBuilder          { b.Props.ClassName = c; return b }
func (b *GridBuilder) Build() contract.GraphNode                { return b.toNode(b.Props) }

// =============================================================================
// layout.stack
// =============================================================================

// StackProps configures a uniformly-spaced stack with an optional divider
// rendered between children.
type StackProps struct {
	Direction Direction `json:"direction,omitempty"`
	Gap       Spacing   `json:"gap,omitempty"`
	Divider   bool      `json:"divider,omitempty"`
	Padding   Spacing   `json:"padding,omitempty"`
	ClassName string    `json:"className,omitempty"`
}

type StackBuilder struct {
	*Common[StackBuilder]
	Props StackProps
}

func Stack(children ...contract.GraphNode) *StackBuilder {
	b := &StackBuilder{}
	b.Common = newCommon[StackBuilder]("layout.stack", b)
	b.children = append(b.children, children...)
	return b
}

func (b *StackBuilder) Direction(d Direction) *StackBuilder { b.Props.Direction = d; return b }
func (b *StackBuilder) Gap(g Spacing) *StackBuilder         { b.Props.Gap = g; return b }
func (b *StackBuilder) Divider(d bool) *StackBuilder        { b.Props.Divider = d; return b }
func (b *StackBuilder) Padding(p Spacing) *StackBuilder     { b.Props.Padding = p; return b }
func (b *StackBuilder) ClassName(c string) *StackBuilder    { b.Props.ClassName = c; return b }
func (b *StackBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// layout.container
// =============================================================================

// ContainerProps configures a max-width wrapper.
type ContainerProps struct {
	Size      ContainerSize `json:"size,omitempty"`
	Padding   Spacing       `json:"padding,omitempty"`
	ClassName string        `json:"className,omitempty"`
}

type ContainerBuilder struct {
	*Common[ContainerBuilder]
	Props ContainerProps
}

func Container(children ...contract.GraphNode) *ContainerBuilder {
	b := &ContainerBuilder{}
	b.Common = newCommon[ContainerBuilder]("layout.container", b)
	b.children = append(b.children, children...)
	return b
}

func (b *ContainerBuilder) Size(s ContainerSize) *ContainerBuilder { b.Props.Size = s; return b }
func (b *ContainerBuilder) Padding(p Spacing) *ContainerBuilder    { b.Props.Padding = p; return b }
func (b *ContainerBuilder) ClassName(c string) *ContainerBuilder   { b.Props.ClassName = c; return b }
func (b *ContainerBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// layout.section
// =============================================================================

// SectionProps configures a semantic section with optional title block.
// The visible heading text lives on node.title (use the inherited Title()
// setter). The `actions` slot renders top-right of the section header.
type SectionProps struct {
	Description string `json:"description,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
	ClassName   string `json:"className,omitempty"`
}

type SectionBuilder struct {
	*Common[SectionBuilder]
	Props SectionProps
}

// Section constructs a layout.section builder. Children flow into the
// "content" slot (sections always render a header before their content).
func Section(children ...contract.GraphNode) *SectionBuilder {
	b := &SectionBuilder{}
	b.Common = newCommon[SectionBuilder]("layout.section", b)
	b.setChildrenKey("content")
	b.children = append(b.children, children...)
	return b
}

func (b *SectionBuilder) Description(d string) *SectionBuilder { b.Props.Description = d; return b }
func (b *SectionBuilder) Compact(c bool) *SectionBuilder       { b.Props.Compact = c; return b }
func (b *SectionBuilder) ClassName(c string) *SectionBuilder   { b.Props.ClassName = c; return b }

// Actions attaches CTA nodes to the section header's actions slot.
func (b *SectionBuilder) Actions(nodes ...contract.GraphNode) *SectionBuilder {
	return b.Slot("actions", nodes...)
}

func (b *SectionBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// layout.card
// =============================================================================

// CardProps configures a shadcn Card. Slots: header, content, footer, actions.
// The visible title lives on node.title (use the inherited Title() setter).
type CardProps struct {
	Description string `json:"description,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
	NoPadding   bool   `json:"noPadding,omitempty"`
	Bordered    *bool  `json:"bordered,omitempty"`
	ClassName   string `json:"className,omitempty"`
}

type CardBuilder struct {
	*Common[CardBuilder]
	Props CardProps
}

// Card constructs a layout.card builder. Children flow into the "content"
// slot by default.
func Card(children ...contract.GraphNode) *CardBuilder {
	b := &CardBuilder{}
	b.Common = newCommon[CardBuilder]("layout.card", b)
	b.setChildrenKey("content")
	b.children = append(b.children, children...)
	return b
}

func (b *CardBuilder) Description(d string) *CardBuilder { b.Props.Description = d; return b }
func (b *CardBuilder) Compact(c bool) *CardBuilder       { b.Props.Compact = c; return b }
func (b *CardBuilder) NoPadding(n bool) *CardBuilder     { b.Props.NoPadding = n; return b }
func (b *CardBuilder) Bordered(bo bool) *CardBuilder     { b.Props.Bordered = &bo; return b }
func (b *CardBuilder) ClassName(c string) *CardBuilder   { b.Props.ClassName = c; return b }

// Content explicitly appends to the content slot. Equivalent to Children().
func (b *CardBuilder) Content(nodes ...contract.GraphNode) *CardBuilder {
	return b.Slot("content", nodes...)
}

// Header attaches nodes to the card header slot (above the title).
func (b *CardBuilder) Header(nodes ...contract.GraphNode) *CardBuilder {
	return b.Slot("header", nodes...)
}

// Footer attaches nodes to the card footer slot.
func (b *CardBuilder) Footer(nodes ...contract.GraphNode) *CardBuilder {
	return b.Slot("footer", nodes...)
}

// Actions attaches CTA nodes to the card header's actions slot.
func (b *CardBuilder) Actions(nodes ...contract.GraphNode) *CardBuilder {
	return b.Slot("actions", nodes...)
}

func (b *CardBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// layout.tabs
// =============================================================================

// TabItem describes one tab. Children are rendered in the slot whose name
// matches the Value field.
type TabItem struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Badge       string `json:"badge,omitempty"`
}

// TabsProps configures a shadcn Tabs container.
type TabsProps struct {
	DefaultValue string      `json:"defaultValue,omitempty"`
	Orientation  Orientation `json:"orientation,omitempty"`
	Items        []TabItem   `json:"items"`
	Variant      string      `json:"variant,omitempty"` // "default" | "underline" | "pills"
	ClassName    string      `json:"className,omitempty"`
}

type TabsBuilder struct {
	*Common[TabsBuilder]
	Props TabsProps
}

// Tabs constructs a layout.tabs builder.
func Tabs() *TabsBuilder {
	b := &TabsBuilder{}
	b.Common = newCommon[TabsBuilder]("layout.tabs", b)
	return b
}

// Item adds a tab. Pass content via Panel(value, nodes...).
func (b *TabsBuilder) Item(value, label string) *TabsBuilder {
	b.Props.Items = append(b.Props.Items, TabItem{Value: value, Label: label})
	return b
}

// AddItem adds a fully-specified tab definition.
func (b *TabsBuilder) AddItem(item TabItem) *TabsBuilder {
	b.Props.Items = append(b.Props.Items, item)
	return b
}

// Panel attaches nodes to the slot for a specific tab value.
func (b *TabsBuilder) Panel(value string, nodes ...contract.GraphNode) *TabsBuilder {
	return b.Slot(value, nodes...)
}

func (b *TabsBuilder) Default(v string) *TabsBuilder          { b.Props.DefaultValue = v; return b }
func (b *TabsBuilder) Orientation(o Orientation) *TabsBuilder { b.Props.Orientation = o; return b }
func (b *TabsBuilder) Variant(v string) *TabsBuilder          { b.Props.Variant = v; return b }
func (b *TabsBuilder) ClassName(c string) *TabsBuilder        { b.Props.ClassName = c; return b }
func (b *TabsBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// layout.accordion
// =============================================================================

// AccordionItem describes one collapsible section. Children render in the
// slot whose name matches the Value.
type AccordionItem struct {
	Value       string `json:"value"`
	Title       string `json:"title"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// AccordionProps configures an accordion.
type AccordionProps struct {
	Type        string          `json:"type,omitempty"` // "single" | "multiple"
	Collapsible bool            `json:"collapsible,omitempty"`
	Items       []AccordionItem `json:"items"`
	ClassName   string          `json:"className,omitempty"`
}

type AccordionBuilder struct {
	*Common[AccordionBuilder]
	Props AccordionProps
}

func Accordion() *AccordionBuilder {
	b := &AccordionBuilder{}
	b.Common = newCommon[AccordionBuilder]("layout.accordion", b)
	return b
}

func (b *AccordionBuilder) Item(value, title string) *AccordionBuilder {
	b.Props.Items = append(b.Props.Items, AccordionItem{Value: value, Title: title})
	return b
}

func (b *AccordionBuilder) AddItem(item AccordionItem) *AccordionBuilder {
	b.Props.Items = append(b.Props.Items, item)
	return b
}

func (b *AccordionBuilder) Panel(value string, nodes ...contract.GraphNode) *AccordionBuilder {
	return b.Slot(value, nodes...)
}

func (b *AccordionBuilder) Single() *AccordionBuilder   { b.Props.Type = "single"; return b }
func (b *AccordionBuilder) Multiple() *AccordionBuilder { b.Props.Type = "multiple"; return b }
func (b *AccordionBuilder) Collapsible(c bool) *AccordionBuilder {
	b.Props.Collapsible = c
	return b
}
func (b *AccordionBuilder) ClassName(c string) *AccordionBuilder { b.Props.ClassName = c; return b }
func (b *AccordionBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// layout.split
// =============================================================================

// SplitProps configures a resizable two-pane layout. Children go into the
// "start" and "end" slots.
type SplitProps struct {
	Direction   Direction `json:"direction,omitempty"`
	DefaultSize int       `json:"defaultSize,omitempty"` // percent for the start pane
	MinSize     int       `json:"minSize,omitempty"`
	MaxSize     int       `json:"maxSize,omitempty"`
	Collapsible bool      `json:"collapsible,omitempty"`
	ClassName   string    `json:"className,omitempty"`
}

type SplitBuilder struct {
	*Common[SplitBuilder]
	Props SplitProps
}

func Split() *SplitBuilder {
	b := &SplitBuilder{}
	b.Common = newCommon[SplitBuilder]("layout.split", b)
	return b
}

func (b *SplitBuilder) Direction(d Direction) *SplitBuilder { b.Props.Direction = d; return b }
func (b *SplitBuilder) DefaultSize(n int) *SplitBuilder     { b.Props.DefaultSize = n; return b }
func (b *SplitBuilder) MinSize(n int) *SplitBuilder         { b.Props.MinSize = n; return b }
func (b *SplitBuilder) MaxSize(n int) *SplitBuilder         { b.Props.MaxSize = n; return b }
func (b *SplitBuilder) Collapsible(c bool) *SplitBuilder    { b.Props.Collapsible = c; return b }
func (b *SplitBuilder) ClassName(c string) *SplitBuilder    { b.Props.ClassName = c; return b }

// Start attaches the leading pane content.
func (b *SplitBuilder) Start(nodes ...contract.GraphNode) *SplitBuilder {
	return b.Slot("start", nodes...)
}

// End attaches the trailing pane content.
func (b *SplitBuilder) End(nodes ...contract.GraphNode) *SplitBuilder {
	return b.Slot("end", nodes...)
}

func (b *SplitBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// layout.divider
// =============================================================================

// DividerProps configures a separator line.
type DividerProps struct {
	Orientation Orientation `json:"orientation,omitempty"`
	Decorative  bool        `json:"decorative,omitempty"`
	Label       string      `json:"label,omitempty"` // optional centered label
	ClassName   string      `json:"className,omitempty"`
}

type DividerBuilder struct {
	*Common[DividerBuilder]
	Props DividerProps
}

func Divider() *DividerBuilder {
	b := &DividerBuilder{}
	b.Common = newCommon[DividerBuilder]("layout.divider", b)
	return b
}

func (b *DividerBuilder) Orientation(o Orientation) *DividerBuilder {
	b.Props.Orientation = o
	return b
}
func (b *DividerBuilder) Decorative(d bool) *DividerBuilder  { b.Props.Decorative = d; return b }
func (b *DividerBuilder) Label(l string) *DividerBuilder     { b.Props.Label = l; return b }
func (b *DividerBuilder) ClassName(c string) *DividerBuilder { b.Props.ClassName = c; return b }
func (b *DividerBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }

// =============================================================================
// layout.spacer
// =============================================================================

// SpacerProps configures a pure-spacing node.
type SpacerProps struct {
	Size      Spacing `json:"size,omitempty"`
	Axis      string  `json:"axis,omitempty"` // "horizontal" | "vertical" | "both"
	ClassName string  `json:"className,omitempty"`
}

type SpacerBuilder struct {
	*Common[SpacerBuilder]
	Props SpacerProps
}

func Spacer() *SpacerBuilder {
	b := &SpacerBuilder{}
	b.Common = newCommon[SpacerBuilder]("layout.spacer", b)
	return b
}

func (b *SpacerBuilder) Size(s Spacing) *SpacerBuilder     { b.Props.Size = s; return b }
func (b *SpacerBuilder) Axis(a string) *SpacerBuilder      { b.Props.Axis = a; return b }
func (b *SpacerBuilder) ClassName(c string) *SpacerBuilder { b.Props.ClassName = c; return b }
func (b *SpacerBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// layout.page
// =============================================================================

// PageCrumb is a single breadcrumb entry.
type PageCrumb struct {
	Label string `json:"label"`
	Href  string `json:"href,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// PageProps configures a top-level page shell with a header (title +
// description + breadcrumbs + actions) and a content area. The visible
// title lives on node.title (set via the title argument or Title()).
type PageProps struct {
	Description    string        `json:"description,omitempty"`
	Breadcrumbs    []PageCrumb   `json:"breadcrumbs,omitempty"`
	ContainerWidth ContainerSize `json:"containerWidth,omitempty"`
	NoChrome       bool          `json:"noChrome,omitempty"`
	ClassName      string        `json:"className,omitempty"`
}

type PageBuilder struct {
	*Common[PageBuilder]
	Props PageProps
}

// Page constructs a layout.page builder. The title argument fills
// node.title — Page("Members") is equivalent to Page("").Title("Members").
func Page(title string) *PageBuilder {
	b := &PageBuilder{}
	b.Common = newCommon[PageBuilder]("layout.page", b)
	b.setChildrenKey("content")
	b.title = title
	return b
}

func (b *PageBuilder) Description(d string) *PageBuilder { b.Props.Description = d; return b }

// Crumb appends a breadcrumb entry.
func (b *PageBuilder) Crumb(label, href string) *PageBuilder {
	b.Props.Breadcrumbs = append(b.Props.Breadcrumbs, PageCrumb{Label: label, Href: href})
	return b
}

// Breadcrumbs replaces the breadcrumb list wholesale.
func (b *PageBuilder) Breadcrumbs(crumbs ...PageCrumb) *PageBuilder {
	b.Props.Breadcrumbs = crumbs
	return b
}

func (b *PageBuilder) ContainerWidth(s ContainerSize) *PageBuilder {
	b.Props.ContainerWidth = s
	return b
}
func (b *PageBuilder) NoChrome(n bool) *PageBuilder    { b.Props.NoChrome = n; return b }
func (b *PageBuilder) ClassName(c string) *PageBuilder { b.Props.ClassName = c; return b }

// Actions attaches CTAs to the page header's actions slot.
func (b *PageBuilder) Actions(nodes ...contract.GraphNode) *PageBuilder {
	return b.Slot("actions", nodes...)
}

func (b *PageBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }
