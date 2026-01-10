package router

import "reflect"

// WithPaginatedResponse creates a route option for paginated list responses
// itemType should be a pointer to the item struct type.
func WithPaginatedResponse(itemType any, statusCode int) RouteOption {
	// Create a paginated wrapper struct dynamically
	paginatedSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"data": {
				Type:        "array",
				Items:       generateSchemaForType(itemType),
				Description: "List of items",
			},
			"total": {
				Type:        "integer",
				Description: "Total number of items",
				Example:     100,
			},
			"page": {
				Type:        "integer",
				Description: "Current page number",
				Example:     1,
				Minimum:     1,
			},
			"pageSize": {
				Type:        "integer",
				Description: "Number of items per page",
				Example:     20,
				Minimum:     1,
			},
			"totalPages": {
				Type:        "integer",
				Description: "Total number of pages",
				Example:     5,
				Minimum:     1,
			},
		},
		Required: []string{"data", "total", "page", "pageSize", "totalPages"},
	}

	return WithResponseSchema(statusCode, "Paginated response", paginatedSchema)
}

// WithErrorResponses adds standard HTTP error responses to a route
// Includes 400, 401, 403, 404, 500.
func WithErrorResponses() RouteOption {
	return &errorResponsesOpt{}
}

type errorResponsesOpt struct{}

func (o *errorResponsesOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	responses, ok := config.Metadata["openapi.responseSchemas"].(map[int]*ResponseSchemaDef)
	if !ok {
		responses = make(map[int]*ResponseSchemaDef)
		config.Metadata["openapi.responseSchemas"] = responses
	}

	errorSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"error": {
				Type:        "string",
				Description: "Error message",
			},
			"code": {
				Type:        "integer",
				Description: "HTTP status code",
			},
			"details": {
				Type:        "string",
				Description: "Additional error details",
			},
		},
		Required: []string{"error", "code"},
	}

	// Only add if not already present
	if _, exists := responses[400]; !exists {
		responses[400] = &ResponseSchemaDef{
			Description: "Bad Request",
			Schema:      errorSchema,
		}
	}

	if _, exists := responses[401]; !exists {
		responses[401] = &ResponseSchemaDef{
			Description: "Unauthorized",
			Schema:      errorSchema,
		}
	}

	if _, exists := responses[403]; !exists {
		responses[403] = &ResponseSchemaDef{
			Description: "Forbidden",
			Schema:      errorSchema,
		}
	}

	if _, exists := responses[404]; !exists {
		responses[404] = &ResponseSchemaDef{
			Description: "Not Found",
			Schema:      errorSchema,
		}
	}

	if _, exists := responses[500]; !exists {
		responses[500] = &ResponseSchemaDef{
			Description: "Internal Server Error",
			Schema:      errorSchema,
		}
	}
}

// WithStandardRESTResponses adds standard REST CRUD responses for a resource
// Includes 200 (GET/list), 201 (POST), 204 (DELETE), 400, 404, 500.
func WithStandardRESTResponses(resourceType any) RouteOption {
	return &standardRESTResponsesOpt{resourceType: resourceType}
}

type standardRESTResponsesOpt struct {
	resourceType any
}

func (o *standardRESTResponsesOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	responses, ok := config.Metadata["openapi.responseSchemas"].(map[int]*ResponseSchemaDef)
	if !ok {
		responses = make(map[int]*ResponseSchemaDef)
		config.Metadata["openapi.responseSchemas"] = responses
	}

	// 200 OK - Resource retrieved
	if _, exists := responses[200]; !exists {
		responses[200] = &ResponseSchemaDef{
			Description: "Success",
			Schema:      o.resourceType,
		}
	}

	// 201 Created - Resource created
	if _, exists := responses[201]; !exists {
		responses[201] = &ResponseSchemaDef{
			Description: "Created",
			Schema:      o.resourceType,
		}
	}

	// 204 No Content - Resource deleted
	if _, exists := responses[204]; !exists {
		responses[204] = &ResponseSchemaDef{
			Description: "No Content",
			Schema:      nil,
		}
	}

	// Add standard error responses
	errorOpt := &errorResponsesOpt{}
	errorOpt.Apply(config)
}

