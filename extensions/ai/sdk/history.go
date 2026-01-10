package sdk

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// MessageHistory provides utilities for working with conversation history.
type MessageHistory struct {
	messages []ConversationMessage
}

// NewMessageHistory creates a new message history.
func NewMessageHistory(messages []ConversationMessage) *MessageHistory {
	return &MessageHistory{messages: messages}
}

// FromConversation creates a history from a conversation.
func FromConversation(conv *Conversation) *MessageHistory {
	return NewMessageHistory(conv.GetMessages())
}

// Messages returns all messages.
func (h *MessageHistory) Messages() []ConversationMessage {
	return h.messages
}

// Len returns the number of messages.
func (h *MessageHistory) Len() int {
	return len(h.messages)
}

// Get returns a message by index.
func (h *MessageHistory) Get(index int) *ConversationMessage {
	if index < 0 || index >= len(h.messages) {
		return nil
	}

	return &h.messages[index]
}

// Last returns the last message.
func (h *MessageHistory) Last() *ConversationMessage {
	if len(h.messages) == 0 {
		return nil
	}

	return &h.messages[len(h.messages)-1]
}

// First returns the first message.
func (h *MessageHistory) First() *ConversationMessage {
	if len(h.messages) == 0 {
		return nil
	}

	return &h.messages[0]
}

// FilterByRole returns messages with a specific role.
func (h *MessageHistory) FilterByRole(role string) []ConversationMessage {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if msg.Role == role {
			result = append(result, msg)
		}
	}

	return result
}

// UserMessages returns all user messages.
func (h *MessageHistory) UserMessages() []ConversationMessage {
	return h.FilterByRole("user")
}

// AssistantMessages returns all assistant messages.
func (h *MessageHistory) AssistantMessages() []ConversationMessage {
	return h.FilterByRole("assistant")
}

// SystemMessages returns all system messages.
func (h *MessageHistory) SystemMessages() []ConversationMessage {
	return h.FilterByRole("system")
}

// Search searches messages by content.
func (h *MessageHistory) Search(query string) []ConversationMessage {
	query = strings.ToLower(query)

	var result []ConversationMessage

	for _, msg := range h.messages {
		if strings.Contains(strings.ToLower(msg.Content), query) {
			result = append(result, msg)
		}
	}

	return result
}

// Since returns messages since a timestamp.
func (h *MessageHistory) Since(t time.Time) []ConversationMessage {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if msg.Timestamp.After(t) || msg.Timestamp.Equal(t) {
			result = append(result, msg)
		}
	}

	return result
}

// Before returns messages before a timestamp.
func (h *MessageHistory) Before(t time.Time) []ConversationMessage {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if msg.Timestamp.Before(t) {
			result = append(result, msg)
		}
	}

	return result
}

// Between returns messages within a time range.
func (h *MessageHistory) Between(start, end time.Time) []ConversationMessage {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if (msg.Timestamp.After(start) || msg.Timestamp.Equal(start)) &&
			(msg.Timestamp.Before(end) || msg.Timestamp.Equal(end)) {
			result = append(result, msg)
		}
	}

	return result
}

// Slice returns a slice of messages.
func (h *MessageHistory) Slice(start, end int) []ConversationMessage {
	if start < 0 {
		start = 0
	}

	if end > len(h.messages) {
		end = len(h.messages)
	}

	if start >= end {
		return nil
	}

	return h.messages[start:end]
}

// TotalTokens returns the total estimated tokens.
func (h *MessageHistory) TotalTokens() int {
	total := 0
	for _, msg := range h.messages {
		total += estimateTokens(msg)
	}

	return total
}

// TotalChars returns the total character count.
func (h *MessageHistory) TotalChars() int {
	total := 0
	for _, msg := range h.messages {
		total += len(msg.Content)
	}

	return total
}

// Stats returns statistics about the history.
func (h *MessageHistory) Stats() HistoryStats {
	stats := HistoryStats{
		TotalMessages: len(h.messages),
		TotalTokens:   h.TotalTokens(),
		TotalChars:    h.TotalChars(),
		RoleCounts:    make(map[string]int),
	}

	for _, msg := range h.messages {
		stats.RoleCounts[msg.Role]++
	}

	if len(h.messages) > 0 {
		stats.FirstMessage = h.messages[0].Timestamp
		stats.LastMessage = h.messages[len(h.messages)-1].Timestamp
	}

	return stats
}

