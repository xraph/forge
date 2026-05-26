package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// atom.button
// =============================================================================

// ButtonProps configures a clickable button. OnClick fires an Action; for
// simple command dispatch, set Op on the builder via the inherited
// Op() setter instead.
type ButtonProps struct {
	Label        string       `json:"label,omitempty"`
	Variant      ColorVariant `json:"variant,omitempty"`
	Size         Size         `json:"size,omitempty"`
	LeadingIcon  string       `json:"leadingIcon,omitempty"`
	TrailingIcon string       `json:"trailingIcon,omitempty"`
	Loading      bool         `json:"loading,omitempty"`
	Disabled     bool         `json:"disabled,omitempty"`
	FullWidth    bool         `json:"fullWidth,omitempty"`
	OnClick      *Action      `json:"onClick,omitempty"`
	Confirm      string       `json:"confirm,omitempty"`
	ClassName    string       `json:"className,omitempty"`
}

type ButtonBuilder struct {
	*Common[ButtonBuilder]
	Props ButtonProps
}

// Button constructs an atom.button builder with the given label.
func Button(label string) *ButtonBuilder {
	b := &ButtonBuilder{}
	b.Common = newCommon[ButtonBuilder]("atom.button", b)
	b.Props.Label = label
	return b
}

func (b *ButtonBuilder) Label(l string) *ButtonBuilder         { b.Props.Label = l; return b }
func (b *ButtonBuilder) Variant(v ColorVariant) *ButtonBuilder { b.Props.Variant = v; return b }
func (b *ButtonBuilder) Size(s Size) *ButtonBuilder            { b.Props.Size = s; return b }
func (b *ButtonBuilder) LeadingIcon(name string) *ButtonBuilder {
	b.Props.LeadingIcon = name
	return b
}
func (b *ButtonBuilder) TrailingIcon(name string) *ButtonBuilder {
	b.Props.TrailingIcon = name
	return b
}
func (b *ButtonBuilder) Loading(l bool) *ButtonBuilder     { b.Props.Loading = l; return b }
func (b *ButtonBuilder) Disabled(d bool) *ButtonBuilder    { b.Props.Disabled = d; return b }
func (b *ButtonBuilder) FullWidth(f bool) *ButtonBuilder   { b.Props.FullWidth = f; return b }
func (b *ButtonBuilder) OnClick(a *Action) *ButtonBuilder  { b.Props.OnClick = a; return b }
func (b *ButtonBuilder) Confirm(msg string) *ButtonBuilder { b.Props.Confirm = msg; return b }
func (b *ButtonBuilder) ClassName(c string) *ButtonBuilder { b.Props.ClassName = c; return b }
func (b *ButtonBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// atom.icon-button
// =============================================================================

// IconButtonProps is a square icon-only button.
type IconButtonProps struct {
	Icon      string       `json:"icon"`
	Variant   ColorVariant `json:"variant,omitempty"`
	Size      Size         `json:"size,omitempty"`
	Tooltip   string       `json:"tooltip,omitempty"`
	AriaLabel string       `json:"ariaLabel,omitempty"`
	Loading   bool         `json:"loading,omitempty"`
	Disabled  bool         `json:"disabled,omitempty"`
	OnClick   *Action      `json:"onClick,omitempty"`
	Confirm   string       `json:"confirm,omitempty"`
	ClassName string       `json:"className,omitempty"`
}

type IconButtonBuilder struct {
	*Common[IconButtonBuilder]
	Props IconButtonProps
}

func IconButton(icon string) *IconButtonBuilder {
	b := &IconButtonBuilder{}
	b.Common = newCommon[IconButtonBuilder]("atom.icon-button", b)
	b.Props.Icon = icon
	return b
}

func (b *IconButtonBuilder) Variant(v ColorVariant) *IconButtonBuilder {
	b.Props.Variant = v
	return b
}
func (b *IconButtonBuilder) Size(s Size) *IconButtonBuilder        { b.Props.Size = s; return b }
func (b *IconButtonBuilder) Tooltip(t string) *IconButtonBuilder   { b.Props.Tooltip = t; return b }
func (b *IconButtonBuilder) AriaLabel(l string) *IconButtonBuilder { b.Props.AriaLabel = l; return b }
func (b *IconButtonBuilder) Loading(l bool) *IconButtonBuilder     { b.Props.Loading = l; return b }
func (b *IconButtonBuilder) Disabled(d bool) *IconButtonBuilder    { b.Props.Disabled = d; return b }
func (b *IconButtonBuilder) OnClick(a *Action) *IconButtonBuilder  { b.Props.OnClick = a; return b }
func (b *IconButtonBuilder) Confirm(m string) *IconButtonBuilder   { b.Props.Confirm = m; return b }
func (b *IconButtonBuilder) ClassName(c string) *IconButtonBuilder { b.Props.ClassName = c; return b }
func (b *IconButtonBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// atom.badge
// =============================================================================

// BadgeProps configures a small status pill.
type BadgeProps struct {
	Label     string       `json:"label"`
	Variant   ColorVariant `json:"variant,omitempty"`
	Icon      string       `json:"icon,omitempty"`
	Dot       bool         `json:"dot,omitempty"`
	ClassName string       `json:"className,omitempty"`
}

type BadgeBuilder struct {
	*Common[BadgeBuilder]
	Props BadgeProps
}

func Badge(label string) *BadgeBuilder {
	b := &BadgeBuilder{}
	b.Common = newCommon[BadgeBuilder]("atom.badge", b)
	b.Props.Label = label
	return b
}

func (b *BadgeBuilder) Variant(v ColorVariant) *BadgeBuilder { b.Props.Variant = v; return b }
func (b *BadgeBuilder) Icon(i string) *BadgeBuilder          { b.Props.Icon = i; return b }
func (b *BadgeBuilder) Dot(d bool) *BadgeBuilder             { b.Props.Dot = d; return b }
func (b *BadgeBuilder) ClassName(c string) *BadgeBuilder     { b.Props.ClassName = c; return b }
func (b *BadgeBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// atom.label
// =============================================================================

// LabelProps configures a form-control label.
type LabelProps struct {
	Text      string `json:"text"`
	For       string `json:"for,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Optional  bool   `json:"optional,omitempty"` // render "(optional)" hint
	ClassName string `json:"className,omitempty"`
}

type LabelBuilder struct {
	*Common[LabelBuilder]
	Props LabelProps
}

func Label(text string) *LabelBuilder {
	b := &LabelBuilder{}
	b.Common = newCommon[LabelBuilder]("atom.label", b)
	b.Props.Text = text
	return b
}

func (b *LabelBuilder) For(id string) *LabelBuilder      { b.Props.For = id; return b }
func (b *LabelBuilder) Required(r bool) *LabelBuilder    { b.Props.Required = r; return b }
func (b *LabelBuilder) Optional(o bool) *LabelBuilder    { b.Props.Optional = o; return b }
func (b *LabelBuilder) ClassName(c string) *LabelBuilder { b.Props.ClassName = c; return b }
func (b *LabelBuilder) Build() contract.GraphNode        { return b.toNode(b.Props) }

// =============================================================================
// atom.text
// =============================================================================

// TextProps configures inline text.
type TextProps struct {
	Text      string      `json:"text"`
	Size      TextSize    `json:"size,omitempty"`
	Variant   TextVariant `json:"variant,omitempty"`
	Weight    string      `json:"weight,omitempty"` // "normal" | "medium" | "semibold" | "bold"
	Italic    bool        `json:"italic,omitempty"`
	Underline bool        `json:"underline,omitempty"`
	Truncate  bool        `json:"truncate,omitempty"`
	Mono      bool        `json:"mono,omitempty"`
	ClassName string      `json:"className,omitempty"`
}

type TextBuilder struct {
	*Common[TextBuilder]
	Props TextProps
}

func Text(text string) *TextBuilder {
	b := &TextBuilder{}
	b.Common = newCommon[TextBuilder]("atom.text", b)
	b.Props.Text = text
	return b
}

func (b *TextBuilder) Size(s TextSize) *TextBuilder       { b.Props.Size = s; return b }
func (b *TextBuilder) Variant(v TextVariant) *TextBuilder { b.Props.Variant = v; return b }
func (b *TextBuilder) Weight(w string) *TextBuilder       { b.Props.Weight = w; return b }
func (b *TextBuilder) Italic(i bool) *TextBuilder         { b.Props.Italic = i; return b }
func (b *TextBuilder) Underline(u bool) *TextBuilder      { b.Props.Underline = u; return b }
func (b *TextBuilder) Truncate(t bool) *TextBuilder       { b.Props.Truncate = t; return b }
func (b *TextBuilder) Mono(m bool) *TextBuilder           { b.Props.Mono = m; return b }
func (b *TextBuilder) ClassName(c string) *TextBuilder    { b.Props.ClassName = c; return b }
func (b *TextBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }

// =============================================================================
// atom.heading
// =============================================================================

// HeadingProps configures a semantic heading.
type HeadingProps struct {
	Text      string       `json:"text"`
	Level     HeadingLevel `json:"level,omitempty"`
	Variant   TextVariant  `json:"variant,omitempty"`
	ClassName string       `json:"className,omitempty"`
}

type HeadingBuilder struct {
	*Common[HeadingBuilder]
	Props HeadingProps
}

func Heading(text string) *HeadingBuilder {
	b := &HeadingBuilder{}
	b.Common = newCommon[HeadingBuilder]("atom.heading", b)
	b.Props.Text = text
	return b
}

func (b *HeadingBuilder) Level(l int) *HeadingBuilder {
	b.Props.Level = HeadingLevel(l)
	return b
}
func (b *HeadingBuilder) Variant(v TextVariant) *HeadingBuilder { b.Props.Variant = v; return b }
func (b *HeadingBuilder) ClassName(c string) *HeadingBuilder    { b.Props.ClassName = c; return b }
func (b *HeadingBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// atom.link
// =============================================================================

// LinkProps configures a hyperlink.
type LinkProps struct {
	Text      string      `json:"text"`
	Href      string      `json:"href"`
	External  bool        `json:"external,omitempty"`
	Variant   TextVariant `json:"variant,omitempty"`
	Icon      string      `json:"icon,omitempty"`
	ClassName string      `json:"className,omitempty"`
}

type LinkBuilder struct {
	*Common[LinkBuilder]
	Props LinkProps
}

func Link(text, href string) *LinkBuilder {
	b := &LinkBuilder{}
	b.Common = newCommon[LinkBuilder]("atom.link", b)
	b.Props.Text = text
	b.Props.Href = href
	return b
}

func (b *LinkBuilder) External(e bool) *LinkBuilder       { b.Props.External = e; return b }
func (b *LinkBuilder) Variant(v TextVariant) *LinkBuilder { b.Props.Variant = v; return b }
func (b *LinkBuilder) Icon(i string) *LinkBuilder         { b.Props.Icon = i; return b }
func (b *LinkBuilder) ClassName(c string) *LinkBuilder    { b.Props.ClassName = c; return b }
func (b *LinkBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }

// =============================================================================
// atom.icon
// =============================================================================

// IconProps configures a lucide icon.
type IconProps struct {
	Name        string `json:"name"`
	Size        int    `json:"size,omitempty"`
	StrokeWidth int    `json:"strokeWidth,omitempty"`
	ClassName   string `json:"className,omitempty"`
}

type IconBuilder struct {
	*Common[IconBuilder]
	Props IconProps
}

func Icon(name string) *IconBuilder {
	b := &IconBuilder{}
	b.Common = newCommon[IconBuilder]("atom.icon", b)
	b.Props.Name = name
	return b
}

func (b *IconBuilder) Size(s int) *IconBuilder         { b.Props.Size = s; return b }
func (b *IconBuilder) StrokeWidth(w int) *IconBuilder  { b.Props.StrokeWidth = w; return b }
func (b *IconBuilder) ClassName(c string) *IconBuilder { b.Props.ClassName = c; return b }
func (b *IconBuilder) Build() contract.GraphNode       { return b.toNode(b.Props) }

// =============================================================================
// atom.avatar
// =============================================================================

// AvatarProps configures an avatar with fallback initials.
type AvatarProps struct {
	Src       string `json:"src,omitempty"`
	Alt       string `json:"alt,omitempty"`
	Fallback  string `json:"fallback,omitempty"`
	Size      Size   `json:"size,omitempty"`
	Status    string `json:"status,omitempty"` // "online" | "offline" | "busy" | "away"
	ClassName string `json:"className,omitempty"`
}

type AvatarBuilder struct {
	*Common[AvatarBuilder]
	Props AvatarProps
}

func Avatar() *AvatarBuilder {
	b := &AvatarBuilder{}
	b.Common = newCommon[AvatarBuilder]("atom.avatar", b)
	return b
}

func (b *AvatarBuilder) Src(s string) *AvatarBuilder       { b.Props.Src = s; return b }
func (b *AvatarBuilder) Alt(a string) *AvatarBuilder       { b.Props.Alt = a; return b }
func (b *AvatarBuilder) Fallback(f string) *AvatarBuilder  { b.Props.Fallback = f; return b }
func (b *AvatarBuilder) Size(s Size) *AvatarBuilder        { b.Props.Size = s; return b }
func (b *AvatarBuilder) Status(s string) *AvatarBuilder    { b.Props.Status = s; return b }
func (b *AvatarBuilder) ClassName(c string) *AvatarBuilder { b.Props.ClassName = c; return b }
func (b *AvatarBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// atom.image
// =============================================================================

// ImageProps configures an image element.
type ImageProps struct {
	Src         string      `json:"src"`
	Alt         string      `json:"alt,omitempty"`
	AspectRatio AspectRatio `json:"aspectRatio,omitempty"`
	ObjectFit   ObjectFit   `json:"objectFit,omitempty"`
	Radius      Radius      `json:"radius,omitempty"`
	Width       string      `json:"width,omitempty"`
	Height      string      `json:"height,omitempty"`
	ClassName   string      `json:"className,omitempty"`
}

type ImageBuilder struct {
	*Common[ImageBuilder]
	Props ImageProps
}

func Image(src string) *ImageBuilder {
	b := &ImageBuilder{}
	b.Common = newCommon[ImageBuilder]("atom.image", b)
	b.Props.Src = src
	return b
}

func (b *ImageBuilder) Alt(a string) *ImageBuilder              { b.Props.Alt = a; return b }
func (b *ImageBuilder) AspectRatio(a AspectRatio) *ImageBuilder { b.Props.AspectRatio = a; return b }
func (b *ImageBuilder) ObjectFit(o ObjectFit) *ImageBuilder     { b.Props.ObjectFit = o; return b }
func (b *ImageBuilder) Radius(r Radius) *ImageBuilder           { b.Props.Radius = r; return b }
func (b *ImageBuilder) Width(w string) *ImageBuilder            { b.Props.Width = w; return b }
func (b *ImageBuilder) Height(h string) *ImageBuilder           { b.Props.Height = h; return b }
func (b *ImageBuilder) ClassName(c string) *ImageBuilder        { b.Props.ClassName = c; return b }
func (b *ImageBuilder) Build() contract.GraphNode               { return b.toNode(b.Props) }

// =============================================================================
// atom.separator
// =============================================================================

// SeparatorProps configures a separator rule.
type SeparatorProps struct {
	Orientation Orientation `json:"orientation,omitempty"`
	Decorative  bool        `json:"decorative,omitempty"`
	ClassName   string      `json:"className,omitempty"`
}

type SeparatorBuilder struct {
	*Common[SeparatorBuilder]
	Props SeparatorProps
}

func Separator() *SeparatorBuilder {
	b := &SeparatorBuilder{}
	b.Common = newCommon[SeparatorBuilder]("atom.separator", b)
	return b
}

func (b *SeparatorBuilder) Orientation(o Orientation) *SeparatorBuilder {
	b.Props.Orientation = o
	return b
}
func (b *SeparatorBuilder) Decorative(d bool) *SeparatorBuilder  { b.Props.Decorative = d; return b }
func (b *SeparatorBuilder) ClassName(c string) *SeparatorBuilder { b.Props.ClassName = c; return b }
func (b *SeparatorBuilder) Build() contract.GraphNode            { return b.toNode(b.Props) }

// =============================================================================
// atom.skeleton
// =============================================================================

// SkeletonProps configures a loading placeholder.
type SkeletonProps struct {
	Width     string `json:"width,omitempty"`
	Height    string `json:"height,omitempty"`
	Radius    Radius `json:"radius,omitempty"`
	Circle    bool   `json:"circle,omitempty"`
	ClassName string `json:"className,omitempty"`
}

type SkeletonBuilder struct {
	*Common[SkeletonBuilder]
	Props SkeletonProps
}

func Skeleton() *SkeletonBuilder {
	b := &SkeletonBuilder{}
	b.Common = newCommon[SkeletonBuilder]("atom.skeleton", b)
	return b
}

func (b *SkeletonBuilder) Width(w string) *SkeletonBuilder     { b.Props.Width = w; return b }
func (b *SkeletonBuilder) Height(h string) *SkeletonBuilder    { b.Props.Height = h; return b }
func (b *SkeletonBuilder) Radius(r Radius) *SkeletonBuilder    { b.Props.Radius = r; return b }
func (b *SkeletonBuilder) Circle(c bool) *SkeletonBuilder      { b.Props.Circle = c; return b }
func (b *SkeletonBuilder) ClassName(c string) *SkeletonBuilder { b.Props.ClassName = c; return b }
func (b *SkeletonBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// atom.spinner
// =============================================================================

// SpinnerProps configures a loading spinner.
type SpinnerProps struct {
	Size      Size        `json:"size,omitempty"`
	Label     string      `json:"label,omitempty"`
	Variant   TextVariant `json:"variant,omitempty"`
	ClassName string      `json:"className,omitempty"`
}

type SpinnerBuilder struct {
	*Common[SpinnerBuilder]
	Props SpinnerProps
}

func Spinner() *SpinnerBuilder {
	b := &SpinnerBuilder{}
	b.Common = newCommon[SpinnerBuilder]("atom.spinner", b)
	return b
}

func (b *SpinnerBuilder) Size(s Size) *SpinnerBuilder    { b.Props.Size = s; return b }
func (b *SpinnerBuilder) Label(l string) *SpinnerBuilder { b.Props.Label = l; return b }
func (b *SpinnerBuilder) Variant(v TextVariant) *SpinnerBuilder {
	b.Props.Variant = v
	return b
}
func (b *SpinnerBuilder) ClassName(c string) *SpinnerBuilder { b.Props.ClassName = c; return b }
func (b *SpinnerBuilder) Build() contract.GraphNode          { return b.toNode(b.Props) }

// =============================================================================
// atom.progress
// =============================================================================

// ProgressProps configures a determinate or indeterminate progress bar.
type ProgressProps struct {
	Value         *float64     `json:"value,omitempty"`
	Max           float64      `json:"max,omitempty"`
	Indeterminate bool         `json:"indeterminate,omitempty"`
	Label         string       `json:"label,omitempty"`
	ShowValue     bool         `json:"showValue,omitempty"`
	Variant       ColorVariant `json:"variant,omitempty"`
	ClassName     string       `json:"className,omitempty"`
}

type ProgressBuilder struct {
	*Common[ProgressBuilder]
	Props ProgressProps
}

func Progress() *ProgressBuilder {
	b := &ProgressBuilder{}
	b.Common = newCommon[ProgressBuilder]("atom.progress", b)
	return b
}

func (b *ProgressBuilder) Value(v float64) *ProgressBuilder {
	b.Props.Value = &v
	return b
}
func (b *ProgressBuilder) Max(m float64) *ProgressBuilder { b.Props.Max = m; return b }
func (b *ProgressBuilder) Indeterminate(i bool) *ProgressBuilder {
	b.Props.Indeterminate = i
	return b
}
func (b *ProgressBuilder) Label(l string) *ProgressBuilder         { b.Props.Label = l; return b }
func (b *ProgressBuilder) ShowValue(s bool) *ProgressBuilder       { b.Props.ShowValue = s; return b }
func (b *ProgressBuilder) Variant(v ColorVariant) *ProgressBuilder { b.Props.Variant = v; return b }
func (b *ProgressBuilder) ClassName(c string) *ProgressBuilder     { b.Props.ClassName = c; return b }
func (b *ProgressBuilder) Build() contract.GraphNode               { return b.toNode(b.Props) }

// =============================================================================
// atom.input
// =============================================================================

// InputType is the underlying HTML input type.
type InputType string

const (
	InputText     InputType = "text"
	InputEmail    InputType = "email"
	InputPassword InputType = "password"
	InputNumber   InputType = "number"
	InputSearch   InputType = "search"
	InputTel      InputType = "tel"
	InputURL      InputType = "url"
	InputDate     InputType = "date"
	InputTime     InputType = "time"
)

// InputProps configures a text input.
type InputProps struct {
	Name         string    `json:"name"`
	Type         InputType `json:"type,omitempty"`
	Placeholder  string    `json:"placeholder,omitempty"`
	Value        string    `json:"value,omitempty"`
	Default      string    `json:"default,omitempty"`
	Disabled     bool      `json:"disabled,omitempty"`
	ReadOnly     bool      `json:"readOnly,omitempty"`
	Required     bool      `json:"required,omitempty"`
	Autofocus    bool      `json:"autofocus,omitempty"`
	Autocomplete string    `json:"autocomplete,omitempty"`
	Prefix       string    `json:"prefix,omitempty"`
	Suffix       string    `json:"suffix,omitempty"`
	PrefixIcon   string    `json:"prefixIcon,omitempty"`
	SuffixIcon   string    `json:"suffixIcon,omitempty"`
	MinLength    int       `json:"minLength,omitempty"`
	MaxLength    int       `json:"maxLength,omitempty"`
	Pattern      string    `json:"pattern,omitempty"`
	Min          *float64  `json:"min,omitempty"`
	Max          *float64  `json:"max,omitempty"`
	Step         *float64  `json:"step,omitempty"`
	OnChange     *Action   `json:"onChange,omitempty"`
	OnSubmit     *Action   `json:"onSubmit,omitempty"`
	Size         Size      `json:"size,omitempty"`
	Error        string    `json:"error,omitempty"`
	ClassName    string    `json:"className,omitempty"`
}

type InputBuilder struct {
	*Common[InputBuilder]
	Props InputProps
}

func Input(name string) *InputBuilder {
	b := &InputBuilder{}
	b.Common = newCommon[InputBuilder]("atom.input", b)
	b.Props.Name = name
	b.Props.Type = InputText
	return b
}

func (b *InputBuilder) Type(t InputType) *InputBuilder      { b.Props.Type = t; return b }
func (b *InputBuilder) Placeholder(p string) *InputBuilder  { b.Props.Placeholder = p; return b }
func (b *InputBuilder) Value(v string) *InputBuilder        { b.Props.Value = v; return b }
func (b *InputBuilder) Default(v string) *InputBuilder      { b.Props.Default = v; return b }
func (b *InputBuilder) Disabled(d bool) *InputBuilder       { b.Props.Disabled = d; return b }
func (b *InputBuilder) ReadOnly(r bool) *InputBuilder       { b.Props.ReadOnly = r; return b }
func (b *InputBuilder) Required(r bool) *InputBuilder       { b.Props.Required = r; return b }
func (b *InputBuilder) Autofocus(a bool) *InputBuilder      { b.Props.Autofocus = a; return b }
func (b *InputBuilder) Autocomplete(a string) *InputBuilder { b.Props.Autocomplete = a; return b }
func (b *InputBuilder) Prefix(p string) *InputBuilder       { b.Props.Prefix = p; return b }
func (b *InputBuilder) Suffix(s string) *InputBuilder       { b.Props.Suffix = s; return b }
func (b *InputBuilder) PrefixIcon(i string) *InputBuilder   { b.Props.PrefixIcon = i; return b }
func (b *InputBuilder) SuffixIcon(i string) *InputBuilder   { b.Props.SuffixIcon = i; return b }
func (b *InputBuilder) MinLength(n int) *InputBuilder       { b.Props.MinLength = n; return b }
func (b *InputBuilder) MaxLength(n int) *InputBuilder       { b.Props.MaxLength = n; return b }
func (b *InputBuilder) Pattern(p string) *InputBuilder      { b.Props.Pattern = p; return b }
func (b *InputBuilder) Min(v float64) *InputBuilder         { b.Props.Min = &v; return b }
func (b *InputBuilder) Max(v float64) *InputBuilder         { b.Props.Max = &v; return b }
func (b *InputBuilder) Step(v float64) *InputBuilder        { b.Props.Step = &v; return b }
func (b *InputBuilder) OnChange(a *Action) *InputBuilder    { b.Props.OnChange = a; return b }
func (b *InputBuilder) OnSubmit(a *Action) *InputBuilder    { b.Props.OnSubmit = a; return b }
func (b *InputBuilder) Size(s Size) *InputBuilder           { b.Props.Size = s; return b }
func (b *InputBuilder) Error(e string) *InputBuilder        { b.Props.Error = e; return b }
func (b *InputBuilder) ClassName(c string) *InputBuilder    { b.Props.ClassName = c; return b }
func (b *InputBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// atom.textarea
// =============================================================================

// TextareaProps configures a multi-line input.
type TextareaProps struct {
	Name        string  `json:"name"`
	Placeholder string  `json:"placeholder,omitempty"`
	Value       string  `json:"value,omitempty"`
	Default     string  `json:"default,omitempty"`
	Rows        int     `json:"rows,omitempty"`
	MaxRows     int     `json:"maxRows,omitempty"`
	Autosize    bool    `json:"autosize,omitempty"`
	Disabled    bool    `json:"disabled,omitempty"`
	ReadOnly    bool    `json:"readOnly,omitempty"`
	Required    bool    `json:"required,omitempty"`
	MinLength   int     `json:"minLength,omitempty"`
	MaxLength   int     `json:"maxLength,omitempty"`
	OnChange    *Action `json:"onChange,omitempty"`
	Error       string  `json:"error,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type TextareaBuilder struct {
	*Common[TextareaBuilder]
	Props TextareaProps
}

func Textarea(name string) *TextareaBuilder {
	b := &TextareaBuilder{}
	b.Common = newCommon[TextareaBuilder]("atom.textarea", b)
	b.Props.Name = name
	return b
}

func (b *TextareaBuilder) Placeholder(p string) *TextareaBuilder { b.Props.Placeholder = p; return b }
func (b *TextareaBuilder) Value(v string) *TextareaBuilder       { b.Props.Value = v; return b }
func (b *TextareaBuilder) Default(v string) *TextareaBuilder     { b.Props.Default = v; return b }
func (b *TextareaBuilder) Rows(n int) *TextareaBuilder           { b.Props.Rows = n; return b }
func (b *TextareaBuilder) MaxRows(n int) *TextareaBuilder        { b.Props.MaxRows = n; return b }
func (b *TextareaBuilder) Autosize(a bool) *TextareaBuilder      { b.Props.Autosize = a; return b }
func (b *TextareaBuilder) Disabled(d bool) *TextareaBuilder      { b.Props.Disabled = d; return b }
func (b *TextareaBuilder) ReadOnly(r bool) *TextareaBuilder      { b.Props.ReadOnly = r; return b }
func (b *TextareaBuilder) Required(r bool) *TextareaBuilder      { b.Props.Required = r; return b }
func (b *TextareaBuilder) MinLength(n int) *TextareaBuilder      { b.Props.MinLength = n; return b }
func (b *TextareaBuilder) MaxLength(n int) *TextareaBuilder      { b.Props.MaxLength = n; return b }
func (b *TextareaBuilder) OnChange(a *Action) *TextareaBuilder   { b.Props.OnChange = a; return b }
func (b *TextareaBuilder) Error(e string) *TextareaBuilder       { b.Props.Error = e; return b }
func (b *TextareaBuilder) ClassName(c string) *TextareaBuilder   { b.Props.ClassName = c; return b }
func (b *TextareaBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// atom.checkbox
// =============================================================================

// CheckboxProps configures a single checkbox.
type CheckboxProps struct {
	Name          string  `json:"name"`
	Label         string  `json:"label,omitempty"`
	Description   string  `json:"description,omitempty"`
	Checked       *bool   `json:"checked,omitempty"`
	Default       *bool   `json:"default,omitempty"`
	Indeterminate bool    `json:"indeterminate,omitempty"`
	Disabled      bool    `json:"disabled,omitempty"`
	Required      bool    `json:"required,omitempty"`
	OnChange      *Action `json:"onChange,omitempty"`
	Error         string  `json:"error,omitempty"`
	ClassName     string  `json:"className,omitempty"`
}

type CheckboxBuilder struct {
	*Common[CheckboxBuilder]
	Props CheckboxProps
}

func Checkbox(name string) *CheckboxBuilder {
	b := &CheckboxBuilder{}
	b.Common = newCommon[CheckboxBuilder]("atom.checkbox", b)
	b.Props.Name = name
	return b
}

func (b *CheckboxBuilder) Label(l string) *CheckboxBuilder       { b.Props.Label = l; return b }
func (b *CheckboxBuilder) Description(d string) *CheckboxBuilder { b.Props.Description = d; return b }
func (b *CheckboxBuilder) Checked(c bool) *CheckboxBuilder       { b.Props.Checked = &c; return b }
func (b *CheckboxBuilder) Default(c bool) *CheckboxBuilder       { b.Props.Default = &c; return b }
func (b *CheckboxBuilder) Indeterminate(i bool) *CheckboxBuilder { b.Props.Indeterminate = i; return b }
func (b *CheckboxBuilder) Disabled(d bool) *CheckboxBuilder      { b.Props.Disabled = d; return b }
func (b *CheckboxBuilder) Required(r bool) *CheckboxBuilder      { b.Props.Required = r; return b }
func (b *CheckboxBuilder) OnChange(a *Action) *CheckboxBuilder   { b.Props.OnChange = a; return b }
func (b *CheckboxBuilder) Error(e string) *CheckboxBuilder       { b.Props.Error = e; return b }
func (b *CheckboxBuilder) ClassName(c string) *CheckboxBuilder   { b.Props.ClassName = c; return b }
func (b *CheckboxBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// atom.radio
// =============================================================================

// Option is a single choice in a radio/select/combobox group.
type Option struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// RadioProps configures a radio group.
type RadioProps struct {
	Name        string      `json:"name"`
	Options     []Option    `json:"options"`
	Value       string      `json:"value,omitempty"`
	Default     string      `json:"default,omitempty"`
	Orientation Orientation `json:"orientation,omitempty"`
	Disabled    bool        `json:"disabled,omitempty"`
	Required    bool        `json:"required,omitempty"`
	OnChange    *Action     `json:"onChange,omitempty"`
	Error       string      `json:"error,omitempty"`
	ClassName   string      `json:"className,omitempty"`
}

type RadioBuilder struct {
	*Common[RadioBuilder]
	Props RadioProps
}

func Radio(name string) *RadioBuilder {
	b := &RadioBuilder{}
	b.Common = newCommon[RadioBuilder]("atom.radio", b)
	b.Props.Name = name
	return b
}

func (b *RadioBuilder) Options(opts ...Option) *RadioBuilder {
	b.Props.Options = append(b.Props.Options, opts...)
	return b
}
func (b *RadioBuilder) Option(label, value string) *RadioBuilder {
	b.Props.Options = append(b.Props.Options, Option{Label: label, Value: value})
	return b
}
func (b *RadioBuilder) Value(v string) *RadioBuilder            { b.Props.Value = v; return b }
func (b *RadioBuilder) Default(v string) *RadioBuilder          { b.Props.Default = v; return b }
func (b *RadioBuilder) Orientation(o Orientation) *RadioBuilder { b.Props.Orientation = o; return b }
func (b *RadioBuilder) Disabled(d bool) *RadioBuilder           { b.Props.Disabled = d; return b }
func (b *RadioBuilder) Required(r bool) *RadioBuilder           { b.Props.Required = r; return b }
func (b *RadioBuilder) OnChange(a *Action) *RadioBuilder        { b.Props.OnChange = a; return b }
func (b *RadioBuilder) Error(e string) *RadioBuilder            { b.Props.Error = e; return b }
func (b *RadioBuilder) ClassName(c string) *RadioBuilder        { b.Props.ClassName = c; return b }
func (b *RadioBuilder) Build() contract.GraphNode               { return b.toNode(b.Props) }

// =============================================================================
// atom.switch
// =============================================================================

// SwitchProps configures a switch toggle.
type SwitchProps struct {
	Name        string  `json:"name"`
	Label       string  `json:"label,omitempty"`
	Description string  `json:"description,omitempty"`
	Checked     *bool   `json:"checked,omitempty"`
	Default     *bool   `json:"default,omitempty"`
	Disabled    bool    `json:"disabled,omitempty"`
	OnChange    *Action `json:"onChange,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type SwitchBuilder struct {
	*Common[SwitchBuilder]
	Props SwitchProps
}

func Switch(name string) *SwitchBuilder {
	b := &SwitchBuilder{}
	b.Common = newCommon[SwitchBuilder]("atom.switch", b)
	b.Props.Name = name
	return b
}

func (b *SwitchBuilder) Label(l string) *SwitchBuilder       { b.Props.Label = l; return b }
func (b *SwitchBuilder) Description(d string) *SwitchBuilder { b.Props.Description = d; return b }
func (b *SwitchBuilder) Checked(c bool) *SwitchBuilder       { b.Props.Checked = &c; return b }
func (b *SwitchBuilder) Default(c bool) *SwitchBuilder       { b.Props.Default = &c; return b }
func (b *SwitchBuilder) Disabled(d bool) *SwitchBuilder      { b.Props.Disabled = d; return b }
func (b *SwitchBuilder) OnChange(a *Action) *SwitchBuilder   { b.Props.OnChange = a; return b }
func (b *SwitchBuilder) ClassName(c string) *SwitchBuilder   { b.Props.ClassName = c; return b }
func (b *SwitchBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// atom.slider
// =============================================================================

// SliderMark is an optional tick mark on the slider track.
type SliderMark struct {
	Value float64 `json:"value"`
	Label string  `json:"label,omitempty"`
}

// SliderProps configures a numeric slider.
type SliderProps struct {
	Name       string       `json:"name"`
	Min        float64      `json:"min,omitempty"`
	Max        float64      `json:"max,omitempty"`
	Step       float64      `json:"step,omitempty"`
	Value      *float64     `json:"value,omitempty"`
	Default    *float64     `json:"default,omitempty"`
	Range      bool         `json:"range,omitempty"`
	RangeValue []float64    `json:"rangeValue,omitempty"`
	Marks      []SliderMark `json:"marks,omitempty"`
	ShowValue  bool         `json:"showValue,omitempty"`
	Disabled   bool         `json:"disabled,omitempty"`
	OnChange   *Action      `json:"onChange,omitempty"`
	ClassName  string       `json:"className,omitempty"`
}

type SliderBuilder struct {
	*Common[SliderBuilder]
	Props SliderProps
}

func Slider(name string) *SliderBuilder {
	b := &SliderBuilder{}
	b.Common = newCommon[SliderBuilder]("atom.slider", b)
	b.Props.Name = name
	return b
}

func (b *SliderBuilder) Min(v float64) *SliderBuilder   { b.Props.Min = v; return b }
func (b *SliderBuilder) Max(v float64) *SliderBuilder   { b.Props.Max = v; return b }
func (b *SliderBuilder) Step(v float64) *SliderBuilder  { b.Props.Step = v; return b }
func (b *SliderBuilder) Value(v float64) *SliderBuilder { b.Props.Value = &v; return b }
func (b *SliderBuilder) Default(v float64) *SliderBuilder {
	b.Props.Default = &v
	return b
}
func (b *SliderBuilder) Range(r bool) *SliderBuilder            { b.Props.Range = r; return b }
func (b *SliderBuilder) RangeValue(v ...float64) *SliderBuilder { b.Props.RangeValue = v; return b }
func (b *SliderBuilder) Marks(m ...SliderMark) *SliderBuilder   { b.Props.Marks = m; return b }
func (b *SliderBuilder) ShowValue(s bool) *SliderBuilder        { b.Props.ShowValue = s; return b }
func (b *SliderBuilder) Disabled(d bool) *SliderBuilder         { b.Props.Disabled = d; return b }
func (b *SliderBuilder) OnChange(a *Action) *SliderBuilder      { b.Props.OnChange = a; return b }
func (b *SliderBuilder) ClassName(c string) *SliderBuilder      { b.Props.ClassName = c; return b }
func (b *SliderBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// atom.select
// =============================================================================

// SelectProps configures a single-select dropdown.
type SelectProps struct {
	Name        string   `json:"name"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []Option `json:"options"`
	Value       string   `json:"value,omitempty"`
	Default     string   `json:"default,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Size        Size     `json:"size,omitempty"`
	Clearable   bool     `json:"clearable,omitempty"`
	OnChange    *Action  `json:"onChange,omitempty"`
	Error       string   `json:"error,omitempty"`
	ClassName   string   `json:"className,omitempty"`
}

type SelectBuilder struct {
	*Common[SelectBuilder]
	Props SelectProps
}

func Select(name string) *SelectBuilder {
	b := &SelectBuilder{}
	b.Common = newCommon[SelectBuilder]("atom.select", b)
	b.Props.Name = name
	return b
}

func (b *SelectBuilder) Placeholder(p string) *SelectBuilder { b.Props.Placeholder = p; return b }
func (b *SelectBuilder) Options(opts ...Option) *SelectBuilder {
	b.Props.Options = append(b.Props.Options, opts...)
	return b
}
func (b *SelectBuilder) Option(label, value string) *SelectBuilder {
	b.Props.Options = append(b.Props.Options, Option{Label: label, Value: value})
	return b
}
func (b *SelectBuilder) Value(v string) *SelectBuilder     { b.Props.Value = v; return b }
func (b *SelectBuilder) Default(v string) *SelectBuilder   { b.Props.Default = v; return b }
func (b *SelectBuilder) Disabled(d bool) *SelectBuilder    { b.Props.Disabled = d; return b }
func (b *SelectBuilder) Required(r bool) *SelectBuilder    { b.Props.Required = r; return b }
func (b *SelectBuilder) Size(s Size) *SelectBuilder        { b.Props.Size = s; return b }
func (b *SelectBuilder) Clearable(c bool) *SelectBuilder   { b.Props.Clearable = c; return b }
func (b *SelectBuilder) OnChange(a *Action) *SelectBuilder { b.Props.OnChange = a; return b }
func (b *SelectBuilder) Error(e string) *SelectBuilder     { b.Props.Error = e; return b }
func (b *SelectBuilder) ClassName(c string) *SelectBuilder { b.Props.ClassName = c; return b }
func (b *SelectBuilder) Build() contract.GraphNode         { return b.toNode(b.Props) }

// =============================================================================
// atom.kbd
// =============================================================================

// KbdProps configures a keyboard-shortcut hint.
type KbdProps struct {
	Keys      []string `json:"keys"`
	Size      Size     `json:"size,omitempty"`
	ClassName string   `json:"className,omitempty"`
}

type KbdBuilder struct {
	*Common[KbdBuilder]
	Props KbdProps
}

func Kbd(keys ...string) *KbdBuilder {
	b := &KbdBuilder{}
	b.Common = newCommon[KbdBuilder]("atom.kbd", b)
	b.Props.Keys = keys
	return b
}

func (b *KbdBuilder) Size(s Size) *KbdBuilder        { b.Props.Size = s; return b }
func (b *KbdBuilder) ClassName(c string) *KbdBuilder { b.Props.ClassName = c; return b }
func (b *KbdBuilder) Build() contract.GraphNode      { return b.toNode(b.Props) }

// =============================================================================
// atom.code
// =============================================================================

// CodeProps configures inline or block code rendering.
type CodeProps struct {
	Text      string `json:"text"`
	Language  string `json:"language,omitempty"`
	Inline    bool   `json:"inline,omitempty"`
	Copyable  bool   `json:"copyable,omitempty"`
	ClassName string `json:"className,omitempty"`
}

type CodeBuilder struct {
	*Common[CodeBuilder]
	Props CodeProps
}

func Code(text string) *CodeBuilder {
	b := &CodeBuilder{}
	b.Common = newCommon[CodeBuilder]("atom.code", b)
	b.Props.Text = text
	return b
}

func (b *CodeBuilder) Language(l string) *CodeBuilder  { b.Props.Language = l; return b }
func (b *CodeBuilder) Inline(i bool) *CodeBuilder      { b.Props.Inline = i; return b }
func (b *CodeBuilder) Copyable(c bool) *CodeBuilder    { b.Props.Copyable = c; return b }
func (b *CodeBuilder) ClassName(c string) *CodeBuilder { b.Props.ClassName = c; return b }
func (b *CodeBuilder) Build() contract.GraphNode       { return b.toNode(b.Props) }

// =============================================================================
// atom.tooltip
// =============================================================================

// TooltipSide names the popover side.
type TooltipSide string

const (
	TooltipTop    TooltipSide = "top"
	TooltipBottom TooltipSide = "bottom"
	TooltipLeft   TooltipSide = "left"
	TooltipRight  TooltipSide = "right"
)

// TooltipProps wraps a trigger node with a hover tooltip. The trigger
// element lives in the "trigger" slot; pass it via Trigger(node).
type TooltipProps struct {
	Content   string      `json:"content"`
	Side      TooltipSide `json:"side,omitempty"`
	DelayMs   int         `json:"delayMs,omitempty"`
	ClassName string      `json:"className,omitempty"`
}

type TooltipBuilder struct {
	*Common[TooltipBuilder]
	Props TooltipProps
}

func Tooltip(content string) *TooltipBuilder {
	b := &TooltipBuilder{}
	b.Common = newCommon[TooltipBuilder]("atom.tooltip", b)
	b.Props.Content = content
	return b
}

func (b *TooltipBuilder) Side(s TooltipSide) *TooltipBuilder { b.Props.Side = s; return b }
func (b *TooltipBuilder) Delay(ms int) *TooltipBuilder       { b.Props.DelayMs = ms; return b }
func (b *TooltipBuilder) ClassName(c string) *TooltipBuilder { b.Props.ClassName = c; return b }

// Trigger sets the node that triggers the tooltip on hover/focus.
func (b *TooltipBuilder) Trigger(node contract.GraphNode) *TooltipBuilder {
	return b.Slot("trigger", node)
}

func (b *TooltipBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }
