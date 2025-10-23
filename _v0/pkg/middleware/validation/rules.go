package validation

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	json "github.com/json-iterator/go"
)

// Rule represents a validation rule
type Rule interface {
	Validate(value interface{}) error
	Name() string
	Message() string
}

// RuleBuilder provides a fluent interface for building validation rules
type RuleBuilder struct {
	rules []Rule
}

// NewRuleBuilder creates a new rule builder
func NewRuleBuilder() *RuleBuilder {
	return &RuleBuilder{
		rules: make([]Rule, 0),
	}
}

// Required adds a required validation rule
func (rb *RuleBuilder) Required() *RuleBuilder {
	rb.rules = append(rb.rules, &RequiredRule{})
	return rb
}

// MinLength adds a minimum length validation rule
func (rb *RuleBuilder) MinLength(min int) *RuleBuilder {
	rb.rules = append(rb.rules, &MinLengthRule{Min: min})
	return rb
}

// MaxLength adds a maximum length validation rule
func (rb *RuleBuilder) MaxLength(max int) *RuleBuilder {
	rb.rules = append(rb.rules, &MaxLengthRule{Max: max})
	return rb
}

// Length adds an exact length validation rule
func (rb *RuleBuilder) Length(length int) *RuleBuilder {
	rb.rules = append(rb.rules, &LengthRule{Length: length})
	return rb
}

// Min adds a minimum value validation rule
func (rb *RuleBuilder) Min(min float64) *RuleBuilder {
	rb.rules = append(rb.rules, &MinRule{Min: min})
	return rb
}

// Max adds a maximum value validation rule
func (rb *RuleBuilder) Max(max float64) *RuleBuilder {
	rb.rules = append(rb.rules, &MaxRule{Max: max})
	return rb
}

// Range adds a range validation rule
func (rb *RuleBuilder) Range(min, max float64) *RuleBuilder {
	rb.rules = append(rb.rules, &RangeRule{Min: min, Max: max})
	return rb
}

// Email adds an email validation rule
func (rb *RuleBuilder) Email() *RuleBuilder {
	rb.rules = append(rb.rules, &EmailRule{})
	return rb
}

// URL adds a URL validation rule
func (rb *RuleBuilder) URL() *RuleBuilder {
	rb.rules = append(rb.rules, &URLRule{})
	return rb
}

// IP adds an IP address validation rule
func (rb *RuleBuilder) IP() *RuleBuilder {
	rb.rules = append(rb.rules, &IPRule{})
	return rb
}

// IPv4 adds an IPv4 address validation rule
func (rb *RuleBuilder) IPv4() *RuleBuilder {
	rb.rules = append(rb.rules, &IPv4Rule{})
	return rb
}

// IPv6 adds an IPv6 address validation rule
func (rb *RuleBuilder) IPv6() *RuleBuilder {
	rb.rules = append(rb.rules, &IPv6Rule{})
	return rb
}

// Alpha adds an alphabetic characters only validation rule
func (rb *RuleBuilder) Alpha() *RuleBuilder {
	rb.rules = append(rb.rules, &AlphaRule{})
	return rb
}

// AlphaNumeric adds an alphanumeric characters only validation rule
func (rb *RuleBuilder) AlphaNumeric() *RuleBuilder {
	rb.rules = append(rb.rules, &AlphaNumericRule{})
	return rb
}

// Numeric adds a numeric characters only validation rule
func (rb *RuleBuilder) Numeric() *RuleBuilder {
	rb.rules = append(rb.rules, &NumericRule{})
	return rb
}

// Regex adds a regular expression validation rule
func (rb *RuleBuilder) Regex(pattern string) *RuleBuilder {
	rb.rules = append(rb.rules, &RegexRule{Pattern: pattern})
	return rb
}

// In adds an inclusion validation rule
func (rb *RuleBuilder) In(values ...interface{}) *RuleBuilder {
	rb.rules = append(rb.rules, &InRule{Values: values})
	return rb
}

// NotIn adds an exclusion validation rule
func (rb *RuleBuilder) NotIn(values ...interface{}) *RuleBuilder {
	rb.rules = append(rb.rules, &NotInRule{Values: values})
	return rb
}

// DateFormat adds a date format validation rule
func (rb *RuleBuilder) DateFormat(format string) *RuleBuilder {
	rb.rules = append(rb.rules, &DateFormatRule{Format: format})
	return rb
}

