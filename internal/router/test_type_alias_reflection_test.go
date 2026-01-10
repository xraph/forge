package router

import (
	"reflect"
	"testing"
)

// TestTypeAliasReflection investigates what information we can get from type aliases.
func TestTypeAliasReflection(t *testing.T) {
	type Workspace struct {
		ID string `json:"id"`
	}

	type PageMeta struct {
		Page int `json:"page"`
	}

	type PaginatedResponse[T any] struct {
		Data []T       `json:"data"`
		Meta *PageMeta `json:"meta,omitempty"`
	}

	// Type alias (transparent)
	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// Named type (creates new type)
	type ListWorkspacesResultNamed PaginatedResponse[*Workspace]

	type ResponseWithAlias struct {
		Body ListWorkspacesResult `json:"body"`
	}

	type ResponseWithNamed struct {
		Body ListWorkspacesResultNamed `json:"body"`
	}

	type ResponseWithDirect struct {
		Body PaginatedResponse[*Workspace] `json:"body"`
	}

	t.Run("Type Alias (transparent)", func(t *testing.T) {
		rt := reflect.TypeFor[ResponseWithAlias]()
		field, _ := rt.FieldByName("Body")

		t.Logf("Field type: %v", field.Type)
		t.Logf("Field type name: %q", field.Type.Name())
		t.Logf("Field type string: %q", field.Type.String())
		t.Logf("Field type kind: %v", field.Type.Kind())
		t.Logf("Field type pkg path: %q", field.Type.PkgPath())

		if field.Type.Name() != "" {
			t.Logf("✓ Field has type name: %q", field.Type.Name())
		} else {
			t.Log("✗ Field type has no name (anonymous)")
		}
	})

	t.Run("Named Type (new type)", func(t *testing.T) {
		rt := reflect.TypeFor[ResponseWithNamed]()
		field, _ := rt.FieldByName("Body")

		t.Logf("Field type: %v", field.Type)
		t.Logf("Field type name: %q", field.Type.Name())
		t.Logf("Field type string: %q", field.Type.String())
		t.Logf("Field type kind: %v", field.Type.Kind())
		t.Logf("Field type pkg path: %q", field.Type.PkgPath())

		if field.Type.Name() != "" {
			t.Logf("✓ Field has type name: %q", field.Type.Name())
		} else {
			t.Log("✗ Field type has no name (anonymous)")
		}
	})

	t.Run("Direct Generic (no alias)", func(t *testing.T) {
		rt := reflect.TypeFor[ResponseWithDirect]()
		field, _ := rt.FieldByName("Body")

		t.Logf("Field type: %v", field.Type)
		t.Logf("Field type name: %q", field.Type.Name())
		t.Logf("Field type string: %q", field.Type.String())
		t.Logf("Field type kind: %v", field.Type.Kind())
		t.Logf("Field type pkg path: %q", field.Type.PkgPath())

		if field.Type.Name() != "" {
			t.Logf("✓ Field has type name: %q", field.Type.Name())
		} else {
			t.Log("✗ Field type has no name (anonymous)")
		}
	})

	t.Run("Compare types", func(t *testing.T) {
		rtAlias := reflect.TypeFor[ResponseWithAlias]()
		rtDirect := reflect.TypeFor[ResponseWithDirect]()

		fieldAlias, _ := rtAlias.FieldByName("Body")
		fieldDirect, _ := rtDirect.FieldByName("Body")

		if fieldAlias.Type == fieldDirect.Type {
			t.Log("✓ Type alias and direct type are identical (aliases are transparent)")
		} else {
			t.Log("✗ Types differ")
		}

		// Check type name
		aliasName := fieldAlias.Type.Name()
		directName := fieldDirect.Type.Name()

		t.Logf("Alias field type name: %q", aliasName)
		t.Logf("Direct field type name: %q", directName)

		if aliasName == directName {
			t.Log("Type alias name is not preserved (expected behavior in Go)")
		}
	})

	// Test with actual instantiated generic
	t.Run("Instantiated Generic Type", func(t *testing.T) {
		// var instance PaginatedResponse[*Workspace]

		rt := reflect.TypeFor[PaginatedResponse[*Workspace]]()

		t.Logf("Instance type: %v", rt)
		t.Logf("Instance type name: %q", rt.Name())
		t.Logf("Instance type string: %q", rt.String())
		t.Logf("Instance type pkg path: %q", rt.PkgPath())

		// Try to get a better name by parsing the string
		typeStr := rt.String()
		t.Logf("Type string for parsing: %q", typeStr)

		// Can we extract a cleaner name?
		cleanName := cleanGenericTypeName(typeStr)
		t.Logf("Cleaned name: %q", cleanName)
	})
}

func TestCleanGenericNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple generic with pointer",
			input:    "PaginatedResponse[*router.Workspace]",
			expected: "PaginatedResponse[*Workspace]",
		},
		{
			name:     "Generic with package prefix on base type",
			input:    "router.PaginatedResponse[*router.Workspace]",
			expected: "PaginatedResponse[*Workspace]",
		},
		{
			name:     "Fully qualified package path",
			input:    "PaginatedResponse[*github.com/wakflo/kineta/extensions/workspace.Workspace]",
			expected: "PaginatedResponse[*Workspace]",
		},
		{
			name:     "Non-generic type",
			input:    "Response",
			expected: "Response",
		},
		{
			name:     "Generic with primitive",
			input:    "Response[int]",
			expected: "Response[int]",
		},
		{
			name:     "Multiple type parameters",
			input:    "Map[*pkg.Key,*pkg.Value]",
			expected: "Map[*Key,*Value]",
		},
		{
			name:     "With instance marker",
			input:    "PaginatedResponse[*github.com/xraph/forge/internal/router.Workspace·69]",
			expected: "PaginatedResponse[*Workspace]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanGenericTypeName(tt.input)
			if result != tt.expected {
				t.Errorf("cleanGenericTypeName(%q) = %q, want %q", tt.input, result, tt.expected)
			} else {
				t.Logf("✓ %q -> %q", tt.input, result)
			}
		})
	}
}
