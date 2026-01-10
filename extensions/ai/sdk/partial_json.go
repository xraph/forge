package sdk

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode"
)

// PartialJSONParser parses incomplete JSON and extracts available fields.
// This enables streaming structured objects as fields arrive from the LLM.
type PartialJSONParser struct {
	buffer       strings.Builder
	depth        int
	inString     bool
	escaped      bool
	lastComplete string
}

// NewPartialJSONParser creates a new partial JSON parser.
func NewPartialJSONParser() *PartialJSONParser {
	return &PartialJSONParser{}
}

// Append adds new content to the parser buffer and returns true if the JSON became more complete.
func (p *PartialJSONParser) Append(content string) bool {
	p.buffer.WriteString(content)

	return p.hasNewCompleteFields()
}

// Reset clears the parser state.
func (p *PartialJSONParser) Reset() {
	p.buffer.Reset()
	p.depth = 0
	p.inString = false
	p.escaped = false
	p.lastComplete = ""
}

// Buffer returns the current buffer content.
func (p *PartialJSONParser) Buffer() string {
	return p.buffer.String()
}

// hasNewCompleteFields checks if new complete fields are available.
func (p *PartialJSONParser) hasNewCompleteFields() bool {
	current := p.repairJSON(p.buffer.String())
	if current != p.lastComplete {
		p.lastComplete = current

		return true
	}

	return false
}

// Parse attempts to parse the current buffer as partial JSON into the target type.
func (p *PartialJSONParser) Parse(target any) error {
	repaired := p.repairJSON(p.buffer.String())
	if repaired == "" {
		return nil
	}

	return json.Unmarshal([]byte(repaired), target)
}

// ParseToMap parses the current buffer as a map for field-by-field access.
func (p *PartialJSONParser) ParseToMap() (map[string]any, error) {
	repaired := p.repairJSON(p.buffer.String())
	if repaired == "" {
		return make(map[string]any), nil
	}

	var result map[string]any

	err := json.Unmarshal([]byte(repaired), &result)
	if err != nil {
		return make(map[string]any), err
	}

	return result, nil
}

// GetCompletedFields returns paths to fields that have complete values.
func (p *PartialJSONParser) GetCompletedFields() [][]string {
	data, err := p.ParseToMap()
	if err != nil {
		return nil
	}

	return p.extractCompletedPaths(data, nil)
}

// extractCompletedPaths recursively extracts paths to completed fields.
func (p *PartialJSONParser) extractCompletedPaths(data map[string]any, prefix []string) [][]string {
	var paths [][]string

	for key, value := range data {
		currentPath := append(prefix, key)
		pathCopy := make([]string, len(currentPath))
		copy(pathCopy, currentPath)

		switch v := value.(type) {
		case map[string]any:
			// Recurse into nested objects
			nested := p.extractCompletedPaths(v, currentPath)
			paths = append(paths, nested...)
		case []any:
			// Array field is complete
			paths = append(paths, pathCopy)
		default:
			// Primitive field is complete
			if v != nil {
				paths = append(paths, pathCopy)
			}
		}
	}

	return paths
}

