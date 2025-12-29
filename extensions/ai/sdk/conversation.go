package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// Conversation represents a chat conversation with history management.
type Conversation struct {
	ID        string                   `json:"id"`
	Title     string                   `json:"title,omitempty"`
	Messages  []ConversationMessage    `json:"messages"`
	Metadata  map[string]any           `json:"metadata,omitempty"`
	Branches  map[string]*Conversation `json:"-"`
	ParentID  string                   `json:"parentId,omitempty"`
	CreatedAt time.Time                `json:"createdAt"`
	UpdatedAt time.Time                `json:"updatedAt"`

	// Configuration
	maxTokens        int
	pruner           HistoryPruner
	llmManager       StreamingLLMManager
	model            string
	provider         string
	systemPrompt     string
	temperature      float64
	branchingEnabled bool

	mu sync.RWMutex
}

// ConversationMessage represents a message in the conversation.
type ConversationMessage struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []llm.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Tokens     int            `json:"tokens,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ConversationConfig configures a conversation.
type ConversationConfig struct {
	ID               string
	Title            string
	MaxTokens        int
	Pruner           HistoryPruner
	LLMManager       StreamingLLMManager
	Model            string
	Provider         string
	SystemPrompt     string
	Temperature      float64
	BranchingEnabled bool
	Metadata         map[string]any
}

// NewConversation creates a new conversation.
func NewConversation(config ConversationConfig) *Conversation {
	if config.ID == "" {
		config.ID = fmt.Sprintf("conv_%d", time.Now().UnixNano())
	}

	if config.MaxTokens == 0 {
		config.MaxTokens = 4000
	}

	if config.Pruner == nil {
		config.Pruner = &OldestFirstPruner{}
	}

	conv := &Conversation{
		ID:               config.ID,
		Title:            config.Title,
		Messages:         make([]ConversationMessage, 0),
		Metadata:         config.Metadata,
		Branches:         make(map[string]*Conversation),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		maxTokens:        config.MaxTokens,
		pruner:           config.Pruner,
		llmManager:       config.LLMManager,
		model:            config.Model,
		provider:         config.Provider,
		systemPrompt:     config.SystemPrompt,
		temperature:      config.Temperature,
		branchingEnabled: config.BranchingEnabled,
	}

	if conv.Metadata == nil {
		conv.Metadata = make(map[string]any)
	}

	return conv
}

// AddMessage adds a message to the conversation.
func (c *Conversation) AddMessage(msg ConversationMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	c.Messages = append(c.Messages, msg)
	c.UpdatedAt = time.Now()
}

// AddUserMessage adds a user message.
func (c *Conversation) AddUserMessage(content string) {
	c.AddMessage(ConversationMessage{
		Role:    "user",
		Content: content,
	})
}

// AddAssistantMessage adds an assistant message.
func (c *Conversation) AddAssistantMessage(content string) {
	c.AddMessage(ConversationMessage{
		Role:    "assistant",
		Content: content,
	})
}

// AddSystemMessage adds a system message.
func (c *Conversation) AddSystemMessage(content string) {
	c.AddMessage(ConversationMessage{
		Role:    "system",
		Content: content,
	})
}

// GetMessages returns all messages.
func (c *Conversation) GetMessages() []ConversationMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := make([]ConversationMessage, len(c.Messages))
	copy(msgs, c.Messages)
	return msgs
}

// GetPrunedMessages returns messages pruned to fit within token limits.
func (c *Conversation) GetPrunedMessages() []ConversationMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.pruner == nil {
		return c.Messages
	}

	return c.pruner.Prune(c.Messages, c.maxTokens)
}

// TotalTokens returns the total tokens in the conversation.
func (c *Conversation) TotalTokens() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := 0
	for _, msg := range c.Messages {
		if msg.Tokens > 0 {
			total += msg.Tokens
		} else {
			// Estimate tokens (rough approximation)
			total += len(msg.Content) / 4
		}
	}
	return total
}

// Send sends a message and gets a response.
func (c *Conversation) Send(ctx context.Context, content string) (*ConversationMessage, error) {
	// Add user message
	c.AddUserMessage(content)

	// Build request
	prunedMessages := c.GetPrunedMessages()
	llmMessages := c.toLLMMessages(prunedMessages)

	request := llm.ChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Messages: llmMessages,
	}

	if c.temperature != 0 {
		request.Temperature = &c.temperature
	}

	// Send request
	response, err := c.llmManager.Chat(ctx, request)
	if err != nil {
		return nil, err
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	// Extract response
	choice := response.Choices[0]
	assistantMsg := ConversationMessage{
		Role:    "assistant",
		Content: choice.Message.Content,
	}

	if response.Usage != nil {
		assistantMsg.Tokens = int(response.Usage.OutputTokens)
	}

	// Add assistant message
	c.AddMessage(assistantMsg)

	return &assistantMsg, nil
}

// SendStream sends a message and streams the response.
func (c *Conversation) SendStream(ctx context.Context, content string, onDelta func(string)) (*ConversationMessage, error) {
	// Add user message
	c.AddUserMessage(content)

	// Build request
	prunedMessages := c.GetPrunedMessages()
	llmMessages := c.toLLMMessages(prunedMessages)

	request := llm.ChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Messages: llmMessages,
		Stream:   true,
	}

	if c.temperature != 0 {
		request.Temperature = &c.temperature
	}

	// Collect response
	var fullContent string
	var tokens int

	err := c.llmManager.ChatStream(ctx, request, func(event llm.ChatStreamEvent) error {
		if len(event.Choices) > 0 {
			delta := ""
			if event.Choices[0].Delta != nil {
				delta = event.Choices[0].Delta.Content
			}

			if delta != "" {
				fullContent += delta
				if onDelta != nil {
					onDelta(delta)
				}
			}
		}

		if event.Usage != nil {
			tokens = int(event.Usage.OutputTokens)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Create and add assistant message
	assistantMsg := ConversationMessage{
		Role:    "assistant",
		Content: fullContent,
		Tokens:  tokens,
	}

	c.AddMessage(assistantMsg)

	return &assistantMsg, nil
}

// toLLMMessages converts conversation messages to LLM messages.
func (c *Conversation) toLLMMessages(msgs []ConversationMessage) []llm.ChatMessage {
	llmMsgs := make([]llm.ChatMessage, 0, len(msgs)+1)

	// Add system prompt if set
	if c.systemPrompt != "" {
		llmMsgs = append(llmMsgs, llm.ChatMessage{
			Role:    "system",
			Content: c.systemPrompt,
		})
	}

	for _, msg := range msgs {
		llmMsg := llm.ChatMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
		llmMsgs = append(llmMsgs, llmMsg)
	}

	return llmMsgs
}

// Branch creates a branch of the conversation at the current point.
func (c *Conversation) Branch(name string) (*Conversation, error) {
	if !c.branchingEnabled {
		return nil, fmt.Errorf("branching is not enabled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.Branches[name]; exists {
		return nil, fmt.Errorf("branch %s already exists", name)
	}

	// Create branch with same configuration
	branch := &Conversation{
		ID:               fmt.Sprintf("%s_%s", c.ID, name),
		Title:            fmt.Sprintf("%s (branch: %s)", c.Title, name),
		Messages:         make([]ConversationMessage, len(c.Messages)),
		Metadata:         make(map[string]any),
		Branches:         make(map[string]*Conversation),
		ParentID:         c.ID,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		maxTokens:        c.maxTokens,
		pruner:           c.pruner,
		llmManager:       c.llmManager,
		model:            c.model,
		provider:         c.provider,
		systemPrompt:     c.systemPrompt,
		temperature:      c.temperature,
		branchingEnabled: c.branchingEnabled,
	}

	// Copy messages
	copy(branch.Messages, c.Messages)

	// Copy metadata
	for k, v := range c.Metadata {
		branch.Metadata[k] = v
	}

	c.Branches[name] = branch

	return branch, nil
}

// GetBranch returns a branch by name.
func (c *Conversation) GetBranch(name string) (*Conversation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	branch, ok := c.Branches[name]
	return branch, ok
}

// ListBranches returns all branch names.
func (c *Conversation) ListBranches() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.Branches))
	for name := range c.Branches {
		names = append(names, name)
	}
	return names
}

// Clear clears all messages.
func (c *Conversation) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Messages = make([]ConversationMessage, 0)
	c.UpdatedAt = time.Now()
}

// Rollback removes the last N messages.
func (c *Conversation) Rollback(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if n >= len(c.Messages) {
		c.Messages = make([]ConversationMessage, 0)
	} else {
		c.Messages = c.Messages[:len(c.Messages)-n]
	}
	c.UpdatedAt = time.Now()
}

// ToJSON serializes the conversation to JSON.
func (c *Conversation) ToJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.Marshal(c)
}

// ConversationFromJSON deserializes a conversation from JSON.
func ConversationFromJSON(data []byte) (*Conversation, error) {
	var conv Conversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, err
	}
	if conv.Branches == nil {
		conv.Branches = make(map[string]*Conversation)
	}
	if conv.Metadata == nil {
		conv.Metadata = make(map[string]any)
	}
	return &conv, nil
}

// ConversationStore interface for persisting conversations.
type ConversationStore interface {
	Save(ctx context.Context, conv *Conversation) error
	Load(ctx context.Context, id string) (*Conversation, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]string, error)
}

// InMemoryConversationStore stores conversations in memory.
type InMemoryConversationStore struct {
	conversations map[string]*Conversation
	mu            sync.RWMutex
}

// NewInMemoryConversationStore creates a new in-memory store.
func NewInMemoryConversationStore() *InMemoryConversationStore {
	return &InMemoryConversationStore{
		conversations: make(map[string]*Conversation),
	}
}

// Save implements ConversationStore.
func (s *InMemoryConversationStore) Save(ctx context.Context, conv *Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conversations[conv.ID] = conv
	return nil
}

// Load implements ConversationStore.
func (s *InMemoryConversationStore) Load(ctx context.Context, id string) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conv, ok := s.conversations[id]
	if !ok {
		return nil, fmt.Errorf("conversation not found: %s", id)
	}
	return conv, nil
}

// Delete implements ConversationStore.
func (s *InMemoryConversationStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conversations, id)
	return nil
}

// List implements ConversationStore.
func (s *InMemoryConversationStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.conversations))
	for id := range s.conversations {
		ids = append(ids, id)
	}
	return ids, nil
}

// ConversationManager manages multiple conversations.
type ConversationManager struct {
	store         ConversationStore
	activeConv    map[string]*Conversation
	llmManager    StreamingLLMManager
	defaultConfig ConversationConfig
	mu            sync.RWMutex
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager(store ConversationStore, llmManager StreamingLLMManager) *ConversationManager {
	return &ConversationManager{
		store:      store,
		activeConv: make(map[string]*Conversation),
		llmManager: llmManager,
	}
}

// SetDefaultConfig sets default configuration for new conversations.
func (m *ConversationManager) SetDefaultConfig(config ConversationConfig) {
	m.defaultConfig = config
}

// Create creates a new conversation.
func (m *ConversationManager) Create(config ConversationConfig) *Conversation {
	// Merge with defaults
	if config.LLMManager == nil {
		config.LLMManager = m.llmManager
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = m.defaultConfig.MaxTokens
	}
	if config.Pruner == nil {
		config.Pruner = m.defaultConfig.Pruner
	}
	if config.Model == "" {
		config.Model = m.defaultConfig.Model
	}
	if config.Provider == "" {
		config.Provider = m.defaultConfig.Provider
	}
	if config.SystemPrompt == "" {
		config.SystemPrompt = m.defaultConfig.SystemPrompt
	}

	conv := NewConversation(config)

	m.mu.Lock()
	m.activeConv[conv.ID] = conv
	m.mu.Unlock()

	return conv
}

// Get returns a conversation by ID.
func (m *ConversationManager) Get(ctx context.Context, id string) (*Conversation, error) {
	m.mu.RLock()
	conv, ok := m.activeConv[id]
	m.mu.RUnlock()

	if ok {
		return conv, nil
	}

	// Try loading from store
	if m.store != nil {
		conv, err := m.store.Load(ctx, id)
		if err != nil {
			return nil, err
		}

		// Restore runtime configuration
		conv.llmManager = m.llmManager
		if conv.pruner == nil {
			conv.pruner = m.defaultConfig.Pruner
		}

		m.mu.Lock()
		m.activeConv[id] = conv
		m.mu.Unlock()

		return conv, nil
	}

	return nil, fmt.Errorf("conversation not found: %s", id)
}

// Save saves a conversation to the store.
func (m *ConversationManager) Save(ctx context.Context, id string) error {
	m.mu.RLock()
	conv, ok := m.activeConv[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("conversation not found: %s", id)
	}

	if m.store != nil {
		return m.store.Save(ctx, conv)
	}

	return nil
}

// Delete removes a conversation.
func (m *ConversationManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	delete(m.activeConv, id)
	m.mu.Unlock()

	if m.store != nil {
		return m.store.Delete(ctx, id)
	}

	return nil
}

// List returns all conversation IDs.
func (m *ConversationManager) List(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.activeConv))
	for id := range m.activeConv {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	// Add stored conversations
	if m.store != nil {
		storedIDs, err := m.store.List(ctx)
		if err != nil {
			return ids, err
		}

		// Deduplicate
		idSet := make(map[string]bool)
		for _, id := range ids {
			idSet[id] = true
		}
		for _, id := range storedIDs {
			if !idSet[id] {
				ids = append(ids, id)
			}
		}
	}

	return ids, nil
}