// Before adds a before date validation rule
func (rb *RuleBuilder) Before(date time.Time) *RuleBuilder {
	rb.rules = append(rb.rules, &BeforeRule{Date: date})
	return rb
}

// After adds an after date validation rule
func (rb *RuleBuilder) After(date time.Time) *RuleBuilder {
	rb.rules = append(rb.rules, &AfterRule{Date: date})
	return rb
}

// UUID adds a UUID validation rule
func (rb *RuleBuilder) UUID() *RuleBuilder {
	rb.rules = append(rb.rules, &UUIDRule{})
	return rb
}

// JSON adds a JSON validation rule
func (rb *RuleBuilder) JSON() *RuleBuilder {
	rb.rules = append(rb.rules, &JSONRule{})
	return rb
}

// Phone adds a phone number validation rule
func (rb *RuleBuilder) Phone() *RuleBuilder {
	rb.rules = append(rb.rules, &PhoneRule{})
	return rb
}

// CreditCard adds a credit card validation rule
func (rb *RuleBuilder) CreditCard() *RuleBuilder {
	rb.rules = append(rb.rules, &CreditCardRule{})
	return rb
}

// Custom adds a custom validation rule
func (rb *RuleBuilder) Custom(name string, validator func(interface{}) error, message string) *RuleBuilder {
	rb.rules = append(rb.rules, &CustomRule{
		name:      name,
		validator: validator,
		message:   message,
	})
	return rb
}

// Build returns the list of validation rules
func (rb *RuleBuilder) Build() []Rule {
	return rb.rules
}

