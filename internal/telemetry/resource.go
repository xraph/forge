package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// BuildResource builds the one Resource every signal shares. Order matters:
// later sources override earlier ones, so the app's name is the fallback,
// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES beat it, and attributes from
// forge config beat everything.
//
// A detector failing returns a partial resource with resource.ErrPartialResource.
// That is not fatal; the caller logs it and carries on with what was detected.
func BuildResource(ctx context.Context, name, version, environment string, extra map[string]string) (*resource.Resource, error) {
	base := []attribute.KeyValue{
		attribute.String("service.name", name),
		attribute.String("deployment.environment.name", environment),
	}
	if version != "" {
		base = append(base, attribute.String("service.version", version))
	}

	overrides := make([]attribute.KeyValue, 0, len(extra))
	for k, v := range extra {
		overrides = append(overrides, attribute.String(k, v))
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(base...),
		resource.WithHost(),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(overrides...),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return resource.NewSchemaless(base...), err
	}

	return res, err
}
