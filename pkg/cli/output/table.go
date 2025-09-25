package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// TableFormatter formats data as tables
type TableFormatter struct {
	config TableConfig
}

// TableConfig contains table formatting configuration
type TableConfig struct {
	ShowHeaders   bool              `yaml:"show_headers" json:"show_headers"`
	Separator     string            `yaml:"separator" json:"separator"`
	Padding       int               `yaml:"padding" json:"padding"`
	Alignment     string            `yaml:"alignment" json:"alignment"` // left, right, center
	MaxWidth      int               `yaml:"max_width" json:"max_width"`
	WrapText      bool              `yaml:"wrap_text" json:"wrap_text"`
	BorderStyle   string            `yaml:"border_style" json:"border_style"` // none, ascii, unicode
	SortBy        string            `yaml:"sort_by" json:"sort_by"`
	SortOrder     string            `yaml:"sort_order" json:"sort_order"` // asc, desc
	IncludeFields []string          `yaml:"include_fields" json:"include_fields"`
	ExcludeFields []string          `yaml:"exclude_fields" json:"exclude_fields"`
	CustomHeaders map[string]string `yaml:"custom_headers" json:"custom_headers"`
}

// NewTableFormatter creates a new table formatter
func NewTableFormatter() OutputFormatter {
	return &TableFormatter{
		config: DefaultTableConfig(),
	}
}

// NewTableFormatterWithConfig creates a table formatter with custom config
func NewTableFormatterWithConfig(config TableConfig) OutputFormatter {
	return &TableFormatter{
		config: config,
	}
}

// Format formats data as a table
func (tf *TableFormatter) Format(data interface{}) ([]byte, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		return tf.formatFromMap(v)
	case []interface{}:
		return tf.formatFromSlice(v)
	case []map[string]interface{}:
		return tf.formatFromMapSlice(v)
	default:
		return tf.formatFromStruct(data)
	}
}

// MimeType returns the MIME type
func (tf *TableFormatter) MimeType() string {
	return "text/plain"
}

// FileExtension returns the file extension
func (tf *TableFormatter) FileExtension() string {
	return ".txt"
}

// formatFromMap formats data from a map (key-value pairs)
func (tf *TableFormatter) formatFromMap(data map[string]interface{}) ([]byte, error) {
	if headers, exists := data["headers"]; exists {
		if rows, exists := data["rows"]; exists {
			return tf.formatTable(headers, rows)
		}
	}

	// Convert map to table format
	var keys []string
	for k := range data {
		if tf.shouldIncludeField(k) {
			keys = append(keys, k)
		}
	}

	// Sort keys if needed
	if tf.config.SortBy != "" || tf.config.SortOrder != "" {
		sort.Strings(keys)
		if tf.config.SortOrder == "desc" {
			for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	headers := []string{"Key", "Value"}
	var rows [][]string

	for _, key := range keys {
		value := tf.formatValue(data[key])
		rows = append(rows, []string{key, value})
	}

	return tf.renderTable(headers, rows)
}

// formatFromSlice formats data from a slice
func (tf *TableFormatter) formatFromSlice(data []interface{}) ([]byte, error) {
	if len(data) == 0 {
		return []byte("No data"), nil
	}

	// Check if all items are maps (common case)
	allMaps := true
	for _, item := range data {
		if _, ok := item.(map[string]interface{}); !ok {
			allMaps = false
			break
		}
	}

	if allMaps {
		mapSlice := make([]map[string]interface{}, len(data))
		for i, item := range data {
			mapSlice[i] = item.(map[string]interface{})
		}
		return tf.formatFromMapSlice(mapSlice)
	}

	// Handle slice of primitive values
	headers := []string{"Index", "Value"}
	var rows [][]string

	for i, item := range data {
		rows = append(rows, []string{strconv.Itoa(i), tf.formatValue(item)})
	}

	return tf.renderTable(headers, rows)
}

// formatFromMapSlice formats data from a slice of maps
func (tf *TableFormatter) formatFromMapSlice(data []map[string]interface{}) ([]byte, error) {
	if len(data) == 0 {
		return []byte("No data"), nil
	}

	// Extract all unique keys
	keySet := make(map[string]bool)
	for _, item := range data {
		for key := range item {
			if tf.shouldIncludeField(key) {
				keySet[key] = true
			}
		}
	}

	// Convert to sorted slice
	var headers []string
	for key := range keySet {
		headers = append(headers, key)
	}
	sort.Strings(headers)

	// Apply custom headers
	for i, header := range headers {
		if customHeader, exists := tf.config.CustomHeaders[header]; exists {
			headers[i] = customHeader
		}
	}

	// Build rows
	var rows [][]string
	for _, item := range data {
		var row []string
		for _, key := range headers {
			// Use original key for lookup, not custom header
			originalKey := key
			for orig, custom := range tf.config.CustomHeaders {
				if custom == key {
					originalKey = orig
					break
				}
			}

			value := tf.formatValue(item[originalKey])
			row = append(row, value)
		}
		rows = append(rows, row)
	}

	// Sort rows if configured
	if tf.config.SortBy != "" {
		tf.sortRows(headers, rows)
	}

	return tf.renderTable(headers, rows)
}

// formatFromStruct formats data from a struct using reflection
func (tf *TableFormatter) formatFromStruct(data interface{}) ([]byte, error) {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		return tf.formatSingleStruct(v)
	case reflect.Slice:
		return tf.formatStructSlice(v)
	default:
		// Single value
		headers := []string{"Value"}
		rows := [][]string{{tf.formatValue(data)}}
		return tf.renderTable(headers, rows)
	}
}

// formatSingleStruct formats a single struct
func (tf *TableFormatter) formatSingleStruct(v reflect.Value) ([]byte, error) {
	t := v.Type()
	headers := []string{"Field", "Value"}
	var rows [][]string

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldName := field.Name
		if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
			fieldName = strings.Split(tag, ",")[0]
		}

		if !tf.shouldIncludeField(fieldName) {
			continue
		}

		value := tf.formatValue(v.Field(i).Interface())
		rows = append(rows, []string{fieldName, value})
	}

	return tf.renderTable(headers, rows)
}

