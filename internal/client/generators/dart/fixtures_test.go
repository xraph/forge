package dart

import (
	"github.com/xraph/forge/internal/client"
)

// gateFixture is one specification and configuration the gate, determinism
// and parity tests all run.
type gateFixture struct {
	Name   string
	Spec   *client.APISpec
	Config client.GeneratorConfig
}

func ref(name string) *client.Schema { return &client.Schema{Ref: "#/components/schemas/" + name} }

func jsonContent(s *client.Schema) map[string]*client.MediaType {
	return map[string]*client.MediaType{"application/json": {Schema: s}}
}

// ordersSpec exercises every shape the Dart generator distinguishes: an
// int64 id, snake_case fields, an inline enum, a nullable optional field, a
// list of models, a timestamp, a map, a discriminated and an undiscriminated
// union, a schema named after a forge_client export, a component enum, a
// paginated envelope, a PATCH body, an alias, and every body and response
// content type the REST client handles.
func ordersSpec() *client.APISpec {
	order := &client.EntityRef{Type: "Order", IDField: "id"}
	bearer := []client.SecurityRequirement{{SchemeName: "bearerAuth", Scopes: []string{"orders.read"}}}

	return &client.APISpec{
		Info: client.APIInfo{Title: "Orders API", Version: "1.0.0", Description: "Orders for the probe."},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type:     "object",
				Required: []string{"id", "order_number", "status", "lines"},
				Properties: map[string]*client.Schema{
					"id":           {Type: "integer", Format: "int64", Description: "Server-assigned identifier."},
					"order_number": {Type: "string"},
					"status":       {Type: "string", Enum: []any{"pending", "shipped"}},
					"note":         {Type: "string", Nullable: true},
					"lines":        {Type: "array", Items: ref("LineItem")},
					"created_at":   {Type: "string", Format: "date-time"},
					"metadata":     {Type: "object", AdditionalProperties: &client.Schema{Type: "string"}},
					"customer":     ref("Customer"),
					"shipping":     {Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
					"attachment":   {Type: "string", Format: "byte"},
				},
			},
			"LineItem": {
				Type: "object", Required: []string{"sku", "qty"},
				Properties: map[string]*client.Schema{"sku": {Type: "string"}, "qty": {Type: "integer"}},
			},
			"Customer": {
				Type: "object", Required: []string{"id"},
				Properties: map[string]*client.Schema{"id": {Type: "string"}, "name": {Type: "string"}},
			},
			"Pet": {
				OneOf: []*client.Schema{ref("Cat"), ref("Dog")},
				Discriminator: &client.Discriminator{
					PropertyName: "pet_type",
					Mapping:      map[string]string{"cat": "#/components/schemas/Cat", "dog": "#/components/schemas/Dog"},
				},
			},
			"Cat": {
				Type: "object", Required: []string{"pet_type"},
				Properties: map[string]*client.Schema{"pet_type": {Type: "string"}, "meows": {Type: "boolean"}},
			},
			"Dog": {
				Type: "object", Required: []string{"pet_type"},
				Properties: map[string]*client.Schema{"pet_type": {Type: "string"}, "barks": {Type: "boolean"}},
			},
			"Shape": {OneOf: []*client.Schema{ref("Circle"), ref("Square"), {Type: "string"}}},
			"Circle": {
				Type: "object", Required: []string{"radius"},
				Properties: map[string]*client.Schema{"radius": {Type: "number"}},
			},
			"Square": {
				Type: "object", Required: []string{"side"},
				Properties: map[string]*client.Schema{"side": {Type: "number"}},
			},
			"Value":      {Type: "object", Properties: map[string]*client.Schema{"amount": {Type: "number"}}},
			"OrderState": {Type: "string", Enum: []any{"open", "closed", "unknown"}},
			"OrderPage": {
				Type: "object", Required: []string{"items"},
				Properties: map[string]*client.Schema{
					"items":       {Type: "array", Items: ref("Order")},
					"next_cursor": {Type: "string"},
					"has_more":    {Type: "boolean"},
				},
			},
			"UpdateOrderRequest": {
				Type: "object", Required: []string{"note"},
				Properties: map[string]*client.Schema{
					"note":  {Type: "string", Nullable: true},
					"state": ref("OrderState"),
					"total": {Type: "number"},
					// Named like the parameter of a generated ==.
					"other": {Type: "string"},
				},
			},
			"Orders": {Type: "array", Items: ref("Order")},
			"Labels": {Type: "object", AdditionalProperties: &client.Schema{Type: "integer", Format: "int64"}},
			"Node": {
				Type: "object", Required: []string{"label"},
				Properties: map[string]*client.Schema{"label": {Type: "string"}, "children": {Type: "array", Items: ref("Node")}},
			},
		},
		Entities: map[string]*client.EntityRef{
			"Order":    order,
			"Customer": {Type: "Customer", IDField: "id"},
		},
		RoutingTypes: map[string]*client.EntityRef{
			"OrderPage": {Type: "OrderPage", Fields: map[string]string{"items": "Order"}},
		},
		Security: []client.SecurityScheme{{Key: "bearerAuth", Type: "http", Scheme: "bearer"}},
		Endpoints: []client.Endpoint{
			{
				Method: "GET", Path: "/orders/{id}", OperationID: "orders.get", Summary: "Fetch one order.",
				PathParams:  []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
				QueryParams: []client.Parameter{{Name: "include_lines", In: "query", Schema: &client.Schema{Type: "boolean"}}},
				Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Order"))}},
				Security:    bearer, Entity: order, RootType: "Order", StaleTime: 30000,
				CacheTags: client.TagSet{Provides: []string{"Order:{id}"}},
			},
			{
				Method: "GET", Path: "/orders", OperationID: "orders.list",
				QueryParams: []client.Parameter{
					{Name: "cursor", In: "query", Schema: &client.Schema{Type: "string"}},
					{Name: "limit", In: "query", Schema: &client.Schema{Type: "integer"}},
					{Name: "X-Tenant", In: "header", Required: true, Schema: &client.Schema{Type: "string"}},
				},
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("OrderPage"))}},
				Entity:    order, RootType: "OrderPage",
				CacheTags: client.TagSet{Provides: []string{"Order:{id}", "Order[]"}},
			},
			{
				Method: "PATCH", Path: "/orders/{id}", OperationID: "orders.update",
				PathParams:  []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
				RequestBody: &client.RequestBody{Required: true, Content: jsonContent(ref("UpdateOrderRequest"))},
				Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Order"))}},
				Entity:      order, RootType: "Order",
				CacheTags: client.TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
			},
			{
				Method: "DELETE", Path: "/orders/{id}", OperationID: "orders.delete",
				PathParams: []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
				Responses:  map[int]*client.Response{204: {Description: "gone"}},
				Entity:     order, Idempotent: true,
				CacheTags: client.TagSet{Invalidates: []string{"Order[]"}},
			},
			{
				Method: "POST", Path: "/orders/bulk", OperationID: "orders.bulk",
				RequestBody: &client.RequestBody{Required: true, Content: jsonContent(&client.Schema{Type: "array", Items: ref("Order")})},
				Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Orders"))}},
			},
			{
				Method: "GET", Path: "/pets/{petId}", OperationID: "pets.get",
				PathParams: []client.Parameter{{Name: "petId", In: "path", Required: true, Schema: &client.Schema{Type: "integer", Format: "int64"}}},
				// Named like the parameter of a generated ==.
				QueryParams: []client.Parameter{{Name: "other", In: "query", Schema: &client.Schema{Type: "string"}}},
				Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Pet"))}},
			},
			{
				Method: "POST", Path: "/uploads", OperationID: "uploads.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{"multipart/form-data": {Schema: &client.Schema{Type: "object"}}}},
				Responses:   map[int]*client.Response{201: {Content: jsonContent(ref("Customer"))}},
			},
			{
				Method: "POST", Path: "/raw", OperationID: "raw.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}},
				Responses:   map[int]*client.Response{204: {Description: "stored"}},
			},
			{
				Method: "GET", Path: "/text", OperationID: "texts.get",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{"text/plain": {Schema: &client.Schema{Type: "string"}}}}},
			},
			{
				Method: "GET", Path: "/health",
				Responses: map[int]*client.Response{200: {Content: jsonContent(&client.Schema{Type: "object", Properties: map[string]*client.Schema{"ok": {Type: "boolean"}}})}},
			},
		},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID: "orderFeed", Path: "/ws/orders",
				ReceiveSchema: ref("Order"),
				StreamBindings: []client.StreamBinding{
					{Message: "order.updated", EntityType: "Order", Intent: client.StreamUpsert, Invalidates: []string{"Order[]"}},
				},
			},
			{
				ID: "chat", Path: "/ws/chat/{roomId}",
				SendSchema: ref("LineItem"), ReceiveSchema: ref("LineItem"),
				Metadata: map[string]any{"messages": map[string]string{"say": "send", "said": "receive"}},
			},
		},
		SSEs: []client.SSEEndpoint{
			{ID: "notifications", Path: "/sse/notifications", EventSchemas: map[string]*client.Schema{"created": ref("Customer")}},
		},
		WebTransports: []client.WebTransportEndpoint{
			{ID: "telemetry", Path: "/wt/telemetry", DatagramSchema: ref("LineItem")},
		},
		Sync: []client.SyncDecl{
			{Protocol: "grove-crdt", Entity: "Document", Table: "documents", Dataset: "{id}",
				Pull: "/datasets/{id}/sync/pull", Push: "/datasets/{id}/sync/push",
				Stream: "/datasets/{id}/sync/stream", Socket: "/datasets/{id}/sync/ws"},
			{Protocol: "grove-crdt", Entity: "Draft", Table: "drafts", Pull: "/drafts/sync/pull"},
			{Protocol: "grove-crdt", Entity: "DatasetRow", Dataset: "{id}",
				Pull: "/datasets/{id}/rows/pull", Push: "/datasets/{id}/rows/push"},
		},
	}
}

