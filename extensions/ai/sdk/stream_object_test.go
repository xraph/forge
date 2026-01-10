package sdk

import (
	"context"
	"strings"
	"testing"
)

func TestPartialJSONParser_BasicParsing(t *testing.T) {
	parser := NewPartialJSONParser()

	// Simulate streaming JSON
	chunks := []string{
		`{"na`,
		`me": "Jo`,
		`hn", "age":`,
		` 30}`,
	}

	for _, chunk := range chunks {
		parser.Append(chunk)
	}

	var result map[string]any

	err := parser.Parse(&result)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if result["name"] != "John" {
		t.Errorf("Expected name 'John', got %v", result["name"])
	}

	if result["age"] != float64(30) {
		t.Errorf("Expected age 30, got %v", result["age"])
	}
}

func TestPartialJSONParser_IncompleteJSON(t *testing.T) {
	parser := NewPartialJSONParser()

	// Incomplete JSON
	parser.Append(`{"name": "Alice", "hobbies": ["reading"`)

	result, err := parser.ParseToMap()
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Should be able to get the name
	if result["name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", result["name"])
	}
}

func TestPartialJSONParser_GetCompletedFields(t *testing.T) {
	parser := NewPartialJSONParser()

	parser.Append(`{"name": "Bob", "age": 25, "address": {"city": "NYC"}}`)

	paths := parser.GetCompletedFields()

	expectedPaths := map[string]bool{
		"name":         true,
		"age":          true,
		"address.city": true,
	}

	for _, path := range paths {
		key := strings.Join(path, ".")
		if !expectedPaths[key] {
			t.Errorf("Unexpected path: %s", key)
		}

		delete(expectedPaths, key)
	}

	if len(expectedPaths) > 0 {
		t.Errorf("Missing paths: %v", expectedPaths)
	}
}

func TestPartialJSONParser_RepairJSON(t *testing.T) {
	parser := NewPartialJSONParser()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "complete JSON",
			input:   `{"name": "test"}`,
			wantErr: false,
		},
		{
			name:    "unclosed brace",
			input:   `{"name": "test"`,
			wantErr: false,
		},
		{
			name:    "unclosed string",
			input:   `{"name": "test`,
			wantErr: false,
		},
		{
			name:    "nested unclosed",
			input:   `{"data": {"nested": "value"`,
			wantErr: false,
		},
		{
			name:    "array unclosed",
			input:   `{"items": [1, 2, 3`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			parser.Append(tt.input)

			var result map[string]any

			err := parser.Parse(&result)

			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestStreamObjectParser_TypedParsing(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	parser := NewStreamObjectParser[Person]()

	var completedFields [][]string

	parser.OnFieldComplete(func(path []string, value any) {
		completedFields = append(completedFields, path)
	})

	// Stream the JSON
	chunks := []string{
		`{"name":`,
		` "Alice"`,
		`, "age": `,
		`28}`,
	}

	var lastState *PartialObjectState[Person]

	for _, chunk := range chunks {
		state, _ := parser.Append(chunk)
		if state != nil {
			lastState = state
		}
	}

	if lastState == nil {
		t.Fatal("Expected final state")
	}

	if lastState.Value.Name != "Alice" {
		t.Errorf("Expected name 'Alice', got %s", lastState.Value.Name)
	}

	if lastState.Value.Age != 28 {
		t.Errorf("Expected age 28, got %d", lastState.Value.Age)
	}
}

func TestStreamObjectParser_CompletionDetection(t *testing.T) {
	type Item struct {
		ID    string `json:"id"`
		Value int    `json:"value"`
	}

	parser := NewStreamObjectParser[Item]()

	// Partial JSON
	parser.Append(`{"id": "test123"`)
	state := parser.GetCurrentState()

	if state == nil {
		t.Fatal("Expected state after append")
	}

	if state.IsComplete {
		t.Error("Should not be complete yet")
	}

	// Complete the JSON
	parser.Append(`, "value": 42}`)
	state = parser.GetCurrentState()

	if !state.IsComplete {
		t.Error("Should be complete now")
	}
}

func TestDiffPartialObjects(t *testing.T) {
	type Data struct {
		A string `json:"a"`
		B int    `json:"b"`
	}

	old := &PartialObjectState[Data]{
		CompletedPaths: [][]string{{"a"}},
	}

	new := &PartialObjectState[Data]{
		CompletedPaths: [][]string{{"a"}, {"b"}},
	}

	deltas := DiffPartialObjects(old, new)

	if len(deltas) != 1 {
		t.Fatalf("Expected 1 delta, got %d", len(deltas))
	}

	if deltas[0].Path[0] != "b" {
		t.Errorf("Expected delta for 'b', got %v", deltas[0].Path)
	}

	if !deltas[0].IsNew {
		t.Error("Expected IsNew to be true")
	}
}

func TestStreamObjectBuilder_Schema(t *testing.T) {
	type TestStruct struct {
		Name    string   `description:"The name" json:"name"`
		Count   int      `json:"count"`
		Tags    []string `json:"tags,omitempty"`
		Enabled bool     `json:"enabled"`
	}

	builder := NewStreamObjectBuilder[TestStruct](
		context.Background(),
		nil, nil, nil,
	)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Expected properties map")
	}

	// Check name field
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("Expected name property")
	}

	if nameProp["type"] != "string" {
		t.Errorf("Expected name type 'string', got %v", nameProp["type"])
	}

	if nameProp["description"] != "The name" {
		t.Errorf("Expected description 'The name', got %v", nameProp["description"])
	}

	// Check count field
	countProp, ok := props["count"].(map[string]any)
	if !ok {
		t.Fatal("Expected count property")
	}

	if countProp["type"] != "integer" {
		t.Errorf("Expected count type 'integer', got %v", countProp["type"])
	}

	// Check tags field
	tagsProp, ok := props["tags"].(map[string]any)
	if !ok {
		t.Fatal("Expected tags property")
	}

	if tagsProp["type"] != "array" {
		t.Errorf("Expected tags type 'array', got %v", tagsProp["type"])
	}

	// Check enabled field
	enabledProp, ok := props["enabled"].(map[string]any)
	if !ok {
		t.Fatal("Expected enabled property")
	}

	if enabledProp["type"] != "boolean" {
		t.Errorf("Expected enabled type 'boolean', got %v", enabledProp["type"])
	}

	// Check required fields (tags has omitempty so should not be required)
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("Expected required array")
	}

	requiredMap := make(map[string]bool)
	for _, r := range required {
		requiredMap[r] = true
	}

	if !requiredMap["name"] {
		t.Error("Expected 'name' to be required")
	}

	if !requiredMap["count"] {
		t.Error("Expected 'count' to be required")
	}

	if requiredMap["tags"] {
		t.Error("Expected 'tags' to NOT be required (has omitempty)")
	}
}

