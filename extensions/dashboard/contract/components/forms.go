package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// organism.dynamic-form
// =============================================================================

// FieldType identifies the renderer used for a DynamicForm field.
type FieldType string

const (
	FieldText        FieldType = "text"
	FieldEmail       FieldType = "email"
	FieldPassword    FieldType = "password"
	FieldNumber      FieldType = "number"
	FieldTextarea    FieldType = "textarea"
	FieldSelect      FieldType = "select"
	FieldMultiSelect FieldType = "multi-select"
	FieldCheckbox    FieldType = "checkbox"
	FieldSwitch      FieldType = "switch"
	FieldRadio       FieldType = "radio"
	FieldDate        FieldType = "date"
	FieldDateRange   FieldType = "date-range"
	FieldFile        FieldType = "file"
	FieldCombobox    FieldType = "combobox"
	FieldTagInput    FieldType = "tag-input"
	FieldSlider      FieldType = "slider"
	FieldColor       FieldType = "color"
	FieldHidden      FieldType = "hidden"
	FieldCustom      FieldType = "custom"
)

// FieldRuleKind names a validation rule's check type.
type FieldRuleKind string

const (
	RuleRequired  FieldRuleKind = "required"
	RuleMin       FieldRuleKind = "min"       // numeric min
	RuleMax       FieldRuleKind = "max"       // numeric max
	RuleMinLength FieldRuleKind = "minLength" // string min length
	RuleMaxLength FieldRuleKind = "maxLength" // string max length
	RuleRegex     FieldRuleKind = "regex"     // pattern match
	RuleEmail     FieldRuleKind = "email"
	RuleURL       FieldRuleKind = "url"
	RuleUUID      FieldRuleKind = "uuid"
	RuleIntent    FieldRuleKind = "intent" // async check via contract command
)

// FieldRule is a single validation entry. The shell composes a zod schema
// from a FieldSpec.Validate slice at render time.
type FieldRule struct {
	Kind    FieldRuleKind `json:"kind"`
	Value   any           `json:"value,omitempty"`
	Message string        `json:"message,omitempty"`
	Intent  string        `json:"intent,omitempty"` // for kind=intent
}

// FieldLayout controls how a field places itself in the parent form grid.
type FieldLayout struct {
	Span      int  `json:"span,omitempty"`      // 1..12 grid columns
	FullWidth bool `json:"fullWidth,omitempty"` // span entire row
	Indent    int  `json:"indent,omitempty"`    // left padding in grid columns
}

// OptionMapping describes how to extract Option fields from a generic
// record returned by a DataBinding (used by FieldType=select|combobox|radio).
type OptionMapping struct {
	LabelKey       string `json:"labelKey,omitempty"` // default: "label" then "name"
	ValueKey       string `json:"valueKey,omitempty"` // default: "value" then "id"
	DescriptionKey string `json:"descriptionKey,omitempty"`
	IconKey        string `json:"iconKey,omitempty"`
	DisabledKey    string `json:"disabledKey,omitempty"`
}

// OptionSource declares where a field's choices come from — either static
// list, dynamic query, or both (static rendered first).
type OptionSource struct {
	Static []Option              `json:"static,omitempty"`
	Data   *contract.DataBinding `json:"data,omitempty"`
	Map    *OptionMapping        `json:"map,omitempty"`
}

// FieldSpec is the wire shape for one dynamic-form field.
type FieldSpec struct {
	Name         string              `json:"name"`
	Type         FieldType           `json:"type"`
	Label        string              `json:"label,omitempty"`
	Description  string              `json:"description,omitempty"`
	Placeholder  string              `json:"placeholder,omitempty"`
	Default      any                 `json:"default,omitempty"`
	Required     bool                `json:"required,omitempty"`
	Disabled     *contract.Predicate `json:"disabled,omitempty"`
	Visible      *contract.Predicate `json:"visible,omitempty"`
	Validate     []FieldRule         `json:"validate,omitempty"`
	Options      *OptionSource       `json:"options,omitempty"`
	Dependencies []string            `json:"dependencies,omitempty"`
	Layout       *FieldLayout        `json:"layout,omitempty"`
	HelpText     string              `json:"helpText,omitempty"`
	Mask         string              `json:"mask,omitempty"`
	Format       string              `json:"format,omitempty"`
	Custom       string              `json:"custom,omitempty"` // intent name for FieldType=custom
	Min          *float64            `json:"min,omitempty"`
	Max          *float64            `json:"max,omitempty"`
	Step         *float64            `json:"step,omitempty"`
	MinLength    int                 `json:"minLength,omitempty"`
	MaxLength    int                 `json:"maxLength,omitempty"`
	Rows         int                 `json:"rows,omitempty"`
	Accept       string              `json:"accept,omitempty"` // for FieldFile
	Multiple     bool                `json:"multiple,omitempty"`
}

