package ai

import (
	"fmt"

	aisdk "github.com/xraph/ai-sdk"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/training"
)

// Helper functions for convenient AI service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// =============================================================================
// LLM Manager Helpers
// =============================================================================

// GetLLMManager retrieves the LLM Manager from the container.
// Returns error if not found or type assertion fails.
func GetLLMManager(c forge.Container) (aisdk.LLMManager, error) {
	// Try type-based resolution first
	if llm, err := forge.InjectType[aisdk.LLMManager](c); err == nil && llm != nil {
		return llm, nil
	}

	// Fallback to string-based resolution (try SDK key first, then legacy)
	if llm, err := forge.Inject[aisdk.LLMManager](c); err == nil {
		return llm, nil
	}
	return forge.Inject[aisdk.LLMManager](c)
}

// MustGetLLMManager retrieves the LLM Manager from the container.
// Panics if not found or type assertion fails.
func MustGetLLMManager(c forge.Container) aisdk.LLMManager {
	llm, err := GetLLMManager(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get LLM manager: %v", err))
	}
	return llm
}

// =============================================================================
// Agent Manager Helpers
// =============================================================================

// GetAgentManager retrieves the AgentManager from the container.
// Returns error if not found or type assertion fails.
func GetAgentManager(c forge.Container) (*AgentManager, error) {
	// Try type-based resolution first
	if mgr, err := forge.InjectType[*AgentManager](c); err == nil && mgr != nil {
		return mgr, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[*AgentManager](c)
}

// MustGetAgentManager retrieves the AgentManager from the container.
// Panics if not found or type assertion fails.
func MustGetAgentManager(c forge.Container) *AgentManager {
	mgr, err := GetAgentManager(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get agent manager: %v", err))
	}
	return mgr
}

// =============================================================================
// Agent Factory Helpers
// =============================================================================

// GetAgentFactory retrieves the AgentFactory from the container.
// Returns error if not found or type assertion fails.
func GetAgentFactory(c forge.Container) (*AgentFactory, error) {
	// Try type-based resolution first
	if factory, err := forge.InjectType[*AgentFactory](c); err == nil && factory != nil {
		return factory, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[*AgentFactory](c)
}

// MustGetAgentFactory retrieves the AgentFactory from the container.
// Panics if not found or type assertion fails.
func MustGetAgentFactory(c forge.Container) *AgentFactory {
	factory, err := GetAgentFactory(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get agent factory: %v", err))
	}
	return factory
}

// =============================================================================
// State Store Helpers
// =============================================================================

// GetStateStore retrieves the StateStore from the container.
// Returns error if not found or type assertion fails.
func GetStateStore(c forge.Container) (aisdk.StateStore, error) {
	// Try type-based resolution first
	if store, err := forge.InjectType[aisdk.StateStore](c); err == nil && store != nil {
		return store, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[aisdk.StateStore](c)
}

// MustGetStateStore retrieves the StateStore from the container.
// Panics if not found or type assertion fails.
func MustGetStateStore(c forge.Container) aisdk.StateStore {
	store, err := GetStateStore(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get state store: %v", err))
	}
	return store
}

// =============================================================================
// Vector Store Helpers
// =============================================================================

// GetVectorStore retrieves the VectorStore from the container.
// Returns error if not found or type assertion fails.
func GetVectorStore(c forge.Container) (aisdk.VectorStore, error) {
	// Try type-based resolution first
	if store, err := forge.InjectType[aisdk.VectorStore](c); err == nil && store != nil {
		return store, nil
	}

	// Fallback to string-based resolution (try new key first, then legacy)
	if store, err := forge.Inject[aisdk.VectorStore](c); err == nil {
		return store, nil
	}
	return forge.Inject[aisdk.VectorStore](c)
}

// MustGetVectorStore retrieves the VectorStore from the container.
// Panics if not found or type assertion fails.
func MustGetVectorStore(c forge.Container) aisdk.VectorStore {
	store, err := GetVectorStore(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get vector store: %v", err))
	}
	return store
}

// =============================================================================
// Training Service Helpers (if enabled)
// =============================================================================

// GetModelTrainer retrieves the ModelTrainer from the container.
// Returns error if not found or training not enabled.
func GetModelTrainer(c forge.Container) (training.ModelTrainer, error) {
	// Try type-based resolution first
	if trainer, err := forge.InjectType[training.ModelTrainer](c); err == nil && trainer != nil {
		return trainer, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[training.ModelTrainer](c)
}

// MustGetModelTrainer retrieves the ModelTrainer from the container.
// Panics if not found.
func MustGetModelTrainer(c forge.Container) training.ModelTrainer {
	trainer, err := GetModelTrainer(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get model trainer: %v", err))
	}
	return trainer
}

// GetDataManager retrieves the DataManager from the container.
// Returns error if not found or training not enabled.
func GetDataManager(c forge.Container) (training.DataManager, error) {
	// Try type-based resolution first
	if dm, err := forge.InjectType[training.DataManager](c); err == nil && dm != nil {
		return dm, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[training.DataManager](c)
}

// MustGetDataManager retrieves the DataManager from the container.
// Panics if not found.
func MustGetDataManager(c forge.Container) training.DataManager {
	dm, err := GetDataManager(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get data manager: %v", err))
	}
	return dm
}

// GetPipelineManager retrieves the PipelineManager from the container.
// Returns error if not found or training not enabled.
func GetPipelineManager(c forge.Container) (training.PipelineManager, error) {
	// Try type-based resolution first
	if pm, err := forge.InjectType[training.PipelineManager](c); err == nil && pm != nil {
		return pm, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[training.PipelineManager](c)
}

// MustGetPipelineManager retrieves the PipelineManager from the container.
// Panics if not found.
func MustGetPipelineManager(c forge.Container) training.PipelineManager {
	pm, err := GetPipelineManager(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get pipeline manager: %v", err))
	}
	return pm
}
