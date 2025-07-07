package router

// Schema helper methods for easier documentation creation
// These extend the base Schema type with fluent API methods

// WithFormat sets the format for a schema
func (s *Schema) WithFormat(format string) *Schema {
	s.Format = format
	return s
}

// WithExample sets an example value for the schema
func (s *Schema) WithExample(example interface{}) *Schema {
	s.Example = example
	return s
}

// WithDescription sets the description for the schema
func (s *Schema) WithDescription(description string) *Schema {
	s.Description = description
	return s
}

// WithTitle sets the title for the schema
func (s *Schema) WithTitle(title string) *Schema {
	s.Title = title
	return s
}

// WithDefault sets the default value for the schema
func (s *Schema) WithDefault(defaultValue interface{}) *Schema {
	s.Default = defaultValue
	return s
}

// WithEnum sets the enum values for the schema
func (s *Schema) WithEnum(values ...interface{}) *Schema {
	s.Enum = values
	return s
}

// WithPattern sets the pattern for string validation
func (s *Schema) WithPattern(pattern string) *Schema {
	s.Pattern = pattern
	return s
}

// String validation methods

// WithMinLength sets the minimum length for string schemas
func (s *Schema) WithMinLength(minLength int) *Schema {
	s.MinLength = minLength
	return s
}

// WithMaxLength sets the maximum length for string schemas
func (s *Schema) WithMaxLength(maxLength int) *Schema {
	s.MaxLength = maxLength
	return s
}

// Number validation methods

// WithMinimum sets the minimum value for numeric schemas
func (s *Schema) WithMinimum(minimum float64) *Schema {
	s.Minimum = minimum
	return s
}

// WithMaximum sets the maximum value for numeric schemas
func (s *Schema) WithMaximum(maximum float64) *Schema {
	s.Maximum = maximum
	return s
}

// WithExclusiveMinimum sets exclusive minimum for numeric schemas
func (s *Schema) WithExclusiveMinimum(exclusive bool) *Schema {
	s.ExclusiveMinimum = exclusive
	return s
}

// WithExclusiveMaximum sets exclusive maximum for numeric schemas
func (s *Schema) WithExclusiveMaximum(exclusive bool) *Schema {
	s.ExclusiveMaximum = exclusive
	return s
}

// WithMultipleOf sets the multipleOf constraint for numeric schemas
func (s *Schema) WithMultipleOf(multipleOf float64) *Schema {
	s.MultipleOf = multipleOf
	return s
}

// Array validation methods

// WithMinItems sets the minimum number of items for array schemas
func (s *Schema) WithMinItems(minItems int) *Schema {
	s.MinItems = minItems
	return s
}

// WithMaxItems sets the maximum number of items for array schemas
func (s *Schema) WithMaxItems(maxItems int) *Schema {
	s.MaxItems = maxItems
	return s
}

// WithUniqueItems sets whether array items should be unique
func (s *Schema) WithUniqueItems(unique bool) *Schema {
	s.UniqueItems = unique
	return s
}

// Object validation methods

// WithMinProperties sets the minimum number of properties for object schemas
func (s *Schema) WithMinProperties(minProperties int) *Schema {
	s.MinProperties = minProperties
	return s
}

// WithMaxProperties sets the maximum number of properties for object schemas
func (s *Schema) WithMaxProperties(maxProperties int) *Schema {
	s.MaxProperties = maxProperties
	return s
}

// WithAdditionalProperties sets additional properties schema
func (s *Schema) WithAdditionalProperties(schema *Schema) *Schema {
	s.AdditionalProperties = schema
	return s
}

// Metadata methods

// WithReadOnly marks the schema as read-only
func (s *Schema) WithReadOnly(readOnly bool) *Schema {
	s.ReadOnly = readOnly
	return s
}

// WithWriteOnly marks the schema as write-only
func (s *Schema) WithWriteOnly(writeOnly bool) *Schema {
	s.WriteOnly = writeOnly
	return s
}

// WithDeprecated marks the schema as deprecated
func (s *Schema) WithDeprecated(deprecated bool) *Schema {
	s.Deprecated = deprecated
	return s
}

