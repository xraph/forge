package typescript

import (
	"github.com/xraph/forge/internal/client"
)

// buildTables collects the values ops.ts, entities.ts, stream-bindings.ts,
// security.ts and capabilities.ts render, in the language-neutral form the
// Dart generator also produces. Every row comes from the function the
// TypeScript renderer reads, so the file cannot say something the source
// does not.
func buildTables(spec *client.APISpec, config client.GeneratorConfig) client.GeneratedTables {
	needsCodecs := codecsNeeded(config)
	rows := entityRows(spec, config)

	known := make(map[string]bool, len(rows))
	entities := make(map[string]client.TableEntity, len(rows))

	for _, row := range rows {
		known[row.name] = true
		entities[row.name] = client.TableEntity{IDField: row.idField, Fields: row.fields}
	}

	keys := operationKeys(spec.Endpoints)
	ops := make(map[string]client.TableOp, len(spec.Endpoints))

	for i := range spec.Endpoints {
		ops[keys[i]] = operationRow(&spec.Endpoints[i], spec, config, known, needsCodecs)
	}

	security := make(map[string]client.TableSecurity, len(spec.Security))
	for _, s := range spec.Security {
		security[s.Key] = client.TableSecurity{Type: s.Type, In: s.In, Name: s.ParamName, Scheme: s.Scheme}
	}

	return client.GeneratedTables{
		Ops:             ops,
		Entities:        entities,
		Streams:         streamRows(spec, needsCodecs && streamsWithCodecs(spec)),
		SecuritySchemes: security,
		Capabilities:    capabilityTables(spec, keys),
	}
}

// capabilityTables is the data capabilities.ts renders: the three
// vocabularies, every operation name, and the per-operation requirements,
// with ungated operations absent.
func capabilityTables(spec *client.APISpec, keys []string) client.TableCapabilities {
	auth := client.NewAuthCodeGenerator()

	out := client.TableCapabilities{
		Scopes:                auth.CollectCapabilities(spec),
		Roles:                 auth.CollectRoles(spec),
		Permissions:           auth.CollectPermissions(spec),
		Operations:            keys,
		RequiredCapabilities:  map[string][][]string{},
		RequiredAuthorization: map[string]client.TableAuthorization{},
	}

	for i := range spec.Endpoints {
		if alternatives := auth.EndpointCapabilities(spec.Endpoints[i]); len(alternatives) > 0 {
			out.RequiredCapabilities[keys[i]] = alternatives
		}

		authz := auth.EndpointAuthorization(spec.Endpoints[i])
		if authz == nil {
			continue
		}

		roles := sortedUniqueStrings(authz.Roles)
		permissions := sortedUniqueStrings(authz.Permissions)

		if len(roles) == 0 && len(permissions) == 0 {
			continue
		}

		out.RequiredAuthorization[keys[i]] = client.TableAuthorization{Roles: roles, Permissions: permissions}
	}

	return out
}
