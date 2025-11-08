package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/xraph/forge/internal/client/generators"
)

// Generator orchestrates client code generation.
type Generator struct {
	mu         sync.RWMutex
	generators map[string]generators.LanguageGenerator
}

// NewGenerator creates a new generator with registered language generators.
func NewGenerator() *Generator {
	return &Generator{
		generators: make(map[string]generators.LanguageGenerator),
	}
}

// Register registers a language generator.
func (g *Generator) Register(gen generators.LanguageGenerator) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	name := gen.Name()
	if _, exists := g.generators[name]; exists {
		return fmt.Errorf("generator already registered: %s", name)
	}

	g.generators[name] = gen

	return nil
}

// Generate generates a client for the specified language.
func (g *Generator) Generate(ctx context.Context, spec *APISpec, config GeneratorConfig) (*generators.GeneratedClient, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Get generator for the language
	gen, err := g.getGenerator(config.Language)
	if err != nil {
		return nil, err
	}

	// Validate spec for this language
	if err := gen.Validate(generators.APISpec(spec)); err != nil {
		return nil, fmt.Errorf("validate spec: %w", err)
	}

	// Generate client
	client, err := gen.Generate(ctx, generators.APISpec(spec), generators.GeneratorConfig(config))
	if err != nil {
		return nil, fmt.Errorf("generate client: %w", err)
	}

	return client, nil
}

// GenerateFromRouter generates a client by introspecting a router.
func (g *Generator) GenerateFromRouter(ctx context.Context, r any, config GeneratorConfig) (*generators.GeneratedClient, error) {
	// Import router package would cause import cycle, so we use interface assertion
	// The router must implement the Router interface from router package

	// Type assert to the router interface (commented out to avoid import cycle)
	// For now, this method should not be used directly
	// Use GenerateFromFile instead
	return nil, errors.New("GenerateFromRouter is not yet implemented - use GenerateFromFile instead")
}

// GenerateFromFile generates a client from a spec file.
func (g *Generator) GenerateFromFile(ctx context.Context, filePath string, config GeneratorConfig) (*generators.GeneratedClient, error) {
	// Parse spec file
	parser := NewSpecParser()

	spec, err := parser.ParseFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("parse spec file: %w", err)
	}

	// Generate client
	return g.Generate(ctx, spec, config)
}

// getGenerator retrieves a registered generator by language.
func (g *Generator) getGenerator(language string) (generators.LanguageGenerator, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	gen, exists := g.generators[language]
	if !exists {
		return nil, fmt.Errorf("no generator registered for language: %s", language)
	}

	return gen, nil
}

// ListGenerators returns all registered generators.
func (g *Generator) ListGenerators() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	names := make([]string, 0, len(g.generators))
	for name := range g.generators {
		names = append(names, name)
	}

	return names
}

// GetGeneratorInfo returns information about a specific generator.
func (g *Generator) GetGeneratorInfo(language string) (GeneratorInfo, error) {
	gen, err := g.getGenerator(language)
	if err != nil {
		return GeneratorInfo{}, err
	}

	return GeneratorInfo{
		Name:              gen.Name(),
		SupportedFeatures: gen.SupportedFeatures(),
	}, nil
}

// GeneratorInfo provides information about a language generator.
type GeneratorInfo struct {
	Name              string
	SupportedFeatures []string
}

// DefaultGenerator returns a generator with all built-in generators registered.
func DefaultGenerator() *Generator {
	gen := NewGenerator()

	// Register built-in generators
	// Note: Actual generators should be registered when needed to avoid import cycles
	// They are registered in the CLI plugin instead

	return gen
}
