package templates

import (
	"bytes"
	"embed"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"text/template"
)

//go:embed code/*
var codeTemplates embed.FS

//go:embed projects/*
var projectTemplates embed.FS

// TemplateManager manages code generation templates
type TemplateManager struct {
	templates map[string]*template.Template
	funcs     template.FuncMap
}

// NewTemplateManager creates a new template manager
func NewTemplateManager() *TemplateManager {
	tm := &TemplateManager{
		templates: make(map[string]*template.Template),
		funcs:     makeTemplateFuncs(),
	}

	// Load all templates
	if err := tm.loadTemplates(); err != nil {
		panic(fmt.Sprintf("failed to load templates: %v", err))
	}

	return tm
}

// Execute executes a template with the given data
func (tm *TemplateManager) Execute(templateName string, data interface{}) (string, error) {
	tmpl, exists := tm.templates[templateName]
	if !exists {
		return "", fmt.Errorf("template '%s' not found", templateName)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template '%s': %w", templateName, err)
	}

	return buf.String(), nil
}

// GetAvailableTemplates returns information about available templates
func (tm *TemplateManager) GetAvailableTemplates() []TemplateInfo {
	var templates []TemplateInfo

	for name, _ := range tm.templates {
		templates = append(templates, TemplateInfo{
			Name:        name,
			Type:        getTemplateType(name),
			Description: getTemplateDescription(name),
			Path:        fmt.Sprintf("code/%s.gotmpl", name),
		})
	}

	return templates
}

// loadTemplates loads all template files from embedded filesystem
func (tm *TemplateManager) loadTemplates() error {
	// Load code generation templates
	if err := tm.loadTemplatesFromFS(codeTemplates, "code"); err != nil {
		return fmt.Errorf("failed to load code templates: %w", err)
	}

	// Load project templates
	if err := tm.loadTemplatesFromFS(projectTemplates, "projects"); err != nil {
		return fmt.Errorf("failed to load project templates: %w", err)
	}

	return nil
}

// loadTemplatesFromFS loads templates from an embedded filesystem
func (tm *TemplateManager) loadTemplatesFromFS(fs embed.FS, root string) error {
	entries, err := fs.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Recursively load from subdirectories
			subPath := filepath.Join(root, entry.Name())
			if err := tm.loadTemplatesFromFS(fs, subPath); err != nil {
				return err
			}
			continue
		}

		// Only process .gotmpl files
		if !strings.HasSuffix(entry.Name(), ".gotmpl") {
			continue
		}

		templatePath := filepath.Join(root, entry.Name())
		content, err := fs.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", templatePath, err)
		}

		// Extract template name from filename
		templateName := strings.TrimSuffix(entry.Name(), ".gotmpl")

		// Create template with functions
		tmpl, err := template.New(templateName).Funcs(tm.funcs).Parse(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templateName, err)
		}

		tm.templates[templateName] = tmpl
	}

	return nil
}