func baseConfig() client.GeneratorConfig {
	cfg := client.DefaultConfig()
	cfg.Language = "dart"
	cfg.PackageName = "orders_forge_client"
	cfg.Hooks = true

	return cfg
}

func allStreaming(cfg client.GeneratorConfig) client.GeneratorConfig {
	cfg.IncludeStreaming = true
	cfg.Streaming.EnableRooms = true
	cfg.Streaming.EnablePresence = true
	cfg.Streaming.EnableTyping = true
	cfg.Streaming.EnableChannels = true
	cfg.Streaming.EnableHistory = true
	cfg.Streaming.GenerateModularClients = true
	cfg.Streaming.GenerateUnifiedClient = true

	return cfg
}

// reservedSpec names schemas after a Dart keyword, a core type, a generated
// error class and forge_client exports, and gives a model fields named after
// keywords and Object members.
func reservedSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Reserved API", Version: "1"},
		Schemas: map[string]*client.Schema{
			"class":      {Type: "object", Properties: map[string]*client.Schema{"default": {Type: "string"}, "hashCode": {Type: "integer"}}},
			"String":     {Type: "object", Properties: map[string]*client.Schema{"value": {Type: "string"}}},
			"NotFound":   {Type: "object", Properties: map[string]*client.Schema{"reason": {Type: "string"}}},
			"QueryState": {Type: "string", Enum: []any{"index", "name", "values", "1st"}},
			"Assign":     {Type: "object", Properties: map[string]*client.Schema{"to": {Type: "string"}}},
			// Fields named after Dart's lowercase built-in types.
			"Tally": {
				Type: "object", Required: []string{"int"},
				Properties: map[string]*client.Schema{
					"int": {Type: "integer"}, "double": {Type: "number"}, "bool": {Type: "boolean"}, "num": {Type: "number"},
				},
			},
		},
		Endpoints: []client.Endpoint{
			{
				Method: "GET", Path: "/query", OperationID: "query",
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("NotFound"))}},
			},
			// widgets' op constant is opWidgets, which is also the binding
			// opWidgets would take.
			{
				Method: "GET", Path: "/widgets", OperationID: "widgets",
				QueryParams: []client.Parameter{
					{Name: "int", In: "query", Schema: &client.Schema{Type: "integer"}},
					{Name: "bool", In: "query", Schema: &client.Schema{Type: "boolean"}},
				},
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("Tally"))}},
			},
			{
				Method: "GET", Path: "/op-widgets", OperationID: "opWidgets",
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("NotFound"))}},
			},
			// A binding named deepEquals would shadow the helper its own
			// Args == calls.
			{
				Method: "POST", Path: "/deep", OperationID: "deepEquals",
				RequestBody: &client.RequestBody{Required: true, Content: jsonContent(&client.Schema{Type: "array", Items: &client.Schema{Type: "string"}})},
				Responses:   map[int]*client.Response{204: {Description: "ok"}},
			},
			// A binding named decodeList would make its file show a support
			// helper it never calls.
			{
				Method: "GET", Path: "/decode", OperationID: "decodeList",
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("NotFound"))}},
			},
		},
	}
}