// FormSection groups fields under a heading for visual structure.
type FormSection struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Fields      []string `json:"fields"` // names referencing FieldSpec.Name
	Collapsible bool     `json:"collapsible,omitempty"`
	DefaultOpen bool     `json:"defaultOpen,omitempty"`
}

// FormLayout names the field arrangement strategy.
type FormLayout string

const (
	FormLayoutSingle    FormLayout = "single"
	FormLayoutTwoColumn FormLayout = "two-column"
	FormLayoutGrid      FormLayout = "grid"
)

// FormValidationMode names when validation runs.
type FormValidationMode string

const (
	ValidateOnSubmit FormValidationMode = "onSubmit"
	ValidateOnChange FormValidationMode = "onChange"
	ValidateOnBlur   FormValidationMode = "onBlur"
)

// SubmitConfig configures the submit button.
type SubmitConfig struct {
	Label     string       `json:"label,omitempty"`
	Icon      string       `json:"icon,omitempty"`
	Variant   ColorVariant `json:"variant,omitempty"`
	FullWidth bool         `json:"fullWidth,omitempty"`
}

// CancelConfig configures the cancel button.
type CancelConfig struct {
	Label   string       `json:"label,omitempty"`
	Action  *Action      `json:"action,omitempty"` // default: history back
	Variant ColorVariant `json:"variant,omitempty"`
}

// ResetConfig configures the reset button.
type ResetConfig struct {
	Label   string       `json:"label,omitempty"`
	Variant ColorVariant `json:"variant,omitempty"`
}

// DynamicFormProps configures organism.dynamic-form.
type DynamicFormProps struct {
	Fields        []FieldSpec           `json:"fields"`
	Sections      []FormSection         `json:"sections,omitempty"`
	Layout        FormLayout            `json:"layout,omitempty"`
	GridCols      int                   `json:"gridCols,omitempty"` // for FormLayoutGrid
	DefaultValues map[string]any        `json:"defaultValues,omitempty"`
	InitialData   *contract.DataBinding `json:"initialData,omitempty"`
	Validation    FormValidationMode    `json:"validation,omitempty"`
	Submit        SubmitConfig          `json:"submit"`
	Cancel        *CancelConfig         `json:"cancel,omitempty"`
	Reset         *ResetConfig          `json:"reset,omitempty"`
	OnSuccess     *Action               `json:"onSuccess,omitempty"`
	OnError       *Action               `json:"onError,omitempty"`
	ClassName     string                `json:"className,omitempty"`
}

type DynamicFormBuilder struct {
	*Common[DynamicFormBuilder]
	Props DynamicFormProps
}

// DynamicForm constructs an organism.dynamic-form builder. The Op is the
// command intent invoked on submit — set it via .Op("users.create").
func DynamicForm() *DynamicFormBuilder {
	b := &DynamicFormBuilder{}
	b.Common = newCommon[DynamicFormBuilder]("organism.dynamic-form", b)
	b.Props.Submit = SubmitConfig{Label: "Save"}
	b.Props.Layout = FormLayoutSingle
	b.Props.Validation = ValidateOnSubmit
	return b
}

func (b *DynamicFormBuilder) Field(spec FieldSpec) *DynamicFormBuilder {
	b.Props.Fields = append(b.Props.Fields, spec)
	return b
}