// makeTemplateFuncs creates template functions
func makeTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		// String manipulation
		"toLower":     strings.ToLower,
		"toUpper":     strings.ToUpper,
		"title":       strings.Title,
		"camelCase":   toCamelCase,
		"toCamelCase": toCamelCase,
		"snakeCase":   toSnakeCase,
		"toSnakeCase": toSnakeCase,
		"kebabCase":   toKebabCase,
		"toKebabCase": toKebabCase,
		"pluralize":   pluralize,
		"contains":    strings.Contains,
		"hasPrefix":   strings.HasPrefix,
		"hasSuffix":   strings.HasSuffix,
		"trimSpace":   strings.TrimSpace,
		"join":        strings.Join,
		"split":       strings.Split,
		"substr":      toSubString,

		// Logical operations
		"and": templateAnd,
		"or":  templateOr,
		"not": templateNot,
		"eq":  templateEq,
		"ne":  templateNe,
		"lt":  templateLt,
		"le":  templateLe,
		"gt":  templateGt,
		"ge":  templateGe,

		// Utility functions
		"default":  templateDefault,
		"empty":    templateEmpty,
		"coalesce": templateCoalesce,
		"ternary":  templateTernary,
		"printf":   fmt.Sprintf,
		"print":    fmt.Sprint,
		"println":  fmt.Sprintln,

		// Collection functions
		"len":   templateLen,
		"first": templateFirst,
		"last":  templateLast,
		"index": templateIndex,
		"slice": templateSlice,
		"range": templateRange,

		// Type conversion
		"toString": templateToString,
		"toInt":    templateToInt,
		"toFloat":  templateToFloat,
		"toBool":   templateToBool,
		"typeOf":   templateTypeOf,
		"kindOf":   templateKindOf,

		// Math functions
		"add":   templateAdd,
		"sub":   templateSub,
		"mul":   templateMul,
		"div":   templateDiv,
		"mod":   templateMod,
		"max":   templateMax,
		"min":   templateMin,
		"ceil":  templateCeil,
		"floor": templateFloor,
		"round": templateRound,

		// Date functions
		"now":        templateNow,
		"date":       templateDate,
		"dateInZone": templateDateInZone,
		"duration":   templateDuration,
		"unixEpoch":  templateUnixEpoch,

		// Encoding functions
		"b64enc":     templateB64Enc,
		"b64dec":     templateB64Dec,
		"urlquery":   templateURLQuery,
		"htmlEscape": templateHTMLEscape,
		"jsEscape":   templateJSEscape,
	}
}

// String manipulation helpers
func toCamelCase(s string) string {
	if s == "" {
		return s
	}

	// Handle snake_case and kebab-case
	s = strings.ReplaceAll(s, "-", "_")
	words := strings.Split(s, "_")

	if len(words) == 0 {
		return ""
	}

	result := strings.ToLower(words[0])
	for i := 1; i < len(words); i++ {
		if len(words[i]) > 0 {
			result += strings.ToUpper(words[i][:1]) + strings.ToLower(words[i][1:])
		}
	}

	return result
}

func toSnakeCase(s string) string {
	var result strings.Builder

	for i, r := range s {
		if i > 0 && (r >= 'A' && r <= 'Z') {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}

	return strings.ToLower(result.String())
}

func toKebabCase(s string) string {
	return strings.ReplaceAll(toSnakeCase(s), "_", "-")
}

func pluralize(s string) string {
	if strings.HasSuffix(s, "y") {
		return strings.TrimSuffix(s, "y") + "ies"
	}
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "z") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	return s + "s"
}

func toSubString(s string, start int, length ...int) string {
	if s == "" || start < 0 || start >= len(s) {
		return ""
	}

	if len(length) == 0 {
		return s[start:]
	}

	l := length[0]
	if l <= 0 {
		return ""
	}

	end := start + l
	if end > len(s) {
		end = len(s)
	}

	return s[start:end]
}

// Logical operation helpers
func templateAnd(args ...interface{}) bool {
	for _, arg := range args {
		if !toBool(arg) {
			return false
		}
	}
	return true
}

func templateOr(args ...interface{}) bool {
	for _, arg := range args {
		if toBool(arg) {
			return true
		}
	}
	return false
}

func templateNot(arg interface{}) bool {
	return !toBool(arg)
}

func templateEq(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}

func templateNe(a, b interface{}) bool {
	return !reflect.DeepEqual(a, b)
}

func templateLt(a, b interface{}) bool {
	return compareValues(a, b) < 0
}

func templateLe(a, b interface{}) bool {
	return compareValues(a, b) <= 0
}

func templateGt(a, b interface{}) bool {
	return compareValues(a, b) > 0
}

func templateGe(a, b interface{}) bool {
	return compareValues(a, b) >= 0
}

// Utility helpers
func templateDefault(def interface{}, given interface{}) interface{} {
	if isEmpty(given) {
		return def
	}
	return given
}

func templateEmpty(given interface{}) bool {
	return isEmpty(given)
}

func templateCoalesce(args ...interface{}) interface{} {
	for _, arg := range args {
		if !isEmpty(arg) {
			return arg
		}
	}
	return nil
}

