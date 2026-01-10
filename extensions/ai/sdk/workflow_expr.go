package sdk

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ExpressionEvaluator evaluates simple expressions for workflow condition and transform nodes.
// It supports basic comparison operators, logical operators, and field access from context data.
//
// Supported operators:
//   - Comparison: ==, !=, <, >, <=, >=
//   - Logical: and, or, not, &&, ||, !
//   - Arithmetic (for transforms): +, -, *, /
//
// Field access:
//   - Simple: field_name
//   - Nested: object.field
//   - Map access: data["key"]
//
// Examples:
//   - "status == 'completed'"
//   - "count > 10 and active == true"
//   - "result.score >= 0.8"
//   - "value * 2"
//   - "items.length > 0"
type ExpressionEvaluator struct {
	// Reserved for future options like custom functions
}

// NewExpressionEvaluator creates a new expression evaluator.
func NewExpressionEvaluator() *ExpressionEvaluator {
	return &ExpressionEvaluator{}
}

// EvaluateCondition evaluates a condition expression and returns a boolean result.
func (e *ExpressionEvaluator) EvaluateCondition(expr string, data map[string]any) (bool, error) {
	if expr == "" {
		return false, errors.New("empty expression")
	}

	// Normalize the expression
	expr = strings.TrimSpace(expr)

	// Handle literal booleans
	lowerExpr := strings.ToLower(expr)
	if lowerExpr == "true" {
		return true, nil
	}

	if lowerExpr == "false" {
		return false, nil
	}

	// Handle logical operators (lowest precedence)
	if result, ok, err := e.evaluateLogicalExpression(expr, data); ok {
		return result, err
	}

	// Handle comparison operators
	if result, ok, err := e.evaluateComparisonExpression(expr, data); ok {
		return result, err
	}

	// Handle field access that returns boolean
	value, err := e.resolveValue(expr, data)
	if err != nil {
		return false, err
	}

	// Convert to boolean
	return e.toBool(value), nil
}

// EvaluateTransform evaluates a transform expression and returns the result.
func (e *ExpressionEvaluator) EvaluateTransform(expr string, data map[string]any) (any, error) {
	if expr == "" {
		return nil, errors.New("empty expression")
	}

	// Normalize the expression
	expr = strings.TrimSpace(expr)

	// Handle arithmetic expressions
	if result, ok, err := e.evaluateArithmeticExpression(expr, data); ok {
		return result, err
	}

	// Handle field access/extraction
	return e.resolveValue(expr, data)
}

// evaluateLogicalExpression handles and/or/not operators.
func (e *ExpressionEvaluator) evaluateLogicalExpression(expr string, data map[string]any) (bool, bool, error) {
	// Handle 'not' prefix
	if strings.HasPrefix(strings.ToLower(expr), "not ") {
		subExpr := strings.TrimPrefix(expr, "not ")
		subExpr = strings.TrimPrefix(subExpr, "NOT ")
		result, err := e.EvaluateCondition(subExpr, data)

		return !result, true, err
	}

	// Handle '!' prefix
	if after, ok := strings.CutPrefix(expr, "!"); ok {
		subExpr := after
		result, err := e.EvaluateCondition(subExpr, data)

		return !result, true, err
	}

	// Find 'and' or '&&' (handle both)
	andPatterns := []string{" and ", " AND ", " && ", "&&"}
	for _, pattern := range andPatterns {
		if idx := strings.Index(expr, pattern); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+len(pattern):])

			leftResult, err := e.EvaluateCondition(left, data)
			if err != nil {
				return false, true, err
			}
			// Short-circuit evaluation
			if !leftResult {
				return false, true, nil
			}

			rightResult, err := e.EvaluateCondition(right, data)

			return leftResult && rightResult, true, err
		}
	}

	// Find 'or' or '||'
	orPatterns := []string{" or ", " OR ", " || ", "||"}
	for _, pattern := range orPatterns {
		if idx := strings.Index(expr, pattern); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+len(pattern):])

			leftResult, err := e.EvaluateCondition(left, data)
			if err != nil {
				return false, true, err
			}
			// Short-circuit evaluation
			if leftResult {
				return true, true, nil
			}

			rightResult, err := e.EvaluateCondition(right, data)

			return leftResult || rightResult, true, err
		}
	}

	return false, false, nil
}