// HistoryStats contains statistics about message history.
type HistoryStats struct {
	TotalMessages int            `json:"totalMessages"`
	TotalTokens   int            `json:"totalTokens"`
	TotalChars    int            `json:"totalChars"`
	RoleCounts    map[string]int `json:"roleCounts"`
	FirstMessage  time.Time      `json:"firstMessage"`
	LastMessage   time.Time      `json:"lastMessage"`
}

// ToText converts history to plain text.
func (h *MessageHistory) ToText() string {
	var sb strings.Builder
	for _, msg := range h.messages {
		sb.WriteString(msg.Role)
		sb.WriteString(": ")
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// ToMarkdown converts history to markdown format.
func (h *MessageHistory) ToMarkdown() string {
	var sb strings.Builder
	for _, msg := range h.messages {
		sb.WriteString("**")
		sb.WriteString(strings.Title(msg.Role))
		sb.WriteString("**\n\n")
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n---\n\n")
	}

	return sb.String()
}

// ToJSON converts history to JSON.
func (h *MessageHistory) ToJSON() ([]byte, error) {
	return json.Marshal(h.messages)
}

// Clone creates a copy of the history.
func (h *MessageHistory) Clone() *MessageHistory {
	msgs := make([]ConversationMessage, len(h.messages))
	copy(msgs, h.messages)

	return &MessageHistory{messages: msgs}
}

// Append adds messages to the history.
func (h *MessageHistory) Append(msgs ...ConversationMessage) *MessageHistory {
	h.messages = append(h.messages, msgs...)

	return h
}

// Prepend adds messages to the beginning.
func (h *MessageHistory) Prepend(msgs ...ConversationMessage) *MessageHistory {
	h.messages = append(msgs, h.messages...)

	return h
}

// Remove removes a message by index.
func (h *MessageHistory) Remove(index int) *MessageHistory {
	if index < 0 || index >= len(h.messages) {
		return h
	}

	h.messages = append(h.messages[:index], h.messages[index+1:]...)

	return h
}

// Compact removes empty messages.
func (h *MessageHistory) Compact() *MessageHistory {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if strings.TrimSpace(msg.Content) != "" {
			result = append(result, msg)
		}
	}

	h.messages = result

	return h
}

// Deduplicate removes duplicate messages.
func (h *MessageHistory) Deduplicate() *MessageHistory {
	seen := make(map[string]bool)

	var result []ConversationMessage

	for _, msg := range h.messages {
		key := msg.Role + ":" + msg.Content
		if !seen[key] {
			seen[key] = true

			result = append(result, msg)
		}
	}

	h.messages = result

	return h
}

// SortByTime sorts messages by timestamp.
func (h *MessageHistory) SortByTime() *MessageHistory {
	sort.Slice(h.messages, func(i, j int) bool {
		return h.messages[i].Timestamp.Before(h.messages[j].Timestamp)
	})

	return h
}

// Reverse reverses the message order.
func (h *MessageHistory) Reverse() *MessageHistory {
	for i, j := 0, len(h.messages)-1; i < j; i, j = i+1, j-1 {
		h.messages[i], h.messages[j] = h.messages[j], h.messages[i]
	}

	return h
}

// Transform applies a transformation to all messages.
func (h *MessageHistory) Transform(fn func(ConversationMessage) ConversationMessage) *MessageHistory {
	for i, msg := range h.messages {
		h.messages[i] = fn(msg)
	}

	return h
}

// Filter filters messages by a predicate.
func (h *MessageHistory) Filter(fn func(ConversationMessage) bool) *MessageHistory {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if fn(msg) {
			result = append(result, msg)
		}
	}

	h.messages = result

	return h
}

// Each applies a function to each message.
func (h *MessageHistory) Each(fn func(int, ConversationMessage)) {
	for i, msg := range h.messages {
		fn(i, msg)
	}
}

// Find finds the first message matching a predicate.
func (h *MessageHistory) Find(fn func(ConversationMessage) bool) *ConversationMessage {
	for _, msg := range h.messages {
		if fn(msg) {
			return &msg
		}
	}

	return nil
}

// FindAll finds all messages matching a predicate.
func (h *MessageHistory) FindAll(fn func(ConversationMessage) bool) []ConversationMessage {
	var result []ConversationMessage

	for _, msg := range h.messages {
		if fn(msg) {
			result = append(result, msg)
		}
	}

	return result
}

