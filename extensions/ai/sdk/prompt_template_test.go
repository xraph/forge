package sdk

import (
	"strings"
	"testing"
	"time"
)

// Test NewPromptTemplateManager

func TestNewPromptTemplateManager(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	if ptm == nil {
		t.Fatal("expected prompt template manager to be created")
	}

	if len(ptm.templates) != 0 {
		t.Error("expected empty templates initially")
	}
}

// Test RegisterTemplate

func TestPromptTemplateManager_RegisterTemplate(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
	}

	err := ptm.RegisterTemplate(tmpl)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ptm.templates) != 1 {
		t.Errorf("expected 1 template, got %d", len(ptm.templates))
	}
}

func TestPromptTemplateManager_RegisterTemplate_NoName(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Template: "Test",
	}

	err := ptm.RegisterTemplate(tmpl)

	if err == nil {
		t.Error("expected error for template without name")
	}
}

func TestPromptTemplateManager_RegisterTemplate_NoTemplate(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Name: "test",
	}

	err := ptm.RegisterTemplate(tmpl)

	if err == nil {
		t.Error("expected error for template without content")
	}
}

func TestPromptTemplateManager_RegisterTemplate_InvalidSyntax(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Name:     "test",
		Template: "Hello {{.Name",  // Missing closing brace
	}

	err := ptm.RegisterTemplate(tmpl)

	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

func TestPromptTemplateManager_RegisterTemplate_ExtractsVariables(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Name:     "test",
		Template: "Hello, {{.Name}}! You are {{.Age}} years old.",
	}

	ptm.RegisterTemplate(tmpl)

	if len(tmpl.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(tmpl.Variables))
	}

	expectedVars := map[string]bool{"Name": true, "Age": true}
	for _, v := range tmpl.Variables {
		if !expectedVars[v] {
			t.Errorf("unexpected variable: %s", v)
		}
	}
}

// Test GetTemplate

func TestPromptTemplateManager_GetTemplate(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	original := &PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	}

	ptm.RegisterTemplate(original)

	retrieved, err := ptm.GetTemplate("greeting", "1.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved.Name != "greeting" {
		t.Errorf("expected name 'greeting', got '%s'", retrieved.Name)
	}
}

func TestPromptTemplateManager_GetTemplate_LatestVersion(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "test",
		Version:  "1.0.0",
		Template: "Version 1",
	})

	time.Sleep(10 * time.Millisecond)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "test",
		Version:  "2.0.0",
		Template: "Version 2",
	})

	retrieved, err := ptm.GetTemplate("test", "")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved.Version != "2.0.0" {
		t.Errorf("expected latest version 2.0.0, got %s", retrieved.Version)
	}
}

func TestPromptTemplateManager_GetTemplate_NotFound(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	_, err := ptm.GetTemplate("nonexistent", "1.0.0")

	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

// Test Render

func TestPromptTemplateManager_Render(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
	})

	result, err := ptm.Render("greeting", "1.0.0", map[string]interface{}{
		"Name": "Alice",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result != "Hello, Alice!" {
		t.Errorf("expected 'Hello, Alice!', got '%s'", result)
	}
}

func TestPromptTemplateManager_Render_MissingVariable(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
	})

	// Template execution will succeed but output "<no value>"
	result, err := ptm.Render("greeting", "1.0.0", map[string]interface{}{})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Go templates handle missing values gracefully
	if !strings.Contains(result, "Hello") {
		t.Errorf("unexpected result: %s", result)
	}
}

// Test UpdateTemplate

func TestPromptTemplateManager_UpdateTemplate(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	})

	newVersion, err := ptm.UpdateTemplate("greeting", "Hi there!")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if newVersion != "1.0.1" {
		t.Errorf("expected version 1.0.1, got %s", newVersion)
	}

	// Old version should be deactivated
	oldTmpl, _ := ptm.GetTemplate("greeting", "1.0.0")
	if oldTmpl.IsActive {
		t.Error("expected old version to be deactivated")
	}

	// New version should be active
	newTmpl, _ := ptm.GetTemplate("greeting", "1.0.1")
	if !newTmpl.IsActive {
		t.Error("expected new version to be active")
	}
}

func TestPromptTemplateManager_UpdateTemplate_NotFound(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	_, err := ptm.UpdateTemplate("nonexistent", "New template")

	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

// Test DeleteTemplate

func TestPromptTemplateManager_DeleteTemplate(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	})

	err := ptm.DeleteTemplate("greeting", "1.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err = ptm.GetTemplate("greeting", "1.0.0")
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestPromptTemplateManager_DeleteTemplate_AllVersions(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	})

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "2.0.0",
		Template: "Hi!",
	})

	err := ptm.DeleteTemplate("greeting", "")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ptm.templates) != 0 {
		t.Error("expected all templates to be deleted")
	}
}

// Test ListTemplates

func TestPromptTemplateManager_ListTemplates(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	for i := 0; i < 3; i++ {
		ptm.RegisterTemplate(&PromptTemplate{
			Name:     "template",
			Version:  string(rune('1' + i)) + ".0.0",
			Template: "Test",
		})
	}

	templates := ptm.ListTemplates()

	if len(templates) != 3 {
		t.Errorf("expected 3 templates, got %d", len(templates))
	}
}

// Test ListVersions

func TestPromptTemplateManager_ListVersions(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "V1",
	})

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "2.0.0",
		Template: "V2",
	})

	versions, err := ptm.ListVersions("greeting")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