// evaluateComparisonExpression handles comparison operators.
func (e *ExpressionEvaluator) evaluateComparisonExpression(expr string, data map[string]any) (bool, bool, error) {
	// Order matters: check longer operators first
	operators := []string{"==", "!=", ">=", "<=", ">", "<"}

	for _, op := range operators {
		if idx := strings.Index(expr, op); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+len(op):])

			leftValue, err := e.resolveValue(left, data)
			if err != nil {
				return false, true, err
			}

			rightValue, err := e.resolveValue(right, data)
			if err != nil {
				return false, true, err
			}

			result, err := e.compare(leftValue, rightValue, op)

			return result, true, err
		}
	}

	return false, false, nil
}

// evaluateArithmeticExpression handles arithmetic operators.
func (e *ExpressionEvaluator) evaluateArithmeticExpression(expr string, data map[string]any) (any, bool, error) {
	// Handle addition/subtraction (lowest precedence in arithmetic)
	for _, op := range []string{"+", "-"} {
		// Find the operator (but not at the start for negative numbers)
		idx := -1

		for i := 1; i < len(expr); i++ {
			if string(expr[i]) == op && expr[i-1] != 'e' && expr[i-1] != 'E' { // Not scientific notation
				idx = i

				break
			}
		}

		if idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+1:])

			leftValue, err := e.resolveValue(left, data)
			if err != nil {
				return nil, true, err
			}

			rightValue, err := e.resolveValue(right, data)
			if err != nil {
				return nil, true, err
			}

			result, err := e.arithmetic(leftValue, rightValue, op)

			return result, true, err
		}
	}

	// Handle multiplication/division (higher precedence)
	for _, op := range []string{"*", "/"} {
		if idx := strings.Index(expr, op); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+1:])

			leftValue, err := e.resolveValue(left, data)
			if err != nil {
				return nil, true, err
			}

			rightValue, err := e.resolveValue(right, data)
			if err != nil {
				return nil, true, err
			}

			result, err := e.arithmetic(leftValue, rightValue, op)

			return result, true, err
		}
	}

	return nil, false, nil
}

// resolveValue resolves a value from the expression - could be a literal or a field path.
func (e *ExpressionEvaluator) resolveValue(expr string, data map[string]any) (any, error) {
	expr = strings.TrimSpace(expr)

	// Check for string literals
	if (strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")) ||
		(strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"")) {
		return expr[1 : len(expr)-1], nil
	}

	// Check for numeric literals
	if num, err := strconv.ParseFloat(expr, 64); err == nil {
		return num, nil
	}

	// Check for integer literals
	if num, err := strconv.ParseInt(expr, 10, 64); err == nil {
		return num, nil
	}

	// Check for boolean literals
	lowerExpr := strings.ToLower(expr)
	if lowerExpr == "true" {
		return true, nil
	}

	if lowerExpr == "false" {
		return false, nil
	}

	// Check for null/nil
	if lowerExpr == "null" || lowerExpr == "nil" {
		return nil, nil
	}

	// Resolve field path
	return e.resolveFieldPath(expr, data)
}

// resolveFieldPath resolves a dotted field path like "object.field" or "data.nested.value".
func (e *ExpressionEvaluator) resolveFieldPath(path string, data map[string]any) (any, error) {
	// Handle map access syntax: data["key"]
	if bracketMatch := regexp.MustCompile(`^(\w+)\["([^"]+)"\]$`).FindStringSubmatch(path); len(bracketMatch) == 3 {
		mapName := bracketMatch[1]
		key := bracketMatch[2]

		mapValue, ok := data[mapName]
		if !ok {
			return nil, fmt.Errorf("field '%s' not found", mapName)
		}

		if m, ok := mapValue.(map[string]any); ok {
			return m[key], nil
		}

		return nil, fmt.Errorf("'%s' is not a map", mapName)
	}

	// Handle dotted path
	parts := strings.Split(path, ".")

	var current any = data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				return nil, fmt.Errorf("field '%s' not found in path '%s'", part, path)
			}

			current = val
		case map[string]string:
			val, ok := v[part]
			if !ok {
				return nil, fmt.Errorf("field '%s' not found in path '%s'", part, path)
			}

			current = val
		default:
			return nil, fmt.Errorf("cannot access field '%s' on non-map value", part)
		}
	}

	return current, nil
}