// Validate validates a value against all rules
func (rb *RuleBuilder) Validate(value interface{}) []error {
	var errors []error
	for _, rule := range rb.rules {
		if err := rule.Validate(value); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// =============================================================================
// RULE IMPLEMENTATIONS
// =============================================================================

// RequiredRule validates that a value is not empty
type RequiredRule struct{}

func (r *RequiredRule) Name() string    { return "required" }
func (r *RequiredRule) Message() string { return "field is required" }

func (r *RequiredRule) Validate(value interface{}) error {
	if value == nil {
		return errors.New(r.Message())
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		if v.String() == "" {
			return errors.New(r.Message())
		}
	case reflect.Slice, reflect.Map, reflect.Array:
		if v.Len() == 0 {
			return errors.New(r.Message())
		}
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return errors.New(r.Message())
		}
	}

	return nil
}

// MinLengthRule validates minimum length
type MinLengthRule struct {
	Min int
}

func (r *MinLengthRule) Name() string    { return "min_length" }
func (r *MinLengthRule) Message() string { return fmt.Sprintf("minimum length is %d", r.Min) }

func (r *MinLengthRule) Validate(value interface{}) error {
	length := getLength(value)
	if length < r.Min {
		return errors.New(r.Message())
	}
	return nil
}

// MaxLengthRule validates maximum length
type MaxLengthRule struct {
	Max int
}

func (r *MaxLengthRule) Name() string    { return "max_length" }
func (r *MaxLengthRule) Message() string { return fmt.Sprintf("maximum length is %d", r.Max) }

func (r *MaxLengthRule) Validate(value interface{}) error {
	length := getLength(value)
	if length > r.Max {
		return errors.New(r.Message())
	}
	return nil
}

// LengthRule validates exact length
type LengthRule struct {
	Length int
}

func (r *LengthRule) Name() string    { return "length" }
func (r *LengthRule) Message() string { return fmt.Sprintf("length must be %d", r.Length) }

func (r *LengthRule) Validate(value interface{}) error {
	length := getLength(value)
	if length != r.Length {
		return errors.New(r.Message())
	}
	return nil
}

// MinRule validates minimum value
type MinRule struct {
	Min float64
}

func (r *MinRule) Name() string    { return "min" }
func (r *MinRule) Message() string { return fmt.Sprintf("minimum value is %g", r.Min) }

func (r *MinRule) Validate(value interface{}) error {
	num, err := getNumericValue(value)
	if err != nil {
		return fmt.Errorf("value must be numeric")
	}
	if num < r.Min {
		return errors.New(r.Message())
	}
	return nil
}

// MaxRule validates maximum value
type MaxRule struct {
	Max float64
}

func (r *MaxRule) Name() string    { return "max" }
func (r *MaxRule) Message() string { return fmt.Sprintf("maximum value is %g", r.Max) }

func (r *MaxRule) Validate(value interface{}) error {
	num, err := getNumericValue(value)
	if err != nil {
		return fmt.Errorf("value must be numeric")
	}
	if num > r.Max {
		return errors.New(r.Message())
	}
	return nil
}

// RangeRule validates value within range
type RangeRule struct {
	Min, Max float64
}

func (r *RangeRule) Name() string { return "range" }
func (r *RangeRule) Message() string {
	return fmt.Sprintf("value must be between %g and %g", r.Min, r.Max)
}

func (r *RangeRule) Validate(value interface{}) error {
	num, err := getNumericValue(value)
	if err != nil {
		return fmt.Errorf("value must be numeric")
	}
	if num < r.Min || num > r.Max {
		return errors.New(r.Message())
	}
	return nil
}

// EmailRule validates email format
type EmailRule struct{}

func (r *EmailRule) Name() string    { return "email" }
func (r *EmailRule) Message() string { return "must be a valid email address" }

func (r *EmailRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	_, err := mail.ParseAddress(str)
	if err != nil {
		return errors.New(r.Message())
	}
	return nil
}

// URLRule validates URL format
type URLRule struct{}

func (r *URLRule) Name() string    { return "url" }
func (r *URLRule) Message() string { return "must be a valid URL" }

func (r *URLRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	_, err := url.ParseRequestURI(str)
	if err != nil {
		return errors.New(r.Message())
	}
	return nil
}

// IPRule validates IP address format
type IPRule struct{}

func (r *IPRule) Name() string    { return "ip" }
func (r *IPRule) Message() string { return "must be a valid IP address" }

func (r *IPRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	if net.ParseIP(str) == nil {
		return errors.New(r.Message())
	}
	return nil
}

// IPv4Rule validates IPv4 address format
type IPv4Rule struct{}

func (r *IPv4Rule) Name() string    { return "ipv4" }
func (r *IPv4Rule) Message() string { return "must be a valid IPv4 address" }

func (r *IPv4Rule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	ip := net.ParseIP(str)
	if ip == nil || ip.To4() == nil {
		return errors.New(r.Message())
	}
	return nil
}

// IPv6Rule validates IPv6 address format
type IPv6Rule struct{}

func (r *IPv6Rule) Name() string    { return "ipv6" }
func (r *IPv6Rule) Message() string { return "must be a valid IPv6 address" }

func (r *IPv6Rule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	ip := net.ParseIP(str)
	if ip == nil || ip.To4() != nil {
		return errors.New(r.Message())
	}
	return nil
}

// AlphaRule validates alphabetic characters only
type AlphaRule struct{}

func (r *AlphaRule) Name() string    { return "alpha" }
func (r *AlphaRule) Message() string { return "must contain only alphabetic characters" }

func (r *AlphaRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	for _, char := range str {
		if !unicode.IsLetter(char) {
			return errors.New(r.Message())
		}
	}
	return nil
}

// AlphaNumericRule validates alphanumeric characters only
type AlphaNumericRule struct{}

func (r *AlphaNumericRule) Name() string    { return "alpha_numeric" }
func (r *AlphaNumericRule) Message() string { return "must contain only alphanumeric characters" }

func (r *AlphaNumericRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	for _, char := range str {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return errors.New(r.Message())
		}
	}
	return nil
}

// NumericRule validates numeric characters only
type NumericRule struct{}

func (r *NumericRule) Name() string    { return "numeric" }
func (r *NumericRule) Message() string { return "must contain only numeric characters" }

func (r *NumericRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	for _, char := range str {
		if !unicode.IsDigit(char) {
			return errors.New(r.Message())
		}
	}
	return nil
}

// RegexRule validates against a regular expression
type RegexRule struct {
	Pattern string
	regex   *regexp.Regexp
}

func (r *RegexRule) Name() string    { return "regex" }
func (r *RegexRule) Message() string { return fmt.Sprintf("must match pattern %s", r.Pattern) }