// JSONSchema creates a string schema for JSON data
func JSONSchema(description string) *Schema {
	return StringSchema(description).WithFormat("json")
}

// Base64Schema creates a string schema for base64 encoded data
func Base64Schema(description string) *Schema {
	return StringSchema(description).WithFormat("base64")
}

// Numeric schema helpers

// PositiveIntegerSchema creates an integer schema with minimum 1
func PositiveIntegerSchema(description string) *Schema {
	return IntegerSchema(description).WithMinimum(1)
}

// NonNegativeIntegerSchema creates an integer schema with minimum 0
func NonNegativeIntegerSchema(description string) *Schema {
	return IntegerSchema(description).WithMinimum(0)
}

// PercentageSchema creates a number schema for percentages (0-100)
func PercentageSchema(description string) *Schema {
	return NumberSchema(description).
		WithMinimum(0).
		WithMaximum(100)
}

// PriceSchema creates a number schema for monetary values
func PriceSchema(description string) *Schema {
	return NumberSchema(description).
		WithMinimum(0).
		WithMultipleOf(0.01)
}

// Array schema helpers

// StringArraySchema creates an array of strings
func StringArraySchema(description string) *Schema {
	return ArraySchema(description, StringSchema("String item"))
}

// IntegerArraySchema creates an array of integers
func IntegerArraySchema(description string) *Schema {
	return ArraySchema(description, IntegerSchema("Integer item"))
}

// UniqueStringArraySchema creates an array of unique strings
func UniqueStringArraySchema(description string) *Schema {
	return StringArraySchema(description).WithUniqueItems(true)
}

// Object schema helpers

// TimestampObjectSchema creates an object with standard timestamp fields
func TimestampObjectSchema(description string, additionalProps map[string]*Schema) *Schema {
	props := map[string]*Schema{
		"created_at": DateTimeSchema("Creation timestamp").WithReadOnly(true),
		"updated_at": DateTimeSchema("Last update timestamp").WithReadOnly(true),
	}

	// Add additional properties
	for key, schema := range additionalProps {
		props[key] = schema
	}

	return ObjectSchema(description, props, nil)
}

// IdentifiedObjectSchema creates an object with ID field
func IdentifiedObjectSchema(description string, additionalProps map[string]*Schema, required []string) *Schema {
	props := map[string]*Schema{
		"id": UUIDSchema("Unique identifier").WithReadOnly(true),
	}

	// Add additional properties
	for key, schema := range additionalProps {
		props[key] = schema
	}

	// Add ID to required fields
	allRequired := append([]string{"id"}, required...)

	return ObjectSchema(description, props, allRequired)
}

// PaginationSchema creates a schema for pagination metadata
func PaginationSchema() *Schema {
	return ObjectSchema("Pagination metadata", map[string]*Schema{
		"page":        PositiveIntegerSchema("Current page number"),
		"per_page":    PositiveIntegerSchema("Items per page"),
		"total":       NonNegativeIntegerSchema("Total number of items"),
		"total_pages": NonNegativeIntegerSchema("Total number of pages"),
		"has_next":    BooleanSchema("Whether there is a next page"),
		"has_prev":    BooleanSchema("Whether there is a previous page"),
	}, []string{"page", "per_page", "total", "total_pages", "has_next", "has_prev"})
}

// ErrorSchema creates a standard error response schema
func ErrorSchema() *Schema {
	return ObjectSchema("Error response", map[string]*Schema{
		"error":    StringSchema("Error message"),
		"code":     StringSchema("Error code"),
		"details":  ObjectSchema("Additional error details", nil, nil),
		"trace_id": UUIDSchema("Request trace ID for debugging").WithReadOnly(true),
	}, []string{"error", "code"})
}

// ValidationErrorSchema creates a schema for validation errors
func ValidationErrorSchema() *Schema {
	return ObjectSchema("Validation error response", map[string]*Schema{
		"error": StringSchema("Error message"),
		"code":  StringSchema("Error code"),
		"details": ObjectSchema("Validation error details", map[string]*Schema{
			"field":   StringSchema("Field name that failed validation"),
			"message": StringSchema("Validation error message"),
			"value":   StringSchema("Invalid value that was provided"),
		}, []string{"field", "message"}),
		"trace_id": UUIDSchema("Request trace ID for debugging").WithReadOnly(true),
	}, []string{"error", "code", "details"})
}