func (b *DynamicFormBuilder) Fields(specs ...FieldSpec) *DynamicFormBuilder {
	b.Props.Fields = append(b.Props.Fields, specs...)
	return b
}

func (b *DynamicFormBuilder) Section(s FormSection) *DynamicFormBuilder {
	b.Props.Sections = append(b.Props.Sections, s)
	return b
}

func (b *DynamicFormBuilder) Layout(l FormLayout) *DynamicFormBuilder {
	b.Props.Layout = l
	return b
}

func (b *DynamicFormBuilder) GridCols(n int) *DynamicFormBuilder {
	b.Props.GridCols = n
	return b
}

func (b *DynamicFormBuilder) DefaultValues(v map[string]any) *DynamicFormBuilder {
	b.Props.DefaultValues = v
	return b
}

func (b *DynamicFormBuilder) InitialData(d *contract.DataBinding) *DynamicFormBuilder {
	b.Props.InitialData = d
	return b
}

func (b *DynamicFormBuilder) Validation(m FormValidationMode) *DynamicFormBuilder {
	b.Props.Validation = m
	return b
}

func (b *DynamicFormBuilder) Submit(c SubmitConfig) *DynamicFormBuilder {
	b.Props.Submit = c
	return b
}

func (b *DynamicFormBuilder) SubmitLabel(l string) *DynamicFormBuilder {
	b.Props.Submit.Label = l
	return b
}

func (b *DynamicFormBuilder) Cancel(c *CancelConfig) *DynamicFormBuilder {
	b.Props.Cancel = c
	return b
}

func (b *DynamicFormBuilder) Reset(c *ResetConfig) *DynamicFormBuilder {
	b.Props.Reset = c
	return b
}

func (b *DynamicFormBuilder) OnSuccess(a *Action) *DynamicFormBuilder {
	b.Props.OnSuccess = a
	return b
}

func (b *DynamicFormBuilder) OnError(a *Action) *DynamicFormBuilder {
	b.Props.OnError = a
	return b
}

func (b *DynamicFormBuilder) ClassName(c string) *DynamicFormBuilder {
	b.Props.ClassName = c
	return b
}

func (b *DynamicFormBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// FieldSpec helpers — common shorthand constructors so callers can stay
// concise:
//
//   DynamicForm().Op("users.create").Fields(
//       TextField("email").Label("Email").Required(true).Email().Build(),
//       SelectField("role").Label("Role").Options(...).Build(),
//   )
// =============================================================================

// FieldSpecBuilder lets callers compose a FieldSpec fluently.
type FieldSpecBuilder struct {
	spec FieldSpec
}

func TextField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldText}}
}

func EmailField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldEmail}}
}

func PasswordField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldPassword}}
}

func NumberField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldNumber}}
}

func TextareaField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldTextarea}}
}

func SelectField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldSelect}}
}

func MultiSelectField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldMultiSelect}}
}

func CheckboxField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldCheckbox}}
}

func SwitchField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldSwitch}}
}

func RadioField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldRadio}}
}

func DateField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldDate}}
}

func DateRangeField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldDateRange}}
}

func FileField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldFile}}
}

func ComboboxField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldCombobox}}
}

func TagInputField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldTagInput}}
}

func SliderField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldSlider}}
}

func HiddenField(name string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldHidden}}
}

func CustomField(name, intent string) *FieldSpecBuilder {
	return &FieldSpecBuilder{spec: FieldSpec{Name: name, Type: FieldCustom, Custom: intent}}
}