func (r *RegexRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	if r.regex == nil {
		var err error
		r.regex, err = regexp.Compile(r.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	if !r.regex.MatchString(str) {
		return errors.New(r.Message())
	}
	return nil
}

// InRule validates that value is in a list of allowed values
type InRule struct {
	Values []interface{}
}

func (r *InRule) Name() string    { return "in" }
func (r *InRule) Message() string { return "must be one of the allowed values" }

func (r *InRule) Validate(value interface{}) error {
	for _, allowed := range r.Values {
		if reflect.DeepEqual(value, allowed) {
			return nil
		}
	}
	return errors.New(r.Message())
}

// NotInRule validates that value is not in a list of forbidden values
type NotInRule struct {
	Values []interface{}
}

func (r *NotInRule) Name() string    { return "not_in" }
func (r *NotInRule) Message() string { return "must not be one of the forbidden values" }

func (r *NotInRule) Validate(value interface{}) error {
	for _, forbidden := range r.Values {
		if reflect.DeepEqual(value, forbidden) {
			return errors.New(r.Message())
		}
	}
	return nil
}

// DateFormatRule validates date format
type DateFormatRule struct {
	Format string
}

func (r *DateFormatRule) Name() string { return "date_format" }
func (r *DateFormatRule) Message() string {
	return fmt.Sprintf("must be a valid date in format %s", r.Format)
}

func (r *DateFormatRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	_, err := time.Parse(r.Format, str)
	if err != nil {
		return errors.New(r.Message())
	}
	return nil
}

// BeforeRule validates that date is before a specific date
type BeforeRule struct {
	Date time.Time
}

func (r *BeforeRule) Name() string { return "before" }
func (r *BeforeRule) Message() string {
	return fmt.Sprintf("must be before %s", r.Date.Format(time.RFC3339))
}

func (r *BeforeRule) Validate(value interface{}) error {
	date, err := parseDate(value)
	if err != nil {
		return err
	}

	if !date.Before(r.Date) {
		return errors.New(r.Message())
	}
	return nil
}

// AfterRule validates that date is after a specific date
type AfterRule struct {
	Date time.Time
}

func (r *AfterRule) Name() string { return "after" }
func (r *AfterRule) Message() string {
	return fmt.Sprintf("must be after %s", r.Date.Format(time.RFC3339))
}

func (r *AfterRule) Validate(value interface{}) error {
	date, err := parseDate(value)
	if err != nil {
		return err
	}

	if !date.After(r.Date) {
		return errors.New(r.Message())
	}
	return nil
}

// UUIDRule validates UUID format
type UUIDRule struct{}

func (r *UUIDRule) Name() string    { return "uuid" }
func (r *UUIDRule) Message() string { return "must be a valid UUID" }

func (r *UUIDRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	// Simple UUID validation
	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	if !uuidRegex.MatchString(str) {
		return errors.New(r.Message())
	}
	return nil
}

// JSONRule validates JSON format
type JSONRule struct{}

func (r *JSONRule) Name() string    { return "json" }
func (r *JSONRule) Message() string { return "must be valid JSON" }

func (r *JSONRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	var js interface{}
	if err := json.Unmarshal([]byte(str), &js); err != nil {
		return errors.New(r.Message())
	}
	return nil
}

// PhoneRule validates phone number format
type PhoneRule struct{}

func (r *PhoneRule) Name() string    { return "phone" }
func (r *PhoneRule) Message() string { return "must be a valid phone number" }

func (r *PhoneRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	// Simple phone number validation (international format)
	phoneRegex := regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
	if !phoneRegex.MatchString(strings.ReplaceAll(str, " ", "")) {
		return errors.New(r.Message())
	}
	return nil
}

// CreditCardRule validates credit card number using Luhn algorithm
type CreditCardRule struct{}

func (r *CreditCardRule) Name() string    { return "credit_card" }
func (r *CreditCardRule) Message() string { return "must be a valid credit card number" }

func (r *CreditCardRule) Validate(value interface{}) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("value must be a string")
	}

	// Remove spaces and dashes
	str = strings.ReplaceAll(strings.ReplaceAll(str, " ", ""), "-", "")

	// Check if all characters are digits
	for _, char := range str {
		if !unicode.IsDigit(char) {
			return errors.New(r.Message())
		}
	}

	// Luhn algorithm
	if !isValidLuhn(str) {
		return errors.New(r.Message())
	}

	return nil
}

// CustomRule allows custom validation logic
type CustomRule struct {
	name      string
	validator func(interface{}) error
	message   string
}

func (r *CustomRule) Name() string    { return r.name }
func (r *CustomRule) Message() string { return r.message }

func (r *CustomRule) Validate(value interface{}) error {
	return r.validator(value)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// getLength returns the length of a value
func getLength(value interface{}) int {
	if value == nil {
		return 0
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return utf8.RuneCountInString(v.String())
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len()
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return getLength(v.Elem().Interface())
	default:
		return 0
	}
}

// getNumericValue converts a value to float64
func getNumericValue(value interface{}) (float64, error) {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return strconv.ParseFloat(v.String(), 64)
	default:
		return 0, fmt.Errorf("cannot convert to numeric value")
	}
}

