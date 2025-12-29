package sdk

import (
	"context"
	"strings"

	"github.com/xraph/forge/extensions/ai/llm"
)

// HistoryPruner prunes conversation history to fit within token limits.
type HistoryPruner interface {
	// Prune removes messages to fit within maxTokens.
	Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage
}

// OldestFirstPruner removes oldest messages first.
type OldestFirstPruner struct{}

// Prune implements HistoryPruner.
func (p *OldestFirstPruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	if len(messages) == 0 {
		return messages
	}

	// Calculate total tokens
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg)
	}

	// Remove oldest messages (but keep system messages)
	for total > maxTokens && len(messages) > 1 {
		// Find first non-system message to remove
		removeIdx := -1
		for i, msg := range messages {
			if msg.Role != "system" {
				removeIdx = i
				break
			}
		}

		if removeIdx == -1 {
			break // Only system messages left
		}

		total -= estimateTokens(messages[removeIdx])
		messages = append(messages[:removeIdx], messages[removeIdx+1:]...)
	}

	return messages
}

// NewestFirstPruner removes newest messages first (except the most recent).
type NewestFirstPruner struct {
	KeepRecent int // Number of recent messages to always keep
}

// Prune implements HistoryPruner.
func (p *NewestFirstPruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	if len(messages) == 0 {
		return messages
	}

	keepRecent := p.KeepRecent
	if keepRecent <= 0 {
		keepRecent = 2
	}

	// Calculate total tokens
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg)
	}

	// Remove from middle (keep system and recent)
	for total > maxTokens && len(messages) > keepRecent+1 {
		// Find message to remove (not system, not in recent)
		removeIdx := -1
		for i := len(messages) - keepRecent - 1; i >= 0; i-- {
			if messages[i].Role != "system" {
				removeIdx = i
				break
			}
		}

		if removeIdx == -1 {
			break
		}

		total -= estimateTokens(messages[removeIdx])
		messages = append(messages[:removeIdx], messages[removeIdx+1:]...)
	}

	return messages
}

// SummarizePruner summarizes old messages instead of removing them.
type SummarizePruner struct {
	llmManager  StreamingLLMManager
	model       string
	provider    string
	summarySize int // Target token size for summary
}

// NewSummarizePruner creates a new summarize pruner.
func NewSummarizePruner(llmManager StreamingLLMManager, provider, model string, summarySize int) *SummarizePruner {
	if summarySize <= 0 {
		summarySize = 500
	}
	return &SummarizePruner{
		llmManager:  llmManager,
		model:       model,
		provider:    provider,
		summarySize: summarySize,
	}
}

// Prune implements HistoryPruner.
func (p *SummarizePruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	if len(messages) == 0 {
		return messages
	}

	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg)
	}

	if total <= maxTokens {
		return messages
	}

	// Find messages to summarize (oldest non-system messages)
	toSummarize := make([]ConversationMessage, 0)
	remaining := make([]ConversationMessage, 0)
	remainingTokens := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgTokens := estimateTokens(msg)

		if remainingTokens+msgTokens <= maxTokens-p.summarySize {
			remaining = append([]ConversationMessage{msg}, remaining...)
			remainingTokens += msgTokens
		} else if msg.Role == "system" {
			// Always keep system messages
			remaining = append([]ConversationMessage{msg}, remaining...)
		} else {
			toSummarize = append([]ConversationMessage{msg}, toSummarize...)
		}
	}

	if len(toSummarize) == 0 {
		return remaining
	}

	// Summarize old messages
	summary := p.summarize(toSummarize)
	if summary != "" {
		summaryMsg := ConversationMessage{
			Role:    "system",
			Content: "Previous conversation summary:\n" + summary,
			Tokens:  p.summarySize,
		}
		remaining = append([]ConversationMessage{summaryMsg}, remaining...)
	}

	return remaining
}

// summarize creates a summary of messages.
func (p *SummarizePruner) summarize(messages []ConversationMessage) string {
	if p.llmManager == nil || len(messages) == 0 {
		// Fallback: simple concatenation
		var parts []string
		for _, msg := range messages {
			parts = append(parts, msg.Role+": "+truncate(msg.Content, 100))
		}
		return strings.Join(parts, "\n")
	}

	// Build conversation text
	var conversation string
	for _, msg := range messages {
		conversation += msg.Role + ": " + msg.Content + "\n"
	}

	// Request summary from LLM
	ctx := context.Background()
	request := llm.ChatRequest{
		Provider: p.provider,
		Model:    p.model,
		Messages: []llm.ChatMessage{
			{
				Role:    "system",
				Content: "Summarize the following conversation concisely, preserving key information and context.",
			},
			{
				Role:    "user",
				Content: conversation,
			},
		},
	}

	response, err := p.llmManager.Chat(ctx, request)
	if err != nil {
		// Fallback
		return truncate(conversation, p.summarySize*4)
	}

	if len(response.Choices) > 0 {
		return response.Choices[0].Message.Content
	}

	return ""
}

// ImportancePruner keeps messages based on importance scoring.
type ImportancePruner struct {
	scoreFunc func(ConversationMessage) float64
}

// NewImportancePruner creates a new importance pruner.
func NewImportancePruner(scoreFunc func(ConversationMessage) float64) *ImportancePruner {
	if scoreFunc == nil {
		scoreFunc = defaultImportanceScore
	}
	return &ImportancePruner{scoreFunc: scoreFunc}
}

