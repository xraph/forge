package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// TableWriter provides table formatting
type TableWriter interface {
	SetHeader(headers []string)
	AppendRow(row []string)
	SetAlignment(alignment TableAlignment)
	SetColumnAlignment(column int, alignment TableAlignment)
	SetStyle(style TableStyle)
	SetMaxColumnWidth(width int)
	SetMinColumnWidth(width int)
	Render()
}

// TableAlignment defines column alignment
type TableAlignment int

const (
	AlignLeft TableAlignment = iota
	AlignCenter
	AlignRight
)

// TableStyle defines the table border style
type TableStyle int

const (
	StyleDefault TableStyle = iota
	StyleRounded
	StyleSimple
	StyleCompact
	StyleMarkdown
)

// table implements TableWriter
type table struct {
	output           io.Writer
	headers          []string
	rows             [][]string
	alignment        TableAlignment
	columnAlignments map[int]TableAlignment
	colors           bool
	style            TableStyle
	maxColumnWidth   int
	minColumnWidth   int
}

// Border characters for different styles
type borderChars struct {
	topLeft     string
	topRight    string
	bottomLeft  string
	bottomRight string
	horizontal  string
	vertical    string
	cross       string
	topT        string
	bottomT     string
	leftT       string
	rightT      string
}

var borderStyles = map[TableStyle]borderChars{
	StyleDefault: {
		topLeft: "┌", topRight: "┐", bottomLeft: "└", bottomRight: "┘",
		horizontal: "─", vertical: "│", cross: "┼",
		topT: "┬", bottomT: "┴", leftT: "├", rightT: "┤",
	},
	StyleRounded: {
		topLeft: "╭", topRight: "╮", bottomLeft: "╰", bottomRight: "╯",
		horizontal: "─", vertical: "│", cross: "┼",
		topT: "┬", bottomT: "┴", leftT: "├", rightT: "┤",
	},
	StyleSimple: {
		topLeft: "+", topRight: "+", bottomLeft: "+", bottomRight: "+",
		horizontal: "-", vertical: "|", cross: "+",
		topT: "+", bottomT: "+", leftT: "+", rightT: "+",
	},
	StyleCompact: {
		topLeft: "", topRight: "", bottomLeft: "", bottomRight: "",
		horizontal: "─", vertical: " ", cross: " ",
		topT: "", bottomT: "", leftT: "", rightT: "",
	},
	StyleMarkdown: {
		topLeft: "|", topRight: "|", bottomLeft: "|", bottomRight: "|",
		horizontal: "-", vertical: "|", cross: "|",
		topT: "|", bottomT: "|", leftT: "|", rightT: "|",
	},
}

// newTable creates a new table
func newTable(output io.Writer, colors bool) TableWriter {
	return &table{
		output:           output,
		headers:          []string{},
		rows:             [][]string{},
		alignment:        AlignLeft,
		columnAlignments: make(map[int]TableAlignment),
		colors:           colors,
		style:            StyleDefault,
		maxColumnWidth:   80,
		minColumnWidth:   3,
	}
}

// SetHeader sets the table headers
func (t *table) SetHeader(headers []string) {
	t.headers = headers
}

// AppendRow appends a row to the table
func (t *table) AppendRow(row []string) {
	t.rows = append(t.rows, row)
}

// SetAlignment sets the default column alignment
func (t *table) SetAlignment(alignment TableAlignment) {
	t.alignment = alignment
}

// SetColumnAlignment sets alignment for a specific column
func (t *table) SetColumnAlignment(column int, alignment TableAlignment) {
	t.columnAlignments[column] = alignment
}

// SetStyle sets the table border style
func (t *table) SetStyle(style TableStyle) {
	t.style = style
}

// SetMaxColumnWidth sets the maximum width for columns
func (t *table) SetMaxColumnWidth(width int) {
	t.maxColumnWidth = width
}

// SetMinColumnWidth sets the minimum width for columns
func (t *table) SetMinColumnWidth(width int) {
	t.minColumnWidth = width
}

// Render renders the table to the output
func (t *table) Render() {
	if len(t.headers) == 0 && len(t.rows) == 0 {
		return
	}

	borders := borderStyles[t.style]

	// Calculate column widths
	colWidths := t.calculateColumnWidths()

	// Render top border
	if t.style != StyleCompact && t.style != StyleMarkdown {
		t.renderBorder(colWidths, borders.topLeft, borders.topT, borders.topRight, borders.horizontal)
	}

	// Render headers
	if len(t.headers) > 0 {
		t.renderRow(t.headers, colWidths, true, borders.vertical)

		if t.style == StyleMarkdown {
			// Markdown-style separator
			t.renderMarkdownSeparator(colWidths)
		} else {
			t.renderBorder(colWidths, borders.leftT, borders.cross, borders.rightT, borders.horizontal)
		}
	}

	// Render rows
	for i, row := range t.rows {
		t.renderRow(row, colWidths, false, borders.vertical)

		// Optional row separator
		if i < len(t.rows)-1 && t.style == StyleSimple {
			t.renderBorder(colWidths, borders.leftT, borders.cross, borders.rightT, borders.horizontal)
		}
	}

	// Render bottom border
	if t.style != StyleCompact && t.style != StyleMarkdown {
		t.renderBorder(colWidths, borders.bottomLeft, borders.bottomT, borders.bottomRight, borders.horizontal)
	}
}