// Contains checks if history contains a message matching a predicate.
func (h *MessageHistory) Contains(fn func(ConversationMessage) bool) bool {
	return h.Find(fn) != nil
}

// ExtractTopics extracts key topics/keywords from the history.
func (h *MessageHistory) ExtractTopics() []string {
	// Simple word frequency analysis
	wordCount := make(map[string]int)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "what": true, "which": true, "who": true,
		"this": true, "that": true, "these": true, "those": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"so": true, "as": true, "of": true, "to": true, "for": true,
		"with": true, "in": true, "on": true, "at": true, "by": true,
		"from": true, "about": true, "into": true, "through": true,
	}

	for _, msg := range h.messages {
		if msg.Role == "system" {
			continue
		}

		words := strings.FieldsSeq(strings.ToLower(msg.Content))
		for word := range words {
			// Clean word
			word = strings.Trim(word, ".,!?;:\"'()[]{}*")
			if len(word) < 3 {
				continue
			}

			if stopWords[word] {
				continue
			}

			wordCount[word]++
		}
	}

	// Get top words
	type wordFreq struct {
		word  string
		count int
	}

	var frequencies []wordFreq

	for word, count := range wordCount {
		if count >= 2 { // Only words appearing multiple times
			frequencies = append(frequencies, wordFreq{word, count})
		}
	}

	sort.Slice(frequencies, func(i, j int) bool {
		return frequencies[i].count > frequencies[j].count
	})

	// Return top 10 topics
	var topics []string

	for i, wf := range frequencies {
		if i >= 10 {
			break
		}

		topics = append(topics, wf.word)
	}

	return topics
}

// MessagePair represents a user-assistant message pair.
type MessagePair struct {
	User      ConversationMessage
	Assistant ConversationMessage
}

// GetPairs returns user-assistant message pairs.
func (h *MessageHistory) GetPairs() []MessagePair {
	var (
		pairs    []MessagePair
		lastUser *ConversationMessage
	)

	for i := range h.messages {
		msg := &h.messages[i]
		if msg.Role == "user" {
			lastUser = msg
		} else if msg.Role == "assistant" && lastUser != nil {
			pairs = append(pairs, MessagePair{
				User:      *lastUser,
				Assistant: *msg,
			})
			lastUser = nil
		}
	}

	return pairs
}

// HistoryDiff represents the difference between two histories.
type HistoryDiff struct {
	Added   []ConversationMessage
	Removed []ConversationMessage
	Common  int
}

// Diff compares two histories.
func (h *MessageHistory) Diff(other *MessageHistory) HistoryDiff {
	diff := HistoryDiff{}

	thisSet := make(map[string]bool)
	otherSet := make(map[string]bool)

	for _, msg := range h.messages {
		key := msg.Role + ":" + msg.Content
		thisSet[key] = true
	}

	for _, msg := range other.messages {
		key := msg.Role + ":" + msg.Content
		otherSet[key] = true

		if thisSet[key] {
			diff.Common++
		} else {
			diff.Added = append(diff.Added, msg)
		}
	}

	for _, msg := range h.messages {
		key := msg.Role + ":" + msg.Content
		if !otherSet[key] {
			diff.Removed = append(diff.Removed, msg)
		}
	}

	return diff
}

// ContextWindow represents a sliding context window over history.
type ContextWindow struct {
	history  *MessageHistory
	size     int
	position int
}

// NewContextWindow creates a new context window.
func NewContextWindow(history *MessageHistory, size int) *ContextWindow {
	return &ContextWindow{
		history:  history,
		size:     size,
		position: 0,
	}
}

// Current returns the current window of messages.
func (w *ContextWindow) Current() []ConversationMessage {
	start := w.position

	end := start + w.size
	if end > w.history.Len() {
		end = w.history.Len()
	}

	return w.history.Slice(start, end)
}

// Next advances the window.
func (w *ContextWindow) Next() bool {
	if w.position+w.size >= w.history.Len() {
		return false
	}

	w.position++

	return true
}

// Previous moves the window back.
func (w *ContextWindow) Previous() bool {
	if w.position == 0 {
		return false
	}

	w.position--

	return true
}

// Reset resets the window to the beginning.
func (w *ContextWindow) Reset() {
	w.position = 0
}

// ToEnd moves the window to show the most recent messages.
func (w *ContextWindow) ToEnd() {
	if w.history.Len() > w.size {
		w.position = w.history.Len() - w.size
	} else {
		w.position = 0
	}
}