// formatStructSlice formats a slice of structs
func (tf *TableFormatter) formatStructSlice(v reflect.Value) ([]byte, error) {
	if v.Len() == 0 {
		return []byte("No data"), nil
	}

	// Get field names from the first struct
	firstElem := v.Index(0)
	if firstElem.Kind() == reflect.Ptr {
		firstElem = firstElem.Elem()
	}

	t := firstElem.Type()
	var headers []string
	var fieldNames []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldName := field.Name
		displayName := fieldName

		if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
			fieldName = strings.Split(tag, ",")[0]
			displayName = fieldName
		}

		if tag := field.Tag.Get("table"); tag != "" {
			displayName = tag
		}

		if !tf.shouldIncludeField(fieldName) {
			continue
		}

		headers = append(headers, displayName)
		fieldNames = append(fieldNames, fieldName)
	}

	// Build rows
	var rows [][]string
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}

		var row []string
		for _, fieldName := range fieldNames {
			var value interface{}

			// Find field by name (handle json tag names)
			found := false
			elemType := elem.Type()
			for k := 0; k < elem.NumField(); k++ {
				field := elemType.Field(k)
				checkName := field.Name

				if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
					checkName = strings.Split(tag, ",")[0]
				}

				if checkName == fieldName {
					value = elem.Field(k).Interface()
					found = true
					break
				}
			}

			if !found {
				value = ""
			}

			row = append(row, tf.formatValue(value))
		}
		rows = append(rows, row)
	}

	return tf.renderTable(headers, rows)
}

// formatTable formats with explicit headers and rows
func (tf *TableFormatter) formatTable(headers, rows interface{}) ([]byte, error) {
	headerSlice, ok := tf.interfaceToStringSlice(headers)
	if !ok {
		return nil, fmt.Errorf("headers must be a string slice")
	}

	rowsSlice, ok := tf.interfaceToStringSliceSlice(rows)
	if !ok {
		return nil, fmt.Errorf("rows must be a slice of string slices")
	}

	return tf.renderTable(headerSlice, rowsSlice)
}

// renderTable renders the final table
func (tf *TableFormatter) renderTable(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer

	// Apply border style
	switch tf.config.BorderStyle {
	case "unicode":
		return tf.renderUnicodeTable(&buf, headers, rows)
	case "ascii":
		return tf.renderASCIITable(&buf, headers, rows)
	default:
		return tf.renderPlainTable(&buf, headers, rows)
	}
}