func TestStreamObjectBuilder_NestedSchema(t *testing.T) {
	type Address struct {
		City    string `json:"city"`
		Country string `json:"country"`
	}

	type Person struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	builder := NewStreamObjectBuilder[Person](
		context.Background(),
		nil, nil, nil,
	)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	props := schema["properties"].(map[string]any)
	addressProp := props["address"].(map[string]any)

	if addressProp["type"] != "object" {
		t.Errorf("Expected address type 'object', got %v", addressProp["type"])
	}

	addressProps := addressProp["properties"].(map[string]any)
	if _, ok := addressProps["city"]; !ok {
		t.Error("Expected 'city' in address properties")
	}

	if _, ok := addressProps["country"]; !ok {
		t.Error("Expected 'country' in address properties")
	}
}

func TestReflectPartialValue(t *testing.T) {
	type Data struct {
		Name  string
		Value int
	}

	source := Data{Name: "test", Value: 0} // Value is zero
	target := Data{Name: "", Value: 100}

	err := ReflectPartialValue(source, &target)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Name should be copied (non-zero)
	if target.Name != "test" {
		t.Errorf("Expected Name 'test', got %s", target.Name)
	}

	// Value should NOT be copied (zero value in source)
	if target.Value != 100 {
		t.Errorf("Expected Value 100, got %d", target.Value)
	}
}

func TestStreamObjectBuilder_FluentAPI(t *testing.T) {
	type Result struct {
		Data string `json:"data"`
	}

	builder := NewStreamObjectBuilder[Result](
		context.Background(),
		nil, nil, nil,
	).
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("Test prompt {{.var}}").
		WithVar("var", "value").
		WithSystemPrompt("System prompt").
		WithTemperature(0.7).
		WithMaxTokens(100).
		WithTopP(0.9).
		WithTopK(40).
		WithStop("END").
		WithTimeout(30 * 1000000000). // 30s
		WithSchemaStrict(true)

	if builder.provider != "openai" {
		t.Error("Provider not set")
	}

	if builder.model != "gpt-4" {
		t.Error("Model not set")
	}

	if builder.prompt != "Test prompt {{.var}}" {
		t.Error("Prompt not set")
	}

	if builder.vars["var"] != "value" {
		t.Error("Var not set")
	}

	if builder.systemPrompt != "System prompt" {
		t.Error("System prompt not set")
	}

	if *builder.temperature != 0.7 {
		t.Error("Temperature not set")
	}

	if *builder.maxTokens != 100 {
		t.Error("MaxTokens not set")
	}
}
