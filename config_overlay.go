package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/xraph/confy"
	"github.com/xraph/confy/formats"
	"github.com/xraph/confy/sources"
)

// PriorityConfigOverlay loads deployment bindings after local files and before environment overrides.
const PriorityConfigOverlay = 250

func configOverlaySources(logger Logger) ([]confy.ConfigSource, func(), error) {
	var result []confy.ConfigSource

	cleanup := func() {}

	for path := range strings.SplitSeq(os.Getenv("FORGE_CONFIG_OVERLAY"), ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		if len(result) >= 49 {
			return nil, cleanup, errors.New("FORGE_CONFIG_OVERLAY accepts at most 49 sources")
		}
		// #nosec G703 -- Application operators explicitly set overlay paths, which may be outside the working directory.
		if _, err := os.Stat(path); err != nil {
			return nil, cleanup, fmt.Errorf("FORGE_CONFIG_OVERLAY names %s: %w", path, err)
		}

		source, err := sources.NewFileSource(path, sources.FileSourceOptions{
			Name: fmt.Sprintf("config.overlay.%d", len(result)), Priority: PriorityConfigOverlay + len(result),
			ExpandEnvVars: true, RequireFile: true, Logger: logger,
		})
		if err != nil {
			return nil, cleanup, fmt.Errorf("FORGE_CONFIG_OVERLAY names %s: %w", path, err)
		}

		result = append(result, source)
	}

	if data := os.Getenv("FORGE_CONFIG_OVERLAY_YAML"); strings.TrimSpace(data) != "" {
		result = append(result, &inlineOverlaySource{data: data, priority: PriorityConfigOverlay + len(result)})
	}

	return result, cleanup, nil
}

// inlineOverlaySource retains the YAML for reloads without writing credentials to a temporary file.
type inlineOverlaySource struct {
	data     string
	priority int
}

func (s *inlineOverlaySource) Name() string                       { return "config.overlay.inline" }
func (s *inlineOverlaySource) GetName() string                    { return s.Name() }
func (s *inlineOverlaySource) GetType() string                    { return "yaml" }
func (s *inlineOverlaySource) Priority() int                      { return s.priority }
func (s *inlineOverlaySource) IsWatchable() bool                  { return false }
func (s *inlineOverlaySource) SupportsSecrets() bool              { return false }
func (s *inlineOverlaySource) IsAvailable(_ context.Context) bool { return true }
func (s *inlineOverlaySource) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := formats.NewYAMLProcessor().Parse([]byte(s.data))
	if err != nil {
		return nil, err
	}

	return expandOverlayValues(data).(map[string]any), nil
}

func expandOverlayValues(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = expandOverlayValues(item)
		}
	case []any:
		for i, item := range v {
			v[i] = expandOverlayValues(item)
		}
	case string:
		return os.Expand(v, func(key string) string {
			if name, fallback, ok := strings.Cut(key, ":-"); ok {
				if current := os.Getenv(name); current != "" {
					return current
				}

				return fallback
			}

			if name, fallback, ok := strings.Cut(key, "-"); ok {
				if current, exists := os.LookupEnv(name); exists {
					return current
				}

				return fallback
			}

			return os.Getenv(key)
		})
	}

	return value
}
func (s *inlineOverlaySource) Reload(ctx context.Context) error {
	_, err := s.Load(ctx)

	return err
}
func (s *inlineOverlaySource) Watch(_ context.Context, _ func(map[string]any)) error {
	return errors.New("inline configuration does not support watching")
}
func (s *inlineOverlaySource) StopWatch() error { return nil }
func (s *inlineOverlaySource) GetSecret(_ context.Context, _ string) (string, error) {
	return "", errors.New("inline configuration has no secret resolver")
}
