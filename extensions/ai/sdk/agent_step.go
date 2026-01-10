package sdk

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"time"
)

// AgentStep represents a single step in agent execution.
type AgentStep struct {
	// Step identification
	Index       int    `json:"index"`
	ID          string `json:"id"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`

	// Input/Output
	Input     string `json:"input"`
	Output    string `json:"output"`
	Reasoning string `json:"reasoning,omitempty"`

	// Tool interactions
	ToolCalls   []StepToolCall   `json:"toolCalls,omitempty"`
	ToolResults []StepToolResult `json:"toolResults,omitempty"`

	// Metadata
	StartTime  time.Time      `json:"startTime"`
	EndTime    time.Time      `json:"endTime"`
	Duration   time.Duration  `json:"duration"`
	TokensUsed int            `json:"tokensUsed"`
	Metadata   map[string]any `json:"metadata,omitempty"`

	// State
	State         StepState           `json:"state"`
	Error         string              `json:"error,omitempty"`
	Observations  []string            `json:"observations,omitempty"`
	Modifications []StateModification `json:"modifications,omitempty"`
}

// StepToolCall represents a tool call within a step.
type StepToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	StartTime time.Time      `json:"startTime"`
}

// StepToolResult represents the result of a tool call.
type StepToolResult struct {
	ToolCallID string        `json:"toolCallId"`
	Name       string        `json:"name"`
	Result     any           `json:"result"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// StateModification represents a change to agent state.
type StateModification struct {
	Path     string `json:"path"`
	OldValue any    `json:"oldValue"`
	NewValue any    `json:"newValue"`
	Action   string `json:"action"` // set, delete, append
}

// StepState represents the state of a step.
type StepState string

const (
	StepStatePending   StepState = "pending"
	StepStateRunning   StepState = "running"
	StepStateWaiting   StepState = "waiting" // Waiting for tool results
	StepStateCompleted StepState = "completed"
	StepStateFailed    StepState = "failed"
	StepStateCancelled StepState = "cancelled"
)

// Summary returns a brief summary of the step.
func (s *AgentStep) Summary() string {
	var parts []string

	if s.Input != "" {
		if len(s.Input) > 100 {
			parts = append(parts, "Input: "+s.Input[:100]+"...")
		} else {
			parts = append(parts, "Input: "+s.Input)
		}
	}

	if len(s.ToolCalls) > 0 {
		toolNames := make([]string, len(s.ToolCalls))
		for i, tc := range s.ToolCalls {
			toolNames[i] = tc.Name
		}

		parts = append(parts, "Tools: "+strings.Join(toolNames, ", "))
	}

	if s.Output != "" {
		if len(s.Output) > 100 {
			parts = append(parts, "Output: "+s.Output[:100]+"...")
		} else {
			parts = append(parts, "Output: "+s.Output)
		}
	}

	return strings.Join(parts, " | ")
}

// HasToolCalls returns true if this step has tool calls.
func (s *AgentStep) HasToolCalls() bool {
	return len(s.ToolCalls) > 0
}

// ContainsKeyword checks if the output contains a keyword.
func (s *AgentStep) ContainsKeyword(keyword string) bool {
	return strings.Contains(strings.ToLower(s.Output), strings.ToLower(keyword))
}

// ContainsToolCall checks if a specific tool was called.
func (s *AgentStep) ContainsToolCall(toolName string) bool {
	for _, tc := range s.ToolCalls {
		if tc.Name == toolName {
			return true
		}
	}

	return false
}

// GetToolResult returns the result of a tool call by name.
func (s *AgentStep) GetToolResult(toolName string) (any, bool) {
	for _, tr := range s.ToolResults {
		if tr.Name == toolName {
			return tr.Result, true
		}
	}

	return nil, false
}

// IsSuccess returns true if the step completed successfully.
func (s *AgentStep) IsSuccess() bool {
	return s.State == StepStateCompleted && s.Error == ""
}

// IsFinal checks if this step represents a final answer.
func (s *AgentStep) IsFinal() bool {
	// Common patterns for final answers
	finalPatterns := []string{
		"final answer",
		"conclusion",
		"in summary",
		"to summarize",
		"the answer is",
		"therefore,",
	}

	lowerOutput := strings.ToLower(s.Output)
	for _, pattern := range finalPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}

	// No tool calls usually indicates final answer
	return !s.HasToolCalls()
}

// Clone creates a deep copy of the step.
func (s *AgentStep) Clone() *AgentStep {
	clone := *s
	clone.ToolCalls = make([]StepToolCall, len(s.ToolCalls))
	copy(clone.ToolCalls, s.ToolCalls)
	clone.ToolResults = make([]StepToolResult, len(s.ToolResults))
	copy(clone.ToolResults, s.ToolResults)
	clone.Observations = make([]string, len(s.Observations))
	copy(clone.Observations, s.Observations)

	if s.Metadata != nil {
		clone.Metadata = make(map[string]any)
		maps.Copy(clone.Metadata, s.Metadata)
	}

	return &clone
}

// ToJSON serializes the step to JSON.
func (s *AgentStep) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// StepFromJSON deserializes a step from JSON.
func StepFromJSON(data []byte) (*AgentStep, error) {
	var step AgentStep
	if err := json.Unmarshal(data, &step); err != nil {
		return nil, err
	}

	return &step, nil
}

// StepHistory represents the history of agent steps.
type StepHistory struct {
	Steps []*AgentStep
}

// NewStepHistory creates a new step history.
func NewStepHistory() *StepHistory {
	return &StepHistory{
		Steps: make([]*AgentStep, 0),
	}
}

// Add adds a step to the history.
func (h *StepHistory) Add(step *AgentStep) {
	h.Steps = append(h.Steps, step)
}

// Last returns the last step, or nil if empty.
func (h *StepHistory) Last() *AgentStep {
	if len(h.Steps) == 0 {
		return nil
	}

	return h.Steps[len(h.Steps)-1]
}

// Len returns the number of steps.
func (h *StepHistory) Len() int {
	return len(h.Steps)
}

// Get returns a step by index.
func (h *StepHistory) Get(index int) *AgentStep {
	if index < 0 || index >= len(h.Steps) {
		return nil
	}

	return h.Steps[index]
}

// GetByID returns a step by ID.
func (h *StepHistory) GetByID(id string) *AgentStep {
	for _, step := range h.Steps {
		if step.ID == id {
			return step
		}
	}

	return nil
}

// Filter returns steps matching a predicate.
func (h *StepHistory) Filter(predicate func(*AgentStep) bool) []*AgentStep {
	var result []*AgentStep

	for _, step := range h.Steps {
		if predicate(step) {
			result = append(result, step)
		}
	}

	return result
}

// GetSuccessfulSteps returns only successful steps.
func (h *StepHistory) GetSuccessfulSteps() []*AgentStep {
	return h.Filter(func(s *AgentStep) bool {
		return s.IsSuccess()
	})
}

// GetStepsWithToolCalls returns steps that have tool calls.
func (h *StepHistory) GetStepsWithToolCalls() []*AgentStep {
	return h.Filter(func(s *AgentStep) bool {
		return s.HasToolCalls()
	})
}

// TotalTokens returns the total tokens used across all steps.
func (h *StepHistory) TotalTokens() int {
	total := 0
	for _, step := range h.Steps {
		total += step.TokensUsed
	}

	return total
}

// TotalDuration returns the total duration of all steps.
func (h *StepHistory) TotalDuration() time.Duration {
	var total time.Duration
	for _, step := range h.Steps {
		total += step.Duration
	}

	return total
}

// Summary returns a summary of the step history.
func (h *StepHistory) Summary() StepHistorySummary {
	summary := StepHistorySummary{
		TotalSteps:    len(h.Steps),
		TotalTokens:   h.TotalTokens(),
		TotalDuration: h.TotalDuration(),
	}

	for _, step := range h.Steps {
		summary.TotalToolCalls += len(step.ToolCalls)
		switch step.State {
		case StepStateCompleted:
			summary.SuccessfulSteps++
		case StepStateFailed:
			summary.FailedSteps++
		}
	}

	return summary
}

// StepHistorySummary provides a summary of step history.
type StepHistorySummary struct {
	TotalSteps      int           `json:"totalSteps"`
	SuccessfulSteps int           `json:"successfulSteps"`
	FailedSteps     int           `json:"failedSteps"`
	TotalToolCalls  int           `json:"totalToolCalls"`
	TotalTokens     int           `json:"totalTokens"`
	TotalDuration   time.Duration `json:"totalDuration"`
}

// StepBuilder helps construct agent steps.
type StepBuilder struct {
	step *AgentStep
}

// NewStepBuilder creates a new step builder.
func NewStepBuilder(agentID, executionID string, index int) *StepBuilder {
	return &StepBuilder{
		step: &AgentStep{
			Index:       index,
			AgentID:     agentID,
			ExecutionID: executionID,
			State:       StepStatePending,
			StartTime:   time.Now(),
			Metadata:    make(map[string]any),
		},
	}
}

// WithID sets the step ID.
func (b *StepBuilder) WithID(id string) *StepBuilder {
	b.step.ID = id

	return b
}

// WithInput sets the input.
func (b *StepBuilder) WithInput(input string) *StepBuilder {
	b.step.Input = input

	return b
}

// WithOutput sets the output.
func (b *StepBuilder) WithOutput(output string) *StepBuilder {
	b.step.Output = output

	return b
}

// WithReasoning sets the reasoning.
func (b *StepBuilder) WithReasoning(reasoning string) *StepBuilder {
	b.step.Reasoning = reasoning

	return b
}

// WithToolCall adds a tool call.
func (b *StepBuilder) WithToolCall(id, name string, args map[string]any) *StepBuilder {
	b.step.ToolCalls = append(b.step.ToolCalls, StepToolCall{
		ID:        id,
		Name:      name,
		Arguments: args,
		StartTime: time.Now(),
	})

	return b
}

// WithToolResult adds a tool result.
func (b *StepBuilder) WithToolResult(toolCallID, name string, result any, err error, duration time.Duration) *StepBuilder {
	tr := StepToolResult{
		ToolCallID: toolCallID,
		Name:       name,
		Result:     result,
		Duration:   duration,
	}
	if err != nil {
		tr.Error = err.Error()
	}

	b.step.ToolResults = append(b.step.ToolResults, tr)

	return b
}

// WithMetadata adds metadata.
func (b *StepBuilder) WithMetadata(key string, value any) *StepBuilder {
	b.step.Metadata[key] = value

	return b
}

// WithObservation adds an observation.
func (b *StepBuilder) WithObservation(observation string) *StepBuilder {
	b.step.Observations = append(b.step.Observations, observation)

	return b
}

// WithState sets the state.
func (b *StepBuilder) WithState(state StepState) *StepBuilder {
	b.step.State = state

	return b
}

// WithError sets an error.
func (b *StepBuilder) WithError(err error) *StepBuilder {
	if err != nil {
		b.step.Error = err.Error()
		b.step.State = StepStateFailed
	}

	return b
}

// WithTokens sets token usage.
func (b *StepBuilder) WithTokens(tokens int) *StepBuilder {
	b.step.TokensUsed = tokens

	return b
}

// Complete marks the step as completed.
func (b *StepBuilder) Complete() *StepBuilder {
	b.step.State = StepStateCompleted
	b.step.EndTime = time.Now()
	b.step.Duration = b.step.EndTime.Sub(b.step.StartTime)

	return b
}

// Build returns the constructed step.
func (b *StepBuilder) Build() *AgentStep {
	if b.step.EndTime.IsZero() {
		b.step.EndTime = time.Now()
		b.step.Duration = b.step.EndTime.Sub(b.step.StartTime)
	}

	return b.step
}

// StepCondition is a function that evaluates a step against history.
type StepCondition func(step *AgentStep, history []*AgentStep) bool

// Common step conditions

// StopOnKeyword creates a condition that stops when a keyword is found.
func StopOnKeyword(keyword string) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return step.ContainsKeyword(keyword)
	}
}

// StopOnFinalAnswer creates a condition that stops when a final answer is detected.
func StopOnFinalAnswer() StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return step.IsFinal()
	}
}

// StopOnMaxSteps creates a condition that stops after max steps.
func StopOnMaxSteps(max int) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return len(history) >= max
	}
}

// StopOnNoToolCalls creates a condition that stops when no tools are called.
func StopOnNoToolCalls() StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return !step.HasToolCalls()
	}
}

// StopOnToolCall creates a condition that stops after a specific tool is called.
func StopOnToolCall(toolName string) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return step.ContainsToolCall(toolName)
	}
}

// StopOnError creates a condition that stops on any error.
func StopOnError() StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		return step.Error != ""
	}
}

// StopOnConsecutiveFailures creates a condition that stops after N consecutive failures.
func StopOnConsecutiveFailures(n int) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		if len(history) < n {
			return false
		}

		consecutive := 0
		for i := len(history) - 1; i >= 0 && consecutive < n; i-- {
			if history[i].State == StepStateFailed {
				consecutive++
			} else {
				break
			}
		}

		return consecutive >= n
	}
}

// CombineConditions combines multiple conditions with AND logic.
func CombineConditions(conditions ...StepCondition) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		for _, cond := range conditions {
			if !cond(step, history) {
				return false
			}
		}

		return true
	}
}

// AnyCondition combines multiple conditions with OR logic.
func AnyCondition(conditions ...StepCondition) StepCondition {
	return func(step *AgentStep, history []*AgentStep) bool {
		for _, cond := range conditions {
			if cond(step, history) {
				return true
			}
		}

		return false
	}
}

// StepPreparer is a function that can modify a step before execution.
type StepPreparer func(ctx context.Context, step *AgentStep) (*AgentStep, error)

// Common step preparers

// AddSystemContext adds system context to the step metadata.
func AddSystemContext(key string, value any) StepPreparer {
	return func(ctx context.Context, step *AgentStep) (*AgentStep, error) {
		if step.Metadata == nil {
			step.Metadata = make(map[string]any)
		}

		step.Metadata[key] = value

		return step, nil
	}
}

// InjectCurrentTime injects the current time into metadata.
func InjectCurrentTime() StepPreparer {
	return func(ctx context.Context, step *AgentStep) (*AgentStep, error) {
		if step.Metadata == nil {
			step.Metadata = make(map[string]any)
		}

		step.Metadata["current_time"] = time.Now().Format(time.RFC3339)

		return step, nil
	}
}

// ValidateInput validates the input before processing.
func ValidateInput(validator func(string) error) StepPreparer {
	return func(ctx context.Context, step *AgentStep) (*AgentStep, error) {
		if err := validator(step.Input); err != nil {
			return nil, err
		}

		return step, nil
	}
}

// TruncateInput truncates input to max length.
func TruncateInput(maxLength int) StepPreparer {
	return func(ctx context.Context, step *AgentStep) (*AgentStep, error) {
		if len(step.Input) > maxLength {
			step.Input = step.Input[:maxLength] + "..."
		}

		return step, nil
	}
}

// CombinePreparers combines multiple preparers.
func CombinePreparers(preparers ...StepPreparer) StepPreparer {
	return func(ctx context.Context, step *AgentStep) (*AgentStep, error) {
		var err error
		for _, prep := range preparers {
			step, err = prep(ctx, step)
			if err != nil {
				return nil, err
			}
		}

		return step, nil
	}
}
