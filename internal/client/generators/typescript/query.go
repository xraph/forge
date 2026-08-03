package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// ReactQueryGenerator emits TanStack Query hooks over the generated REST
// client.
//
// A layer, not a second client. The hooks call the methods rest.go already
// produced and add caching, request deduplication and invalidation on top;
// nothing here re-derives the API surface from the specification. Deriving it
// twice is how a hook and the method it is supposed to wrap end up disagreeing
// about a parameter, which no compiler catches because both sides were
// generated from a spec that never changed.
type ReactQueryGenerator struct {
	rest *RESTGenerator
}

func NewReactQueryGenerator() *ReactQueryGenerator {
	return &ReactQueryGenerator{rest: NewRESTGenerator()}
}

// queryEndpoint is one endpoint with the access path its method sits at.
type queryEndpoint struct {
	// Path is the access path on the client, e.g. ["networkmodel", "list"].
	Path []string

	Endpoint *client.Endpoint
}

// accessor renders the call path: `client.networkmodel.list`.
func (q queryEndpoint) accessor() string {
	return "client." + strings.Join(q.Path, ".")
}

// hookName renders `useNetworkmodelList`.
func (q queryEndpoint) hookName() string {
	var b strings.Builder

	b.WriteString("use")

	for _, part := range q.Path {
		b.WriteString(toPascal(part))
	}

	return b.String()
}

// isQuery reports whether the endpoint reads rather than writes.
//
// GET and HEAD are cacheable and become useQuery; everything else becomes
// useMutation. A mutation is not keyed and not cached, which is the whole
// distinction — caching a POST would serve a stale answer to a request whose
// entire purpose was to change something.
func (q queryEndpoint) isQuery() bool {
	method := strings.ToUpper(q.Endpoint.Method)

	return method == "GET" || method == "HEAD"
}

// Generate produces query.ts.
func (g *ReactQueryGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) (string, []string) {
	endpoints := g.collect(spec)
	if len(endpoints) == 0 {
		return "", nil
	}

	var warnings []string

	var buf strings.Builder

	buf.WriteString(`/**
 * TanStack Query hooks over the generated client.
 *
 * Generated. Every hook calls a method on the REST client rather than issuing
 * its own request, so there is one description of the API and one place a
 * change to it lands.
 *
 * The client is passed in rather than read from a module-level singleton or a
 * context this file invents. A generated file should not decide how an
 * application provides its dependencies, and an explicit argument keeps these
 * usable from a test without a provider tree.
 */

`)

	buf.WriteString("import {\n  useMutation,\n  useQuery,\n  type UseMutationOptions,\n  type UseQueryOptions,\n} from '@tanstack/react-query';\n")
	buf.WriteString("import type { RESTClient } from './rest';\n")
	buf.WriteString("import * as types from './types';\n\n")

	g.writeQueryKeys(&buf, endpoints, spec)
	g.writeHooks(&buf, endpoints, spec, config, &warnings)

	return buf.String(), warnings
}

// writeQueryKeys emits the key builders.
func (g *ReactQueryGenerator) writeQueryKeys(
	buf *strings.Builder,
	endpoints []queryEndpoint,
	spec *client.APISpec,
) {
	buf.WriteString(`/**
 * Cache keys.
 *
 * Every parameter the method accepts is part of its key. That is not
 * thoroughness for its own sake: a key that omits a parameter serves one
 * request's cached answer to a different request. Where an API is versioned
 * along more than one axis — a valid time and a knowledge time, say — a key
 * carrying only the first will happily return what is true now to a caller
 * that asked what was known then, and the answer looks entirely plausible.
 *
 * Keys are prefixed by their access path, so invalidating a whole group is
 * ` + "`queryClient.invalidateQueries({ queryKey: ['networkmodel'] })`" + `.
 */
export const queryKeys = {
`)

	for _, ep := range endpoints {
		if !ep.isQuery() {
			continue
		}

		params := g.rest.methodParams(*ep.Endpoint, spec)
		names := make([]string, 0, len(params))

		for _, p := range params {
			names = append(names, p.Name)
		}

		var sig strings.Builder

		for i, p := range params {
			if i > 0 {
				sig.WriteString(", ")
			}

			sig.WriteString(p.Name)

			if p.Optional {
				sig.WriteString("?: ")
			} else {
				sig.WriteString(": ")
			}

			sig.WriteString(p.TSType)
		}

		literals := make([]string, 0, len(ep.Path))
		for _, part := range ep.Path {
			literals = append(literals, "'"+part+"'")
		}

		payload := ""
		if len(names) > 0 {
			payload = ", { " + strings.Join(names, ", ") + " }"
		}

		fmt.Fprintf(buf, "  %s: (%s) =>\n    [%s%s] as const,\n",
			g.keyName(ep), sig.String(), strings.Join(literals, ", "), payload)
	}

	buf.WriteString("} as const;\n\n")
}

// keyName renders the key builder's property name: `networkmodelList`.
func (g *ReactQueryGenerator) keyName(ep queryEndpoint) string {
	if len(ep.Path) == 0 {
		return "root"
	}

	name := ep.Path[0]
	for _, part := range ep.Path[1:] {
		name += toPascal(part)
	}

	return toCamel(name)
}

