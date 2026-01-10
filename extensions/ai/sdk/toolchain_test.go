package sdk

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestToolChainExecution(t *testing.T) {
	// Create a mock tool registry
	registry := NewToolRegistry(nil, nil)

	// Register test tools
	_ = registry.RegisterTool(&ToolDefinition{
		Name:        "multiply",
		Version:     "1.0.0",
		Description: "Multiplies two numbers",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"a": {Type: "integer"},
				"b": {Type: "integer"},
			},
			Required: []string{"a", "b"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			a := int(params["a"].(float64))
			b := int(params["b"].(float64))

			return a * b, nil
		},
	})

	_ = registry.RegisterTool(&ToolDefinition{
		Name:        "add",
		Version:     "1.0.0",
		Description: "Adds two numbers",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"x": {Type: "integer"},
				"y": {Type: "integer"},
			},
			Required: []string{"x", "y"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			x := int(params["x"].(float64))
			y := int(params["y"].(float64))

			return x + y, nil
		},
	})

	t.Run("simple chain execution", func(t *testing.T) {
		chain := NewToolChain(registry).
			Step("multiply", WithInput(map[string]any{"a": float64(3), "b": float64(4)}))

		result, err := chain.Execute(context.Background())
		if err != nil {
			t.Fatalf("chain execution failed: %v", err)
		}

		if result.FinalResult != 12 {
			t.Errorf("expected 12, got %v", result.FinalResult)
		}

		if result.StepsExecuted != 1 {
			t.Errorf("expected 1 step executed, got %d", result.StepsExecuted)
		}
	})

	t.Run("chain with transformer", func(t *testing.T) {
		chain := NewToolChain(registry).
			Step("multiply", WithInput(map[string]any{"a": float64(3), "b": float64(4)})).
			Transform(func(ctx *ChainContext, result any) any {
				return result.(int) * 2
			})

		result, err := chain.Execute(context.Background())
		if err != nil {
			t.Fatalf("chain execution failed: %v", err)
		}

		if result.FinalResult != 24 {
			t.Errorf("expected 24, got %v", result.FinalResult)
		}
	})

	t.Run("conditional step", func(t *testing.T) {
		executed := false

		_ = registry.RegisterTool(&ToolDefinition{
			Name:    "conditional_tool",
			Version: "1.0.0",
			Parameters: ToolParameterSchema{
				Type:       "object",
				Properties: map[string]ToolParameterProperty{},
				Required:   []string{},
			},
			Handler: func(ctx context.Context, params map[string]any) (any, error) {
				executed = true

				return "executed", nil
			},
		})

		chain := NewToolChain(registry).
			Step("multiply", WithInput(map[string]any{"a": float64(3), "b": float64(4)})).
			ConditionalStep("conditional_tool", func(ctx *ChainContext) bool {
				result := ctx.GetLastResult()

				return result.(int) > 10
			}, WithInput(map[string]any{}))

		_, err := chain.Execute(context.Background())
		if err != nil {
			t.Fatalf("chain execution failed: %v", err)
		}

		if !executed {
			t.Error("conditional step should have been executed")
		}
	})

	t.Run("chain context data sharing", func(t *testing.T) {
		chain := NewToolChain(registry).
			Step("multiply", WithInput(map[string]any{"a": float64(2), "b": float64(5)})).
			Transform(func(ctx *ChainContext, result any) any {
				ctx.Set("multiplied", result)

				return result
			}).
			Step("add", WithInputMapper(func(ctx *ChainContext) map[string]any {
				multiplied := ctx.Get("multiplied").(int)

				return map[string]any{"x": float64(multiplied), "y": float64(10)}
			}))

		result, err := chain.Execute(context.Background())
		if err != nil {
			t.Fatalf("chain execution failed: %v", err)
		}

		// 2*5=10, then 10+10=20
		if result.FinalResult != 20 {
			t.Errorf("expected 20, got %v", result.FinalResult)
		}
	})

	t.Run("chain timeout", func(t *testing.T) {
		_ = registry.RegisterTool(&ToolDefinition{
			Name:    "slow_tool",
			Version: "1.0.0",
			Handler: func(ctx context.Context, params map[string]any) (any, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(5 * time.Second):
					return "done", nil
				}
			},
		})

		chain := NewToolChain(registry).
			WithTimeout(100 * time.Millisecond).
			Step("slow_tool")

		_, err := chain.Execute(context.Background())
		if err == nil {
			t.Error("expected timeout error")
		}
	})

	t.Run("continue on error", func(t *testing.T) {
		_ = registry.RegisterTool(&ToolDefinition{
			Name:    "failing_tool",
			Version: "1.0.0",
			Handler: func(ctx context.Context, params map[string]any) (any, error) {
				return nil, errors.New("intentional error")
			},
		})

		chain := NewToolChain(registry).
			ContinueOnError(true).
			Step("failing_tool").
			Step("multiply", WithInput(map[string]any{"a": float64(2), "b": float64(3)}))

		result, err := chain.Execute(context.Background())
		if err != nil {
			t.Fatalf("chain should not have failed with ContinueOnError: %v", err)
		}

		if result.FinalResult != 6 {
			t.Errorf("expected 6, got %v", result.FinalResult)
		}

		if len(result.Errors) != 1 {
			t.Errorf("expected 1 error recorded, got %d", len(result.Errors))
		}
	})
}

func TestChainContext(t *testing.T) {
	ctx := &ChainContext{
		data:    make(map[string]any),
		results: make(map[string]any),
		errors:  make(map[string]error),
	}

	t.Run("set and get", func(t *testing.T) {
		ctx.Set("key", "value")

		if ctx.Get("key") != "value" {
			t.Error("get should return the set value")
		}
	})

	t.Run("get string", func(t *testing.T) {
		ctx.Set("string_key", "hello")

		if ctx.GetString("string_key") != "hello" {
			t.Error("GetString should return the string value")
		}
	})

	t.Run("get int", func(t *testing.T) {
		ctx.Set("int_key", 42)

		if ctx.GetInt("int_key") != 42 {
			t.Error("GetInt should return the int value")
		}
	})

	t.Run("get bool", func(t *testing.T) {
		ctx.Set("bool_key", true)

		if !ctx.GetBool("bool_key") {
			t.Error("GetBool should return true")
		}
	})
}

func TestPipeline(t *testing.T) {
	registry := NewToolRegistry(nil, nil)

	_ = registry.RegisterTool(&ToolDefinition{
		Name:    "step1",
		Version: "1.0.0",
		Parameters: ToolParameterSchema{
			Type:       "object",
			Properties: map[string]ToolParameterProperty{},
			Required:   []string{},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]any{"value": float64(10)}, nil
		},
	})

	_ = registry.RegisterTool(&ToolDefinition{
		Name:    "step2",
		Version: "1.0.0",
		Parameters: ToolParameterSchema{
			Type: "object",
			Properties: map[string]ToolParameterProperty{
				"value": {Type: "number"},
			},
			Required: []string{"value"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			value := params["value"].(float64)

			return map[string]any{"doubled": value * 2}, nil
		},
	})

	chain := Pipeline(registry, "step1", "step2")

	result, err := chain.Execute(context.Background())
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	finalResult := result.FinalResult.(map[string]any)
	if finalResult["doubled"] != float64(20) {
		t.Errorf("expected doubled=20, got %v", finalResult["doubled"])
	}
}