// Response schema helpers

// PaginatedResponseSchema creates a schema for paginated responses
func PaginatedResponseSchema(itemSchema *Schema, itemsFieldName string) *Schema {
	return ObjectSchema("Paginated response", map[string]*Schema{
		itemsFieldName: ArraySchema("List of items", itemSchema),
		"pagination":   PaginationSchema(),
	}, []string{itemsFieldName, "pagination"})
}

// CreatedResponseSchema creates a schema for resource creation responses
func CreatedResponseSchema(resourceSchema *Schema) *Schema {
	props := map[string]*Schema{
		"message": StringSchema("Success message"),
		"data":    resourceSchema,
	}

	return ObjectSchema("Resource created response", props, []string{"message", "data"})
}

// UpdatedResponseSchema creates a schema for resource update responses
func UpdatedResponseSchema(resourceSchema *Schema) *Schema {
	props := map[string]*Schema{
		"message": StringSchema("Success message"),
		"data":    resourceSchema,
	}

	return ObjectSchema("Resource updated response", props, []string{"message", "data"})
}

// DeletedResponseSchema creates a schema for resource deletion responses
func DeletedResponseSchema() *Schema {
	return ObjectSchema("Resource deleted response", map[string]*Schema{
		"message": StringSchema("Success message"),
	}, []string{"message"})
}

// Advanced schema composition helpers

// OneOfSchema creates a schema with oneOf composition
func OneOfSchema(description string, schemas ...*Schema) *Schema {
	return &Schema{
		Description: description,
		OneOf:       schemas,
	}
}

// AllOfSchema creates a schema with allOf composition
func AllOfSchema(description string, schemas ...*Schema) *Schema {
	return &Schema{
		Description: description,
		AllOf:       schemas,
	}
}

// AnyOfSchema creates a schema with anyOf composition
func AnyOfSchema(description string, schemas ...*Schema) *Schema {
	return &Schema{
		Description: description,
		AnyOf:       schemas,
	}
}

// NotSchema creates a schema with not composition
func NotSchema(description string, schema *Schema) *Schema {
	return &Schema{
		Description: description,
		Not:         schema,
	}
}

// Common enum schemas

// StatusEnumSchema creates an enum schema for common status values
func StatusEnumSchema(description string, values ...string) *Schema {
	if len(values) == 0 {
		values = []string{"active", "inactive", "pending", "suspended"}
	}

	enumValues := make([]interface{}, len(values))
	for i, v := range values {
		enumValues[i] = v
	}

	return StringSchema(description).WithEnum(enumValues...)
}

// SortOrderSchema creates an enum schema for sort order
func SortOrderSchema() *Schema {
	return StringSchema("Sort order").
		WithEnum("asc", "desc").
		WithDefault("asc")
}

// HTTP method schema
func HTTPMethodSchema() *Schema {
	return StringSchema("HTTP method").
		WithEnum("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS")
}

// Content type schema
func ContentTypeSchema() *Schema {
	return StringSchema("Content type").
		WithEnum(
			"application/json",
			"application/xml",
			"text/plain",
			"text/html",
			"multipart/form-data",
			"application/x-www-form-urlencoded",
		)
}

// File upload schema helpers

// FileUploadSchema creates a schema for file uploads
func FileUploadSchema(description string) *Schema {
	return ObjectSchema(description, map[string]*Schema{
		"filename":     StringSchema("Original filename"),
		"content_type": ContentTypeSchema(),
		"size":         PositiveIntegerSchema("File size in bytes"),
		"data":         Base64Schema("Base64 encoded file data"),
	}, []string{"filename", "content_type", "size", "data"})
}

// ImageUploadSchema creates a schema for image uploads
func ImageUploadSchema(description string) *Schema {
	return ObjectSchema(description, map[string]*Schema{
		"filename": StringSchema("Original filename"),
		"content_type": StringSchema("Image content type").WithEnum(
			"image/jpeg", "image/png", "image/gif", "image/webp",
		),
		"size":   PositiveIntegerSchema("File size in bytes"),
		"width":  PositiveIntegerSchema("Image width in pixels"),
		"height": PositiveIntegerSchema("Image height in pixels"),
		"data":   Base64Schema("Base64 encoded image data"),
	}, []string{"filename", "content_type", "size", "data"})
}