// parseDate parses a date from various formats
func parseDate(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		// Try common date formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02",
			"2006-01-02 15:04:05",
			"01/02/2006",
			"02/01/2006",
		}

		for _, format := range formats {
			if date, err := time.Parse(format, v); err == nil {
				return date, nil
			}
		}
		return time.Time{}, fmt.Errorf("unable to parse date")
	default:
		return time.Time{}, fmt.Errorf("value must be a time.Time or string")
	}
}

// isValidLuhn validates a string using the Luhn algorithm
func isValidLuhn(str string) bool {
	sum := 0
	alternate := false

	// Process digits from right to left
	for i := len(str) - 1; i >= 0; i-- {
		digit := int(str[i] - '0')

		if alternate {
			digit *= 2
			if digit > 9 {
				digit = digit%10 + digit/10
			}
		}

		sum += digit
		alternate = !alternate
	}

	return sum%10 == 0
}

// =============================================================================
// PREDEFINED RULE SETS
// =============================================================================

// CommonRules provides commonly used validation rule sets
type CommonRules struct{}

// Password creates password validation rules
func (CommonRules) Password() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		MinLength(8).
		MaxLength(128).
		Regex(`[A-Z]`).     // At least one uppercase
		Regex(`[a-z]`).     // At least one lowercase
		Regex(`[0-9]`).     // At least one digit
		Regex(`[!@#$%^&*]`) // At least one special character
}

// Username creates username validation rules
func (CommonRules) Username() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		MinLength(3).
		MaxLength(50).
		AlphaNumeric()
}

// Name creates name validation rules
func (CommonRules) Name() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		MinLength(1).
		MaxLength(100).
		Alpha()
}

// Age creates age validation rules
func (CommonRules) Age() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		Range(0, 150)
}

// PostalCode creates postal code validation rules
func (CommonRules) PostalCode() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		Regex(`^[A-Za-z0-9\s\-]{3,10}$`)
}

// CurrencyAmount creates currency amount validation rules
func (CommonRules) CurrencyAmount() *RuleBuilder {
	return NewRuleBuilder().
		Required().
		Min(0).
		Regex(`^\d+(\.\d{1,2})?$`)
}

// =============================================================================
// VALIDATION HELPERS
// =============================================================================

// ValidateStruct validates a struct using field tags
func ValidateStruct(s interface{}) map[string][]error {
	errors := make(map[string][]error)

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		errors["_struct"] = []error{fmt.Errorf("value must be a struct")}
		return errors
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		if !fieldValue.CanInterface() {
			continue
		}

		// Parse validation tags
		rules := parseValidationTags(field.Tag.Get("validate"))
		if len(rules) == 0 {
			continue
		}

		// Validate field
		fieldErrors := []error{}
		for _, rule := range rules {
			if err := rule.Validate(fieldValue.Interface()); err != nil {
				fieldErrors = append(fieldErrors, err)
			}
		}

		if len(fieldErrors) > 0 {
			errors[field.Name] = fieldErrors
		}
	}

	return errors
}

// parseValidationTags parses validation tags and returns rules
func parseValidationTags(tag string) []Rule {
	if tag == "" {
		return nil
	}

	builder := NewRuleBuilder()
	rules := strings.Split(tag, ",")

	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		parts := strings.Split(rule, "=")

		switch parts[0] {
		case "required":
			builder.Required()
		case "min":
			if len(parts) > 1 {
				if min, err := strconv.ParseFloat(parts[1], 64); err == nil {
					builder.Min(min)
				}
			}
		case "max":
			if len(parts) > 1 {
				if max, err := strconv.ParseFloat(parts[1], 64); err == nil {
					builder.Max(max)
				}
			}
		case "len":
			if len(parts) > 1 {
				if length, err := strconv.Atoi(parts[1]); err == nil {
					builder.Length(length)
				}
			}
		case "email":
			builder.Email()
		case "url":
			builder.URL()
		case "alpha":
			builder.Alpha()
		case "alphanum":
			builder.AlphaNumeric()
		case "numeric":
			builder.Numeric()
		case "uuid":
			builder.UUID()
		}
	}

	return builder.Build()
}
