package router

// Schema builders for common types
func StringSchema(description string) *Schema {
	return &Schema{
		Type:        "string",
		Description: description,
	}
}

func IntegerSchema(description string) *Schema {
	return &Schema{
		Type:        "integer",
		Description: description,
	}
}

func BooleanSchema(description string) *Schema {
	return &Schema{
		Type:        "boolean",
		Description: description,
	}
}

func NumberSchema(description string) *Schema {
	return &Schema{
		Type:        "number",
		Description: description,
	}
}

// Additional number schema helpers
func FloatSchema(description string) *Schema {
	return NumberSchema(description)
}

func DoubleSchema(description string) *Schema {
	return NumberSchema(description).WithFormat("double")
}

// Format-specific string schemas
func EmailSchema(description string) *Schema {
	return StringSchema(description).WithFormat("email")
}

func URISchema(description string) *Schema {
	return StringSchema(description).WithFormat("uri")
}

func DateSchema(description string) *Schema {
	return StringSchema(description).WithFormat("date")
}

func DateTimeSchema(description string) *Schema {
	return StringSchema(description).WithFormat("date-time")
}

func UUIDSchema(description string) *Schema {
	return StringSchema(description).WithFormat("uuid")
}

func PasswordSchema(description string) *Schema {
	return StringSchema(description).WithFormat("password")
}

func ArraySchema(description string, items *Schema) *Schema {
	return &Schema{
		Type:        "array",
		Description: description,
		Items:       items,
	}
}

func ObjectSchema(description string, properties map[string]*Schema, required []string) *Schema {
	return &Schema{
		Type:        "object",
		Description: description,
		Properties:  properties,
		Required:    required,
	}
}

func RefSchema(ref string) *Schema {
	return &Schema{
		Ref: ref,
	}
}