// Utility functions for common patterns

// CreateRequestSchema creates a schema for resource creation requests
func CreateRequestSchema(name string, properties map[string]*Schema, required []string) *Schema {
	// Exclude read-only fields from create requests
	createProps := make(map[string]*Schema)
	for key, schema := range properties {
		if !schema.ReadOnly {
			createProps[key] = schema
		}
	}

	return ObjectSchema("Create "+name+" request", createProps, required)
}

// UpdateRequestSchema creates a schema for resource update requests
func UpdateRequestSchema(name string, properties map[string]*Schema) *Schema {
	// Make all fields optional for updates, exclude read-only fields
	updateProps := make(map[string]*Schema)
	for key, schema := range properties {
		if !schema.ReadOnly {
			updateProps[key] = schema
		}
	}

	return ObjectSchema("Update "+name+" request", updateProps, nil)
}

// Example usage functions demonstrating the helpers

func ExampleUserSchemas() map[string]*Schema {
	// Base user schema
	userSchema := IdentifiedObjectSchema("User", map[string]*Schema{
		"username":   StringSchema("Username").WithMinLength(3).WithMaxLength(50),
		"email":      EmailSchema("Email address"),
		"first_name": StringSchema("First name").WithMaxLength(100),
		"last_name":  StringSchema("Last name").WithMaxLength(100),
		"avatar_url": URISchema("Avatar image URL"),
		"status":     StatusEnumSchema("User status", "active", "inactive", "suspended"),
		"last_login": DateTimeSchema("Last login timestamp").WithReadOnly(true),
		"created_at": DateTimeSchema("Account creation timestamp").WithReadOnly(true),
		"updated_at": DateTimeSchema("Last update timestamp").WithReadOnly(true),
	}, []string{"username", "email", "first_name", "last_name"})

	// Create user request schema
	createUserSchema := CreateRequestSchema("User", userSchema.Properties, []string{
		"username", "email", "first_name", "last_name", "password",
	})
	createUserSchema.Properties["password"] = PasswordSchema("User password")

	// Update user request schema
	updateUserSchema := UpdateRequestSchema("User", userSchema.Properties)

	// User list response schema
	userListSchema := PaginatedResponseSchema(userSchema, "users")

	return map[string]*Schema{
		"User":            userSchema,
		"CreateUser":      createUserSchema,
		"UpdateUser":      updateUserSchema,
		"UserList":        userListSchema,
		"UserCreated":     CreatedResponseSchema(userSchema),
		"UserUpdated":     UpdatedResponseSchema(userSchema),
		"UserDeleted":     DeletedResponseSchema(),
		"Error":           ErrorSchema(),
		"ValidationError": ValidationErrorSchema(),
	}
}

// Extension to Schema struct (this would be added to the main Schema type)
type SchemaExtension struct {
	// Additional fields for enhanced documentation
	Example    interface{}   `json:"example,omitempty"`
	Examples   []interface{} `json:"examples,omitempty"`
	Deprecated bool          `json:"deprecated,omitempty"`
	ReadOnly   bool          `json:"readOnly,omitempty"`
	WriteOnly  bool          `json:"writeOnly,omitempty"`

	// Validation extensions
	MinLength int    `json:"minLength,omitempty"`
	MaxLength int    `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Format    string `json:"format,omitempty"`

	// Numeric validation
	Minimum          float64 `json:"minimum,omitempty"`
	Maximum          float64 `json:"maximum,omitempty"`
	ExclusiveMinimum bool    `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum bool    `json:"exclusiveMaximum,omitempty"`
	MultipleOf       float64 `json:"multipleOf,omitempty"`

	// Array validation
	MinItems    int  `json:"minItems,omitempty"`
	MaxItems    int  `json:"maxItems,omitempty"`
	UniqueItems bool `json:"uniqueItems,omitempty"`

	// Object validation
	MinProperties int `json:"minProperties,omitempty"`
	MaxProperties int `json:"maxProperties,omitempty"`
}
