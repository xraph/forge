package client

import (
	"strings"
	"testing"
)

func dartConfig(pkg string) GeneratorConfig {
	cfg := DefaultConfig()
	cfg.Language = "dart"
	cfg.PackageName = pkg

	return cfg
}

func TestValidateAcceptsDart(t *testing.T) {
	cfg := dartConfig("orders_forge_client")

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if cfg.Language != "dart" {
		t.Errorf("Language = %q, want dart", cfg.Language)
	}
}

func TestValidateRejectsInvalidDartPackageNames(t *testing.T) {
	for _, name := range []string{"Orders", "orders-client", "1orders", "class", "orders.client", ""} {
		cfg := dartConfig(name)

		if err := cfg.Validate(); err == nil {
			t.Errorf("package name %q was accepted", name)
		}
	}

	for _, name := range []string{"orders", "_orders", "orders_forge_client2"} {
		cfg := dartConfig(name)

		if err := cfg.Validate(); err != nil {
			t.Errorf("package name %q was rejected: %v", name, err)
		}
	}
}

func TestValidateChecksInt64Mode(t *testing.T) {
	for _, mode := range []Int64Mode{"", Int64String, Int64Int} {
		cfg := dartConfig("orders")
		cfg.Int64 = mode

		if err := cfg.Validate(); err != nil {
			t.Errorf("Int64 %q rejected: %v", mode, err)
		}
	}

	cfg := dartConfig("orders")
	cfg.Int64 = "bigint"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "bigint") {
		t.Errorf("Validate = %v, want an error naming the bad mode", err)
	}
}

func TestValidateNamesDartInTheUnsupportedLanguageError(t *testing.T) {
	cfg := dartConfig("orders")
	cfg.Language = "kotlin"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "dart") {
		t.Errorf("Validate = %v, want the supported list to include dart", err)
	}
}

func TestGeneratedTablesMarshalCanonicalFillsRequiredLists(t *testing.T) {
	tables := GeneratedTables{Ops: map[string]TableOp{"b": {Method: "GET", Path: "/b"}, "a": {Method: "GET", Path: "/a"}}}

	first, err := tables.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}

	second, err := tables.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Fatal("MarshalCanonical is not deterministic")
	}

	text := string(first)

	for _, want := range []string{`"provides": []`, `"invalidates": []`, `"streams": []`, `"scopes": []`, `"requiredCapabilities": {}`} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %s:\n%s", want, text)
		}
	}

	if strings.Index(text, `"a"`) > strings.Index(text, `"b"`) {
		t.Errorf("ops keys are not sorted:\n%s", text)
	}
}
