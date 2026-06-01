package components

// ColorVariant names the shadcn semantic palette. Only canonical tokens
// are exposed — there is no Success, Warning, or Info to keep the contract
// strictly shadcn-aligned. Map non-shadcn semantics to the closest token:
// errors → Destructive, warnings → Muted, info/notices → Accent.
type ColorVariant string

const (
	VariantDefault     ColorVariant = "default"
	VariantSecondary   ColorVariant = "secondary"
	VariantDestructive ColorVariant = "destructive"
	VariantOutline     ColorVariant = "outline"
	VariantGhost       ColorVariant = "ghost"
	VariantLink        ColorVariant = "link"
	VariantMuted       ColorVariant = "muted"
	VariantAccent      ColorVariant = "accent"
)

// Size is the shared sizing scale for buttons, badges, inputs, avatars.
type Size string

const (
	SizeSm      Size = "sm"
	SizeDefault Size = "default"
	SizeLg      Size = "lg"
	SizeIcon    Size = "icon"
	SizeXs      Size = "xs"
	SizeXl      Size = "xl"
)

// Spacing maps to Tailwind's spacing scale (0, 0.5, 1, 1.5, ..., 24).
// Use the string form to avoid coupling Go callers to Tailwind's number
// alphabet — the React renderer interprets it as a Tailwind gap utility.
type Spacing string

const (
	SpacingNone Spacing = "0"
	SpacingXs   Spacing = "1"
	SpacingSm   Spacing = "2"
	SpacingMd   Spacing = "4"
	SpacingLg   Spacing = "6"
	SpacingXl   Spacing = "8"
	Spacing2xl  Spacing = "10"
)

// Align maps to items-* (flex cross-axis alignment).
type Align string

const (
	AlignStart    Align = "start"
	AlignCenter   Align = "center"
	AlignEnd      Align = "end"
	AlignStretch  Align = "stretch"
	AlignBaseline Align = "baseline"
)

// Justify maps to justify-* (flex main-axis alignment).
type Justify string

const (
	JustifyStart   Justify = "start"
	JustifyCenter  Justify = "center"
	JustifyEnd     Justify = "end"
	JustifyBetween Justify = "between"
	JustifyAround  Justify = "around"
	JustifyEvenly  Justify = "evenly"
)

// Direction is the flex axis.
type Direction string

const (
	DirectionRow      Direction = "row"
	DirectionColumn   Direction = "column"
	DirectionRowRev   Direction = "row-reverse"
	DirectionColumnRv Direction = "column-reverse"
)

// Radius maps to Tailwind's rounded-* utilities.
type Radius string

const (
	RadiusNone Radius = "none"
	RadiusSm   Radius = "sm"
	RadiusMd   Radius = "md"
	RadiusLg   Radius = "lg"
	RadiusXl   Radius = "xl"
	RadiusFull Radius = "full"
)

// Shadow maps to Tailwind's shadow-* utilities.
type Shadow string

const (
	ShadowNone Shadow = "none"
	ShadowSm   Shadow = "sm"
	ShadowMd   Shadow = "md"
	ShadowLg   Shadow = "lg"
	ShadowXl   Shadow = "xl"
)

// Orientation distinguishes horizontal/vertical for separators, splits,
// radio groups, accordions.
type Orientation string

const (
	OrientationHorizontal Orientation = "horizontal"
	OrientationVertical   Orientation = "vertical"
)

// ContainerSize is the max-width preset for layout.container.
type ContainerSize string

const (
	ContainerSm   ContainerSize = "sm"
	ContainerMd   ContainerSize = "md"
	ContainerLg   ContainerSize = "lg"
	ContainerXl   ContainerSize = "xl"
	Container2xl  ContainerSize = "2xl"
	ContainerFull ContainerSize = "full"
)

// ResponsiveCols is the grid column count. Allows either a fixed integer
// or a per-breakpoint object.
type ResponsiveCols struct {
	Base int `json:"base,omitempty"`
	Sm   int `json:"sm,omitempty"`
	Md   int `json:"md,omitempty"`
	Lg   int `json:"lg,omitempty"`
	Xl   int `json:"xl,omitempty"`
	X2xl int `json:"2xl,omitempty"`
}

// Cols returns a ResponsiveCols with the same column count across all
// breakpoints. Use the struct literal directly when you need responsive
// column counts.
func Cols(n int) ResponsiveCols {
	return ResponsiveCols{Base: n}
}

// HeadingLevel is the semantic level of a heading element (1–6).
type HeadingLevel int

// AspectRatio maps to Tailwind's aspect-* utilities.
type AspectRatio string

const (
	AspectSquare AspectRatio = "square"
	AspectVideo  AspectRatio = "video"
	AspectAuto   AspectRatio = "auto"
)

// ObjectFit maps to Tailwind's object-* utilities.
type ObjectFit string

const (
	ObjectCover   ObjectFit = "cover"
	ObjectContain ObjectFit = "contain"
	ObjectFill    ObjectFit = "fill"
	ObjectNone    ObjectFit = "none"
)

// TextSize is the typographic size for atom.text.
type TextSize string

const (
	TextXs   TextSize = "xs"
	TextSm   TextSize = "sm"
	TextBase TextSize = "base"
	TextLg   TextSize = "lg"
	TextXl   TextSize = "xl"
)

// TextVariant is the semantic color tone for atom.text. Maps to shadcn
// foreground tokens.
type TextVariant string

const (
	TextVariantDefault     TextVariant = "default"
	TextVariantMuted       TextVariant = "muted"
	TextVariantDestructive TextVariant = "destructive"
	TextVariantPrimary     TextVariant = "primary"
	TextVariantSecondary   TextVariant = "secondary"
)