// WithFileUploadResponse creates a response for file upload success.
func WithFileUploadResponse(statusCode int) RouteOption {
	uploadResponseSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"fileId": {
				Type:        "string",
				Description: "Unique identifier for the uploaded file",
			},
			"filename": {
				Type:        "string",
				Description: "Original filename",
			},
			"size": {
				Type:        "integer",
				Description: "File size in bytes",
			},
			"contentType": {
				Type:        "string",
				Description: "MIME type of the file",
			},
			"url": {
				Type:        "string",
				Format:      "uri",
				Description: "URL to access the uploaded file",
			},
		},
		Required: []string{"fileId", "filename", "size"},
	}

	return WithResponseSchema(statusCode, "File upload successful", uploadResponseSchema)
}

// WithNoContentResponse creates a 204 No Content response.
func WithNoContentResponse() RouteOption {
	return WithResponseSchema(204, "No Content", nil)
}

// WithCreatedResponse creates a 201 Created response with Location header.
func WithCreatedResponse(resourceType any) RouteOption {
	return WithResponseSchema(201, "Created", resourceType)
}

// WithAcceptedResponse creates a 202 Accepted response for async operations.
func WithAcceptedResponse() RouteOption {
	asyncSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"jobId": {
				Type:        "string",
				Description: "Unique identifier for the async job",
			},
			"status": {
				Type:        "string",
				Description: "Current status of the job",
				Enum:        []any{"pending", "processing", "completed", "failed"},
			},
			"statusUrl": {
				Type:        "string",
				Format:      "uri",
				Description: "URL to check the job status",
			},
		},
		Required: []string{"jobId", "status"},
	}

	return WithResponseSchema(202, "Accepted", asyncSchema)
}

// generateSchemaForType generates a schema from a type.
func generateSchemaForType(itemType any) *Schema {
	gen := newSchemaGenerator(nil, nil) // Inline schema generation

	schema, _ := gen.GenerateSchema(itemType)

	return schema
}

// WithListResponse creates a simple list response (array of items).
func WithListResponse(itemType any, statusCode int) RouteOption {
	listSchema := &Schema{
		Type:  "array",
		Items: generateSchemaForType(itemType),
	}

	return WithResponseSchema(statusCode, "List of items", listSchema)
}

// WithBatchResponse creates a response for batch operations.
func WithBatchResponse(itemType any, statusCode int) RouteOption {
	batchSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"successful": {
				Type:        "array",
				Items:       generateSchemaForType(itemType),
				Description: "Successfully processed items",
			},
			"failed": {
				Type: "array",
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"item": generateSchemaForType(itemType),
						"error": {
							Type:        "string",
							Description: "Error message",
						},
					},
				},
				Description: "Failed items with errors",
			},
			"totalProcessed": {
				Type:        "integer",
				Description: "Total number of items processed",
			},
			"successCount": {
				Type:        "integer",
				Description: "Number of successful items",
			},
			"failureCount": {
				Type:        "integer",
				Description: "Number of failed items",
			},
		},
		Required: []string{"successful", "failed", "totalProcessed", "successCount", "failureCount"},
	}

	return WithResponseSchema(statusCode, "Batch operation result", batchSchema)
}

// WithValidationErrorResponse adds a 422 Unprocessable Entity response for validation errors.
func WithValidationErrorResponse() RouteOption {
	validationSchema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"error": {
				Type:        "string",
				Description: "Error message",
			},
			"validationErrors": {
				Type: "array",
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"field": {
							Type:        "string",
							Description: "Field name that failed validation",
						},
						"message": {
							Type:        "string",
							Description: "Validation error message",
						},
						"value": {
							Description: "The value that failed validation",
						},
					},
					Required: []string{"field", "message"},
				},
				Description: "List of validation errors",
			},
		},
		Required: []string{"error", "validationErrors"},
	}

	return WithResponseSchema(422, "Validation Error", validationSchema)
}

// GetSchemaFromType is a helper to get schema from a type.
func GetSchemaFromType(t reflect.Type) *Schema {
	gen := newSchemaGenerator(nil, nil) // Inline schema generation

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	instance := reflect.New(t).Interface()

	schema, _ := gen.GenerateSchema(instance)

	return schema
}