// renderPlainTable renders a plain table using tabwriter
func (tf *TableFormatter) renderPlainTable(buf *bytes.Buffer, headers []string, rows [][]string) ([]byte, error) {
	w := tabwriter.NewWriter(buf, 0, tf.config.Padding, 1, '\t', 0)

	// Write headers if enabled
	if tf.config.ShowHeaders {
		headerLine := strings.Join(headers, "\t")
		fmt.Fprintln(w, headerLine)

		// Add separator line
		var separators []string
		for range headers {
			separators = append(separators, strings.Repeat("-", 10))
		}
		fmt.Fprintln(w, strings.Join(separators, "\t"))
	}

	// Write rows
	for _, row := range rows {
		// Ensure row has same number of columns as headers
		adjustedRow := make([]string, len(headers))
		for i := range headers {
			if i < len(row) {
				adjustedRow[i] = tf.truncateIfNeeded(row[i])
			}
		}
		fmt.Fprintln(w, strings.Join(adjustedRow, "\t"))
	}

	w.Flush()
	return buf.Bytes(), nil
}

// renderASCIITable renders a table with ASCII borders
func (tf *TableFormatter) renderASCIITable(buf *bytes.Buffer, headers []string, rows [][]string) ([]byte, error) {
	// Calculate column widths
	colWidths := tf.calculateColumnWidths(headers, rows)

	// Top border
	tf.writeHorizontalBorder(buf, colWidths, "+", "-", "+")
	buf.WriteString("\n")

	// Headers
	if tf.config.ShowHeaders {
		tf.writeRow(buf, headers, colWidths, "|")
		buf.WriteString("\n")
		tf.writeHorizontalBorder(buf, colWidths, "+", "-", "+")
		buf.WriteString("\n")
	}

	// Rows
	for _, row := range rows {
		tf.writeRow(buf, row, colWidths, "|")
		buf.WriteString("\n")
	}

	// Bottom border
	tf.writeHorizontalBorder(buf, colWidths, "+", "-", "+")
	buf.WriteString("\n")

	return buf.Bytes(), nil
}

// renderUnicodeTable renders a table with Unicode borders
func (tf *TableFormatter) renderUnicodeTable(buf *bytes.Buffer, headers []string, rows [][]string) ([]byte, error) {
	// Calculate column widths
	colWidths := tf.calculateColumnWidths(headers, rows)

	// Top border
	tf.writeHorizontalBorder(buf, colWidths, "┌", "─", "┬")
	buf.WriteString("┐\n")

	// Headers
	if tf.config.ShowHeaders {
		tf.writeRow(buf, headers, colWidths, "│")
		buf.WriteString("│\n")
		tf.writeHorizontalBorder(buf, colWidths, "├", "─", "┼")
		buf.WriteString("┤\n")
	}

	// Rows
	for i, row := range rows {
		tf.writeRow(buf, row, colWidths, "│")
		buf.WriteString("│\n")

		// Add separator between rows (except last)
		if i < len(rows)-1 {
			tf.writeHorizontalBorder(buf, colWidths, "├", "─", "┼")
			buf.WriteString("┤\n")
		}
	}

	// Bottom border
	tf.writeHorizontalBorder(buf, colWidths, "└", "─", "┴")
	buf.WriteString("┘\n")

	return buf.Bytes(), nil
}

// Helper methods

// calculateColumnWidths calculates the width needed for each column
func (tf *TableFormatter) calculateColumnWidths(headers []string, rows [][]string) []int {
	colCount := len(headers)
	widths := make([]int, colCount)

	// Initialize with header widths
	for i, header := range headers {
		widths[i] = len(header)
	}

	// Check row widths
	for _, row := range rows {
		for i := 0; i < colCount && i < len(row); i++ {
			cellWidth := len(row[i])
			if tf.config.MaxWidth > 0 && cellWidth > tf.config.MaxWidth {
				cellWidth = tf.config.MaxWidth
			}
			if cellWidth > widths[i] {
				widths[i] = cellWidth
			}
		}
	}

	return widths
}

// writeHorizontalBorder writes a horizontal border
func (tf *TableFormatter) writeHorizontalBorder(buf *bytes.Buffer, widths []int, left, middle, sep string) {
	buf.WriteString(left)
	for i, width := range widths {
		buf.WriteString(strings.Repeat(middle, width+2))
		if i < len(widths)-1 {
			buf.WriteString(sep)
		}
	}
}

