package ai

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk"
)

// GetAIService resolves the AI service from the container.
func GetAIService(container forge.Container) (*service, error) {
	instance, err := container.Resolve(ServiceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AI service: %w", err)
	}

	svc, ok := instance.(*service)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not *service, got %T", instance)
	}

	return svc, nil
}

// GetAIManager resolves the AI manager from the container.
func GetAIManager(container forge.Container) (internal.AI, error) {
	instance, err := container.Resolve(ManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AI manager: %w", err)
	}

	manager, ok := instance.(internal.AI)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not internal.AI, got %T", instance)
	}

	return manager, nil
}

// GetAgentFactory resolves the agent factory from the container.
func GetAgentFactory(container forge.Container) (*AgentFactory, error) {
	instance, err := container.Resolve(AgentFactoryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent factory: %w", err)
	}

	factory, ok := instance.(*AgentFactory)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not *AgentFactory, got %T", instance)
	}

	return factory, nil
}

// GetLLMManager resolves the LLM manager from the container.
func GetLLMManager(container forge.Container) (*llm.LLMManager, error) {
	instance, err := container.Resolve(LLMManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve LLM manager: %w", err)
	}

	manager, ok := instance.(*llm.LLMManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not *llm.LLMManager, got %T", instance)
	}

	return manager, nil
}

// GetSDKLLMManager resolves the SDK LLM manager interface from the container.
func GetSDKLLMManager(container forge.Container) (sdk.LLMManager, error) {
	instance, err := container.Resolve(SDKLLMManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve SDK LLM manager: %w", err)
	}

	manager, ok := instance.(sdk.LLMManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not sdk.LLMManager, got %T", instance)
	}

	return manager, nil
}