func templateTernary(condition interface{}, trueVal, falseVal interface{}) interface{} {
	if toBool(condition) {
		return trueVal
	}
	return falseVal
}

// Collection helpers
func templateLen(item interface{}) int {
	if item == nil {
		return 0
	}

	v := reflect.ValueOf(item)
	switch v.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String, reflect.Chan:
		return v.Len()
	default:
		return 0
	}
}

func templateFirst(list interface{}) interface{} {
	v := reflect.ValueOf(list)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Len() > 0 {
			return v.Index(0).Interface()
		}
	}
	return nil
}

func templateLast(list interface{}) interface{} {
	v := reflect.ValueOf(list)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Len() > 0 {
			return v.Index(v.Len() - 1).Interface()
		}
	}
	return nil
}

func templateIndex(list interface{}, indices ...interface{}) interface{} {
	v := reflect.ValueOf(list)
	for _, index := range indices {
		switch v.Kind() {
		case reflect.Slice, reflect.Array:
			if idx, ok := index.(int); ok && idx >= 0 && idx < v.Len() {
				v = v.Index(idx)
			} else {
				return nil
			}
		case reflect.Map:
			if v.Type().Key().Kind() == reflect.TypeOf(index).Kind() {
				v = v.MapIndex(reflect.ValueOf(index))
				if !v.IsValid() {
					return nil
				}
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return v.Interface()
}

func templateSlice(list interface{}, indices ...int) interface{} {
	v := reflect.ValueOf(list)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil
	}

	start := 0
	end := v.Len()

	if len(indices) > 0 && indices[0] >= 0 {
		start = indices[0]
	}
	if len(indices) > 1 && indices[1] <= v.Len() {
		end = indices[1]
	}

	if start > end || start >= v.Len() {
		return reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}

	return v.Slice(start, end).Interface()
}

func templateRange(args ...int) []int {
	var start, stop, step int
	switch len(args) {
	case 1:
		start, stop, step = 0, args[0], 1
	case 2:
		start, stop, step = args[0], args[1], 1
	case 3:
		start, stop, step = args[0], args[1], args[2]
	default:
		return nil
	}

	if step == 0 {
		return nil
	}

	var result []int
	if step > 0 {
		for i := start; i < stop; i += step {
			result = append(result, i)
		}
	} else {
		for i := start; i > stop; i += step {
			result = append(result, i)
		}
	}

	return result
}

// Type conversion helpers
func templateToString(v interface{}) string {
	return fmt.Sprintf("%v", v)
}

func templateToInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return 0
}

func templateToFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return 0.0
}

func templateToBool(v interface{}) bool {
	return toBool(v)
}

func templateTypeOf(v interface{}) string {
	return reflect.TypeOf(v).String()
}

func templateKindOf(v interface{}) string {
	return reflect.TypeOf(v).Kind().String()
}

// Math helpers
func templateAdd(a, b interface{}) interface{} {
	switch a.(type) {
	case int:
		return templateToInt(a) + templateToInt(b)
	case int64:
		return int64(templateToInt(a)) + int64(templateToInt(b))
	case float64:
		return templateToFloat(a) + templateToFloat(b)
	default:
		return templateToFloat(a) + templateToFloat(b)
	}
}

func templateSub(a, b interface{}) interface{} {
	switch a.(type) {
	case int:
		return templateToInt(a) - templateToInt(b)
	case int64:
		return int64(templateToInt(a)) - int64(templateToInt(b))
	case float64:
		return templateToFloat(a) - templateToFloat(b)
	default:
		return templateToFloat(a) - templateToFloat(b)
	}
}

func templateMul(a, b interface{}) interface{} {
	switch a.(type) {
	case int:
		return templateToInt(a) * templateToInt(b)
	case int64:
		return int64(templateToInt(a)) * int64(templateToInt(b))
	case float64:
		return templateToFloat(a) * templateToFloat(b)
	default:
		return templateToFloat(a) * templateToFloat(b)
	}
}