// writeRow writes a table row
func (tf *TableFormatter) writeRow(buf *bytes.Buffer, row []string, widths []int, sep string) {
	buf.WriteString(sep)
	for i, width := range widths {
		var cell string
		if i < len(row) {
			cell = tf.truncateIfNeeded(row[i])
		}

		// Pad cell
		padding := width - len(cell)
		switch tf.config.Alignment {
		case "right":
			buf.WriteString(" ")
			buf.WriteString(strings.Repeat(" ", padding))
			buf.WriteString(cell)
			buf.WriteString(" ")
		case "center":
			leftPad := padding / 2
			rightPad := padding - leftPad
			buf.WriteString(" ")
			buf.WriteString(strings.Repeat(" ", leftPad))
			buf.WriteString(cell)
			buf.WriteString(strings.Repeat(" ", rightPad))
			buf.WriteString(" ")
		default: // left
			buf.WriteString(" ")
			buf.WriteString(cell)
			buf.WriteString(strings.Repeat(" ", padding))
			buf.WriteString(" ")
		}
		buf.WriteString(sep)
	}
}

// sortRows sorts rows based on configuration
func (tf *TableFormatter) sortRows(headers []string, rows [][]string) {
	if tf.config.SortBy == "" {
		return
	}

	// Find sort column index
	sortColIndex := -1
	for i, header := range headers {
		if header == tf.config.SortBy {
			sortColIndex = i
			break
		}
	}

	if sortColIndex == -1 {
		return
	}

	// Sort rows
	sort.Slice(rows, func(i, j int) bool {
		if sortColIndex >= len(rows[i]) || sortColIndex >= len(rows[j]) {
			return false
		}

		result := strings.Compare(rows[i][sortColIndex], rows[j][sortColIndex])
		if tf.config.SortOrder == "desc" {
			return result > 0
		}
		return result < 0
	})
}

// shouldIncludeField checks if a field should be included
func (tf *TableFormatter) shouldIncludeField(fieldName string) bool {
	// Check exclude list first
	for _, excluded := range tf.config.ExcludeFields {
		if excluded == fieldName {
			return false
		}
	}

	// If include list is specified, field must be in it
	if len(tf.config.IncludeFields) > 0 {
		for _, included := range tf.config.IncludeFields {
			if included == fieldName {
				return true
			}
		}
		return false
	}

	return true
}

// formatValue formats a value for display
func (tf *TableFormatter) formatValue(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []interface{}:
		if len(v) == 0 {
			return "[]"
		}
		var parts []string
		for _, item := range v {
			parts = append(parts, tf.formatValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]interface{}:
		if len(v) == 0 {
			return "{}"
		}
		jsonBytes, _ := json.Marshal(v)
		return string(jsonBytes)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// truncateIfNeeded truncates text if it exceeds max width
func (tf *TableFormatter) truncateIfNeeded(text string) string {
	if tf.config.MaxWidth <= 0 || len(text) <= tf.config.MaxWidth {
		return text
	}

	if tf.config.MaxWidth <= 3 {
		return text[:tf.config.MaxWidth]
	}

	return text[:tf.config.MaxWidth-3] + "..."
}

// interfaceToStringSlice converts interface{} to []string
func (tf *TableFormatter) interfaceToStringSlice(data interface{}) ([]string, bool) {
	switch v := data.(type) {
	case []string:
		return v, true
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result, true
	default:
		return nil, false
	}
}

// interfaceToStringSliceSlice converts interface{} to [][]string
func (tf *TableFormatter) interfaceToStringSliceSlice(data interface{}) ([][]string, bool) {
	switch v := data.(type) {
	case [][]string:
		return v, true
	case []interface{}:
		result := make([][]string, len(v))
		for i, item := range v {
			if row, ok := tf.interfaceToStringSlice(item); ok {
				result[i] = row
			} else {
				return nil, false
			}
		}
		return result, true
	default:
		return nil, false
	}
}

// DefaultTableConfig returns default table configuration
func DefaultTableConfig() TableConfig {
	return TableConfig{
		ShowHeaders:   true,
		Separator:     "\t",
		Padding:       1,
		Alignment:     "left",
		MaxWidth:      0,
		WrapText:      false,
		BorderStyle:   "none",
		SortOrder:     "asc",
		CustomHeaders: make(map[string]string),
	}
}