// writeHooks emits one hook per endpoint.
func (g *ReactQueryGenerator) writeHooks(
	buf *strings.Builder,
	endpoints []queryEndpoint,
	spec *client.APISpec,
	config client.GeneratorConfig,
	warnings *[]string,
) {
	for _, ep := range endpoints {
		params := g.rest.methodParams(*ep.Endpoint, spec)
		returnType, _ := g.rest.generateReturnType(*ep.Endpoint, spec)

		if returnType == "" {
			returnType = "void"
			*warnings = append(*warnings, fmt.Sprintf(
				"endpoint %q: no return type could be derived; its hook resolves to void",
				endpointLabel(ep.Endpoint)))
		}

		returnType = qualifyTypes(returnType)

		args := make([]string, 0, len(params))
		for _, p := range params {
			args = append(args, p.Name)
		}

		if ep.Endpoint.Description != "" {
			fmt.Fprintf(buf, "/** %s */\n", strings.ReplaceAll(ep.Endpoint.Description, "\n", " "))
		}

		if ep.isQuery() {
			g.writeQueryHook(buf, ep, params, args, returnType)

			continue
		}

		g.writeMutationHook(buf, ep, params, args, returnType, config)
	}
}

func (g *ReactQueryGenerator) writeQueryHook(
	buf *strings.Builder,
	ep queryEndpoint,
	params []MethodParam,
	args []string,
	returnType string,
) {
	fmt.Fprintf(buf, "export function %s(\n  client: RESTClient,\n", ep.hookName())

	for _, p := range params {
		if p.Optional {
			fmt.Fprintf(buf, "  %s?: %s,\n", p.Name, p.TSType)

			continue
		}

		fmt.Fprintf(buf, "  %s: %s,\n", p.Name, p.TSType)
	}

	fmt.Fprintf(buf,
		"  options?: Omit<UseQueryOptions<%s, Error>, 'queryKey' | 'queryFn'>,\n) {\n",
		returnType)

	fmt.Fprintf(buf, "  return useQuery({\n    queryKey: queryKeys.%s(%s),\n",
		g.keyName(ep), strings.Join(args, ", "))

	// The signal is forwarded so an unmounted component's request is actually
	// cancelled rather than merely ignored.
	callArgs := append(append([]string{}, args...), "{ signal }")
	fmt.Fprintf(buf, "    queryFn: ({ signal }) => %s(%s),\n",
		ep.accessor(), strings.Join(callArgs, ", "))

	buf.WriteString("    ...options,\n  });\n}\n\n")
}

func (g *ReactQueryGenerator) writeMutationHook(
	buf *strings.Builder,
	ep queryEndpoint,
	params []MethodParam,
	args []string,
	returnType string,
	_ client.GeneratorConfig,
) {
	varsType := "void"
	if len(params) > 0 {
		fields := make([]string, 0, len(params))

		for _, p := range params {
			marker := ": "
			if p.Optional {
				marker = "?: "
			}

			fields = append(fields, p.Name+marker+p.TSType)
		}

		varsType = "{ " + strings.Join(fields, "; ") + " }"
	}

	fmt.Fprintf(buf, "export function %s(\n  client: RESTClient,\n", ep.hookName())
	fmt.Fprintf(buf,
		"  options?: Omit<UseMutationOptions<%s, Error, %s>, 'mutationFn'>,\n) {\n",
		returnType, varsType)

	buf.WriteString("  return useMutation({\n")

	if len(params) == 0 {
		fmt.Fprintf(buf, "    mutationFn: () => %s(),\n", ep.accessor())
	} else {
		destructure := strings.Join(args, ", ")
		fmt.Fprintf(buf, "    mutationFn: ({ %s }: %s) => %s(%s),\n",
			destructure, varsType, ep.accessor(), destructure)
	}

	buf.WriteString("    ...options,\n  });\n}\n\n")
}

// collect walks the endpoint tree the REST generator builds, so hook access
// paths match the methods that actually exist.
func (g *ReactQueryGenerator) collect(spec *client.APISpec) []queryEndpoint {
	root := g.rest.buildEndpointTree(spec.Endpoints)

	var out []queryEndpoint

	var walk func(node *EndpointNode, path []string)

	walk = func(node *EndpointNode, path []string) {
		names := make([]string, 0, len(node.Children))
		for name := range node.Children {
			names = append(names, name)
		}

		// Sorted: map order is random, and a generator whose output changes
		// between runs cannot be reviewed in a diff.
		sort.Strings(names)

		for _, name := range names {
			child := node.Children[name]
			childPath := append(append([]string{}, path...), name)

			if child.IsLeaf && child.Endpoint != nil {
				out = append(out, queryEndpoint{Path: childPath, Endpoint: child.Endpoint})

				continue
			}

			walk(child, childPath)
		}
	}

	walk(root, nil)

	return out
}

// qualifyTypes prefixes bare schema names with the types namespace.
//
// generateReturnType renders names as the REST file refers to them, and that
// file imports the namespace under the same alias — so a union like
// "types.A | void" arrives already qualified while a bare "void" or "string"
// must be left alone.
func qualifyTypes(t string) string {
	parts := strings.Split(t, " | ")
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		switch trimmed {
		case "void", "string", "Blob", "unknown", "any", "number", "boolean":
			parts[i] = trimmed
		default:
			if strings.HasPrefix(trimmed, "types.") {
				parts[i] = trimmed

				continue
			}

			parts[i] = "types." + trimmed
		}
	}

	return strings.Join(parts, " | ")
}