// Test A/B Testing

func TestPromptTemplateManager_CreateABTest(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "greeting_test",
		Variants: []ABVariant{
			{Name: "control", TemplateVersion: "1.0.0", TrafficWeight: 0.5},
			{Name: "variant", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
		},
	}

	err := ptm.CreateABTest(test)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !test.IsActive {
		t.Error("expected test to be active")
	}
}

func TestPromptTemplateManager_CreateABTest_InvalidWeights(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "test",
		Variants: []ABVariant{
			{Name: "a", TemplateVersion: "1.0.0", TrafficWeight: 0.3},
			{Name: "b", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
		},
	}

	err := ptm.CreateABTest(test)

	if err == nil {
		t.Error("expected error for weights not summing to 1.0")
	}
}

func TestPromptTemplateManager_CreateABTest_TooFewVariants(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "test",
		Variants: []ABVariant{
			{Name: "a", TemplateVersion: "1.0.0", TrafficWeight: 1.0},
		},
	}

	err := ptm.CreateABTest(test)

	if err == nil {
		t.Error("expected error for too few variants")
	}
}

func TestPromptTemplateManager_RecordABTestResult(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "test",
		Variants: []ABVariant{
			{Name: "control", TemplateVersion: "1.0.0", TrafficWeight: 0.5},
			{Name: "variant", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
		},
	}

	ptm.CreateABTest(test)

	err := ptm.RecordABTestResult("test", "control", true, 100*time.Millisecond, 0.05)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	results, _ := ptm.GetABTestResults("test")
	controlResults := results["control"]

	if controlResults.Successes != 1 {
		t.Errorf("expected 1 success, got %d", controlResults.Successes)
	}
}

func TestPromptTemplateManager_GetABTestResults(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "test",
		Variants: []ABVariant{
			{Name: "a", TemplateVersion: "1.0.0", TrafficWeight: 0.5},
			{Name: "b", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
		},
	}

	ptm.CreateABTest(test)

	results, err := ptm.GetABTestResults("test")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 result sets, got %d", len(results))
	}
}

func TestPromptTemplateManager_StopABTest(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	test := &ABTest{
		Name: "test",
		Variants: []ABVariant{
			{Name: "a", TemplateVersion: "1.0.0", TrafficWeight: 0.5},
			{Name: "b", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
		},
	}

	ptm.CreateABTest(test)

	err := ptm.StopABTest("test")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if test.IsActive {
		t.Error("expected test to be inactive")
	}
}

// Test CompareTemplates

func TestPromptTemplateManager_CompareTemplates_Same(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	tmpl := &PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	}

	ptm.RegisterTemplate(tmpl)
	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "2.0.0",
		Template: "Hello!",
	})

	diff, err := ptm.CompareTemplates("greeting", "1.0.0", "2.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !diff.IsSame {
		t.Error("expected templates to be the same")
	}
}

func TestPromptTemplateManager_CompareTemplates_Different(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello!",
	})

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "2.0.0",
		Template: "Hi there!",
	})

	diff, err := ptm.CompareTemplates("greeting", "1.0.0", "2.0.0")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if diff.IsSame {
		t.Error("expected templates to be different")
	}
}

// Test Export/Import

func TestPromptTemplateManager_ExportImport(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	original := &PromptTemplate{
		Name:        "greeting",
		Version:     "1.0.0",
		Description: "A greeting template",
		Template:    "Hello, {{.Name}}!",
		Tags:        []string{"greeting", "test"},
	}

	ptm.RegisterTemplate(original)

	exported, err := ptm.ExportTemplate("greeting", "1.0.0")

	if err != nil {
		t.Fatalf("expected no error exporting, got %v", err)
	}

	// Create new manager and import
	ptm2 := NewPromptTemplateManager(nil, nil)
	err = ptm2.ImportTemplate(exported)

	if err != nil {
		t.Fatalf("expected no error importing, got %v", err)
	}

	imported, _ := ptm2.GetTemplate("greeting", "1.0.0")

	if imported.Name != original.Name {
		t.Errorf("expected name %s, got %s", original.Name, imported.Name)
	}

	if imported.Template != original.Template {
		t.Errorf("expected template %s, got %s", original.Template, imported.Template)
	}
}

// Test ValidateVariables

func TestPromptTemplateManager_ValidateVariables_Success(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
	})

	err := ptm.ValidateVariables("greeting", "1.0.0", map[string]interface{}{
		"Name": "Alice",
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPromptTemplateManager_ValidateVariables_Missing(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	ptm.RegisterTemplate(&PromptTemplate{
		Name:     "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}} {{.Age}}!",
	})

	err := ptm.ValidateVariables("greeting", "1.0.0", map[string]interface{}{
		"Name": "Alice",
	})

	if err == nil {
		t.Error("expected error for missing variable")
	}
}

// Test Thread Safety

func TestPromptTemplateManager_ThreadSafety(t *testing.T) {
	ptm := NewPromptTemplateManager(nil, nil)

	done := make(chan bool)

	// Concurrent registrations
	for i := 0; i < 5; i++ {
		go func(index int) {
			ptm.RegisterTemplate(&PromptTemplate{
				Name:     string(rune('a' + index)),
				Version:  "1.0.0",
				Template: "Test",
			})
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			ptm.ListTemplates()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 5 templates
	templates := ptm.ListTemplates()
	if len(templates) < 5 {
		t.Errorf("expected at least 5 templates, got %d", len(templates))
	}
}

