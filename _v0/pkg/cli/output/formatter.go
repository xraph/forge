package output

import (
	"fmt"
	"io"
)

// OutputFormatter defines the interface for output formatting
type OutputFormatter interface {
	// Format formats data into the desired output format
	Format(data interface{}) ([]byte, error)

	// MimeType returns the MIME type for this format
	MimeType() string

	// FileExtension returns the file extension for this format
	FileExtension() string
}

// OutputManager manages multiple output formatters
type OutputManager struct {
	formatters    map[string]OutputFormatter
	defaultFormat string
}

// NewOutputManager creates a new output manager
func NewOutputManager() *OutputManager {
	manager := &OutputManager{
		formatters:    make(map[string]OutputFormatter),
		defaultFormat: "json",
	}

	// Register default formatters
	manager.RegisterFormatter("json", NewJSONFormatter())
	manager.RegisterFormatter("yaml", NewYAMLFormatter())
	manager.RegisterFormatter("table", NewTableFormatter())

	return manager
}

// RegisterFormatter registers a new output formatter
func (om *OutputManager) RegisterFormatter(name string, formatter OutputFormatter) {
	om.formatters[name] = formatter
}

// GetFormatter returns a formatter by name
func (om *OutputManager) GetFormatter(name string) (OutputFormatter, error) {
	if formatter, exists := om.formatters[name]; exists {
		return formatter, nil
	}
	return nil, fmt.Errorf("formatter '%s' not found", name)
}

// GetAvailableFormats returns all available format names
func (om *OutputManager) GetAvailableFormats() []string {
	formats := make([]string, 0, len(om.formatters))
	for name := range om.formatters {
		formats = append(formats, name)
	}
	return formats
}

// SetDefaultFormat sets the default format
func (om *OutputManager) SetDefaultFormat(format string) error {
	if _, exists := om.formatters[format]; !exists {
		return fmt.Errorf("format '%s' not available", format)
	}
	om.defaultFormat = format
	return nil
}

// GetDefaultFormat returns the default format name
func (om *OutputManager) GetDefaultFormat() string {
	return om.defaultFormat
}

// Format formats data using the specified format
func (om *OutputManager) Format(data interface{}, format string) ([]byte, error) {
	if format == "" {
		format = om.defaultFormat
	}

	formatter, err := om.GetFormatter(format)
	if err != nil {
		return nil, err
	}

	return formatter.Format(data)
}

// Write formats and writes data to the specified writer
func (om *OutputManager) Write(writer io.Writer, data interface{}, format string) error {
	formatted, err := om.Format(data, format)
	if err != nil {
		return err
	}

	_, err = writer.Write(formatted)
	return err
}

// StreamingOutputFormatter defines the interface for streaming output
type StreamingOutputFormatter interface {
	OutputFormatter

	// BeginStream starts a streaming output session
	BeginStream(writer io.Writer) error

	// WriteItem writes a single item to the stream
	WriteItem(item interface{}) error

	// EndStream ends the streaming session
	EndStream() error
}

// MultiFormatOutput handles output in multiple formats simultaneously
type MultiFormatOutput struct {
	outputs map[string]io.Writer
	manager *OutputManager
}

// NewMultiFormatOutput creates a new multi-format output
func NewMultiFormatOutput(manager *OutputManager) *MultiFormatOutput {
	return &MultiFormatOutput{
		outputs: make(map[string]io.Writer),
		manager: manager,
	}
}

// AddOutput adds an output writer for a specific format
func (mfo *MultiFormatOutput) AddOutput(format string, writer io.Writer) {
	mfo.outputs[format] = writer
}

// WriteAll writes data to all registered outputs
func (mfo *MultiFormatOutput) WriteAll(data interface{}) error {
	for format, writer := range mfo.outputs {
		if err := mfo.manager.Write(writer, data, format); err != nil {
			return fmt.Errorf("failed to write %s format: %w", format, err)
		}
	}
	return nil
}

// ConditionalFormatter wraps a formatter with conditions
type ConditionalFormatter struct {
	formatter OutputFormatter
	condition func(interface{}) bool
	fallback  OutputFormatter
}

// NewConditionalFormatter creates a new conditional formatter
func NewConditionalFormatter(formatter OutputFormatter, condition func(interface{}) bool, fallback OutputFormatter) *ConditionalFormatter {
	return &ConditionalFormatter{
		formatter: formatter,
		condition: condition,
		fallback:  fallback,
	}
}

// Format formats data using conditional logic
func (cf *ConditionalFormatter) Format(data interface{}) ([]byte, error) {
	if cf.condition(data) {
		return cf.formatter.Format(data)
	}
	if cf.fallback != nil {
		return cf.fallback.Format(data)
	}
	return cf.formatter.Format(data)
}

// MimeType returns the MIME type
func (cf *ConditionalFormatter) MimeType() string {
	return cf.formatter.MimeType()
}

// FileExtension returns the file extension
func (cf *ConditionalFormatter) FileExtension() string {
	return cf.formatter.FileExtension()
}

// TemplateFormatter formats data using Go templates
type TemplateFormatter struct {
	template  string
	mimeType  string
	extension string
}

// NewTemplateFormatter creates a new template formatter
func NewTemplateFormatter(template, mimeType, extension string) *TemplateFormatter {
	return &TemplateFormatter{
		template:  template,
		mimeType:  mimeType,
		extension: extension,
	}
}

// Format formats data using the template
func (tf *TemplateFormatter) Format(data interface{}) ([]byte, error) {
	// This would implement Go template formatting
	// For now, return a simple implementation
	return []byte(fmt.Sprintf(tf.template, data)), nil
}

// MimeType returns the MIME type
func (tf *TemplateFormatter) MimeType() string {
	return tf.mimeType
}

// FileExtension returns the file extension
func (tf *TemplateFormatter) FileExtension() string {
	return tf.extension
}

// ChainedFormatter allows chaining multiple formatters
type ChainedFormatter struct {
	formatters []OutputFormatter
}

// NewChainedFormatter creates a new chained formatter
func NewChainedFormatter(formatters ...OutputFormatter) *ChainedFormatter {
	return &ChainedFormatter{
		formatters: formatters,
	}
}

// Format applies all formatters in sequence
func (cf *ChainedFormatter) Format(data interface{}) ([]byte, error) {
	current := data
	var result []byte
	var err error

	for _, formatter := range cf.formatters {
		result, err = formatter.Format(current)
		if err != nil {
			return nil, err
		}
		current = result
	}

	return result, nil
}

// MimeType returns the MIME type of the last formatter
func (cf *ChainedFormatter) MimeType() string {
	if len(cf.formatters) > 0 {
		return cf.formatters[len(cf.formatters)-1].MimeType()
	}
	return "text/plain"
}

// FileExtension returns the file extension of the last formatter
func (cf *ChainedFormatter) FileExtension() string {
	if len(cf.formatters) > 0 {
		return cf.formatters[len(cf.formatters)-1].FileExtension()
	}
	return ".txt"
}
