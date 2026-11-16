package client

import (
	"fmt"
	"mime"
	"strings"
)

// Values of Endpoint.Stream, and of the x-forge-protocol extension on an
// OpenAPI operation. The same strings the router's RouteKind renders, so a
// route's kind, the document's marker and the IR's classification are one
// vocabulary.
const (
	StreamSSE          = "sse"
	StreamWebSocket    = "websocket"
	StreamWebTransport = "webtransport"
)

func isStreamProtocol(v string) bool {
	return v == StreamSSE || v == StreamWebSocket || v == StreamWebTransport
}

// resolveEndpointStream records whether an OpenAPI operation holds its
// connection open rather than answering once.
//
// A streaming route reaches OpenAPI as an ordinary GET. Left there, every
// language generator binds it as a request: the TypeScript client emits
// `query(...)` over it, and calling that issues a plain fetch at an endpoint
// that never finishes its body, so it hangs or buffers the whole stream as one
// response. Nothing about the binding says it is not a query.
//
// The router marks the operations it knows stream with x-forge-protocol. A
// document written by something else gets the same treatment when its success
// responses say so in the only way OpenAPI can: every 2xx body is
// text/event-stream, or the only success is 101 Switching Protocols.
func resolveEndpointStream(spec *APISpec, ep *Endpoint, ext map[string]any) {
	if raw, present := ext[protocolExtension]; present {
		if v, ok := raw.(string); ok && isStreamProtocol(v) {
			ep.Stream = v

			return
		}

		unusable(spec, endpointOrigin(ep), protocolExtension,
			fmt.Sprintf("one of %q, %q or %q", StreamSSE, StreamWebSocket, StreamWebTransport))
	}

	ep.Stream = streamFromResponses(ep)
}

// streamFromResponses classifies an endpoint by what its success responses
// declare. Conservative on purpose: one 2xx response with a non-stream body
// means the endpoint can answer as an ordinary request, and it stays one.
func streamFromResponses(ep *Endpoint) string {
	var sawSuccess, sawSSE, sawOther bool

	for code, resp := range ep.Responses {
		if code < 200 || code > 299 {
			continue
		}

		sawSuccess = true

		if resp == nil {
			continue
		}

		for contentType := range resp.Content {
			if isEventStream(contentType) {
				sawSSE = true
			} else {
				sawOther = true
			}
		}
	}

	if sawSSE && !sawOther {
		return StreamSSE
	}

	if !sawSuccess {
		if _, upgrades := ep.Responses[101]; upgrades {
			return StreamWebSocket
		}
	}

	return ""
}

func isEventStream(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		media = strings.TrimSpace(strings.ToLower(contentType))
	}

	return media == "text/event-stream"
}

// SeparateStreamEndpoints takes every streaming endpoint out of spec.Endpoints,
// so no generator binds it as a request.
//
// A stream the AsyncAPI side already describes is dropped from the REST list:
// the router writes every SSE, WebSocket and WebTransport route into both
// documents, and the AsyncAPI channel is the one carrying the event schemas
// and stream bindings. A stream only OpenAPI knows about -- a document from a
// gateway, a hand-written handler that declares text/event-stream -- becomes a
// stream endpoint of its kind with what the operation said about it, so the
// client still has a way to reach it and that way is the right one.
//
// Unless its path has parameters. The stream clients concatenate a channel's
// path onto the base URL as written, so a client built for
// `/instances/{id}/logs` would connect to a literal `{id}`. That stream is
// left out with a warning rather than generated broken; declaring it in
// AsyncAPI is the way to a client for it.
//
// Run by Generator.Generate, after any merge and filter, because a stream and
// its channel can arrive from two separate documents and only the merged spec
// has both. Idempotent: a second call finds nothing left to move.
func (s *APISpec) SeparateStreamEndpoints() {
	if s == nil {
		return
	}

	known := make(map[string]bool, len(s.SSEs)+len(s.WebSockets)+len(s.WebTransports))

	for i := range s.SSEs {
		known[StreamSSE+" "+streamPathKey(s.SSEs[i].Path)] = true
	}

	for i := range s.WebSockets {
		known[StreamWebSocket+" "+streamPathKey(s.WebSockets[i].Path)] = true
	}

	for i := range s.WebTransports {
		known[StreamWebTransport+" "+streamPathKey(s.WebTransports[i].Path)] = true
	}

	kept := make([]Endpoint, 0, len(s.Endpoints))

	for i := range s.Endpoints {
		ep := s.Endpoints[i]

		if ep.Stream == "" {
			kept = append(kept, ep)

			continue
		}

		key := ep.Stream + " " + streamPathKey(ep.Path)
		if known[key] {
			continue
		}

		known[key] = true

		if strings.Contains(streamPathKey(ep.Path), "{}") {
			s.Warnings = append(s.Warnings, fmt.Sprintf(
				"client: %s streams (%s), so it is left out of the REST client; its path has parameters, "+
					"which the generated stream clients do not substitute, so no stream client is generated for it either. "+
					"Describe it in AsyncAPI to bind it as a stream.",
				endpointOrigin(&ep), ep.Stream))

			continue
		}

		switch ep.Stream {
		case StreamSSE:
			s.SSEs = append(s.SSEs, SSEEndpoint{
				ID:            streamEndpointID(&ep),
				Path:          ep.Path,
				Summary:       ep.Summary,
				Description:   ep.Description,
				Tags:          ep.Tags,
				EventSchemas:  map[string]*Schema{},
				Security:      ep.Security,
				Authorization: ep.Authorization,
				Metadata:      ep.Metadata,
			})
		case StreamWebSocket:
			s.WebSockets = append(s.WebSockets, WebSocketEndpoint{
				ID:            streamEndpointID(&ep),
				Path:          ep.Path,
				Summary:       ep.Summary,
				Description:   ep.Description,
				Tags:          ep.Tags,
				Parameters:    ep.PathParams,
				Security:      ep.Security,
				Authorization: ep.Authorization,
				Metadata:      ep.Metadata,
			})
		case StreamWebTransport:
			s.WebTransports = append(s.WebTransports, WebTransportEndpoint{
				ID:            streamEndpointID(&ep),
				Path:          ep.Path,
				Summary:       ep.Summary,
				Description:   ep.Description,
				Tags:          ep.Tags,
				Security:      ep.Security,
				Authorization: ep.Authorization,
				Metadata:      ep.Metadata,
			})
		}
	}

	s.Endpoints = kept
}

// streamPathKey reduces a path to its shape, so `/logs/{id}` from OpenAPI and
// `/logs/:id` from a raw route or channel address name the same stream.
func streamPathKey(path string) string {
	segments := strings.Split(strings.TrimSuffix(path, "/"), "/")

	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") ||
			(strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			segments[i] = "{}"
		}
	}

	return strings.Join(segments, "/")
}

// streamEndpointID names a stream built from an operation: its operationId
// when it has one, otherwise its path in the shape the AsyncAPI generator gives
// a channel, so the class a generator derives from it reads like any other.
func streamEndpointID(ep *Endpoint) string {
	if ep.OperationID != "" {
		return ep.OperationID
	}

	if ep.ID != "" {
		return ep.ID
	}

	parts := make([]string, 0, 8)

	for seg := range strings.SplitSeq(ep.Path, "/") {
		seg = strings.Trim(seg, "{}:*")
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	if len(parts) == 0 {
		return "root"
	}

	return strings.Join(parts, "-")
}