// minimalSpec has no components at all: one operation returning an inline
// object.
func minimalSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Minimal API", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/ping", OperationID: "ping",
			Responses: map[int]*client.Response{200: {Content: jsonContent(&client.Schema{Type: "object", Properties: map[string]*client.Schema{"pong": {Type: "boolean"}}})}},
		}},
	}
}

func gateFixtures() []gateFixture {
	noHooks := allStreaming(baseConfig())
	noHooks.Hooks = false

	int64Int := baseConfig()
	int64Int.Int64 = client.Int64Int

	preserve := baseConfig()
	preserve.FieldNaming = client.NamingPreserve

	clientOnly := baseConfig()
	clientOnly.ClientOnly = true

	noAuth := baseConfig()
	noAuth.IncludeAuth = false

	reserved := baseConfig()
	reserved.PackageName = "reserved_client"

	minimal := baseConfig()
	minimal.PackageName = "minimal_client"

	return []gateFixture{
		{Name: "default", Spec: ordersSpec(), Config: allStreaming(baseConfig())},
		{Name: "no-hooks", Spec: ordersSpec(), Config: noHooks},
		{Name: "int64-int", Spec: ordersSpec(), Config: int64Int},
		{Name: "preserve", Spec: ordersSpec(), Config: preserve},
		{Name: "no-auth", Spec: ordersSpec(), Config: noAuth},
		{Name: "client-only", Spec: ordersSpec(), Config: clientOnly},
		{Name: "reserved", Spec: reservedSpec(), Config: reserved},
		{Name: "minimal", Spec: minimalSpec(), Config: minimal},
	}
}

