package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// Example: Basic Ollama Chat.
func ExampleOllamaChat() {
	// Create Ollama provider
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
		Timeout: 120 * time.Second,
		Models:  []string{"llama2", "mistral", "codellama"},
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Create chat request
	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "system",
				Content: "You are a helpful AI assistant.",
			},
			{
				Role:    "user",
				Content: "What is the capital of France?",
			},
		},
		RequestID: "example-chat-1",
	}

	// Execute chat
	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Response: %s\n", response.Choices[0].Message.Content)
	fmt.Printf("Tokens used: %d input, %d output\n",
		response.Usage.InputTokens,
		response.Usage.OutputTokens)
}

// Example: Streaming Chat.
func ExampleOllamaStreamingChat() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
		Timeout: 120 * time.Second,
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Write a short story about a robot.",
			},
		},
		RequestID: "example-stream-1",
	}

	// Stream handler
	handler := func(event llm.ChatStreamEvent) error {
		switch event.Type {
		case "message":
			if len(event.Choices) > 0 && event.Choices[0].Delta != nil {
				fmt.Print(event.Choices[0].Delta.Content)
			}
		case "done":
			fmt.Println("\n\n--- Stream Complete ---")

			if event.Usage != nil {
				fmt.Printf("Total tokens: %d\n", event.Usage.TotalTokens)
			}
		case "error":
			fmt.Printf("Error: %s\n", event.Error)
		}

		return nil
	}

	// Execute streaming chat
	if err := provider.ChatStream(context.Background(), request, handler); err != nil {
		panic(err)
	}
}

// Example: Text Completion.
func ExampleOllamaCompletion() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	temp := 0.7
	maxTokens := 100

	request := llm.CompletionRequest{
		Model:       "codellama",
		Prompt:      "def fibonacci(n):",
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		RequestID:   "example-completion-1",
	}

	response, err := provider.Complete(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Completion:\n%s\n", response.Choices[0].Text)
}

// Example: Embeddings.
func ExampleOllamaEmbeddings() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Single embedding
	request := llm.EmbeddingRequest{
		Model:     "nomic-embed-text",
		Input:     []string{"This is a test sentence for embedding."},
		RequestID: "example-embed-1",
	}

	response, err := provider.Embed(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Embedding dimension: %d\n", len(response.Data[0].Embedding))
	fmt.Printf("First 5 values: %v\n", response.Data[0].Embedding[:5])
}

// Example: Batch Embeddings.
func ExampleOllamaBatchEmbeddings() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Multiple embeddings
	request := llm.EmbeddingRequest{
		Model: "nomic-embed-text",
		Input: []string{
			"First document to embed",
			"Second document to embed",
			"Third document to embed",
		},
		RequestID: "example-batch-embed-1",
	}

	response, err := provider.Embed(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Generated %d embeddings\n", len(response.Data))

	for i, emb := range response.Data {
		fmt.Printf("Embedding %d: dimension %d\n", i, len(emb.Embedding))
	}
}

// Example: Chat with Tools/Functions.
func ExampleOllamaChatWithTools() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Define tools
	tools := []llm.Tool{
		{
			Type: "function",
			Function: &llm.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]any{
							"type": "string",
							"enum": []string{"celsius", "fahrenheit"},
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "What's the weather like in San Francisco?",
			},
		},
		Tools:     tools,
		RequestID: "example-tools-1",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	// Check if model wants to call a tool
	if len(response.Choices[0].Message.ToolCalls) > 0 {
		for _, toolCall := range response.Choices[0].Message.ToolCalls {
			fmt.Printf("Tool call: %s\n", toolCall.Function.Name)
			fmt.Printf("Arguments: %s\n", toolCall.Function.Arguments)
		}
	} else {
		fmt.Printf("Response: %s\n", response.Choices[0].Message.Content)
	}
}

// Example: Advanced Options.
func ExampleOllamaAdvancedOptions() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
		Timeout: 180 * time.Second,
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Configure advanced parameters
	temp := 0.8
	topP := 0.9
	topK := 40
	maxTokens := 500

	request := llm.ChatRequest{
		Model: "mistral",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Explain quantum computing in simple terms.",
			},
		},
		Temperature: &temp,
		TopP:        &topP,
		TopK:        &topK,
		MaxTokens:   &maxTokens,
		Stop:        []string{"\n\n", "---"},
		RequestID:   "example-advanced-1",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Response: %s\n", response.Choices[0].Message.Content)
	fmt.Printf("Finish reason: %s\n", response.Choices[0].FinishReason)
}

// Example: Health Check and Usage Stats.
func ExampleOllamaHealthAndUsage() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Health check
	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err != nil {
		fmt.Printf("Ollama is not healthy: %v\n", err)

		return
	}

	fmt.Println("Ollama is healthy!")

	// Get available models
	models := provider.Models()
	fmt.Printf("Available models: %v\n", models)

	// Get usage statistics
	usage := provider.GetUsage()
	fmt.Printf("Total requests: %d\n", usage.RequestCount)
	fmt.Printf("Total tokens: %d\n", usage.TotalTokens)
	fmt.Printf("Average latency: %v\n", usage.AverageLatency)
	fmt.Printf("Error count: %d\n", usage.ErrorCount)
}

// Example: Multi-modal with Images (if supported by model).
func ExampleOllamaMultiModal() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Note: For vision models, images are typically passed via the content field
	// or through metadata. The exact format depends on the model's requirements.
	// Consult Ollama documentation for vision model usage.

	request := llm.ChatRequest{
		Model: "llava", // Vision model
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Describe what you see in the image.",
				// Images would be passed via Metadata or through Ollama's specific API
				Metadata: map[string]any{
					"images": []string{"base64_encoded_image_here"},
				},
			},
		},
		RequestID: "example-vision-1",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Vision response: %s\n", response.Choices[0].Message.Content)
}

// Example: Context Management for Long Conversations.
func ExampleOllamaContextManagement() {
	provider, err := NewOllamaProvider(OllamaConfig{
		BaseURL: "http://localhost:11434",
	}, nil, nil)
	if err != nil {
		panic(err)
	}

	// Build conversation history
	messages := []llm.ChatMessage{
		{
			Role:    "system",
			Content: "You are a helpful coding assistant.",
		},
		{
			Role:    "user",
			Content: "How do I reverse a string in Python?",
		},
		{
			Role:    "assistant",
			Content: "You can reverse a string in Python using slicing: `s[::-1]`",
		},
		{
			Role:    "user",
			Content: "Can you show me a complete example?",
		},
	}

	request := llm.ChatRequest{
		Model:     "codellama",
		Messages:  messages,
		RequestID: "example-context-1",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Response: %s\n", response.Choices[0].Message.Content)

	// Continue conversation
	messages = append(messages, llm.ChatMessage{
		Role:    "assistant",
		Content: response.Choices[0].Message.Content,
	})
	messages = append(messages, llm.ChatMessage{
		Role:    "user",
		Content: "Now show me how to do it in Go.",
	})

	request.Messages = messages
	request.RequestID = "example-context-2"

	response, err = provider.Chat(context.Background(), request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Follow-up response: %s\n", response.Choices[0].Message.Content)
}