// calculateColumnWidths calculates the width of each column
func (t *table) calculateColumnWidths() []int {
	numCols := len(t.headers)
	for _, row := range t.rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}

	widths := make([]int, numCols)

	// Check header widths
	for i, header := range t.headers {
		width := visualLength(header)
		if width > widths[i] {
			widths[i] = width
		}
	}

	// Check row widths
	for _, row := range t.rows {
		for i, cell := range row {
			if i < numCols {
				width := visualLength(cell)
				if width > widths[i] {
					widths[i] = width
				}
			}
		}
	}

	// Apply min/max constraints and add padding
	for i := range widths {
		if widths[i] < t.minColumnWidth {
			widths[i] = t.minColumnWidth
		}
		if widths[i] > t.maxColumnWidth {
			widths[i] = t.maxColumnWidth
		}
		widths[i] += 2 // 1 space padding on each side
	}

	return widths
}

// renderBorder renders a table border
func (t *table) renderBorder(widths []int, left, middle, right, horizontal string) {
	if left != "" {
		fmt.Fprint(t.output, left)
	}

	for i, width := range widths {
		fmt.Fprint(t.output, strings.Repeat(horizontal, width))
		if i < len(widths)-1 {
			if middle != "" {
				fmt.Fprint(t.output, middle)
			}
		}
	}

	if right != "" {
		fmt.Fprintln(t.output, right)
	} else {
		fmt.Fprintln(t.output)
	}
}

// renderMarkdownSeparator renders a markdown-style separator
func (t *table) renderMarkdownSeparator(widths []int) {
	fmt.Fprint(t.output, "|")
	for i, width := range widths {
		// Get alignment for this column
		align := t.alignment
		if colAlign, ok := t.columnAlignments[i]; ok {
			align = colAlign
		}

		switch align {
		case AlignLeft:
			fmt.Fprint(t.output, ":")
			fmt.Fprint(t.output, strings.Repeat("-", width-1))
		case AlignRight:
			fmt.Fprint(t.output, strings.Repeat("-", width-1))
			fmt.Fprint(t.output, ":")
		case AlignCenter:
			fmt.Fprint(t.output, ":")
			fmt.Fprint(t.output, strings.Repeat("-", width-2))
			fmt.Fprint(t.output, ":")
		default:
			fmt.Fprint(t.output, strings.Repeat("-", width))
		}

		if i < len(widths)-1 {
			fmt.Fprint(t.output, "|")
		}
	}
	fmt.Fprintln(t.output, "|")
}

// renderRow renders a table row
func (t *table) renderRow(row []string, widths []int, isHeader bool, vertical string) {
	if vertical != "" {
		fmt.Fprint(t.output, vertical)
	}

	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}

		// Get alignment for this column
		align := t.alignment
		if colAlign, ok := t.columnAlignments[i]; ok {
			align = colAlign
		}

		// Format cell with alignment
		formatted := t.formatCell(cell, width, align)

		// Apply bold to headers
		if isHeader && t.colors {
			formatted = Bold(formatted)
		}

		fmt.Fprint(t.output, formatted)

		if i < len(widths)-1 && vertical != "" {
			fmt.Fprint(t.output, vertical)
		}
	}

	if vertical != "" {
		fmt.Fprintln(t.output, vertical)
	} else {
		fmt.Fprintln(t.output)
	}
}

// formatCell formats a cell with padding and alignment
func (t *table) formatCell(cell string, width int, align TableAlignment) string {
	// Truncate if too long
	cellLen := visualLength(cell)
	if cellLen > width-2 { // Account for padding
		cell = truncateString(cell, width-5) + "..."
		cellLen = visualLength(cell)
	}

	padding := width - cellLen

	if padding <= 0 {
		return cell
	}

	switch align {
	case AlignLeft:
		return " " + cell + strings.Repeat(" ", padding-1)
	case AlignRight:
		return strings.Repeat(" ", padding-1) + cell + " "
	case AlignCenter:
		leftPad := padding / 2
		rightPad := padding - leftPad
		return strings.Repeat(" ", leftPad) + cell + strings.Repeat(" ", rightPad)
	default:
		return " " + cell + strings.Repeat(" ", padding-1)
	}
}

// visualLength calculates the visual length of a string (excluding ANSI codes)
func visualLength(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

// stripANSI removes ANSI color codes from a string for length calculation
// Uses regex for more robust ANSI code removal
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// truncateString truncates a string to the specified visual length
func truncateString(s string, maxLen int) string {
	stripped := stripANSI(s)
	if utf8.RuneCountInString(stripped) <= maxLen {
		return s
	}

	// Find ANSI codes in original string
	ansiCodes := ansiRegex.FindAllString(s, -1)

	runes := []rune(stripped)
	if len(runes) > maxLen {
		result := string(runes[:maxLen])
		// Reapply first ANSI code if present
		if len(ansiCodes) > 0 {
			result = ansiCodes[0] + result + "\x1b[0m" // Reset
		}
		return result
	}
	return s
}