// Prune implements HistoryPruner.
func (p *ImportancePruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	if len(messages) == 0 {
		return messages
	}

	// Score all messages
	type scoredMessage struct {
		index int
		msg   ConversationMessage
		score float64
	}

	scored := make([]scoredMessage, len(messages))
	for i, msg := range messages {
		scored[i] = scoredMessage{
			index: i,
			msg:   msg,
			score: p.scoreFunc(msg),
		}
	}

	// Calculate total tokens
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg)
	}

	// Remove lowest scored messages
	for total > maxTokens && len(scored) > 1 {
		// Find lowest scored non-system message
		lowestIdx := -1
		lowestScore := float64(0)

		for i, sm := range scored {
			if sm.msg.Role == "system" {
				continue
			}
			if lowestIdx == -1 || sm.score < lowestScore {
				lowestIdx = i
				lowestScore = sm.score
			}
		}

		if lowestIdx == -1 {
			break
		}

		total -= estimateTokens(scored[lowestIdx].msg)
		scored = append(scored[:lowestIdx], scored[lowestIdx+1:]...)
	}

	// Rebuild messages in original order
	result := make([]ConversationMessage, len(scored))
	for i, sm := range scored {
		result[i] = sm.msg
	}

	return result
}

// defaultImportanceScore provides default importance scoring.
func defaultImportanceScore(msg ConversationMessage) float64 {
	score := 1.0

	// System messages are most important
	if msg.Role == "system" {
		score += 10.0
	}

	// Recent messages are more important (based on position)
	// This will be handled by the pruning logic

	// Messages with tool calls are important
	if len(msg.ToolCalls) > 0 {
		score += 2.0
	}

	// Longer messages may contain more information
	contentLen := len(msg.Content)
	if contentLen > 500 {
		score += 1.0
	} else if contentLen < 50 {
		score -= 0.5
	}

	// Messages with certain keywords are important
	importantKeywords := []string{
		"important", "critical", "remember", "note",
		"error", "warning", "must", "required",
	}
	lowerContent := strings.ToLower(msg.Content)
	for _, kw := range importantKeywords {
		if strings.Contains(lowerContent, kw) {
			score += 0.5
		}
	}

	return score
}

// SlidingWindowPruner keeps a sliding window of recent messages.
type SlidingWindowPruner struct {
	WindowSize int // Number of message pairs to keep
}

// Prune implements HistoryPruner.
func (p *SlidingWindowPruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	windowSize := p.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}

	// Separate system messages
	var systemMsgs []ConversationMessage
	var otherMsgs []ConversationMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// Keep only recent messages
	if len(otherMsgs) > windowSize*2 {
		otherMsgs = otherMsgs[len(otherMsgs)-windowSize*2:]
	}

	// Combine
	result := append(systemMsgs, otherMsgs...)

	// Check tokens
	total := 0
	for _, msg := range result {
		total += estimateTokens(msg)
	}

	// If still over, apply oldest-first pruning
	if total > maxTokens {
		oldestPruner := &OldestFirstPruner{}
		return oldestPruner.Prune(result, maxTokens)
	}

	return result
}

// CompositePruner applies multiple pruning strategies in sequence.
type CompositePruner struct {
	Pruners []HistoryPruner
}

// Prune implements HistoryPruner.
func (p *CompositePruner) Prune(messages []ConversationMessage, maxTokens int) []ConversationMessage {
	result := messages
	for _, pruner := range p.Pruners {
		result = pruner.Prune(result, maxTokens)
	}
	return result
}

// Helper functions

func estimateTokens(msg ConversationMessage) int {
	if msg.Tokens > 0 {
		return msg.Tokens
	}
	// Rough estimation: 1 token ≈ 4 characters
	return len(msg.Content)/4 + 4 // +4 for role/formatting overhead
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Built-in pruner instances
var (
	PruneOldest     HistoryPruner = &OldestFirstPruner{}
	PruneNewest     HistoryPruner = &NewestFirstPruner{KeepRecent: 4}
	PruneImportance HistoryPruner = NewImportancePruner(nil)
	PruneWindow     HistoryPruner = &SlidingWindowPruner{WindowSize: 10}
)

// PrunerConfig configures a history pruner.
type PrunerConfig struct {
	Strategy    PruningStrategy
	MaxTokens   int
	KeepRecent  int
	WindowSize  int
	SummarySize int
	ScoreFunc   func(ConversationMessage) float64
	LLMManager  StreamingLLMManager
	Provider    string
	Model       string
}

// PruningStrategy represents a pruning strategy type.
type PruningStrategy string

const (
	PruningStrategyOldestFirst   PruningStrategy = "oldest_first"
	PruningStrategyNewestFirst   PruningStrategy = "newest_first"
	PruningStrategySummarize     PruningStrategy = "summarize"
	PruningStrategyImportance    PruningStrategy = "importance"
	PruningStrategySlidingWindow PruningStrategy = "sliding_window"
)

// NewPruner creates a pruner from configuration.
func NewPruner(config PrunerConfig) HistoryPruner {
	switch config.Strategy {
	case PruningStrategyOldestFirst:
		return &OldestFirstPruner{}
	case PruningStrategyNewestFirst:
		return &NewestFirstPruner{KeepRecent: config.KeepRecent}
	case PruningStrategySummarize:
		return NewSummarizePruner(config.LLMManager, config.Provider, config.Model, config.SummarySize)
	case PruningStrategyImportance:
		return NewImportancePruner(config.ScoreFunc)
	case PruningStrategySlidingWindow:
		return &SlidingWindowPruner{WindowSize: config.WindowSize}
	default:
		return &OldestFirstPruner{}
	}
}