func (b *FieldSpecBuilder) Label(l string) *FieldSpecBuilder       { b.spec.Label = l; return b }
func (b *FieldSpecBuilder) Description(d string) *FieldSpecBuilder { b.spec.Description = d; return b }
func (b *FieldSpecBuilder) Placeholder(p string) *FieldSpecBuilder { b.spec.Placeholder = p; return b }
func (b *FieldSpecBuilder) Default(v any) *FieldSpecBuilder        { b.spec.Default = v; return b }
func (b *FieldSpecBuilder) Required(r bool) *FieldSpecBuilder {
	b.spec.Required = r
	if r {
		b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleRequired})
	}
	return b
}
func (b *FieldSpecBuilder) Disabled(p *contract.Predicate) *FieldSpecBuilder {
	b.spec.Disabled = p
	return b
}
func (b *FieldSpecBuilder) Visible(p *contract.Predicate) *FieldSpecBuilder {
	b.spec.Visible = p
	return b
}
func (b *FieldSpecBuilder) Validate(rules ...FieldRule) *FieldSpecBuilder {
	b.spec.Validate = append(b.spec.Validate, rules...)
	return b
}
func (b *FieldSpecBuilder) Email() *FieldSpecBuilder {
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleEmail})
	return b
}
func (b *FieldSpecBuilder) URL() *FieldSpecBuilder {
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleURL})
	return b
}
func (b *FieldSpecBuilder) MinLength(n int) *FieldSpecBuilder {
	b.spec.MinLength = n
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleMinLength, Value: n})
	return b
}
func (b *FieldSpecBuilder) MaxLength(n int) *FieldSpecBuilder {
	b.spec.MaxLength = n
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleMaxLength, Value: n})
	return b
}
func (b *FieldSpecBuilder) Regex(pattern, message string) *FieldSpecBuilder {
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleRegex, Value: pattern, Message: message})
	return b
}
func (b *FieldSpecBuilder) Min(v float64) *FieldSpecBuilder {
	b.spec.Min = &v
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleMin, Value: v})
	return b
}
func (b *FieldSpecBuilder) Max(v float64) *FieldSpecBuilder {
	b.spec.Max = &v
	b.spec.Validate = append(b.spec.Validate, FieldRule{Kind: RuleMax, Value: v})
	return b
}
func (b *FieldSpecBuilder) Step(v float64) *FieldSpecBuilder { b.spec.Step = &v; return b }
func (b *FieldSpecBuilder) Options(opts ...Option) *FieldSpecBuilder {
	if b.spec.Options == nil {
		b.spec.Options = &OptionSource{}
	}
	b.spec.Options.Static = append(b.spec.Options.Static, opts...)
	return b
}
func (b *FieldSpecBuilder) OptionsFromQuery(d *contract.DataBinding, m *OptionMapping) *FieldSpecBuilder {
	if b.spec.Options == nil {
		b.spec.Options = &OptionSource{}
	}
	b.spec.Options.Data = d
	b.spec.Options.Map = m
	return b
}
func (b *FieldSpecBuilder) Dependencies(deps ...string) *FieldSpecBuilder {
	b.spec.Dependencies = deps
	return b
}
func (b *FieldSpecBuilder) Layout(l *FieldLayout) *FieldSpecBuilder { b.spec.Layout = l; return b }
func (b *FieldSpecBuilder) Span(n int) *FieldSpecBuilder {
	if b.spec.Layout == nil {
		b.spec.Layout = &FieldLayout{}
	}
	b.spec.Layout.Span = n
	return b
}
func (b *FieldSpecBuilder) FullWidth() *FieldSpecBuilder {
	if b.spec.Layout == nil {
		b.spec.Layout = &FieldLayout{}
	}
	b.spec.Layout.FullWidth = true
	return b
}
func (b *FieldSpecBuilder) HelpText(h string) *FieldSpecBuilder { b.spec.HelpText = h; return b }
func (b *FieldSpecBuilder) Mask(m string) *FieldSpecBuilder     { b.spec.Mask = m; return b }
func (b *FieldSpecBuilder) Format(f string) *FieldSpecBuilder   { b.spec.Format = f; return b }
func (b *FieldSpecBuilder) Rows(n int) *FieldSpecBuilder        { b.spec.Rows = n; return b }
func (b *FieldSpecBuilder) Accept(a string) *FieldSpecBuilder   { b.spec.Accept = a; return b }
func (b *FieldSpecBuilder) Multiple(m bool) *FieldSpecBuilder   { b.spec.Multiple = m; return b }

// Build returns the configured FieldSpec.
func (b *FieldSpecBuilder) Build() FieldSpec { return b.spec }