// compare compares two values using the given operator.
func (e *ExpressionEvaluator) compare(left, right any, op string) (bool, error) {
	// Handle nil comparisons
	if left == nil || right == nil {
		switch op {
		case "==":
			return left == right, nil
		case "!=":
			return left != right, nil
		default:
			return false, fmt.Errorf("cannot compare nil with '%s'", op)
		}
	}

	// Try numeric comparison
	leftNum, leftIsNum := e.toFloat64(left)
	rightNum, rightIsNum := e.toFloat64(right)

	if leftIsNum && rightIsNum {
		switch op {
		case "==":
			return leftNum == rightNum, nil
		case "!=":
			return leftNum != rightNum, nil
		case ">":
			return leftNum > rightNum, nil
		case "<":
			return leftNum < rightNum, nil
		case ">=":
			return leftNum >= rightNum, nil
		case "<=":
			return leftNum <= rightNum, nil
		}
	}

	// Try string comparison
	leftStr := fmt.Sprintf("%v", left)
	rightStr := fmt.Sprintf("%v", right)

	switch op {
	case "==":
		return leftStr == rightStr, nil
	case "!=":
		return leftStr != rightStr, nil
	case ">":
		return leftStr > rightStr, nil
	case "<":
		return leftStr < rightStr, nil
	case ">=":
		return leftStr >= rightStr, nil
	case "<=":
		return leftStr <= rightStr, nil
	}

	return false, fmt.Errorf("unknown operator: %s", op)
}

// arithmetic performs arithmetic operations.
func (e *ExpressionEvaluator) arithmetic(left, right any, op string) (any, error) {
	leftNum, leftOk := e.toFloat64(left)
	rightNum, rightOk := e.toFloat64(right)

	if !leftOk || !rightOk {
		// Handle string concatenation for '+'
		if op == "+" {
			return fmt.Sprintf("%v%v", left, right), nil
		}

		return nil, errors.New("cannot perform arithmetic on non-numeric values")
	}

	switch op {
	case "+":
		return leftNum + rightNum, nil
	case "-":
		return leftNum - rightNum, nil
	case "*":
		return leftNum * rightNum, nil
	case "/":
		if rightNum == 0 {
			return nil, errors.New("division by zero")
		}

		return leftNum / rightNum, nil
	}

	return nil, fmt.Errorf("unknown arithmetic operator: %s", op)
}

// toFloat64 converts a value to float64 if possible.
func (e *ExpressionEvaluator) toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f, true
		}
	}

	return 0, false
}

// toBool converts a value to boolean.
func (e *ExpressionEvaluator) toBool(v any) bool {
	if v == nil {
		return false
	}

	switch b := v.(type) {
	case bool:
		return b
	case int, int64, int32, uint, uint64, uint32:
		return b != 0
	case float64, float32:
		return b != 0
	case string:
		lower := strings.ToLower(b)

		return lower == "true" || lower == "yes" || lower == "1"
	default:
		return true // Non-nil values are truthy
	}
}

// ExpressionError represents an error during expression evaluation.
type ExpressionError struct {
	Expression string
	Message    string
	Cause      error
}

func (e *ExpressionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("expression error in '%s': %s: %v", e.Expression, e.Message, e.Cause)
	}

	return fmt.Sprintf("expression error in '%s': %s", e.Expression, e.Message)
}

func (e *ExpressionError) Unwrap() error {
	return e.Cause
}
