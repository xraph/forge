package ai

import (
	"fmt"

	aisdk "github.com/xraph/ai-sdk"
	"github.com/xraph/forge"
)

// GetAgentManager resolves the agent manager from the container.
func GetAgentManager(container forge.Container) (*AgentManager, error) {
	instance, err := container.Resolve(AgentManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent manager: %w", err)
	}

	manager, ok := instance.(*AgentManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not *AgentManager, got %T", instance)
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
func GetLLMManager(container forge.Container) (aisdk.LLMManager, error) {
	instance, err := container.Resolve(LLMManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve LLM manager: %w", err)
	}

	manager, ok := instance.(aisdk.LLMManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not aisdk.LLMManager, got %T", instance)
	}

	return manager, nil
}

// GetStateStore resolves the state store from the container.
func GetStateStore(container forge.Container) (aisdk.StateStore, error) {
	instance, err := container.Resolve(StateStoreKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve state store: %w", err)
	}

	store, ok := instance.(aisdk.StateStore)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not aisdk.StateStore, got %T", instance)
	}

	return store, nil
}