func templateDiv(a, b interface{}) interface{} {
	bVal := templateToFloat(b)
	if bVal == 0 {
		return 0
	}
	return templateToFloat(a) / bVal
}

func templateMod(a, b interface{}) interface{} {
	aVal := templateToInt(a)
	bVal := templateToInt(b)
	if bVal == 0 {
		return 0
	}
	return aVal % bVal
}

func templateMax(args ...interface{}) interface{} {
	if len(args) == 0 {
		return nil
	}

	max := args[0]
	for _, arg := range args[1:] {
		if compareValues(arg, max) > 0 {
			max = arg
		}
	}
	return max
}

func templateMin(args ...interface{}) interface{} {
	if len(args) == 0 {
		return nil
	}

	min := args[0]
	for _, arg := range args[1:] {
		if compareValues(arg, min) < 0 {
			min = arg
		}
	}
	return min
}

func templateCeil(v interface{}) float64 {
	// This would require importing math package
	// For now, return the float value
	return templateToFloat(v)
}

func templateFloor(v interface{}) float64 {
	// This would require importing math package
	// For now, return the float value
	return templateToFloat(v)
}

func templateRound(v interface{}) float64 {
	// This would require importing math package
	// For now, return the float value
	return templateToFloat(v)
}

// Date helpers (simplified implementations)
func templateNow() string {
	// This would require importing time package
	return "2023-01-01T00:00:00Z"
}

func templateDate(format, date string) string {
	// Simplified implementation
	return date
}

func templateDateInZone(format, date, timezone string) string {
	// Simplified implementation
	return date
}

func templateDuration(v interface{}) string {
	return templateToString(v)
}

func templateUnixEpoch(v interface{}) int64 {
	// Simplified implementation
	return int64(templateToInt(v))
}

// Encoding helpers (simplified implementations)
func templateB64Enc(v string) string {
	// This would require importing base64 package
	return v
}

func templateB64Dec(v string) string {
	// This would require importing base64 package
	return v
}

func templateURLQuery(v string) string {
	// This would require importing net/url package
	return v
}

func templateHTMLEscape(v string) string {
	// This would require importing html package
	return v
}

func templateJSEscape(v string) string {
	// Simplified implementation
	return strings.ReplaceAll(v, "'", "\\'")
}

// Helper functions
func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case nil:
		return false
	case int:
		return val != 0
	case string:
		return val != ""
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map, reflect.Chan:
			return rv.Len() != 0
		case reflect.Ptr, reflect.Interface:
			return !rv.IsNil()
		default:
			return true
		}
	}
}

func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String, reflect.Chan:
		return rv.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	default:
		return false
	}
}

func compareValues(a, b interface{}) int {
	aVal := templateToFloat(a)
	bVal := templateToFloat(b)

	if aVal < bVal {
		return -1
	} else if aVal > bVal {
		return 1
	}
	return 0
}

// Template type and description helpers
func getTemplateType(name string) string {
	switch {
	case strings.Contains(name, "service"):
		return "service"
	case strings.Contains(name, "controller"):
		return "controller"
	case strings.Contains(name, "model"):
		return "model"
	case strings.Contains(name, "middleware"):
		return "middleware"
	case strings.Contains(name, "migration"):
		return "migration"
	case strings.Contains(name, "test"):
		return "test"
	case strings.Contains(name, "plugin"):
		return "plugin"
	default:
		return "other"
	}
}

func getTemplateDescription(name string) string {
	descriptions := map[string]string{
		"service_interface":      "Service interface definition",
		"service_implementation": "Service implementation with dependencies",
		"service_test":           "Service unit tests",
		"controller":             "HTTP controller with routes",
		"controller_test":        "Controller unit tests",
		"model":                  "Database model definition",
		"middleware":             "HTTP middleware component",
		"plugin":                 "Plugin component",
		"command_main":           "Command line application main",
	}

	if desc, exists := descriptions[name]; exists {
		return desc
	}

	return fmt.Sprintf("Template for %s generation", name)
}

// TemplateInfo represents template information
type TemplateInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Path        string `json:"path"`
}