// streamingFixture exercises what the typed streaming clients distinguish:
// path parameters named like the generated locals and fields, a list, an inline
// object and a bare string as messages, a multiplexed direction, an SSE
// endpoint with no named events and one with several, every WebTransport
// shape, and feature clients on paths the document declares.
func streamingFixture() gateFixture {
	spec := ordersSpec()

	spec.WebSockets = append(spec.WebSockets,
		client.WebSocketEndpoint{
			ID: "clash", Path: "/ws/{base}/{url}/{headers}/{heartbeat}/{connection}/{scheme}/{socket}/{options}",
			SendSchema: ref("LineItem"), ReceiveSchema: ref("LineItem"),
		},
		client.WebSocketEndpoint{
			ID: "batch", Path: "/ws/batch",
			SendSchema:    &client.Schema{Type: "array", Items: ref("LineItem")},
			ReceiveSchema: &client.Schema{Type: "array", Items: ref("Customer")},
		},
		client.WebSocketEndpoint{
			ID: "raw", Path: "/ws/raw/$weird",
			SendSchema:    &client.Schema{Type: "string"},
			ReceiveSchema: &client.Schema{Type: "object", Properties: map[string]*client.Schema{"seq": {Type: "integer"}}},
		},
		client.WebSocketEndpoint{
			ID: "mux", Path: "/ws/mux",
			SendSchema:      ref("LineItem"),
			SendMessages:    map[string]*client.Schema{"a": ref("LineItem"), "b": ref("Customer")},
			ReceiveMessages: map[string]*client.Schema{"a": ref("LineItem"), "b": ref("Customer")},
		},
		client.WebSocketEndpoint{Path: "/ws/anonymous"},
	)

	spec.SSEs = append(spec.SSEs,
		client.SSEEndpoint{Path: "/sse/ticks"},
		client.SSEEndpoint{
			ID: "mixed", Path: "/sse/mixed/{topic}",
			EventSchemas: map[string]*client.Schema{"line": ref("LineItem"), "who": ref("Customer")},
		},
	)

	spec.WebTransports = append(spec.WebTransports,
		client.WebTransportEndpoint{
			ID: "duplex", Path: "/wt/duplex",
			BiStreamSchema: &client.StreamSchema{SendSchema: ref("LineItem"), ReceiveSchema: ref("Customer")},
		},
		client.WebTransportEndpoint{
			ID: "feed", Path: "/wt/feed",
			UniStreamSchema: &client.StreamSchema{ReceiveSchema: ref("Customer")},
		},
	)

	spec.Streaming = &client.StreamingSpec{
		Rooms:    &client.RoomOperations{Path: "/realtime/rooms"},
		Presence: &client.PresenceOperations{Path: "/realtime/presence", Statuses: []string{"here", "gone"}},
		Typing:   &client.TypingOperations{Path: "/realtime/typing"},
		Channels: &client.ChannelOperations{Path: "/realtime/channels"},
	}

	cfg := allStreaming(baseConfig())
	cfg.PackageName = "streaming_forge_client"

	return gateFixture{Name: "streaming", Spec: spec, Config: cfg}
}
