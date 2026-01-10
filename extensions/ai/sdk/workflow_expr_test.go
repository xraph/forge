package sdk

import (
	"errors"
	"testing"
)

func TestExpressionEvaluator(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("create evaluator", func(t *testing.T) {
		if evaluator == nil {
			t.Fatal("expected non-nil evaluator")
		}
	})
}

func TestExpressionEvaluatorConditions(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("literal true", func(t *testing.T) {
		result, err := evaluator.EvaluateCondition("true", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("literal false", func(t *testing.T) {
		result, err := evaluator.EvaluateCondition("false", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result {
			t.Error("expected false")
		}
	})

	t.Run("literal TRUE uppercase", func(t *testing.T) {
		result, err := evaluator.EvaluateCondition("TRUE", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("empty expression", func(t *testing.T) {
		_, err := evaluator.EvaluateCondition("", nil)
		if err == nil {
			t.Error("expected error for empty expression")
		}
	})
}

func TestExpressionEvaluatorComparisonOperators(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	testCases := []struct {
		name     string
		expr     string
		data     map[string]any
		expected bool
	}{
		// Equality
		{"number equals", "value == 10", map[string]any{"value": 10}, true},
		{"number not equals", "value == 10", map[string]any{"value": 5}, false},
		{"string equals", "name == 'Alice'", map[string]any{"name": "Alice"}, true},
		{"string equals double quote", "name == \"Alice\"", map[string]any{"name": "Alice"}, true},
		{"string not equals", "name == 'Alice'", map[string]any{"name": "Bob"}, false},

		// Not equals
		{"not equals true", "value != 10", map[string]any{"value": 5}, true},
		{"not equals false", "value != 10", map[string]any{"value": 10}, false},

		// Greater than
		{"greater than true", "value > 5", map[string]any{"value": 10}, true},
		{"greater than false", "value > 5", map[string]any{"value": 3}, false},
		{"greater than equal", "value > 5", map[string]any{"value": 5}, false},

		// Less than
		{"less than true", "value < 10", map[string]any{"value": 5}, true},
		{"less than false", "value < 10", map[string]any{"value": 15}, false},
		{"less than equal", "value < 10", map[string]any{"value": 10}, false},

		// Greater than or equal
		{"gte true greater", "value >= 5", map[string]any{"value": 10}, true},
		{"gte true equal", "value >= 5", map[string]any{"value": 5}, true},
		{"gte false", "value >= 5", map[string]any{"value": 3}, false},

		// Less than or equal
		{"lte true less", "value <= 10", map[string]any{"value": 5}, true},
		{"lte true equal", "value <= 10", map[string]any{"value": 10}, true},
		{"lte false", "value <= 10", map[string]any{"value": 15}, false},

		// Compare with literals
		{"literal comparison", "10 > 5", nil, true},
		{"string literal comparison", "'abc' == 'abc'", nil, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluator.EvaluateCondition(tc.expr, tc.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestExpressionEvaluatorLogicalOperators(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	testCases := []struct {
		name     string
		expr     string
		data     map[string]any
		expected bool
	}{
		// AND operations
		{"and both true", "a == 1 and b == 2", map[string]any{"a": 1, "b": 2}, true},
		{"and left false", "a == 1 and b == 2", map[string]any{"a": 0, "b": 2}, false},
		{"and right false", "a == 1 and b == 2", map[string]any{"a": 1, "b": 0}, false},
		{"and both false", "a == 1 and b == 2", map[string]any{"a": 0, "b": 0}, false},
		{"AND uppercase", "a == 1 AND b == 2", map[string]any{"a": 1, "b": 2}, true},
		{"&& operator", "a == 1 && b == 2", map[string]any{"a": 1, "b": 2}, true},

		// OR operations
		{"or both true", "a == 1 or b == 2", map[string]any{"a": 1, "b": 2}, true},
		{"or left true", "a == 1 or b == 2", map[string]any{"a": 1, "b": 0}, true},
		{"or right true", "a == 1 or b == 2", map[string]any{"a": 0, "b": 2}, true},
		{"or both false", "a == 1 or b == 2", map[string]any{"a": 0, "b": 0}, false},
		{"OR uppercase", "a == 1 OR b == 2", map[string]any{"a": 0, "b": 2}, true},
		{"|| operator", "a == 1 || b == 2", map[string]any{"a": 0, "b": 2}, true},

		// NOT operations
		{"not true", "not false", nil, true},
		{"not false", "not true", nil, false},
		{"not with field", "not active", map[string]any{"active": false}, true},
		{"NOT uppercase", "NOT true", nil, false},
		{"! operator", "!true", nil, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluator.EvaluateCondition(tc.expr, tc.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestExpressionEvaluatorFieldAccess(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("simple field", func(t *testing.T) {
		data := map[string]any{"status": "completed"}

		result, err := evaluator.EvaluateCondition("status == 'completed'", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("nested field", func(t *testing.T) {
		data := map[string]any{
			"result": map[string]any{
				"score": 0.9,
			},
		}

		result, err := evaluator.EvaluateCondition("result.score >= 0.8", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("deeply nested field", func(t *testing.T) {
		data := map[string]any{
			"response": map[string]any{
				"data": map[string]any{
					"value": 42,
				},
			},
		}

		result, err := evaluator.EvaluateCondition("response.data.value == 42", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("map access syntax", func(t *testing.T) {
		data := map[string]any{
			"config": map[string]any{
				"enabled": true,
			},
		}

		result, err := evaluator.EvaluateCondition("config[\"enabled\"] == true", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("field not found", func(t *testing.T) {
		data := map[string]any{"other": "value"}

		_, err := evaluator.EvaluateCondition("missing == 'value'", data)
		if err == nil {
			t.Error("expected error for missing field")
		}
	})

	t.Run("boolean field truthy", func(t *testing.T) {
		data := map[string]any{"active": true}

		result, err := evaluator.EvaluateCondition("active", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("boolean field falsy", func(t *testing.T) {
		data := map[string]any{"active": false}

		result, err := evaluator.EvaluateCondition("active", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result {
			t.Error("expected false")
		}
	})
}

func TestExpressionEvaluatorTransforms(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("multiplication", func(t *testing.T) {
		data := map[string]any{"value": 5.0}

		result, err := evaluator.EvaluateTransform("value * 2", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != 10.0 {
			t.Errorf("expected 10, got %v", result)
		}
	})

	t.Run("division", func(t *testing.T) {
		data := map[string]any{"value": 10.0}

		result, err := evaluator.EvaluateTransform("value / 2", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != 5.0 {
			t.Errorf("expected 5, got %v", result)
		}
	})

	t.Run("addition", func(t *testing.T) {
		data := map[string]any{"value": 5.0}

		result, err := evaluator.EvaluateTransform("value + 3", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != 8.0 {
			t.Errorf("expected 8, got %v", result)
		}
	})

	t.Run("subtraction", func(t *testing.T) {
		data := map[string]any{"value": 10.0}

		result, err := evaluator.EvaluateTransform("value - 3", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != 7.0 {
			t.Errorf("expected 7, got %v", result)
		}
	})

	t.Run("string concatenation", func(t *testing.T) {
		data := map[string]any{"greeting": "Hello"}

		result, err := evaluator.EvaluateTransform("greeting + ' World'", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "Hello World" {
			t.Errorf("expected 'Hello World', got %v", result)
		}
	})

	t.Run("division by zero", func(t *testing.T) {
		data := map[string]any{"value": 10.0}

		_, err := evaluator.EvaluateTransform("value / 0", data)
		if err == nil {
			t.Error("expected error for division by zero")
		}
	})

	t.Run("field extraction", func(t *testing.T) {
		data := map[string]any{
			"result": map[string]any{
				"output": "extracted value",
			},
		}

		result, err := evaluator.EvaluateTransform("result.output", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "extracted value" {
			t.Errorf("expected 'extracted value', got %v", result)
		}
	})

	t.Run("empty expression", func(t *testing.T) {
		_, err := evaluator.EvaluateTransform("", nil)
		if err == nil {
			t.Error("expected error for empty expression")
		}
	})
}

func TestExpressionEvaluatorNullHandling(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("null equals null", func(t *testing.T) {
		result, err := evaluator.EvaluateCondition("null == null", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("nil equals nil", func(t *testing.T) {
		result, err := evaluator.EvaluateCondition("nil == nil", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("field is null", func(t *testing.T) {
		data := map[string]any{"value": nil}

		result, err := evaluator.EvaluateCondition("value == null", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("field is not null", func(t *testing.T) {
		data := map[string]any{"value": "something"}

		result, err := evaluator.EvaluateCondition("value != null", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})
}

func TestExpressionEvaluatorTypeConversions(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("int to float comparison", func(t *testing.T) {
		data := map[string]any{"value": 10}

		result, err := evaluator.EvaluateCondition("value == 10.0", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("int64 comparison", func(t *testing.T) {
		data := map[string]any{"value": int64(100)}

		result, err := evaluator.EvaluateCondition("value > 50", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("string number comparison", func(t *testing.T) {
		data := map[string]any{"value": "42"}

		result, err := evaluator.EvaluateCondition("value == 42", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true (string '42' should equal 42)")
		}
	})
}

func TestExpressionEvaluatorComplexExpressions(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("compound condition", func(t *testing.T) {
		data := map[string]any{
			"status":   "active",
			"priority": 5,
		}

		result, err := evaluator.EvaluateCondition("status == 'active' and priority > 3", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("workflow-like condition", func(t *testing.T) {
		data := map[string]any{
			"node1": map[string]any{
				"result": "success",
			},
			"threshold": 0.8,
		}

		result, err := evaluator.EvaluateCondition("node1.result == 'success'", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})
}

func TestExpressionError(t *testing.T) {
	t.Run("error with cause", func(t *testing.T) {
		cause := &ExpressionError{Expression: "inner", Message: "inner error"}
		err := &ExpressionError{
			Expression: "outer",
			Message:    "outer error",
			Cause:      cause,
		}

		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}

		if !errors.Is(err.Unwrap(), cause) {
			t.Error("expected Unwrap to return cause")
		}
	})

	t.Run("error without cause", func(t *testing.T) {
		err := &ExpressionError{
			Expression: "test",
			Message:    "test error",
		}

		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}

		if err.Unwrap() != nil {
			t.Error("expected Unwrap to return nil")
		}
	})
}

func TestExpressionEvaluatorEdgeCases(t *testing.T) {
	evaluator := NewExpressionEvaluator()

	t.Run("whitespace handling", func(t *testing.T) {
		data := map[string]any{"value": 10}

		result, err := evaluator.EvaluateCondition("  value  ==  10  ", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result {
			t.Error("expected true")
		}
	})

	t.Run("numeric string in data", func(t *testing.T) {
		data := map[string]any{"count": "5"}

		result, err := evaluator.EvaluateTransform("count", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "5" {
			t.Errorf("expected '5', got %v", result)
		}
	})
}