// repairJSON attempts to repair incomplete JSON by closing unclosed brackets/braces.
func (p *PartialJSONParser) repairJSON(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Track state while scanning
	var (
		stack   []rune
		inStr   bool
		escaped bool
		result  strings.Builder
	)

	for _, ch := range input {
		result.WriteRune(ch)

		if escaped {
			escaped = false

			continue
		}

		if ch == '\\' && inStr {
			escaped = true

			continue
		}

		if ch == '"' {
			inStr = !inStr

			continue
		}

		if inStr {
			continue
		}

		switch ch {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == ch {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Handle trailing incomplete values
	repaired := result.String()
	repaired = p.cleanTrailingIncomplete(repaired, inStr)

	// Close unclosed structures
	var repairedSb170 strings.Builder
	for i := len(stack) - 1; i >= 0; i-- {
		repairedSb170.WriteString(string(stack[i]))
	}
	repaired += repairedSb170.String()

	// Validate the repaired JSON
	var test any
	if err := json.Unmarshal([]byte(repaired), &test); err != nil {
		// Try simpler repairs
		return p.fallbackRepair(input)
	}

	return repaired
}

// cleanTrailingIncomplete removes incomplete trailing content.
func (p *PartialJSONParser) cleanTrailingIncomplete(s string, inString bool) string {
	if inString {
		// Close unclosed string
		return s + `"`
	}

	// Remove trailing comma or incomplete key
	s = strings.TrimRightFunc(s, unicode.IsSpace)

	if strings.HasSuffix(s, ",") {
		s = s[:len(s)-1]
	}

	// Remove incomplete key-value pair (e.g., `"key":` without value)
	if idx := strings.LastIndex(s, ":"); idx != -1 {
		afterColon := strings.TrimSpace(s[idx+1:])
		if afterColon == "" || afterColon == "," {
			// Find the start of this key
			keyStart := strings.LastIndex(s[:idx], `"`)
			if keyStart != -1 {
				// Look for the previous complete structure
				prev := strings.TrimRightFunc(s[:keyStart], unicode.IsSpace)
				if strings.HasSuffix(prev, ",") {
					s = prev[:len(prev)-1]
				} else {
					s = prev
				}
			}
		}
	}

	return s
}

// fallbackRepair attempts a simpler repair strategy.
func (p *PartialJSONParser) fallbackRepair(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "{}"
	}

	// Just try to close all brackets
	var stack []rune

	inStr := false
	escaped := false

	for _, ch := range input {
		if escaped {
			escaped = false

			continue
		}

		if ch == '\\' && inStr {
			escaped = true

			continue
		}

		if ch == '"' {
			inStr = !inStr

			continue
		}

		if inStr {
			continue
		}

		switch ch {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// If still in string, close it
	if inStr {
		input += `"`
	}

	// Close all open structures
	var inputSb266 strings.Builder
	for i := len(stack) - 1; i >= 0; i-- {
		inputSb266.WriteString(string(stack[i]))
	}
	input += inputSb266.String()

	return input
}

// PartialObjectState tracks the state of a partially streamed object.
type PartialObjectState[T any] struct {
	Value          T
	CompletedPaths [][]string
	RawJSON        string
	IsComplete     bool
}

// StreamObjectParser provides type-safe partial object parsing.
type StreamObjectParser[T any] struct {
	parser      *PartialJSONParser
	lastState   *PartialObjectState[T]
	onFieldDone func(path []string, value any)
}

// NewStreamObjectParser creates a new typed stream parser.
func NewStreamObjectParser[T any]() *StreamObjectParser[T] {
	return &StreamObjectParser[T]{
		parser: NewPartialJSONParser(),
	}
}

// OnFieldComplete registers a callback for when a field becomes complete.
func (p *StreamObjectParser[T]) OnFieldComplete(fn func(path []string, value any)) {
	p.onFieldDone = fn
}

// Append adds content and returns the updated partial object state.
func (p *StreamObjectParser[T]) Append(content string) (*PartialObjectState[T], error) {
	hasNew := p.parser.Append(content)

	if !hasNew && p.lastState != nil {
		return p.lastState, nil
	}

	var value T

	err := p.parser.Parse(&value)

	state := &PartialObjectState[T]{
		Value:          value,
		CompletedPaths: p.parser.GetCompletedFields(),
		RawJSON:        p.parser.Buffer(),
		IsComplete:     p.isJSONComplete(p.parser.Buffer()),
	}

	// Check for newly completed fields
	if p.onFieldDone != nil && p.lastState != nil {
		newPaths := p.findNewPaths(p.lastState.CompletedPaths, state.CompletedPaths)

		data, _ := p.parser.ParseToMap()
		for _, path := range newPaths {
			if val := getValueAtPath(data, path); val != nil {
				p.onFieldDone(path, val)
			}
		}
	}

	p.lastState = state

	return state, err
}

// GetCurrentState returns the current partial object state.
func (p *StreamObjectParser[T]) GetCurrentState() *PartialObjectState[T] {
	return p.lastState
}

// Reset clears the parser state.
func (p *StreamObjectParser[T]) Reset() {
	p.parser.Reset()
	p.lastState = nil
}

// isJSONComplete checks if the JSON appears complete.
func (p *StreamObjectParser[T]) isJSONComplete(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	var test any

	err := json.Unmarshal([]byte(s), &test)

	return err == nil
}

// findNewPaths finds paths that are in newPaths but not in oldPaths.
func (p *StreamObjectParser[T]) findNewPaths(oldPaths, newPaths [][]string) [][]string {
	oldSet := make(map[string]bool)
	for _, path := range oldPaths {
		oldSet[strings.Join(path, ".")] = true
	}

	var result [][]string

	for _, path := range newPaths {
		key := strings.Join(path, ".")
		if !oldSet[key] {
			result = append(result, path)
		}
	}

	return result
}

// getValueAtPath retrieves a value from a nested map by path.
func getValueAtPath(data map[string]any, path []string) any {
	if len(path) == 0 {
		return nil
	}

	current := any(data)
	for _, key := range path {
		switch v := current.(type) {
		case map[string]any:
			current = v[key]
		default:
			return nil
		}
	}

	return current
}

// FieldDelta represents a change to a specific field.
type FieldDelta struct {
	Path      []string
	Value     any
	IsNew     bool
	IsUpdated bool
}

// DiffPartialObjects compares two partial object states and returns deltas.
func DiffPartialObjects[T any](old, new *PartialObjectState[T]) []FieldDelta {
	if old == nil {
		// All fields in new are new
		var deltas []FieldDelta
		for _, path := range new.CompletedPaths {
			deltas = append(deltas, FieldDelta{
				Path:  path,
				IsNew: true,
			})
		}

		return deltas
	}

	oldMap := make(map[string]bool)
	for _, p := range old.CompletedPaths {
		oldMap[strings.Join(p, ".")] = true
	}

	var deltas []FieldDelta

	for _, path := range new.CompletedPaths {
		key := strings.Join(path, ".")
		if !oldMap[key] {
			deltas = append(deltas, FieldDelta{
				Path:  path,
				IsNew: true,
			})
		}
	}

	return deltas
}

// ReflectPartialValue copies available fields from source to target using reflection.
// This is useful for updating UI state with partial data.
func ReflectPartialValue(source, target any) error {
	srcVal := reflect.ValueOf(source)
	tgtVal := reflect.ValueOf(target)

	if tgtVal.Kind() != reflect.Ptr {
		return nil
	}

	tgtVal = tgtVal.Elem()
	if srcVal.Kind() == reflect.Ptr {
		srcVal = srcVal.Elem()
	}

	if srcVal.Kind() != reflect.Struct || tgtVal.Kind() != reflect.Struct {
		return nil
	}

	srcType := srcVal.Type()
	for i := 0; i < srcVal.NumField(); i++ {
		field := srcType.Field(i)
		if !field.IsExported() {
			continue
		}

		srcField := srcVal.Field(i)
		tgtField := tgtVal.FieldByName(field.Name)

		if !tgtField.IsValid() || !tgtField.CanSet() {
			continue
		}

		// Only copy non-zero values
		if !srcField.IsZero() {
			tgtField.Set(srcField)
		}
	}

	return nil
}
